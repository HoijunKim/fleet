package pgstore

import (
	"context"
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
	// Old hash is now revoked: a second rotation on it must fail.
	if _, err := pg.RotateRefreshToken(ctx, "hash-old", "hash-newer", exp); err == nil {
		t.Fatal("expected error rotating a revoked token")
	}
	// The new hash still rotates.
	if _, err := pg.RotateRefreshToken(ctx, "hash-new", "hash-new2", exp); err != nil {
		t.Fatalf("rotate new: %v", err)
	}
}
