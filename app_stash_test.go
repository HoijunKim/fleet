package main

import "testing"

// TestStashListNeverNil guards the root cause of the "app freezes on repo click"
// bug: git.StashList returns a nil slice when a repo has no stashes, which
// serialized to JS as null and made StashPanel's `entries.length` throw. The
// binding must always return a non-nil slice.
func TestStashListNeverNil(t *testing.T) {
	a := &App{runner: fakeRunner{out: map[string]string{}}} // "stash list" -> "" -> nil from git
	got := a.StashList("/x")
	if got == nil {
		t.Fatal("StashList must never return nil (the front end reads .length on it)")
	}
	if len(got) != 0 {
		t.Errorf("want empty slice, got %v", got)
	}
}
