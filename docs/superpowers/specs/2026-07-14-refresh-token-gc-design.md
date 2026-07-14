# Refresh-Token GC / Pruning - Design Spec

**Date:** 2026-07-14
**Status:** Approved for planning
**Topic:** A periodic background job in `fleetd` that deletes EXPIRED `refresh_tokens` rows so the table stays bounded, with an index to make the delete cheap and a metric for rows pruned. Revoked-but-not-expired rows are kept (they are the reuse-detection tripwires from slice 4b).

## Goal

`refresh_tokens` rows accrue forever: every rotation revokes the old tip and inserts a new row, and nothing ever deletes expired or superseded rows, so the table grows without bound over time. This slice adds the standard OAuth token-cleanup: a periodic GC that deletes rows past their `expires_at` (dead tokens - a rotate/revoke already rejects an expired token, so they carry no value), leaving the table bounded to roughly one TTL window of live/recently-revoked tokens. It follows the graceful-shutdown lifecycle (the GC loop stops with the server) and reports pruned rows via the metrics registry.

## Context

- `refresh_tokens` (`migrations/0001` + `0002`): `id, user_id, token_hash, expires_at timestamptz, revoked bool, created_at, family_id`. Indexes on `token_hash` and `family_id`; NO index on `expires_at`.
- `pgstore.Pg` wraps a `*pgxpool.Pool`; existing methods (`RotateRefreshToken`, `RevokeRefreshToken`) run SERIALIZABLE. `RotateRefreshToken` rejects a token that is `revoked` (reuse -> family revoke) or `expired` (`time.Now().After(exp)` -> invalid), so an expired row can never rotate.
- `cmd/fleetd/main.go` `run(ctx)` builds config/store/metrics/router and serves via `serve(ctx, srv, ln)`; `ctx` is cancelled by SIGINT/SIGTERM. `metrics.New` + `m.SetPoolSource` are wired; auth counters exist (`IncRefreshReuse`/`IncRefreshRotation`/`IncLogin`).
- `internal/server/metrics` renders Prometheus text; adding a counter means an `atomic.Int64` field + an `Inc`/`Add` method + a `writeCounter` line in `Render`.
- Migrations use golang-migrate with embedded `migrations/*.sql`; `testPg` migrates + truncates and skips without `DATABASE_URL_TEST`.

## Global Constraints

- **No new runtime dependencies.** Go stdlib (`time`, `context`) + existing pgx/metrics.
- **Prune EXPIRED only.** `DELETE FROM refresh_tokens WHERE expires_at < now()`. Revoked-but-not-expired rows are RETAINED - they are the slice-4b reuse-detection tripwires for an active family; deleting them would weaken reuse detection. Expired rows are dead (cannot rotate) and their family's tip is also expired, so deleting them loses nothing.
- **Lifecycle-bound.** The GC loop is a goroutine started in `run` that ticks on an interval and exits when `ctx` is cancelled (server shutdown), so it never outlives the process or blocks the drain.
- **Safe under concurrency.** The delete only removes expired rows; live rotations touch non-expired rows via `SELECT ... FOR UPDATE`, so GC and rotation do not contend on the same rows. Postgres row locks serialize the boundary case (a token expiring exactly as it is rotated). Verified `-race`.
- **Observability.** A `fleet_auth_refresh_pruned_total` counter accumulates rows pruned; each GC run logs the count via slog.
- **Green gates:** `go build/vet/test ./...`, `go test -race ./cmd/fleetd/... ./internal/server/...` (with `DATABASE_URL_TEST`), `wails build` compiles.

## Workstream 1 - Prune query + index (`pgstore/pg.go`, migration `0003`)

- **`0003_refresh_expires_idx.up.sql`:** `CREATE INDEX IF NOT EXISTS refresh_tokens_expires_idx ON refresh_tokens(expires_at);` (makes the GC delete an index scan instead of a full-table scan). `.down.sql`: `DROP INDEX IF EXISTS refresh_tokens_expires_idx;`.
- **`func (p *Pg) PruneRefreshTokens(ctx context.Context) (int64, error)`:** `tag, err := p.pool.Exec(ctx, "DELETE FROM refresh_tokens WHERE expires_at < now()")`; return `tag.RowsAffected(), err`. A single autocommit statement (no explicit tx needed - it only touches expired rows). No SERIALIZABLE required: it never races a live rotation's rows (those are non-expired), and a boundary row is protected by the row lock a concurrent rotation holds.

## Workstream 2 - GC loop + wiring (`cmd/fleetd/main.go`)

- **`func runGC(ctx context.Context, interval time.Duration, prune func(context.Context) (int64, error), onPruned func(int64))`** - a testable loop: run `prune` once immediately, then on each `time.Ticker` tick, until `ctx.Done()`. On a prune result, call `onPruned(n)` (which logs + bumps the metric); on a prune error, log it and continue (a transient DB error must not kill the loop). Returns when `ctx` is cancelled (stop the ticker).
- **`run`** starts it: `go runGC(ctx, gcInterval, store.PruneRefreshTokens, func(n int64) { if n > 0 { slog.Info("pruned expired refresh tokens", "rows", n) }; m.IncRefreshPruned(n) })`, where `gcInterval` is a package const (`1 * time.Hour`). The goroutine shares `run`'s `ctx`, so it exits on shutdown; `store.Close()` (deferred) runs after `serve` returns, and the GC goroutine has already observed `ctx.Done()` by then (a prune in flight at shutdown completes or is cancelled via ctx).

## Workstream 3 - Metric (`internal/server/metrics/metrics.go`)

- Add `pruned atomic.Int64` + `func (m *Metrics) IncRefreshPruned(n int64) { if m != nil && n > 0 { m.pruned.Add(n) } }` (nil-safe; ignores 0/negative). In `Render`, add `writeCounter(&b, "fleet_auth_refresh_pruned_total", "Expired refresh tokens pruned.", m.pruned.Load())` next to the other auth counters.

## Data Flow

`run` -> `go runGC(ctx, 1h, store.PruneRefreshTokens, onPruned)` -> every hour (and once at start) `DELETE ... WHERE expires_at < now()` -> rows-affected -> log + `m.IncRefreshPruned(n)` -> visible in `/metrics` as `fleet_auth_refresh_pruned_total`. On SIGINT/SIGTERM, `ctx` cancels -> the loop returns -> the process drains and exits.

## Error Handling / Edge Cases

- Transient DB error during a prune -> logged, loop continues (next tick retries). Never fatal.
- Shutdown mid-prune -> the prune's `ctx` is the run ctx; a cancelled prune returns a context error, logged, loop exits. No partial-state issue (a DELETE is atomic).
- Empty delete (nothing expired) -> `n == 0`, no log line (only log when `n > 0`), metric unchanged.
- Concurrency: GC deletes only `expires_at < now()`; a live rotation's tip is non-expired -> disjoint row sets. A token that expires exactly at the rotation instant is locked by the rotation's `FOR UPDATE`; the DELETE waits for that lock, then either finds it already rotated-away or deletes it after commit - both correct.
- Reuse detection preserved: only expired rows are removed; revoked-but-valid tripwires remain for their full TTL.

## Testing Strategy

- **pgstore (DB-backed, skips without `DATABASE_URL_TEST`):** `TestPruneRefreshTokens` - insert an expired row, an expired+revoked row, a live row, and a revoked-but-not-expired row; `PruneRefreshTokens` returns 2 and only the two expired rows are gone (live + revoked-valid survive). Assert survivors via a count query keyed by hash.
- **`runGC` loop (`cmd/fleetd`, no DB):** a unit test with a fake `prune` that counts calls and returns a known n; assert it runs once immediately, calls `onPruned` with n, and returns promptly when `ctx` is cancelled (a cancelled ctx + a short interval - verify it stops and does not spin). Use a tiny interval and cancel after the first call.
- **metrics:** `IncRefreshPruned` accumulates and renders `fleet_auth_refresh_pruned_total`; nil-safe + ignores 0.
- **`-race`** (Docker Postgres) green; `wails build` compiles.
- **Adversarial review** (backend, like prior slices): probe the prune policy (does it ever delete a still-usable or reuse-tripwire row?), the loop's ctx/shutdown correctness (leak? spin on a cancelled ctx? blocks drain?), concurrency vs rotation, and the migration.

## Out of Scope (YAGNI)

- Pruning revoked-but-not-expired rows (weakens reuse detection; the table is already bounded by the TTL window).
- A configurable interval via env (fixed 1h now; env-tunable later if needed).
- Batched/`LIMIT`ed deletes for very large tables (fleet's scale is tiny; a single DELETE is fine).
- VACUUM/autovacuum tuning (Postgres handles it).
- Pruning other tables.

## File Structure

- **Create:** `internal/server/pgstore/migrations/0003_refresh_expires_idx.up.sql` + `.down.sql`.
- **Modify:** `internal/server/pgstore/pg.go` (`PruneRefreshTokens`), `internal/server/pgstore/pg_test.go` (`TestPruneRefreshTokens`), `cmd/fleetd/main.go` (`runGC` + start it + `gcInterval`), `cmd/fleetd/main_test.go` or a new `cmd/fleetd/gc_test.go` (`runGC` loop test), `internal/server/metrics/metrics.go` (`pruned` counter + `IncRefreshPruned` + Render line), `internal/server/metrics/metrics_test.go` (pruned counter test).
