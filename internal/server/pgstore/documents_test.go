package pgstore

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func mkDoc(docID, updatedAt string, deleted bool) Doc {
	return Doc{
		Kind:      "project",
		DocID:     docID,
		Payload:   json.RawMessage(`{"name":"x"}`),
		UpdatedAt: updatedAt,
		Deleted:   deleted,
	}
}

func TestPushLWWAndCursor(t *testing.T) {
	pg := testPg(t)
	ctx := context.Background()
	u, err := pg.UpsertUserByGitHub(ctx, GitHubIdentity{GitHubID: 1, Login: "a"})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	t1 := "2026-01-01T00:00:00Z"
	t2 := "2026-01-02T00:00:00Z"

	// First push: absent doc is accepted, version 1, cursor 1.
	res, cursor, err := pg.Push(ctx, u.ID, []Doc{mkDoc("d1", t1, false)})
	if err != nil {
		t.Fatalf("push1: %v", err)
	}
	if len(res) != 1 || !res[0].Accepted || res[0].Version != 1 || cursor != 1 {
		t.Fatalf("push1 got %+v cursor=%d", res, cursor)
	}

	// Older updated_at is rejected; version stays 1; cursor unchanged.
	res, cursor, err = pg.Push(ctx, u.ID, []Doc{mkDoc("d1", "2025-12-31T00:00:00Z", false)})
	if err != nil {
		t.Fatalf("push-stale: %v", err)
	}
	if res[0].Accepted || res[0].Version != 1 || cursor != 1 {
		t.Fatalf("stale push not rejected: %+v cursor=%d", res, cursor)
	}

	// Newer updated_at wins; version 2; cursor 2.
	res, cursor, err = pg.Push(ctx, u.ID, []Doc{mkDoc("d1", t2, false)})
	if err != nil {
		t.Fatalf("push-newer: %v", err)
	}
	if !res[0].Accepted || res[0].Version != 2 || cursor != 2 {
		t.Fatalf("newer push wrong: %+v cursor=%d", res, cursor)
	}

	// Tombstone accepted as a newer write; version 3.
	res, _, err = pg.Push(ctx, u.ID, []Doc{mkDoc("d1", "2026-01-03T00:00:00Z", true)})
	if err != nil {
		t.Fatalf("push-tombstone: %v", err)
	}
	if !res[0].Accepted || res[0].Version != 3 {
		t.Fatalf("tombstone wrong: %+v", res)
	}

	// Pull since 0 returns the doc ordered by version; cursor = max version.
	docs, pcur, err := pg.Pull(ctx, u.ID, 0)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if len(docs) != 1 || docs[0].Version != 3 || !docs[0].Deleted || pcur != 3 {
		t.Fatalf("pull wrong: %+v cursor=%d", docs, pcur)
	}

	// Pull since current cursor returns nothing; cursor echoes since.
	docs, pcur, err = pg.Pull(ctx, u.ID, 3)
	if err != nil {
		t.Fatalf("pull-since: %v", err)
	}
	if len(docs) != 0 || pcur != 3 {
		t.Fatalf("pull-since wrong: %+v cursor=%d", docs, pcur)
	}
}

// TestPushConcurrentCreateNewestWins documents the concurrent-create
// expectation: two Pushes that create the SAME (kind, doc_id) for one user at
// the same time must not let an older updated_at survive. The per-user lock
// taken at the start of Push serializes them, so whichever commits second sees
// the row the first inserted and applies LWW, leaving the newer timestamp.
// Skips offline like the other Postgres-backed tests.
func TestPushConcurrentCreateNewestWins(t *testing.T) {
	pg := testPg(t)
	ctx := context.Background()
	u, err := pg.UpsertUserByGitHub(ctx, GitHubIdentity{GitHubID: 20, Login: "c"})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	older := "2026-01-01T00:00:00Z"
	newer := "2026-06-01T00:00:00Z"

	var wg sync.WaitGroup
	wg.Add(2)
	errs := make([]error, 2)
	go func() {
		defer wg.Done()
		_, _, errs[0] = pg.Push(ctx, u.ID, []Doc{mkDoc("race", older, false)})
	}()
	go func() {
		defer wg.Done()
		_, _, errs[1] = pg.Push(ctx, u.ID, []Doc{mkDoc("race", newer, false)})
	}()
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatalf("push %d: %v", i, e)
		}
	}

	docs, _, err := pg.Pull(ctx, u.ID, 0)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("want exactly one row, got %d: %+v", len(docs), docs)
	}
	got, err := time.Parse(time.RFC3339, docs[0].UpdatedAt)
	if err != nil {
		t.Fatalf("parse stored updated_at %q: %v", docs[0].UpdatedAt, err)
	}
	want, _ := time.Parse(time.RFC3339, newer)
	if !got.Equal(want) {
		t.Fatalf("newest-wins violated: stored updated_at = %s, want %s", docs[0].UpdatedAt, newer)
	}
}

func TestPushPerUserIsolation(t *testing.T) {
	pg := testPg(t)
	ctx := context.Background()
	ua, _ := pg.UpsertUserByGitHub(ctx, GitHubIdentity{GitHubID: 10, Login: "a"})
	ub, _ := pg.UpsertUserByGitHub(ctx, GitHubIdentity{GitHubID: 11, Login: "b"})

	if _, _, err := pg.Push(ctx, ua.ID, []Doc{mkDoc("shared", "2026-01-01T00:00:00Z", false)}); err != nil {
		t.Fatalf("push a: %v", err)
	}
	// b pulling sees nothing; b's cursor stays 0.
	docs, cur, err := pg.Pull(ctx, ub.ID, 0)
	if err != nil {
		t.Fatalf("pull b: %v", err)
	}
	if len(docs) != 0 || cur != 0 {
		t.Fatalf("isolation broken: b saw %+v cursor=%d", docs, cur)
	}
	// a's cursor is independent (1).
	_, cura, _ := pg.Pull(ctx, ua.ID, 0)
	if cura != 1 {
		t.Fatalf("a cursor = %d, want 1", cura)
	}
}
