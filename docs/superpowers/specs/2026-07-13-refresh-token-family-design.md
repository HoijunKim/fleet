# Refresh-Token-Family Reuse Revocation - Design Spec (Backend Prod-Readiness 4b)

**Date:** 2026-07-13
**Status:** Approved for planning
**Topic:** Add OAuth-BCP rotation reuse detection to fleet's refresh tokens: tag every rotation chain from one login with a shared `family_id`, and when an already-rotated (revoked) refresh token is presented again, revoke the ENTIRE family - so a stolen-then-rotated token cannot outlive the theft.

## Goal

Refresh tokens rotate single-use today (`RotateRefreshToken` revokes the old and issues a new one in one transaction; a replayed old token is individually rejected with 401, tested). But there is no reuse-detection cascade: if an attacker steals a refresh token and rotates it, the attacker holds a live token; when the legitimate user later presents their now-superseded token it is rejected, yet the attacker's token stays valid (and vice-versa). This slice closes that gap with the standard mitigation (RFC 6819 / OAuth 2.0 Security BCP): a rotation chain shares a `family_id`, and presenting a revoked token is treated as a reuse signal that revokes the whole family, forcing both parties to re-authenticate.

## Context

- `refresh_tokens` (`migrations/0001_init.up.sql:11-19`): `id uuid PK, user_id uuid, token_hash text, expires_at, revoked bool DEFAULT false, created_at`; index on `token_hash`.
- `pgstore.Pg` methods (`pg.go`): `CreateRefreshToken(userID, hash, exp)` INSERTs a row; `RotateRefreshToken(oldHash, newHash, exp) (userID, error)` is one tx - `SELECT user_id, revoked, expires_at FOR UPDATE` by hash, reject if `revoked || expired` with the unexported `errRefreshInvalid`, else `UPDATE old revoked=true` + `INSERT new`; `RevokeRefreshToken(hash)` sets one row revoked.
- Handlers (`auth/handlers.go`): `Exchange` (login) calls `CreateRefreshToken`; `Refresh` calls `RotateRefreshToken` and maps ANY error to `401 "invalid refresh token"`; `Logout` calls `RevokeRefreshToken` then `204`. `Handlers.cfg.Store` is a `pgstore.Store` (`handlers.go:19`), so the handler package already imports `pgstore`.
- `pgstore.Store` interface (`store.go`) is the seam; fakes implement it: `fakeStore` (`auth/handlers_test.go`, a real in-memory refresh map used to test rotation+reuse) and `syncFakeStore` (`http/sync_test.go`, refresh methods are trivial no-op stubs for sync tests). Migrations use golang-migrate with embedded `migrations/*.sql` (`migrate.go`); DB is Postgres 16 (`gen_random_uuid()` is built in). `testPg` migrates + truncates and skips without `DATABASE_URL_TEST`.

## Global Constraints

- **No new runtime dependencies.** Go stdlib + existing pgx/uuid/golang-migrate.
- **Backward-compatible migration.** `0002` adds `family_id` and backfills existing rows (each gets its own fresh family) before `SET NOT NULL`; a `.down.sql` reverses it. Running against an already-migrated or empty DB is idempotent (`IF NOT EXISTS`/`IF EXISTS`).
- **Interface signatures unchanged.** `CreateRefreshToken`/`RotateRefreshToken`/`RevokeRefreshToken` keep their current signatures; the `family_id` is generated and threaded INSIDE `pgstore` (no new parameters), so the `Store` interface and both fakes only change behavior, not shape.
- **Fail-closed + opaque to the client.** Any rotation failure (invalid, expired, reuse) returns the same `401 "invalid refresh token"` - the response never tells the client which case occurred. Reuse is logged server-side only.
- **Transactional correctness.** Reuse detection and family revocation happen inside the existing `FOR UPDATE` transaction so concurrent rotations of the same token cannot both succeed and cannot race the family revoke.
- **Family isolation.** Revoking a family MUST affect only tokens sharing that exact `family_id` - never another user's tokens, never another login's family for the same user.
- **Green gates:** `go build/vet/test ./...`, `go test -race ./cmd/fleetd/... ./internal/server/...`, `wails build` compiles; the DB-backed pgstore/e2e tests pass with `DATABASE_URL_TEST`.

## Workstream 1 - Schema (migration `0002`)

- **`0002_refresh_family.up.sql`:**
  ```sql
  ALTER TABLE refresh_tokens ADD COLUMN IF NOT EXISTS family_id uuid;
  UPDATE refresh_tokens SET family_id = gen_random_uuid() WHERE family_id IS NULL;
  ALTER TABLE refresh_tokens ALTER COLUMN family_id SET NOT NULL;
  CREATE INDEX IF NOT EXISTS refresh_tokens_family_idx ON refresh_tokens(family_id);
  ```
  (Each pre-existing token becomes its own single-member family - correct: its lineage is unknown, so it can only revoke itself on reuse.)
- **`0002_refresh_family.down.sql`:**
  ```sql
  DROP INDEX IF EXISTS refresh_tokens_family_idx;
  ALTER TABLE refresh_tokens DROP COLUMN IF EXISTS family_id;
  ```

## Workstream 2 - Store: family threading + reuse detection (`pgstore/pg.go`, `store.go`)

- **Export `ErrRefreshReuse`** (`pgstore`): `var ErrRefreshReuse = errors.New("refresh token reuse detected")`. Keep `errRefreshInvalid` unexported (the handler only needs to distinguish reuse for logging; both map to 401).
- **`CreateRefreshToken`** (login): generate a fresh `familyID := uuid.NewString()` and `INSERT ... (id, user_id, token_hash, expires_at, family_id)`.
- **`RotateRefreshToken`** (the core), in the existing tx:
  - `SELECT user_id, revoked, expires_at, family_id FROM refresh_tokens WHERE token_hash=$1 FOR UPDATE`.
  - `pgx.ErrNoRows` (unknown token) -> `errRefreshInvalid` (there is no known family to revoke).
  - `revoked == true` (a token already rotated away or revoked - REUSE): `UPDATE refresh_tokens SET revoked=true WHERE family_id=$fam` (revoke the whole family, idempotent), `Commit`, return `("", ErrRefreshReuse)`.
  - `expired` (`time.Now().After(exp)`): `errRefreshInvalid` (normal expiry is not an attack; do not nuke the family).
  - else (valid): `UPDATE old revoked=true`, `INSERT new (..., family_id=$fam)` carrying the SAME family, `Commit`, return `(userID, nil)`.
  - NOTE: the family revoke on the reuse path must `Commit` (not roll back) so the revocation persists.
- **`RevokeRefreshToken`** (logout) becomes family-scoped: `UPDATE refresh_tokens SET revoked=true WHERE family_id = (SELECT family_id FROM refresh_tokens WHERE token_hash=$1)`. Unknown token -> subquery empty -> zero rows (idempotent no-op). This ends the whole session chain on logout, not just the current token. Its doc comment states the family semantics.

## Workstream 3 - Handler: log reuse (`auth/handlers.go`)

- **`Refresh`:** on `RotateRefreshToken` error, if `errors.Is(err, pgstore.ErrRefreshReuse)` log a server-side security event (`slog.Warn("refresh token reuse detected; family revoked", "user_id", userID_if_available)`) - but the response is unchanged: `401 "invalid refresh token"` for every error case (invalid/expired/reuse are indistinguishable to the client). `RotateRefreshToken` returns `""` userID on reuse, so log without a user id (the token hash is not logged - it is a secret). Add `errors`/`log/slog` imports as needed.
- `Exchange` and `Logout` bodies are unchanged (they call the same-signature store methods; the family behavior is internal).

## Data Flow

Login -> `Exchange` -> `CreateRefreshToken` (new family). Refresh -> `Refresh` -> `RotateRefreshToken`: valid -> rotate within family; revoked-token-presented -> revoke family + `ErrRefreshReuse` -> handler logs + 401. Logout -> `RevokeRefreshToken` -> revoke family -> 204. A stolen token that was rotated by the attacker: when the victim (or the attacker) next presents a now-revoked token, the whole family dies and both must re-auth via GitHub.

## Error Handling / Edge Cases

- Unknown/garbage token -> `errRefreshInvalid` -> 401 (no family to revoke).
- Post-logout replay -> token is revoked -> reuse path -> family already revoked (idempotent) -> 401. Harmless.
- Concurrent rotation of the SAME valid token: `FOR UPDATE` serializes; the first commits (rotates), the second now sees `revoked=true` -> reuse -> family revoked. This is correct BCP behavior (a duplicated in-flight token is treated as compromise). Documented so it is not mistaken for a bug.
- Expired token -> `errRefreshInvalid`, family untouched (expiry is normal).
- Family isolation: every revoke/rotate is scoped by `family_id` (or `token_hash` -> its family), never by `user_id`, so other logins/families/users are unaffected.
- Migration on a live DB with existing tokens: each gets a unique family before `NOT NULL`; no token is orphaned.

## Testing Strategy

- **pgstore (DB-backed, skips without `DATABASE_URL_TEST`):** create a login family, rotate it a few times (assert new tokens share the family, old ones revoked); present a revoked (already-rotated) token and assert (a) `ErrRefreshReuse`, (b) EVERY token in that family is now revoked - including the attacker's latest live one; assert an unrelated family/user is untouched; assert expired -> `errRefreshInvalid` with the family still live; assert logout revokes the whole family. Add a migration round-trip note (up applied by `testPg`).
- **auth handler (`handlers_test.go` `fakeStore`):** extend the in-memory `refresh` map with a `familyID` and replicate the reuse-detection + family-revoke logic so the handler's 401-on-reuse and the slog path are exercised; assert the reuse response is an opaque 401 (not distinguishable from invalid). Keep `syncFakeStore` stubs no-op (it does not exercise auth).
- **Adversarial review (like the Grep guard):** a dedicated opus review probing reuse-detection bypasses, transaction races (two concurrent rotations), family-isolation leaks (does a revoke ever touch another family/user?), the migration backfill correctness, and the fake faithfully mirroring the real store.
- Existing suites + `-race` green; `wails build` compiles.

## Out of Scope (YAGNI)

- Access-token (JWT) revocation / denylist - stateless JWTs still expire on their own (15 min); a separate concern.
- Per-device session management UI / listing active families.
- Absolute family lifetime caps or sliding-window limits.
- Refresh-token GC / pruning of revoked rows (a later reliability slice).
- Email/notification on reuse detection (log-only for now).

## File Structure

- **Create:** `internal/server/pgstore/migrations/0002_refresh_family.up.sql`, `internal/server/pgstore/migrations/0002_refresh_family.down.sql`.
- **Modify:** `internal/server/pgstore/pg.go` (`ErrRefreshReuse`, `CreateRefreshToken` family, `RotateRefreshToken` reuse+family, `RevokeRefreshToken` family-scoped), `internal/server/pgstore/pg_test.go` (family/reuse/isolation/logout DB tests), `internal/server/auth/handlers.go` (`Refresh` logs reuse), `internal/server/auth/handlers_test.go` (`fakeStore` family + reuse logic, handler reuse test).
