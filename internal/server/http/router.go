package httpapi

import (
	"net/http"

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

// NewRouter builds the full HTTP handler: public /healthz, IP-rate-limited auth
// routes, and JWT-authenticated per-user-rate-limited /sync routes.
func NewRouter(opts Options) http.Handler {
	r := chi.NewRouter()
	r.Use(LogRequests)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
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
