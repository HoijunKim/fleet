package auth

import (
	"fmt"
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

// TestTTLStoreSweepRemovesExpiredEntries exercises FIX 3: entries that are
// abandoned (never passed to take, e.g. a pending OAuth login that never
// completes) must still be reclaimed, via put()'s throttled opportunistic
// sweep, once the sweep interval has elapsed.
func TestTTLStoreSweepRemovesExpiredEntries(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := now
	s := newTTLStore[int](func() time.Time { return clock })

	for i := 0; i < 5; i++ {
		if !s.put(fmt.Sprintf("stale-%d", i), i, time.Second) {
			t.Fatalf("put stale-%d failed", i)
		}
	}
	s.mu.Lock()
	n := len(s.m)
	s.mu.Unlock()
	if n != 5 {
		t.Fatalf("len(m) after inserts = %d, want 5", n)
	}

	// Advance past both the entries' expiry and the sweep throttle window,
	// then put a fresh entry - this should trigger a sweep that reclaims
	// the stale entries even though take() was never called on them.
	clock = now.Add(sweepInterval + time.Minute)
	if !s.put("fresh", 99, time.Minute) {
		t.Fatal("put fresh failed")
	}

	s.mu.Lock()
	n = len(s.m)
	_, freshStillThere := s.m["fresh"]
	s.mu.Unlock()
	if n != 1 || !freshStillThere {
		t.Fatalf("after sweep len(m) = %d (freshStillThere=%v), want 1 entry (only %q)", n, freshStillThere, "fresh")
	}
}

// TestTTLStorePutFailsPastHardCap exercises FIX 3's hard cap: once the store
// holds maxEntries live entries, put() for a new key must drop the write and
// return false rather than growing the map without bound, but updating an
// already-present key is still allowed since it doesn't grow the map.
func TestTTLStorePutFailsPastHardCap(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := newTTLStore[int](func() time.Time { return now })

	// Fill directly to the hard cap, bypassing put()'s sweep throttling, so
	// the test stays fast and deterministic while still exercising the same
	// cap-check code path that put() runs.
	s.mu.Lock()
	for i := 0; i < maxEntries; i++ {
		s.m[fmt.Sprintf("k%d", i)] = ttlEntry[int]{val: i, exp: now.Add(time.Minute)}
	}
	s.nextSweep = now.Add(sweepInterval) // keep this put() from also sweeping
	s.mu.Unlock()

	if s.put("overflow", 1, time.Minute) {
		t.Fatal("put at hard cap should return false")
	}
	if _, ok := s.take("overflow"); ok {
		t.Fatal("overflow entry should not have been stored")
	}

	if !s.put("k0", 42, time.Minute) {
		t.Fatal("put for an existing key at capacity should still succeed")
	}
}
