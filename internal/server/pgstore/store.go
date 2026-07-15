// Package pgstore is the Postgres-backed server store: identity (users,
// refresh tokens) and the per-user versioned document store with LWW sync.
// The Store interface lets handler tests substitute a fake.
package pgstore

import (
	"context"
	"encoding/json"
	"time"
)

// GitHubIdentity is the subset of a GitHub profile used to upsert a user.
type GitHubIdentity struct {
	GitHubID  int64
	Login     string
	Email     string
	AvatarURL string
}

// User is a fleet account row.
type User struct {
	ID        string
	GitHubID  int64
	Login     string
	Email     string
	AvatarURL string
}

// Doc is one synced document. The JSON shape is the shared v0 sync contract.
type Doc struct {
	Kind      string          `json:"kind"`
	DocID     string          `json:"doc_id"`
	Payload   json.RawMessage `json:"payload"`
	UpdatedAt string          `json:"updated_at"`
	Deleted   bool            `json:"deleted"`
	Version   int64           `json:"version"`
}

// PushResult reports the outcome of one pushed document.
type PushResult struct {
	DocID    string `json:"doc_id"`
	Kind     string `json:"kind"`
	Accepted bool   `json:"accepted"`
	Version  int64  `json:"version"`
}

// Store is the server persistence seam consumed by the HTTP and auth layers.
type Store interface {
	Ping(ctx context.Context) error
	UpsertUserByGitHub(ctx context.Context, id GitHubIdentity) (User, error)
	CreateRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error
	RotateRefreshToken(ctx context.Context, oldHash, newHash string, expiresAt time.Time) (string, error)
	RevokeRefreshToken(ctx context.Context, tokenHash string) error
	Pull(ctx context.Context, userID string, since int64) ([]Doc, int64, error)
	Push(ctx context.Context, userID string, docs []Doc) ([]PushResult, int64, error)
	// DeleteAccount irreversibly removes a user: their documents, version
	// counter, refresh tokens, and the user row, in a single transaction.
	DeleteAccount(ctx context.Context, userID string) error
}
