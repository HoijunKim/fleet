package pgstore

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pg is the Postgres-backed Store implementation.
type Pg struct {
	pool *pgxpool.Pool
}

// New opens a connection pool to databaseURL.
func New(ctx context.Context, databaseURL string) (*Pg, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	return &Pg{pool: pool}, nil
}

// Close releases the pool.
func (p *Pg) Close() { p.pool.Close() }

// errRefreshInvalid marks a refresh token that exists but is revoked/expired.
var errRefreshInvalid = errors.New("refresh token invalid")

// UpsertUserByGitHub inserts or updates a user keyed by github_id and ensures a
// user_versions counter row exists.
func (p *Pg) UpsertUserByGitHub(ctx context.Context, id GitHubIdentity) (User, error) {
	row := p.pool.QueryRow(ctx, `
INSERT INTO users (id, github_id, login, email, avatar_url)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (github_id) DO UPDATE SET
  login = EXCLUDED.login,
  email = EXCLUDED.email,
  avatar_url = EXCLUDED.avatar_url,
  updated_at = now()
RETURNING id, github_id, login, email, avatar_url`,
		uuid.NewString(), id.GitHubID, id.Login, id.Email, id.AvatarURL)
	var u User
	if err := row.Scan(&u.ID, &u.GitHubID, &u.Login, &u.Email, &u.AvatarURL); err != nil {
		return User{}, err
	}
	if _, err := p.pool.Exec(ctx,
		`INSERT INTO user_versions (user_id, current) VALUES ($1, 0) ON CONFLICT DO NOTHING`, u.ID); err != nil {
		return User{}, err
	}
	return u, nil
}

// CreateRefreshToken stores a hashed refresh token.
func (p *Pg) CreateRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`,
		uuid.NewString(), userID, tokenHash, expiresAt)
	return err
}

// RotateRefreshToken validates oldHash (present, not revoked, not expired),
// revokes it, and inserts newHash for the same user, all in one transaction.
func (p *Pg) RotateRefreshToken(ctx context.Context, oldHash, newHash string, expiresAt time.Time) (string, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var userID string
	var revoked bool
	var exp time.Time
	err = tx.QueryRow(ctx,
		`SELECT user_id, revoked, expires_at FROM refresh_tokens WHERE token_hash = $1 FOR UPDATE`, oldHash).
		Scan(&userID, &revoked, &exp)
	if err != nil {
		return "", err
	}
	if revoked || time.Now().After(exp) {
		return "", errRefreshInvalid
	}
	if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked = true WHERE token_hash = $1`, oldHash); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`,
		uuid.NewString(), userID, newHash, expiresAt); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return userID, nil
}

// RevokeRefreshToken marks a refresh token revoked (idempotent).
func (p *Pg) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	_, err := p.pool.Exec(ctx, `UPDATE refresh_tokens SET revoked = true WHERE token_hash = $1`, tokenHash)
	return err
}
