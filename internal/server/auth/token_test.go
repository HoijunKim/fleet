package auth

import (
	"testing"
	"time"
)

func TestIssueAndVerifyAccess(t *testing.T) {
	key := []byte("test-signing-key")
	now := time.Now()

	tok, err := IssueAccess(key, "user-123", 15*time.Minute, now)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	sub, err := VerifyAccess(key, tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if sub != "user-123" {
		t.Fatalf("sub = %q, want user-123", sub)
	}
}

func TestVerifyAccessRejectsWrongKey(t *testing.T) {
	tok, _ := IssueAccess([]byte("key-a"), "u", time.Minute, time.Now())
	if _, err := VerifyAccess([]byte("key-b"), tok); err == nil {
		t.Fatal("expected error verifying with wrong key")
	}
}

func TestVerifyAccessRejectsExpired(t *testing.T) {
	key := []byte("k")
	tok, _ := IssueAccess(key, "u", -time.Minute, time.Now())
	if _, err := VerifyAccess(key, tok); err == nil {
		t.Fatal("expected error verifying an expired token")
	}
}

func TestRefreshTokenHashStable(t *testing.T) {
	raw, hash, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if raw == "" || hash == "" {
		t.Fatal("empty raw/hash")
	}
	if HashRefresh(raw) != hash {
		t.Fatal("HashRefresh not stable with NewRefreshToken")
	}
}
