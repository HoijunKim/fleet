package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hoijun/fleet/internal/server/auth"
	"github.com/hoijun/fleet/internal/server/pgstore"
)

// Options carries the router's dependencies.
type Options struct {
	Store      pgstore.Store
	Auth       *auth.Handlers
	SigningKey []byte
	// TrustProxy controls whether rate limiters honor proxy headers
	// (Fly-Client-IP / X-Forwarded-For) when deriving the client IP. Only set
	// this true when the server sits behind a trusted proxy. Defaults false.
	TrustProxy bool
}

// NewRouter builds the full HTTP handler: public /healthz + /readyz,
// IP-rate-limited auth routes, and JWT-authenticated per-user-rate-limited
// /sync routes.
func NewRouter(opts Options) http.Handler {
	r := chi.NewRouter()
	// RequestID first (so the id is on the context for logging and recovery),
	// then LogRequests (wraps the statusWriter Recoverer reuses), then Recoverer
	// innermost so it catches handler panics and the resulting 500 is logged.
	// NOTE: Recoverer's double-write guard depends on LogRequests being the
	// wrapper immediately outside it (that is where the *statusWriter is
	// created); do not reorder these three without revisiting Recoverer.
	r.Use(RequestID)
	r.Use(LogRequests)
	r.Use(Recoverer)

	// Liveness: cheap, no dependencies, never rate-limited - a 200 means the
	// process is up.
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Readiness: 200 only when the DB is reachable, so a DB blip pulls the
	// instance from rotation without triggering a liveness restart. Rate-limited
	// per IP: it is unauthenticated and pings the DB, so it must not be a lever
	// to contend for pool connections. The limit is generous so Fly's own health
	// check never trips it.
	readyLimit := NewRateLimiter(5, 10, opts.TrustProxy)
	r.Group(func(r chi.Router) {
		r.Use(readyLimit.ByIP)
		r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
			if opts.Store != nil {
				ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
				defer cancel()
				if err := opts.Store.Ping(ctx); err != nil {
					http.Error(w, "not ready", http.StatusServiceUnavailable)
					return
				}
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready"))
		})
	})

	if opts.Auth != nil {
		authLimit := NewRateLimiter(5, 10, opts.TrustProxy) // per-IP on auth routes
		r.Group(func(r chi.Router) {
			r.Use(authLimit.ByIP)
			r.Get("/auth/github/login", opts.Auth.GithubLogin)
			r.Get("/auth/github/callback", opts.Auth.GithubCallback)
			r.Post("/auth/exchange", opts.Auth.Exchange)
			r.Post("/auth/refresh", opts.Auth.Refresh)
			r.Post("/auth/logout", opts.Auth.Logout)
		})
	}

	sync := Sync{Store: opts.Store}
	userLimit := NewRateLimiter(20, 40, opts.TrustProxy) // per-user on sync
	r.Group(func(r chi.Router) {
		r.Use(AuthMiddleware(opts.SigningKey))
		r.Use(userLimit.ByUser)
		r.Get("/sync", sync.Get)
		r.Post("/sync", sync.Post)
	})

	return r
}
