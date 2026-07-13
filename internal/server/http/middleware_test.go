package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strconv"
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
	rl := NewRateLimiter(1, 2, false) // 1 token/sec, burst 2
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

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		trustProxy bool
		remoteAddr string
		flyIP      string
		xff        string
		want       string
	}{
		{
			name:       "trust honors Fly-Client-IP first",
			trustProxy: true,
			remoteAddr: "10.0.0.1:5000",
			flyIP:      "203.0.113.7",
			xff:        "198.51.100.9, 10.0.0.1",
			want:       "203.0.113.7",
		},
		{
			name:       "trust falls to left-most XFF",
			trustProxy: true,
			remoteAddr: "10.0.0.1:5000",
			xff:        "  198.51.100.9 , 10.0.0.1 ",
			want:       "198.51.100.9",
		},
		{
			name:       "trust falls to RemoteAddr host",
			trustProxy: true,
			remoteAddr: "192.0.2.5:44321",
			want:       "192.0.2.5",
		},
		{
			name:       "no-trust ignores headers, uses RemoteAddr",
			trustProxy: false,
			remoteAddr: "192.0.2.5:44321",
			flyIP:      "203.0.113.7",
			xff:        "198.51.100.9",
			want:       "192.0.2.5",
		},
		{
			name:       "RemoteAddr without port returns raw value",
			trustProxy: false,
			remoteAddr: "192.0.2.5",
			want:       "192.0.2.5",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
			r.RemoteAddr = tt.remoteAddr
			if tt.flyIP != "" {
				r.Header.Set("Fly-Client-IP", tt.flyIP)
			}
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}
			if got := clientIP(r, tt.trustProxy); got != tt.want {
				t.Fatalf("clientIP(trustProxy=%v) = %q, want %q", tt.trustProxy, got, tt.want)
			}
		})
	}
}

func TestRateLimiterEvictsIdleBuckets(t *testing.T) {
	now := time.Unix(0, 0)
	rl := NewRateLimiter(1, 100, false) // 1 token/sec, burst 100 (slow refill)
	rl.now = func() time.Time { return now }

	// idle: spends one token; within the sweep window it refills back to full
	// burst, so it carries no state and is safe to evict.
	if !rl.Allow("idle") {
		t.Fatal("idle: first request should pass")
	}
	// busy: drained mid-consumption; even after the sweep window it has refilled
	// well below full burst, so it must be retained.
	rl.buckets["busy"] = &bucket{tokens: 0, last: now}

	// Advance just past the sweep window; the sweep runs on the next Allow.
	now = now.Add(sweepInterval + time.Second)
	rl.Allow("trigger")

	if _, ok := rl.buckets["idle"]; ok {
		t.Fatal("idle bucket (refilled to full burst) should have been evicted")
	}
	if _, ok := rl.buckets["busy"]; !ok {
		t.Fatal("busy bucket (still below full burst) should be retained")
	}
}

func TestRateLimiterHardCapDeniesNewKeys(t *testing.T) {
	now := time.Unix(0, 0)
	rl := NewRateLimiter(1, 2, false)
	rl.now = func() time.Time { return now }

	// Fill the map to the hard cap with drained buckets so the sweep cannot
	// reclaim them, then verify a brand-new key is denied while an existing
	// (still non-empty) key continues to be served.
	for i := 0; i < maxEntries; i++ {
		k := "k" + strconv.Itoa(i)
		rl.buckets[k] = &bucket{tokens: 0, last: now}
	}
	if len(rl.buckets) != maxEntries {
		t.Fatalf("setup: len(buckets)=%d, want %d", len(rl.buckets), maxEntries)
	}

	if rl.Allow("brand-new-key") {
		t.Fatal("new key past hard cap should be denied")
	}
	if _, ok := rl.buckets["brand-new-key"]; ok {
		t.Fatal("denied new key must not be inserted")
	}

	// An existing key with tokens still works (no new map entry needed).
	rl.buckets["existing"] = &bucket{tokens: 2, last: now}
	if !rl.Allow("existing") {
		t.Fatal("existing key with tokens should still be served past the cap")
	}
}

func TestRecovererCatchesPanic(t *testing.T) {
	h := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic("boom") }))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil)) // must not panic out of ServeHTTP
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", rr.Code)
	}
}

func TestRecovererNoDoubleWriteAfterResponse(t *testing.T) {
	h := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("partial"))
		panic("late boom")
	}))
	rr := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rr, status: http.StatusOK}
	h.ServeHTTP(sw, httptest.NewRequest("GET", "/", nil))
	if rr.Code != http.StatusTeapot {
		t.Fatalf("code = %d, want 418 (recoverer must not override an already-written response)", rr.Code)
	}
}

func TestRequestIDGeneratesAndEchoes(t *testing.T) {
	var captured string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = RequestIDOf(r.Context())
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	got := rr.Header().Get("X-Request-Id")
	if got == "" || got != captured {
		t.Fatalf("header=%q ctx=%q: want a non-empty id present in both", got, captured)
	}
}

func TestRequestIDHonorsValidInbound(t *testing.T) {
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-Id", "abc-123_XYZ.1")
	h.ServeHTTP(rr, req)
	if got := rr.Header().Get("X-Request-Id"); got != "abc-123_XYZ.1" {
		t.Fatalf("valid inbound id not echoed: %q", got)
	}
}

func TestRequestIDRejectsInvalidInbound(t *testing.T) {
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-Id", "bad id with space")
	h.ServeHTTP(rr, req)
	got := rr.Header().Get("X-Request-Id")
	if got == "bad id with space" || got == "" {
		t.Fatalf("invalid inbound id not replaced: %q", got)
	}
}
