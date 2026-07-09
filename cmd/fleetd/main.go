// Command fleetd is the fleet backend server: GitHub OAuth identity plus a
// per-user versioned document store with LWW sync, backed by Postgres.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/hoijun/fleet/internal/server/auth"
	httpapi "github.com/hoijun/fleet/internal/server/http"
	"github.com/hoijun/fleet/internal/server/pgstore"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	databaseURL := mustEnv("DATABASE_URL")
	signingKey := []byte(mustEnv("JWT_SIGNING_KEY"))
	clientID := mustEnv("GITHUB_OAUTH_CLIENT_ID")
	clientSecret := mustEnv("GITHUB_OAUTH_CLIENT_SECRET")
	callbackURL := mustEnv("GITHUB_OAUTH_CALLBACK_URL")
	addr := ":" + envOr("PORT", "8080")
	trustProxy := envBool("TRUST_PROXY")

	if err := pgstore.Migrate(databaseURL); err != nil {
		slog.Error("migrate failed", "err", err)
		os.Exit(1)
	}

	store, err := pgstore.New(context.Background(), databaseURL)
	if err != nil {
		slog.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	authH := auth.New(auth.Config{
		Store:       store,
		GitHub:      auth.NewHTTPGitHub(clientID, clientSecret),
		SigningKey:  signingKey,
		ClientID:    clientID,
		CallbackURL: callbackURL,
	})

	router := httpapi.NewRouter(httpapi.Options{
		Store:      store,
		Auth:       authH,
		SigningKey: signingKey,
		TrustProxy: trustProxy,
	})

	slog.Info("listening", "addr", addr, "trust_proxy", trustProxy)
	if err := http.ListenAndServe(addr, router); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
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
