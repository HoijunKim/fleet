package auth

import (
	"testing"
	"time"
)

func TestTTLStoreTakeIsOneTime(t *testing.T) {
	s := newTTLStore[string](time.Now)
	s.put("k", "v", time.Minute)

	got, ok := s.take("k")
	if !ok || got != "v" {
		t.Fatalf("first take = %q,%v, want v,true", got, ok)
	}
	if _, ok := s.take("k"); ok {
		t.Fatal("second take should miss (one-time)")
	}
}

func TestTTLStoreExpires(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := now
	s := newTTLStore[int](func() time.Time { return clock })
	s.put("k", 5, time.Minute)

	clock = now.Add(2 * time.Minute) // advance past TTL
	if _, ok := s.take("k"); ok {
		t.Fatal("expired entry should not be returned")
	}
}
