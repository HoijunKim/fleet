package pgstore

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// maxSerializeRetries bounds retries when a SERIALIZABLE transaction aborts with
// a transient conflict. Family revocation runs SERIALIZABLE so a concurrent
// tip-rotation cannot phantom-insert a live token past a revoke; on the rare
// conflict the loser retries and sees the committed state.
const maxSerializeRetries = 5

// isRetryable reports whether err is a transient Postgres conflict worth
// retrying: a serialization failure (40001) or a deadlock (40P01), both
// possible outcomes of concurrent family-scoped writes and both resolved by
// re-running against the committed state.
func isRetryable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "40001" || pgErr.Code == "40P01")
}

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

// Stat returns a snapshot of the connection pool (used for the /metrics gauges).
func (p *Pg) Stat() *pgxpool.Stat { return p.pool.Stat() }

// PruneRefreshTokens deletes expired refresh tokens and returns the number
// removed. An expired token can never mint access (RotateRefreshToken rejects it
// on the expiry check), so these rows are dead weight. Revoked-but-not-expired
// rows are RETAINED as the slice-4b reuse-detection tripwires, so any ACTIVE
// compromise is still caught - a still-usable stolen token is by definition
// non-expired. (Rotation extends expiry, so an ancestor can be expired while its
// family's tip is live; the only thing pruning forgoes is the alarm for a stale
// replay of an already-expired token, which grants nothing anyway - matching the
// OAuth BCP of dropping reuse records at expiry.) A single atomic DELETE that
// only touches expired rows, so it never contends with a live rotation.
func (p *Pg) PruneRefreshTokens(ctx context.Context) (int64, error) {
	tag, err := p.pool.Exec(ctx, `DELETE FROM refresh_tokens WHERE expires_at < now()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

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
// and leave any family untouched. It runs SERIALIZABLE with a bounded retry so a
// concurrent tip-rotation cannot survive a reuse-triggered family revoke.
func (p *Pg) RotateRefreshToken(ctx context.Context, oldHash, newHash string, expiresAt time.Time) (string, error) {
	for attempt := 0; ; attempt++ {
		userID, err := p.rotateOnce(ctx, oldHash, newHash, expiresAt)
		if isRetryable(err) && attempt < maxSerializeRetries {
			continue
		}
		return userID, err
	}
}

func (p *Pg) rotateOnce(ctx context.Context, oldHash, newHash string, expiresAt time.Time) (string, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
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
		return userID, ErrRefreshReuse // userID for the security log; caller still 401s
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
// token matches no family (the IN-subquery is empty) and is a no-op (idempotent).
// Like rotation it runs SERIALIZABLE with a bounded retry so a concurrent
// tip-rotation cannot survive the logout revoke.
func (p *Pg) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	for attempt := 0; ; attempt++ {
		err := p.revokeFamilyOnce(ctx, tokenHash)
		if isRetryable(err) && attempt < maxSerializeRetries {
			continue
		}
		return err
	}
}

func (p *Pg) revokeFamilyOnce(ctx context.Context, tokenHash string) error {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`UPDATE refresh_tokens SET revoked = true
		 WHERE family_id IN (SELECT family_id FROM refresh_tokens WHERE token_hash = $1)`, tokenHash); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
