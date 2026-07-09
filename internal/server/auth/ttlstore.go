package auth

import (
	"sync"
	"time"
)

type ttlEntry[V any] struct {
	val V
	exp time.Time
}

// ttlStore is a concurrency-safe map of string -> V with per-entry expiry and
// one-time take semantics. Used for pending OAuth state and link codes.
type ttlStore[V any] struct {
	mu  sync.Mutex
	m   map[string]ttlEntry[V]
	now func() time.Time
}

func newTTLStore[V any](now func() time.Time) *ttlStore[V] {
	if now == nil {
		now = time.Now
	}
	return &ttlStore[V]{m: map[string]ttlEntry[V]{}, now: now}
}

func (s *ttlStore[V]) put(key string, v V, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = ttlEntry[V]{val: v, exp: s.now().Add(ttl)}
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
