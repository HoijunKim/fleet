package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hoijun/fleet/internal/server/auth"
)

func TestAuthMiddlewareSetsUserID(t *testing.T) {
	key := []byte("k")
	tok, _ := auth.IssueAccess(key, "user-9", 15*time.Minute, time.Now())

	var seen string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := UserID(r.Context())
		seen = id
		w.WriteHeader(http.StatusOK)
	})
	h := AuthMiddleware(key)(next)

	// Valid token.
	req := httptest.NewRequest(http.MethodGet, "/sync", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || seen != "user-9" {
		t.Fatalf("valid: code=%d userID=%q", rec.Code, seen)
	}

	// Missing header.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token code=%d, want 401", rec.Code)
	}

	// Bad token.
	req = httptest.NewRequest(http.MethodGet, "/sync", nil)
	req.Header.Set("Authorization", "Bearer garbage")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token code=%d, want 401", rec.Code)
	}
}

func TestRateLimiterAllowAndRefill(t *testing.T) {
	now := time.Unix(0, 0)
	rl := NewRateLimiter(1, 2) // 1 token/sec, burst 2
	rl.now = func() time.Time { return now }

	if !rl.Allow("ip") || !rl.Allow("ip") {
		t.Fatal("first two requests should pass (burst 2)")
	}
	if rl.Allow("ip") {
		t.Fatal("third request should be limited")
	}
	now = now.Add(1100 * time.Millisecond) // ~1 token refilled
	if !rl.Allow("ip") {
		t.Fatal("request after refill should pass")
	}
}
