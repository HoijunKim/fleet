package pgstore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestUpsertUserByGitHubIsIdempotent(t *testing.T) {
	pg := testPg(t)
	ctx := context.Background()

	u1, err := pg.UpsertUserByGitHub(ctx, GitHubIdentity{GitHubID: 42, Login: "octocat", AvatarURL: "a1"})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	u2, err := pg.UpsertUserByGitHub(ctx, GitHubIdentity{GitHubID: 42, Login: "octocat-renamed", AvatarURL: "a2"})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if u1.ID != u2.ID {
		t.Fatalf("id changed on re-upsert: %s -> %s", u1.ID, u2.ID)
	}
	if u2.Login != "octocat-renamed" || u2.AvatarURL != "a2" {
		t.Fatalf("profile not updated: %+v", u2)
	}
}

func TestRefreshTokenRotation(t *testing.T) {
	pg := testPg(t)
	ctx := context.Background()
	u, err := pg.UpsertUserByGitHub(ctx, GitHubIdentity{GitHubID: 7, Login: "u7"})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	exp := time.Now().Add(24 * time.Hour)
	if err := pg.CreateRefreshToken(ctx, u.ID, "hash-old", exp); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := pg.RotateRefreshToken(ctx, "hash-old", "hash-new", exp)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if got != u.ID {
		t.Fatalf("rotate returned user %q, want %q", got, u.ID)
	}
	// The live (rotated) token still rotates forward.
	if _, err := pg.RotateRefreshToken(ctx, "hash-new", "hash-new2", exp); err != nil {
		t.Fatalf("rotate new: %v", err)
	}
	// Reusing the original (already-rotated) token fails; family-wide revocation
	// on reuse is covered by TestRefreshTokenFamilyRevocation.
	if _, err := pg.RotateRefreshToken(ctx, "hash-old", "hash-newer", exp); err == nil {
		t.Fatal("expected error rotating a revoked token")
	}
}

func TestPing(t *testing.T) {
	pg := testPg(t)
	if err := pg.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestRefreshTokenFamilyRevocation(t *testing.T) {
	pg := testPg(t)
	ctx := context.Background()
	u, err := pg.UpsertUserByGitHub(ctx, GitHubIdentity{GitHubID: 71, Login: "u71"})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	exp := time.Now().Add(24 * time.Hour)

	// Family A: login t0 -> t1 -> t2 (one lineage).
	if err := pg.CreateRefreshToken(ctx, u.ID, "t0", exp); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := pg.RotateRefreshToken(ctx, "t0", "t1", exp); err != nil {
		t.Fatalf("rotate t0->t1: %v", err)
	}
	if _, err := pg.RotateRefreshToken(ctx, "t1", "t2", exp); err != nil {
		t.Fatalf("rotate t1->t2: %v", err)
	}
	// A separate login is an independent family.
	if err := pg.CreateRefreshToken(ctx, u.ID, "other", exp); err != nil {
		t.Fatalf("create other: %v", err)
	}

	// Reuse: presenting the already-rotated t0 is a reuse signal.
	if _, err := pg.RotateRefreshToken(ctx, "t0", "tX", exp); !errors.Is(err, ErrRefreshReuse) {
		t.Fatalf("reuse of rotated token: err = %v, want ErrRefreshReuse", err)
	}
	// The whole family is revoked: the live token t2 (an attacker's, in a theft)
	// no longer rotates.
	if _, err := pg.RotateRefreshToken(ctx, "t2", "tY", exp); !errors.Is(err, ErrRefreshReuse) {
		t.Fatalf("live token after family revoke: err = %v, want ErrRefreshReuse", err)
	}
	// Isolation: the independent family is untouched and still rotates.
	if _, err := pg.RotateRefreshToken(ctx, "other", "other2", exp); err != nil {
		t.Fatalf("independent family wrongly revoked: %v", err)
	}
}

func TestLogoutRevokesFamily(t *testing.T) {
	pg := testPg(t)
	ctx := context.Background()
	u, err := pg.UpsertUserByGitHub(ctx, GitHubIdentity{GitHubID: 81, Login: "u81"})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	exp := time.Now().Add(24 * time.Hour)
	if err := pg.CreateRefreshToken(ctx, u.ID, "l0", exp); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := pg.RotateRefreshToken(ctx, "l0", "l1", exp); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	// Logout with the live token revokes the whole family.
	if err := pg.RevokeRefreshToken(ctx, "l1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := pg.RotateRefreshToken(ctx, "l1", "l2", exp); !errors.Is(err, ErrRefreshReuse) {
		t.Fatalf("token after logout: err = %v, want ErrRefreshReuse (family revoked)", err)
	}
}

func TestExpiredRefreshIsInvalidNotReuse(t *testing.T) {
	pg := testPg(t)
	ctx := context.Background()
	u, err := pg.UpsertUserByGitHub(ctx, GitHubIdentity{GitHubID: 91, Login: "u91"})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := pg.CreateRefreshToken(ctx, u.ID, "exp0", time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = pg.RotateRefreshToken(ctx, "exp0", "exp1", time.Now().Add(time.Hour))
	if !errors.Is(err, errRefreshInvalid) {
		t.Fatalf("expired token: err = %v, want errRefreshInvalid (not reuse)", err)
	}
}

// TestRefreshFamilyConcurrentReuseRevokesTip stresses the phantom-row race: for
// many families it concurrently reuses the (revoked) original token and rotates
// the live tip. A reuse always fires (the original is already rotated), so the
// family MUST end with zero live tokens - a surviving live token would mean a
// tip-rotation phantomed past the family revoke. SERIALIZABLE + retry guarantees
// the invariant.
func TestRefreshFamilyConcurrentReuseRevokesTip(t *testing.T) {
	pg := testPg(t)
	ctx := context.Background()
	u, err := pg.UpsertUserByGitHub(ctx, GitHubIdentity{GitHubID: 101, Login: "u101"})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	exp := time.Now().Add(24 * time.Hour)
	const N = 60

	var wg sync.WaitGroup
	fail := make(chan string, N)
	for i := 0; i < N; i++ {
		orig := fmt.Sprintf("c%d-t0", i)
		tip := fmt.Sprintf("c%d-t1", i)
		if err := pg.CreateRefreshToken(ctx, u.ID, orig, exp); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		if _, err := pg.RotateRefreshToken(ctx, orig, tip, exp); err != nil {
			t.Fatalf("seed rotate %d: %v", i, err)
		}
		wg.Add(1)
		go func(i int, orig, tip string) {
			defer wg.Done()
			var inner sync.WaitGroup
			inner.Add(2)
			go func() { defer inner.Done(); _, _ = pg.RotateRefreshToken(ctx, orig, fmt.Sprintf("c%d-reuse", i), exp) }()
			go func() { defer inner.Done(); _, _ = pg.RotateRefreshToken(ctx, tip, fmt.Sprintf("c%d-t2", i), exp) }()
			inner.Wait()

			var live int
			err := pg.pool.QueryRow(ctx,
				`SELECT count(*) FROM refresh_tokens
				 WHERE family_id = (SELECT family_id FROM refresh_tokens WHERE token_hash = $1)
				   AND revoked = false`, orig).Scan(&live)
			if err != nil {
				fail <- fmt.Sprintf("family %d query: %v", i, err)
				return
			}
			if live != 0 {
				fail <- fmt.Sprintf("family %d: %d live token(s) after concurrent reuse (phantom survived revoke)", i, live)
			}
		}(i, orig, tip)
	}
	wg.Wait()
	close(fail)
	for msg := range fail {
		t.Error(msg)
	}
}

func TestPruneRefreshTokens(t *testing.T) {
	pg := testPg(t)
	ctx := context.Background()
	u, err := pg.UpsertUserByGitHub(ctx, GitHubIdentity{GitHubID: 111, Login: "u111"})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	ins := func(hash string, exp time.Time, revoked bool) {
		if _, err := pg.pool.Exec(ctx,
			`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, revoked, family_id)
			 VALUES (gen_random_uuid(), $1, $2, $3, $4, gen_random_uuid())`, u.ID, hash, exp, revoked); err != nil {
			t.Fatalf("insert %s: %v", hash, err)
		}
	}
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	ins("exp-live", past, false)
	ins("exp-revoked", past, true)
	ins("live", future, false)
	ins("revoked-valid", future, true) // reuse tripwire - must survive

	n, err := pg.PruneRefreshTokens(ctx)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 2 {
		t.Fatalf("pruned %d, want 2 (the two expired rows)", n)
	}

	var survivors, expired int
	if err := pg.pool.QueryRow(ctx,
		`SELECT count(*) FROM refresh_tokens WHERE token_hash IN ('live','revoked-valid')`).Scan(&survivors); err != nil {
		t.Fatal(err)
	}
	if survivors != 2 {
		t.Fatalf("survivors = %d, want 2 (live + revoked-valid retained)", survivors)
	}
	if err := pg.pool.QueryRow(ctx,
		`SELECT count(*) FROM refresh_tokens WHERE token_hash IN ('exp-live','exp-revoked')`).Scan(&expired); err != nil {
		t.Fatal(err)
	}
	if expired != 0 {
		t.Fatalf("expired survivors = %d, want 0", expired)
	}
}
