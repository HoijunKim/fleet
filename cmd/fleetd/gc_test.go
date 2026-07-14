package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestRunGCRunsThenStopsOnCancel verifies runGC prunes immediately, keeps
// ticking, reports pruned rows, and returns promptly when ctx is cancelled.
func TestRunGCRunsThenStopsOnCancel(t *testing.T) {
	var calls, pruned int64
	prune := func(ctx context.Context) (int64, error) {
		atomic.AddInt64(&calls, 1)
		return 5, nil
	}
	onPruned := func(n int64) { atomic.AddInt64(&pruned, n) }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { runGC(ctx, 5*time.Millisecond, prune, onPruned); close(done) }()

	time.Sleep(40 * time.Millisecond) // immediate tick + a few interval ticks
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runGC did not return after ctx cancel")
	}
	if atomic.LoadInt64(&calls) < 1 {
		t.Fatalf("expected >=1 prune call, got %d", atomic.LoadInt64(&calls))
	}
	if atomic.LoadInt64(&pruned) < 5 {
		t.Fatalf("onPruned not invoked with the pruned count: %d", atomic.LoadInt64(&pruned))
	}
}

// TestRunGCContinuesOnPruneError verifies a transient prune error does not kill
// the loop.
func TestRunGCContinuesOnPruneError(t *testing.T) {
	var calls int64
	prune := func(ctx context.Context) (int64, error) {
		if atomic.AddInt64(&calls, 1) == 1 {
			return 0, errors.New("transient")
		}
		return 1, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runGC(ctx, 5*time.Millisecond, prune, func(int64) {})

	deadline := time.After(time.Second)
	for atomic.LoadInt64(&calls) < 3 {
		select {
		case <-deadline:
			t.Fatalf("loop stalled after the first error: calls=%d", atomic.LoadInt64(&calls))
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
}
