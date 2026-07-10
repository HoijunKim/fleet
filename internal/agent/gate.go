package agent

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Decision is the user's answer to one gated tool call.
type Decision struct {
	Approved bool
	Reason   string
}

// Coordinator maps each pending approval to a buffered channel the GUI decision
// is delivered on. It is safe for concurrent use and ordering-independent:
// Decide may run before or after Await. At most one decision is ever delivered
// per id; Await is the sole owner of cleanup (it discards the entry on exit),
// and timeout/cancel both deny fail-safe.
type Coordinator struct {
	mu      sync.Mutex
	pending map[string]chan Decision
	seq     atomic.Uint64
}

// NewCoordinator returns an empty Coordinator.
func NewCoordinator() *Coordinator {
	return &Coordinator{pending: make(map[string]chan Decision)}
}

// Register creates a new pending approval and returns its id. The channel is
// buffered (cap 1) so Decide never blocks and the decision survives until Await
// receives it, regardless of Decide/Await ordering.
func (c *Coordinator) Register() string {
	id := "act-" + strconv.FormatUint(c.seq.Add(1), 36)
	c.mu.Lock()
	c.pending[id] = make(chan Decision, 1)
	c.mu.Unlock()
	return id
}

// Decide delivers the user's decision for id. It returns false if id is unknown,
// already discarded (timeout/cancel/awaited), or already decided. It never
// deletes the entry (Await owns cleanup) and never blocks: the send is
// non-blocking on the cap-1 buffer, so a duplicate decision is a no-op false.
func (c *Coordinator) Decide(id string, approved bool, reason string) bool {
	c.mu.Lock()
	ch, ok := c.pending[id]
	c.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- Decision{Approved: approved, Reason: reason}:
		return true
	default:
		return false // already decided (buffer full)
	}
}

// Await blocks until id is decided, the timeout elapses, or ctx is cancelled.
// Timeout and cancellation both resolve to a deny (fail-safe). Await always
// discards the pending entry on exit, so any later Decide is a no-op.
func (c *Coordinator) Await(ctx context.Context, id string, timeout time.Duration) Decision {
	c.mu.Lock()
	ch, ok := c.pending[id]
	c.mu.Unlock()
	if !ok {
		return Decision{Approved: false, Reason: "unknown approval id"}
	}
	defer c.discard(id)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case d := <-ch:
		return d
	case <-timer.C:
		return Decision{Approved: false, Reason: "approval timed out"}
	case <-ctx.Done():
		return Decision{Approved: false, Reason: "cancelled"}
	}
}

// discard removes a pending entry if it is still present.
func (c *Coordinator) discard(id string) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}
