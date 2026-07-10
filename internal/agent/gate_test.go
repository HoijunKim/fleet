package agent

import (
	"context"
	"testing"
	"time"
)

func TestCoordinatorAllow(t *testing.T) {
	// Decide BEFORE Await: the buffered channel must still deliver the decision.
	c := NewCoordinator()
	id := c.Register()
	if !c.Decide(id, true, "ok") {
		t.Fatal("Decide on a live id must return true")
	}
	d := c.Await(context.Background(), id, time.Second)
	if !d.Approved || d.Reason != "ok" {
		t.Errorf("await = %+v", d)
	}
}

func TestCoordinatorAwaitThenDecide(t *testing.T) {
	// Decide AFTER Await has started blocking (the normal loopback flow).
	c := NewCoordinator()
	id := c.Register()
	go func() {
		time.Sleep(10 * time.Millisecond)
		c.Decide(id, true, "yes")
	}()
	d := c.Await(context.Background(), id, time.Second)
	if !d.Approved || d.Reason != "yes" {
		t.Errorf("await = %+v", d)
	}
}

func TestCoordinatorDeny(t *testing.T) {
	c := NewCoordinator()
	id := c.Register()
	c.Decide(id, false, "nope")
	d := c.Await(context.Background(), id, time.Second)
	if d.Approved || d.Reason != "nope" {
		t.Errorf("await = %+v", d)
	}
}

func TestCoordinatorTimeout(t *testing.T) {
	c := NewCoordinator()
	id := c.Register()
	d := c.Await(context.Background(), id, 20*time.Millisecond)
	if d.Approved || d.Reason != "approval timed out" {
		t.Errorf("timeout await = %+v", d)
	}
	if c.Decide(id, true, "late") {
		t.Error("Decide after timeout must return false (entry discarded)")
	}
}

func TestCoordinatorCancel(t *testing.T) {
	c := NewCoordinator()
	id := c.Register()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := c.Await(ctx, id, time.Second)
	if d.Approved || d.Reason != "cancelled" {
		t.Errorf("cancel await = %+v", d)
	}
}

func TestCoordinatorDoubleDecide(t *testing.T) {
	c := NewCoordinator()
	id := c.Register()
	if !c.Decide(id, true, "first") {
		t.Fatal("first Decide must return true")
	}
	if c.Decide(id, false, "second") {
		t.Error("second Decide must return false (already decided)")
	}
	d := c.Await(context.Background(), id, time.Second)
	if !d.Approved || d.Reason != "first" {
		t.Errorf("await = %+v", d)
	}
}

func TestCoordinatorUnknownID(t *testing.T) {
	c := NewCoordinator()
	if c.Decide("nope", true, "x") {
		t.Error("Decide on unknown id must return false")
	}
	d := c.Await(context.Background(), "nope", time.Second)
	if d.Approved {
		t.Error("Await on unknown id must not approve")
	}
}
