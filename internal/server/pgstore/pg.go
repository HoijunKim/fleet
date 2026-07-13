package pgstore

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// Ping verifies a live connection to the database (used by the readiness probe).
func (p *Pg) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }

// errRefreshInvalid marks a refresh token that is unknown or expired (a normal
// failure, not an attack). Revoked-token reuse is ErrRefreshReuse instead.
var errRefreshInvalid = errors.New("refresh token invalid")

// ErrRefreshReuse is returned when an already-rotated (revoked) refresh token is
// presented again. This is a reuse signal (the token was likely stolen), so
// RotateRefreshToken revokes the whole family before returning it.
var ErrRefreshReuse = errors.New("refresh token reuse detected")

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

// CreateRefreshToken stores a hashed refresh token, starting a fresh rotation
// family (each login is its own lineage).
func (p *Pg) CreateRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, family_id) VALUES ($1, $2, $3, $4, $5)`,
		uuid.NewString(), userID, tokenHash, expiresAt, uuid.NewString())
	return err
}

// RotateRefreshToken rotates oldHash to newHash in one transaction. A valid
// token is revoked and newHash inserted into the SAME family. Presenting an
// already-revoked token is treated as reuse: the whole family is revoked and
// ErrRefreshReuse is returned. Unknown or expired tokens return errRefreshInvalid
// and leave any family untouched.
func (p *Pg) RotateRefreshToken(ctx context.Context, oldHash, newHash string, expiresAt time.Time) (string, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var userID, familyID string
	var revoked bool
	var exp time.Time
	err = tx.QueryRow(ctx,
		`SELECT user_id, revoked, expires_at, family_id FROM refresh_tokens WHERE token_hash = $1 FOR UPDATE`, oldHash).
		Scan(&userID, &revoked, &exp, &familyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errRefreshInvalid // unknown token: no known family to revoke
	}
	if err != nil {
		return "", err
	}
	if revoked {
		// Reuse of an already-rotated/revoked token: revoke the entire family so a
		// stolen-then-rotated token cannot outlive the theft. Commit the revoke.
		if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked = true WHERE family_id = $1`, familyID); err != nil {
			return "", err
		}
		if err := tx.Commit(ctx); err != nil {
			return "", err
		}
		return "", ErrRefreshReuse
	}
	if time.Now().After(exp) {
		return "", errRefreshInvalid // normal expiry, not an attack: leave the family live
	}
	if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked = true WHERE token_hash = $1`, oldHash); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, family_id) VALUES ($1, $2, $3, $4, $5)`,
		uuid.NewString(), userID, newHash, expiresAt, familyID); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return userID, nil
}

// RevokeRefreshToken revokes the entire family of the given token, so logout
// ends the whole rotation chain rather than only the presented token. An unknown
// token matches no family and is a no-op (idempotent).
func (p *Pg) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	_, err := p.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked = true
		 WHERE family_id = (SELECT family_id FROM refresh_tokens WHERE token_hash = $1)`, tokenHash)
	return err
}
