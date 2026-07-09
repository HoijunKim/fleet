// Package httpapi is the fleet backend HTTP layer: router, middleware, and
// handlers. It imports the server-only auth and pgstore packages (never Wails).
package httpapi

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hoijun/fleet/internal/server/auth"
)

// statusWriter captures the response status for structured request logging.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// LogRequests logs one structured line per request via slog.
func LogRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"dur_ms", time.Since(start).Milliseconds(),
		)
	})
}

// ctxKey namespaces context values for this package.
type ctxKey int

const userIDKey ctxKey = 0

// WithUserID stores the authenticated user id on the context.
func WithUserID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, userIDKey, id)
}

// UserID reads the authenticated user id from the context.
func UserID(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(userIDKey).(string)
	return v, ok
}

// AuthMiddleware requires a valid Bearer JWT and puts its subject on the context.
func AuthMiddleware(signingKey []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			const prefix = "Bearer "
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, prefix) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			userID, err := auth.VerifyAccess(signingKey, strings.TrimPrefix(h, prefix))
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithUserID(r.Context(), userID)))
		})
	}
}

// sweepInterval throttles how often Allow performs an opportunistic eviction
// sweep. maxEntries is a hard cap on live buckets: once reached, Allow refuses
// brand-new keys instead of growing the map without bound. A flood of distinct
// keys (e.g. rotating client IPs) would otherwise be an unbounded-memory DoS.
const (
	sweepInterval = time.Minute
	maxEntries    = 100000
)

// RateLimiter is a per-key token-bucket limiter.
type RateLimiter struct {
	mu         sync.Mutex
	buckets    map[string]*bucket
	rate       float64 // tokens per second
	burst      float64
	trustProxy bool
	now        func() time.Time
	nextSweep  time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewRateLimiter builds a limiter with the given steady rate and burst.
// trustProxy controls whether ByIP honors proxy headers when deriving the
// client IP (see clientIP); set it true only when behind a trusted proxy.
func NewRateLimiter(rate, burst float64, trustProxy bool) *RateLimiter {
	return &RateLimiter{buckets: map[string]*bucket{}, rate: rate, burst: burst, trustProxy: trustProxy, now: time.Now}
}

// Allow reports whether a request for key may proceed, consuming a token.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	t := rl.now()
	if !t.Before(rl.nextSweep) {
		rl.sweepLocked(t)
		rl.nextSweep = t.Add(sweepInterval)
	}
	b, ok := rl.buckets[key]
	if !ok {
		if len(rl.buckets) >= maxEntries {
			return false // fail closed rather than grow past the cap
		}
		rl.buckets[key] = &bucket{tokens: rl.burst - 1, last: t}
		return true
	}
	b.tokens = min(rl.burst, b.tokens+t.Sub(b.last).Seconds()*rl.rate)
	b.last = t
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sweepLocked evicts idle buckets: any bucket whose tokens have refilled back
// to full burst carries no rate state, so dropping it is safe and it will be
// recreated identically on the next request. Caller must hold rl.mu.
func (rl *RateLimiter) sweepLocked(now time.Time) {
	for k, b := range rl.buckets {
		if min(rl.burst, b.tokens+now.Sub(b.last).Seconds()*rl.rate) >= rl.burst {
			delete(rl.buckets, k)
		}
	}
}

// clientIP derives the per-IP rate-limit key for r. When trustProxy is true the
// process sits behind a trusted proxy (Fly.io), so it honors the Fly-Client-IP
// header, then the left-most X-Forwarded-For entry, before falling back to the
// host of RemoteAddr. When trustProxy is false it uses only RemoteAddr: a
// directly-connected client could otherwise forge XFF to dodge per-IP limits.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if ip := strings.TrimSpace(r.Header.Get("Fly-Client-IP")); ip != "" {
			return ip
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if ip := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0]); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ByIP rate-limits by client IP (for unauthenticated auth routes).
func (rl *RateLimiter) ByIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.Allow(clientIP(r, rl.trustProxy)) {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ByUser rate-limits by authenticated user id, falling back to client IP.
func (rl *RateLimiter) ByUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := UserID(r.Context())
		if id == "" {
			id = clientIP(r, rl.trustProxy)
		}
		if !rl.Allow(id) {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
