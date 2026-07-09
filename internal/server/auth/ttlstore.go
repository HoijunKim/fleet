package auth

import (
	"sync"
	"time"
)

// sweepInterval throttles how often put() performs an opportunistic expiry
// sweep of the store. maxEntries is a hard cap on live entries: once
// reached, put() drops the write instead of growing the map without bound.
// take() already reclaims entries on the happy path; these two knobs are
// the memory-safety backstop for entries that are never taken (e.g. an
// abandoned /auth/github/login that never completes its callback).
const (
	sweepInterval = time.Minute
	maxEntries    = 100000
)

type ttlEntry[V any] struct {
	val V
	exp time.Time
}

// ttlStore is a concurrency-safe map of string -> V with per-entry expiry and
// one-time take semantics. Used for pending OAuth state and link codes.
type ttlStore[V any] struct {
	mu        sync.Mutex
	m         map[string]ttlEntry[V]
	now       func() time.Time
	nextSweep time.Time
}

func newTTLStore[V any](now func() time.Time) *ttlStore[V] {
	if now == nil {
		now = time.Now
	}
	return &ttlStore[V]{m: map[string]ttlEntry[V]{}, now: now}
}

// put stores v under key with the given ttl. Before writing, if the
// throttle window has elapsed it opportunistically sweeps expired entries.
// It returns false (dropping the write) if the store is at maxEntries even
// after sweeping; callers must treat a false return as failure.
func (s *ttlStore[V]) put(key string, v V, ttl time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if !now.Before(s.nextSweep) {
		s.sweepLocked(now)
		s.nextSweep = now.Add(sweepInterval)
	}
	if _, exists := s.m[key]; !exists && len(s.m) >= maxEntries {
		return false
	}
	s.m[key] = ttlEntry[V]{val: v, exp: now.Add(ttl)}
	return true
}

// sweepLocked deletes all expired entries. Caller must hold s.mu.
func (s *ttlStore[V]) sweepLocked(now time.Time) {
	for k, e := range s.m {
		if now.After(e.exp) {
			delete(s.m, k)
		}
	}
}

// take removes and returns the entry; ok is false when missing or expired.
func (s *ttlStore[V]) take(key string) (V, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[key]
	if !ok {
		var zero V
		return zero, false
	}
	delete(s.m, key)
	if s.now().After(e.exp) {
		var zero V
		return zero, false
	}
	return e.val, true
}
