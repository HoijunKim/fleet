package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Push applies the LWW rule per document in one transaction. A doc is accepted
// iff its updated_at is newer than the stored one (or the doc is absent). Each
// accepted write bumps the per-user counter (same tx) and stamps the version.
// The returned cursor is the user's current counter after the batch.
func (p *Pg) Push(ctx context.Context, userID string, docs []Doc) ([]PushResult, int64, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)

	// Serialize concurrent Pushes for the same user by locking the per-user
	// counter row for the whole transaction. Without this, two Pushes creating
	// the same (kind, doc_id) both see the documents row absent (an absent row
	// grants no lock under READ COMMITTED) and both insert, letting a later
	// committer overwrite a newer updated_at and violate newest-wins. Holding
	// this lock forces same-user Pushes to run one-at-a-time, so the per-doc
	// SELECT ... FOR UPDATE + LWW decision below always sees committed state,
	// including for first-inserts.
	var current int64
	err = tx.QueryRow(ctx,
		`SELECT current FROM user_versions WHERE user_id=$1 FOR UPDATE`, userID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		// The row is normally bootstrapped at user creation; recreate it
		// defensively, then re-take the lock so the gate still holds.
		if _, err := tx.Exec(ctx,
			`INSERT INTO user_versions (user_id, current) VALUES ($1, 0) ON CONFLICT (user_id) DO NOTHING`, userID); err != nil {
			return nil, 0, err
		}
		if err := tx.QueryRow(ctx,
			`SELECT current FROM user_versions WHERE user_id=$1 FOR UPDATE`, userID).Scan(&current); err != nil {
			return nil, 0, err
		}
	} else if err != nil {
		return nil, 0, err
	}

	results := make([]PushResult, 0, len(docs))
	for _, d := range docs {
		pushedAt, err := time.Parse(time.RFC3339, d.UpdatedAt)
		if err != nil {
			return nil, 0, fmt.Errorf("bad updated_at for doc %q: %w", d.DocID, err)
		}
		// Postgres timestamptz keeps only microsecond resolution, so truncate
		// the parsed value to microseconds before both the LWW compare and the
		// store. Otherwise a re-pushed identical timestamp string carrying
		// sub-microsecond nanoseconds would compare newer than the rounded
		// stored value and be wrongly re-accepted.
		pushedAt = pushedAt.Truncate(time.Microsecond)

		var storedAt time.Time
		var storedVersion int64
		found := true
		err = tx.QueryRow(ctx,
			`SELECT updated_at, version FROM documents WHERE user_id=$1 AND kind=$2 AND doc_id=$3 FOR UPDATE`,
			userID, d.Kind, d.DocID).Scan(&storedAt, &storedVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			found = false
		} else if err != nil {
			return nil, 0, err
		}

		if found {
			// Match resolutions on both sides of the compare.
			storedAt = storedAt.Truncate(time.Microsecond)
			if !pushedAt.After(storedAt) {
				results = append(results, PushResult{DocID: d.DocID, Kind: d.Kind, Accepted: false, Version: storedVersion})
				continue
			}
		}

		var newVersion int64
		if err := tx.QueryRow(ctx,
			`UPDATE user_versions SET current = current + 1 WHERE user_id = $1 RETURNING current`, userID).
			Scan(&newVersion); err != nil {
			return nil, 0, err
		}

		payload := d.Payload
		if len(payload) == 0 {
			payload = json.RawMessage("null")
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO documents (user_id, kind, doc_id, payload, updated_at, deleted, version)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (user_id, kind, doc_id) DO UPDATE SET
  payload    = EXCLUDED.payload,
  updated_at = EXCLUDED.updated_at,
  deleted    = EXCLUDED.deleted,
  version    = EXCLUDED.version`,
			userID, d.Kind, d.DocID, string(payload), pushedAt, d.Deleted, newVersion); err != nil {
			return nil, 0, err
		}
		results = append(results, PushResult{DocID: d.DocID, Kind: d.Kind, Accepted: true, Version: newVersion})
	}

	var cursor int64
	if err := tx.QueryRow(ctx, `SELECT current FROM user_versions WHERE user_id = $1`, userID).Scan(&cursor); err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, err
	}
	return results, cursor, nil
}

// Pull returns the user's documents with version > since, ordered by version
// ascending. The cursor is the max version returned, or since if none.
func (p *Pg) Pull(ctx context.Context, userID string, since int64) ([]Doc, int64, error) {
	rows, err := p.pool.Query(ctx, `
SELECT kind, doc_id, payload, updated_at, deleted, version
FROM documents WHERE user_id = $1 AND version > $2 ORDER BY version ASC`, userID, since)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	docs := []Doc{}
	cursor := since
	for rows.Next() {
		var d Doc
		var payload []byte
		var updatedAt time.Time
		if err := rows.Scan(&d.Kind, &d.DocID, &payload, &updatedAt, &d.Deleted, &d.Version); err != nil {
			return nil, 0, err
		}
		d.Payload = json.RawMessage(payload)
		d.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		if d.Version > cursor {
			cursor = d.Version
		}
		docs = append(docs, d)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return docs, cursor, nil
}
