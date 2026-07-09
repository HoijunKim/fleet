package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Options carries the router's dependencies. It is empty in the healthz-only
// build and gains fields as later tasks add auth and sync routes.
type Options struct{}

// NewRouter builds the HTTP handler. /healthz is unauthenticated and returns
// the literal text "ok".
func NewRouter(opts Options) http.Handler {
	r := chi.NewRouter()
	r.Use(LogRequests)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	return r
}
