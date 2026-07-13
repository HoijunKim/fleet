# Server Lifecycle and Resilience - Design Spec (Backend Prod-Readiness 4a)

**Date:** 2026-07-13
**Status:** Approved for planning
**Topic:** Make the `fleetd` backend survive deploys and handler panics: graceful shutdown with signal handling and `http.Server` timeouts, panic-recovery middleware, request-ID correlation, a DB-backed readiness endpoint, and `-race` in CI. Scoped to server robustness; refresh-token-family revocation is a separate slice (4b).

## Goal

`fleetd`'s `main()` serves via a bare `http.ListenAndServe(addr, router)` (`cmd/fleetd/main.go:55`) with no signal handling, no timeouts, and no shutdown - a Fly deploy/rollout (SIGINT, then SIGKILL after `kill_timeout`) kills in-flight requests, and a single handler panic (no recovery middleware anywhere) tears down the connection. This slice adds the standard production lifecycle: catch SIGINT/SIGTERM, drain with `http.Server.Shutdown`, bound every phase with a timeout, recover panics into a logged 500, tag each request with a correlation ID, expose a readiness probe that checks the DB, and run the suite under `-race`.

## Context

- `main()` (`cmd/fleetd/main.go:16-59`) reads env, runs `pgstore.Migrate`, opens the pool (`pgstore.New`, `defer store.Close()`), builds `auth.New` + `httpapi.NewRouter`, logs, then `http.ListenAndServe(addr, router)`. `mustEnv`/`envOr`/`envBool` helpers stay.
- `NewRouter(opts Options) http.Handler` (`internal/server/http/router.go:25-57`) is chi; the ONLY global middleware is `r.Use(LogRequests)`; `GET /healthz` returns 200 "ok"; auth routes are per-IP rate-limited, `/sync` is `AuthMiddleware`+per-user-limited.
- `LogRequests` (`middleware.go:29-41`) wraps the writer in `statusWriter` (captures status) and logs method/path/status/dur_ms via slog. `ctxKey`/`userIDKey`/`WithUserID`/`UserID` already define the context-key pattern.
- `pgstore.Store` (`store.go:48-55`) is the persistence seam; `Pg` wraps a `pgxpool.Pool`. Fakes implement `Store` in tests (`auth/handlers_test.go` `fakeStore`, `http/sync_test.go` `syncFakeStore`; the e2e uses the real pgstore).
- `fly.toml` sets `[http_service]` with a `/healthz` check; there is NO `kill_timeout` (Fly default is 5s; Fly's default stop signal is SIGINT).
- `.github/workflows/server.yml` runs `go build`, `go vet`, gofmt, and `go test ./cmd/fleetd/... ./internal/server/...` on ubuntu with a Postgres 16 service - no `-race`.
- slog JSON is the default logger (`main.go:17`).

## Global Constraints

- **No new runtime dependencies.** Go stdlib (`net`, `os/signal`, `syscall`, `context`, `errors`, `crypto/rand`, `runtime/debug`) + existing `chi` + slog. No new module.
- **Behavior-preserving for existing routes and middleware:** auth/sync routing, rate limiting, `AuthMiddleware`, and the `/healthz` body ("ok", 200) are unchanged. New middleware is additive and ordered so existing behavior (status logging) still holds.
- **Fail-safe lifecycle:** a bind failure or serve error returns from `run` and exits non-zero (as today); shutdown is bounded by a drain timeout so a stuck connection cannot hang the process forever.
- **Cross-platform build:** the code compiles on Windows (the desktop dev box) and Linux (deploy). `syscall.SIGTERM` is a defined constant on both; `os.Interrupt` covers SIGINT. No build tags.
- **Correlation-ID safety:** an inbound `X-Request-Id` is honored only if it passes a strict charset+length check (guards against log injection); otherwise a fresh crypto-random ID is generated. The ID is echoed in the `X-Request-Id` response header and included in the request log and any panic log.
- **Green gates:** `go build ./...`, `go vet ./...`, `go test ./...`, AND `go test -race ./cmd/fleetd/... ./internal/server/...` all pass; `wails build` still compiles (the desktop app is untouched but must build).

## Workstream 1 - Graceful shutdown, signals, timeouts (`cmd/fleetd/main.go`)

- **Extract `run(ctx context.Context) error`** holding today's body (env, migrate, pool, auth, router). `main()` becomes: `ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM); defer stop(); if err := run(ctx); err != nil { slog.Error("server exited", "err", err); os.Exit(1) }`. `mustEnv`/`envOr`/`envBool` unchanged. `run` still `defer store.Close()` and returns errors instead of `os.Exit` (except `mustEnv`, which may keep exiting).
- **Bind explicitly + build `http.Server` with timeouts.** In `run`: `ln, err := net.Listen("tcp", addr)` (return err on failure); `srv := &http.Server{Handler: router, ReadHeaderTimeout: 5s, ReadTimeout: 15s, WriteTimeout: 30s, IdleTimeout: 120s}`; then `return serve(ctx, srv, ln)`.
- **New `serve(ctx context.Context, srv *http.Server, ln net.Listener) error`** (the testable lifecycle unit): run `srv.Serve(ln)` in a goroutine feeding an `errc chan error` (buffered 1); `select { case err := <-errc: if errors.Is(err, http.ErrServerClosed) { return nil }; return err; case <-ctx.Done(): sctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout); defer cancel(); slog.Info("shutting down"); return srv.Shutdown(sctx) }`. `shutdownTimeout` = 10s (a package const), comfortably under the raised `kill_timeout`.
- **`fly.toml`:** add top-level `kill_timeout = "15s"` so Fly's SIGINT-to-SIGKILL grace exceeds the 10s drain (otherwise the drain is cut short and graceful shutdown is moot). A comment explains the relationship.

## Workstream 2 - Resilience + correlation middleware (`internal/server/http/middleware.go`, `router.go`)

- **`RequestID(next) http.Handler`:** derive the correlation id - honor an inbound `X-Request-Id` only if `validRequestID(id)` (1..64 chars, each `A-Za-z0-9._-`), else `newRequestID()` (16 crypto-random bytes hex-encoded). Set it on the response header `X-Request-Id` and store it on the context under a new `requestIDKey ctxKey`. Add `RequestIDOf(ctx) string` (empty string if absent).
- **`Recoverer(next) http.Handler`:** `defer`/`recover`; on a non-nil recover that is not `http.ErrAbortHandler` (re-panic that one), `slog.Error("panic recovered", "err", fmt.Sprint(rec), "request_id", RequestIDOf(r.Context()), "method", r.Method, "path", r.URL.Path, "stack", string(debug.Stack()))`, then write a 500 only if nothing was written yet. To detect "already written", extend `statusWriter` with a `wrote bool` (set in `WriteHeader` and `Write`); `Recoverer` type-asserts `w.(*statusWriter)` and writes `http.StatusInternalServerError` only when the assertion holds and `!wrote` (or the assertion fails). This avoids a superfluous-WriteHeader after a handler that partially responded then panicked.
- **`LogRequests`:** add `"request_id", RequestIDOf(r.Context())` to the slog line. Unchanged otherwise.
- **Router order (`router.go`):** `r.Use(RequestID); r.Use(LogRequests); r.Use(Recoverer)` (RequestID outermost so the id is on the context before logging; LogRequests wraps the `statusWriter` that Recoverer reuses; Recoverer innermost so it catches handler panics and the resulting 500 is logged by the outer LogRequests). Existing per-group middleware (rate limiters, `AuthMiddleware`) is unchanged.

## Workstream 3 - Readiness endpoint + DB ping seam (`store.go`, `pg.go`, `router.go`, fakes)

- **Add `Ping(ctx context.Context) error` to `pgstore.Store`** (`store.go`). Impl: `func (p *Pg) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }`. Update every fake `Store` in tests to add a `Ping` (return nil by default; a field lets a test force an error).
- **`GET /readyz` (`router.go`)**, public (no auth, no rate limit, alongside `/healthz`): a handler closured over `opts.Store` that pings with a 2s timeout - `ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second); defer cancel(); if err := opts.Store.Ping(ctx); err != nil { http.Error(w, "not ready", http.StatusServiceUnavailable); return }`, else 200 "ready". `/healthz` stays a pure liveness check (no DB), so a DB blip fails readiness (pull from rotation) without failing liveness (no restart). Guard for a nil `opts.Store` (return 200 or skip) to keep `NewRouter` usable in tests that pass no store.

## Workstream 4 - CI `-race` (`.github/workflows/server.yml`)

- **Add a race step** (or extend the test step): `go test -race ./cmd/fleetd/... ./internal/server/...`. ubuntu-latest has gcc so cgo/`-race` works. Keep the plain `go test` too (or replace with the race run; the race run covers correctness + races). If `-race` surfaces a data race (candidates: `RateLimiter` map access is mutex-guarded already; the `ttlStore` sweep; `serve`'s goroutine/errc - which is race-clean by construction), fix it in the relevant file with the fix noted in the plan.

## Data Flow

Request -> `RequestID` (id on ctx + response header) -> `LogRequests` (statusWriter, logs with request_id) -> `Recoverer` (catches panic -> 500 + slog with request_id/stack) -> route middleware (rate limit / auth) -> handler. Lifecycle: SIGINT/SIGTERM -> `signal.NotifyContext` cancels `ctx` -> `serve` calls `srv.Shutdown(10s)` -> in-flight requests drain, listener closes, `run` returns nil, `store.Close()` runs, process exits 0. `/readyz` -> `Store.Ping(2s)` -> 200/503.

## Error Handling / Edge Cases

- Bind failure -> `net.Listen` err -> `run` returns it -> `main` logs + exit 1.
- Serve error other than `ErrServerClosed` -> returned from `serve` -> exit 1.
- Shutdown exceeds 10s (a stuck handler) -> `Shutdown` returns `context.DeadlineExceeded` -> `serve` returns it -> exit non-zero (the drain was attempted; Fly SIGKILLs at `kill_timeout` regardless).
- Panic after a partial write -> `Recoverer` logs and does NOT double-write (guarded by `statusWriter.wrote`).
- Inbound `X-Request-Id` that is too long or has odd bytes -> ignored, fresh id generated (no log injection).
- `/readyz` with DB down -> 503; `/healthz` still 200 (liveness vs readiness split).
- nil `opts.Store` in a router test -> `/readyz` degrades gracefully (no nil deref).

## Testing Strategy

- **`serve` lifecycle (`cmd/fleetd/main_test.go` or a new `serve_test.go`):** bind `ln` on `127.0.0.1:0`; `srv` with a trivial handler; `ctx, cancel := context.WithCancel(...)`; run `serve` in a goroutine feeding a result chan; GET `http://ln.Addr()/` -> 200; `cancel()`; assert `serve` returns nil within a short deadline and a subsequent GET fails (listener closed). A second test: a serve error (e.g. an already-closed listener) returns non-nil.
- **`Recoverer` (`middleware_test.go`):** a handler that panics -> response 500 and the request completes (no crash); a handler that writes 200 then panics -> still 200 (no double-write), panic logged. Assert via `httptest`.
- **`RequestID` (`middleware_test.go`):** response has a non-empty `X-Request-Id`; a valid inbound id is echoed; an over-long/invalid inbound id is replaced; `RequestIDOf` returns the id inside the handler.
- **`/readyz` (`router_test.go`):** fake store with `Ping` nil -> 200 "ready"; fake with `Ping` returning an error -> 503. `/healthz` unchanged (200 "ok").
- **`Store.Ping`:** pgstore test (DB-backed, skips without `DATABASE_URL_TEST`) asserts `Ping` succeeds against the live pool.
- **Race:** `go test -race ./cmd/fleetd/... ./internal/server/...` green locally and in CI.
- Existing suites stay green; `wails build` compiles.

## Out of Scope (YAGNI)

- Metrics (Prometheus/OTel) and tracing - a later observability slice (4c).
- Refresh-token-family reuse revocation - slice 4b (its own security review).
- Configurable timeouts via env - constants now; env-tunable later if needed.
- DB pool sizing / statement timeouts / expired-token GC - later reliability work.
- Distributed request tracing / propagating the id to the DB or downstream.
- A linter (golangci-lint) in CI - separate.

## File Structure

- **Modify:** `cmd/fleetd/main.go` (`run` + `serve` + signals + timeouts), `cmd/fleetd/main_test.go` (or new `cmd/fleetd/serve_test.go`) (serve lifecycle test), `internal/server/http/middleware.go` (`RequestID`, `Recoverer`, `statusWriter.wrote`, `LogRequests` request_id, `validRequestID`/`newRequestID`/`RequestIDOf`), `internal/server/http/middleware_test.go` (Recoverer + RequestID tests), `internal/server/http/router.go` (middleware order + `/readyz`), `internal/server/http/router_test.go` (readyz + healthz), `internal/server/pgstore/store.go` (`Ping` on `Store`), `internal/server/pgstore/pg.go` (`Pg.Ping`), `internal/server/pgstore/pg_test.go` (Ping DB test), `internal/server/auth/handlers_test.go` + `internal/server/http/sync_test.go` (add `Ping` to fakes), `fly.toml` (`kill_timeout`), `.github/workflows/server.yml` (`-race`).
- **Create:** optionally `cmd/fleetd/serve_test.go` if not folding into `main_test.go`.
