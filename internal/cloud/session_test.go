package cloud

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionRefreshesOn401(t *testing.T) {
	var rotated Tokens
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sync":
			if r.Header.Get("Authorization") != "Bearer new" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"docs": []Doc{}, "cursor": 0})
		case "/auth/refresh":
			var body struct {
				Refresh string `json:"refresh_token"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Refresh != "r0" {
				t.Errorf("refresh sent %q, want r0", body.Refresh)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "new", "refresh_token": "r1"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	s := NewSession(c, "old", "r0", func(t Tokens) { rotated = t })

	calls := 0
	err := s.WithAccess(func(access string) error {
		calls++
		_, _, e := c.Pull(0, access)
		return e
	})
	if err != nil {
		t.Fatalf("WithAccess err: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected fn called twice (401 then retry), got %d", calls)
	}
	if rotated.Access != "new" || rotated.Refresh != "r1" {
		t.Errorf("onRotate got %+v", rotated)
	}
	if s.Access() != "new" {
		t.Errorf("session access not updated: %q", s.Access())
	}
}

// TestSessionRefreshFailureSignsOut verifies that when the refresh call itself
// fails (e.g. the refresh token was revoked), WithAccess surfaces the refresh
// error instead of looping: fn is called exactly once, onRotate is never
// invoked, and the session's tokens are left untouched so the caller can
// treat this as a signed-out condition.
func TestSessionRefreshFailureSignsOut(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sync":
			w.WriteHeader(http.StatusUnauthorized)
		case "/auth/refresh":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	rotateCalls := 0
	s := NewSession(c, "old", "r0", func(t Tokens) { rotateCalls++ })

	calls := 0
	err := s.WithAccess(func(access string) error {
		calls++
		_, _, e := c.Pull(0, access)
		return e
	})
	if err == nil {
		t.Fatal("expected WithAccess to return an error when refresh fails")
	}
	if errors.Is(err, ErrUnauthorized) {
		t.Errorf("expected the refresh error, got ErrUnauthorized passthrough: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected fn called exactly once (no retry after failed refresh), got %d", calls)
	}
	if rotateCalls != 0 {
		t.Errorf("onRotate should not be called on failed refresh, got %d calls", rotateCalls)
	}
	if s.Access() != "old" || s.refresh != "r0" {
		t.Errorf("session tokens must be left untouched on failed refresh, got access=%q refresh=%q", s.Access(), s.refresh)
	}
}
