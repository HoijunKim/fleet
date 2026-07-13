package pgstore

import (
	"context"
	"errors"
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
