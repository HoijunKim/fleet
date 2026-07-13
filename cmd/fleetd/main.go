// Command fleetd is the fleet backend server: GitHub OAuth identity plus a
// per-user versioned document store with LWW sync, backed by Postgres.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/hoijun/fleet/internal/server/auth"
	httpapi "github.com/hoijun/fleet/internal/server/http"
	"github.com/hoijun/fleet/internal/server/metrics"
	"github.com/hoijun/fleet/internal/server/pgstore"
)

// shutdownTimeout bounds the graceful drain after a stop signal. It is kept
// under fly.toml's kill_timeout so the drain completes before Fly SIGKILLs the
// process.
const shutdownTimeout = 10 * time.Second

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	// Fly stops a machine with SIGINT (its default) then SIGKILL after
	// kill_timeout; catch SIGINT and SIGTERM so run drains either way.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
}

// run wires config, storage, and routes, then serves until ctx is cancelled by
// a stop signal and drains gracefully. It returns an error (rather than
// exiting) so failures propagate to main for a single exit point.
func run(ctx context.Context) error {
	databaseURL := mustEnv("DATABASE_URL")
	signingKey := []byte(mustEnv("JWT_SIGNING_KEY"))
	clientID := mustEnv("GITHUB_OAUTH_CLIENT_ID")
	clientSecret := mustEnv("GITHUB_OAUTH_CLIENT_SECRET")
	callbackURL := mustEnv("GITHUB_OAUTH_CALLBACK_URL")
	addr := ":" + envOr("PORT", "8080")
	trustProxy := envBool("TRUST_PROXY")
	metricsToken := envOr("METRICS_TOKEN", "")

	if err := pgstore.Migrate(databaseURL); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	store, err := pgstore.New(context.Background(), databaseURL)
	if err != nil {
		return fmt.Errorf("db connect: %w", err)
	}
	defer store.Close()

	m := metrics.New(envOr("FLEET_VERSION", "dev"), runtime.Version())
	m.SetPoolSource(func() metrics.PoolStats {
		s := store.Stat()
		return metrics.PoolStats{Total: int(s.TotalConns()), Idle: int(s.IdleConns()), Acquired: int(s.AcquiredConns())}
	})

	authH := auth.New(auth.Config{
		Store:       store,
		GitHub:      auth.NewHTTPGitHub(clientID, clientSecret),
		SigningKey:  signingKey,
		ClientID:    clientID,
		CallbackURL: callbackURL,
		Metrics:     m,
	})

	router := httpapi.NewRouter(httpapi.Options{
		Store:        store,
		Auth:         authH,
		SigningKey:   signingKey,
		TrustProxy:   trustProxy,
		Metrics:      m,
		MetricsToken: metricsToken,
	})

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	srv := &http.Server{
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	slog.Info("listening", "addr", addr, "trust_proxy", trustProxy, "metrics", metricsToken != "")
	return serve(ctx, srv, ln)
}

// serve runs srv on ln until ctx is cancelled, then drains in-flight requests
// with a bounded timeout. It returns nil on a clean shutdown, or the serve or
// shutdown error otherwise.
func serve(ctx context.Context, srv *http.Server, ln net.Listener) error {
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
		sctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return srv.Shutdown(sctx)
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("missing required env", "key", key)
		os.Exit(1)
	}
	return v
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envBool parses key as a boolean: "1" or "true" means true; anything else,
// including unset, means false. Used for TRUST_PROXY, which must default to
// false (untrusted) unless explicitly opted in.
func envBool(key string) bool {
	v := os.Getenv(key)
	return v == "1" || v == "true"
}
