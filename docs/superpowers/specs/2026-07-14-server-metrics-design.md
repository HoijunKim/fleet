# Server Metrics / Observability - Design Spec (Backend Prod-Readiness 4c)

**Date:** 2026-07-14
**Status:** Approved for planning
**Topic:** Give `fleetd` a Prometheus-scrapeable `/metrics` endpoint - HTTP request rate/latency/errors/in-flight, DB pool health, and auth security events (refresh reuse, rotations, logins) - implemented with a small stdlib-only metrics registry (no new dependency), cardinality-bounded, and gated behind an opt-in bearer token.

## Goal

The server logs structured request lines but exposes no aggregate metrics: you cannot see request rate, latency distribution, error ratio, in-flight load, pool saturation, or the count of refresh-token reuse events (a security signal) without grepping logs. This slice adds a hand-rolled metrics registry and a `/metrics` endpoint in Prometheus text exposition format, so a scraper (or a manual `curl`) gets the standard signals. It stays dependency-free (a focused registry over `sync`/`atomic`), bounds label cardinality tightly (so a hostile client cannot explode the series set), and protects the endpoint with an opt-in `METRICS_TOKEN`.

## Context

- `internal/server/http`: `NewRouter(opts)` (chi) with the global stack `RequestID -> LogRequests -> Recoverer`; `statusWriter` captures response status; `/healthz` + `/readyz` (rate-limited) exist. chi's `chi.RouteContext(r.Context()).RoutePattern()` yields the matched route pattern (bounded), `""` when unmatched.
- `internal/server/pgstore`: `Pg` wraps a `*pgxpool.Pool`; `pool.Stat()` returns `*pgxpool.Stat` with `TotalConns()`/`IdleConns()/AcquiredConns()`.
- `internal/server/auth`: `Refresh` handler calls `RotateRefreshToken` (returns `pgstore.ErrRefreshReuse` on reuse), `Exchange` mints tokens on login, `Logout` revokes. `Handlers.cfg` (a `Config`) is the injection point.
- `cmd/fleetd/main.go`: `run(ctx)` builds config/store/auth/router and serves; `mustEnv`/`envOr` read config. slog JSON is the default logger.
- The repo constraint held so far: **no new runtime dependencies** (Go stdlib + chi + pgx). Metrics must honor it.

## Global Constraints

- **No new runtime dependencies.** The registry is stdlib (`sync`, `sync/atomic`, `strconv`, `io`, `sort`, `runtime`) + chi (already a dep) only in the httpapi middleware, not in the metrics package.
- **Bounded cardinality (security/ops-critical).** Every label value is drawn from a bounded set: `route` = the chi route PATTERN, or `"other"` when unmatched (never a raw path); `method` = a known HTTP method (GET/POST/PUT/PATCH/DELETE/HEAD/OPTIONS) or `"other"`; `status` = a class (`2xx`/`3xx`/`4xx`/`5xx`/`other`). A hostile client sending random paths/methods cannot grow the series set.
- **The metrics package is a pure leaf.** `internal/server/metrics` imports only stdlib - no chi, no pgx, no httpapi (so nothing depends on it in a cycle). httpapi owns the HTTP middleware (reusing its `statusWriter` + chi), main bridges `pgxpool.Stat` -> `metrics.PoolStats`, auth calls the nil-safe counter methods.
- **Nil-safe by construction.** Every `*Metrics` mutator (`ObserveHTTP`, `IncInFlight`/`DecInFlight`, `IncRefreshReuse`/`IncRefreshRotation`/`IncLogin`, `SetPoolSource`) is a no-op on a nil receiver, so handlers/middleware call unconditionally and tests can pass `nil`.
- **Opt-in + protected `/metrics`.** The endpoint is registered ONLY when `METRICS_TOKEN` is set; it requires `Authorization: Bearer <token>` (constant-time compare) and 401s otherwise. Unset token = no endpoint (not public by default).
- **Concurrency-correct.** Metric writes happen on every request concurrently; the registry is race-free (verified under `-race`).
- **Green gates:** `go build/vet/test ./...`, `go test -race ./cmd/fleetd/... ./internal/server/...`, `wails build` compiles.

## Workstream 1 - The metrics registry (`internal/server/metrics/metrics.go` + test)

- **`type Metrics struct`** holding: a mutex-guarded `map[httpKey]*httpSeries` (per route/method/status counter + per route/method duration histogram - store the histogram under the route/method, counter under route/method/status), an in-flight `atomic.Int64`, three auth `atomic.Int64` counters, a `poolSource func() PoolStats` (guarded), and static build labels (`version`, `goVersion`).
- **`type PoolStats struct { Total, Idle, Acquired int }`** - the metrics package's own pool shape (main adapts pgxpool to it).
- **`func New(version, goVersion string) *Metrics`**.
- **`func (m *Metrics) ObserveHTTP(route, method, status string, dur time.Duration)`** (nil-safe): normalize via `normRoute`/`normMethod`/`statusClass`, then under the mutex bump the request counter and the duration histogram (cumulative buckets + sum + count) for the series. Histogram buckets (seconds): `0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10` (+`+Inf`).
- **`IncInFlight()`/`DecInFlight()`** (nil-safe, atomic).
- **`IncRefreshReuse()`/`IncRefreshRotation()`/`IncLogin()`** (nil-safe, atomic).
- **`SetPoolSource(fn func() PoolStats)`** (nil-safe): stored, called at render time so pool gauges are sampled fresh on scrape.
- **`func (m *Metrics) Render(w io.Writer)`**: emit Prometheus text - `# HELP`/`# TYPE` then series, deterministically ordered (sort keys). Metrics: `fleet_http_requests_total{route,method,status}` (counter), `fleet_http_request_duration_seconds_bucket{route,method,le}` + `_sum` + `_count` (histogram), `fleet_http_in_flight` (gauge), `fleet_db_pool_total_connections`/`_idle_connections`/`_acquired_connections` (gauges, from the sampled `PoolStats`; omitted if no source), `fleet_auth_refresh_reuse_total`/`fleet_auth_refresh_rotations_total`/`fleet_auth_logins_total` (counters), `fleet_build_info{version,go_version}` (gauge=1). Label values escaped (`\`, `"`, `\n`) via an `escapeLabel` helper.
- **`func (m *Metrics) Handler(token string) http.Handler`**: on GET, constant-time-compare the `Bearer` token (`subtle.ConstantTimeCompare`); mismatch/absent -> 401; else set `Content-Type: text/plain; version=0.0.4` and `Render`. (Registration is gated by the caller; the handler enforces the token defensively.)
- Helpers: `normRoute(pattern)` (`""` -> `"other"`), `normMethod(m)` (known list or `"other"`), `statusClass(code)` (`2xx`.. / `"other"` for <100 or >=600).

## Workstream 2 - HTTP metrics middleware + wiring (`httpapi/middleware.go`, `router.go`)

- **`func MetricsMiddleware(m *metrics.Metrics) func(http.Handler) http.Handler`** (in httpapi): wraps the handler - `m.IncInFlight(); defer m.DecInFlight()`; time it; use a `statusWriter` to capture status; after `next.ServeHTTP`, read `chi.RouteContext(r.Context()).RoutePattern()` and call `m.ObserveHTTP(route, r.Method, "", dur)` passing the numeric status (the middleware converts status->class, or passes the code and ObserveHTTP classifies - keep classification in metrics; pass the raw code as an int via a small `ObserveHTTP(route, method string, code int, dur)` signature). (Nil `m` -> the nil-safe methods make it a cheap no-op; still safe to always install.)
- **`Options`** gains `Metrics *metrics.Metrics` and `MetricsToken string`. `NewRouter`: add `MetricsMiddleware(opts.Metrics)` to the global stack (after `RequestID`, around logging); if `opts.MetricsToken != "" && opts.Metrics != nil`, register `r.Method("GET", "/metrics", opts.Metrics.Handler(opts.MetricsToken))`. `/metrics` is NOT rate-limited by the user limiter (it is token-gated instead).
- Middleware order: `RequestID -> MetricsMiddleware -> LogRequests -> Recoverer` (metrics wraps the full handler so it times auth+rate-limit+handler and sees the final status; its own `statusWriter` and LogRequests' are independent passthroughs).

## Workstream 3 - Sources: pool stats + auth counters (`pgstore/pg.go`, `auth/handlers.go`, `cmd/fleetd/main.go`)

- **`pgstore`:** add `func (p *Pg) Stat() *pgxpool.Stat { return p.pool.Stat() }` (concrete `*Pg`; no Store-interface change - main holds the concrete type).
- **`auth`:** `Config` gains `Metrics *metrics.Metrics`. In `Refresh`: on `ErrRefreshReuse` call `h.cfg.Metrics.IncRefreshReuse()`; on success `IncRefreshRotation()`. In `Exchange` (successful login): `IncLogin()`. All nil-safe, so existing auth tests (nil Metrics) are unaffected.
- **`cmd/fleetd/main.go`:** `m := metrics.New(version, runtime.Version())` (version from a build var / `envOr("FLEET_VERSION","dev")`); `m.SetPoolSource(func() metrics.PoolStats { s := store.Stat(); return metrics.PoolStats{Total: int(s.TotalConns()), Idle: int(s.IdleConns()), Acquired: int(s.AcquiredConns())} })`; pass `Metrics: m` to `auth.New` + `NewRouter`, and `MetricsToken: envOr("METRICS_TOKEN","")`. Log at startup whether `/metrics` is enabled (without logging the token).

## Data Flow

Request -> `MetricsMiddleware` (in-flight++, timer, statusWriter) -> ... handler ... -> on return: route pattern + method + status -> `m.ObserveHTTP` (counter + histogram, cardinality-bounded). Auth `Refresh`/`Exchange` -> nil-safe counter bumps. Scrape `GET /metrics` (bearer-checked) -> `Render` samples `poolSource()` + reads all counters/histograms -> Prometheus text.

## Error Handling / Edge Cases

- Unmatched route (404) -> `route="other"`; unknown method -> `method="other"`; odd status (<100/>=600) -> `status="other"`: cardinality stays bounded regardless of client input.
- `METRICS_TOKEN` unset -> `/metrics` not registered (404); set -> wrong/absent bearer -> 401 (constant-time compare, no timing oracle).
- No pool source (e.g. a test router) -> pool gauges omitted from output, everything else renders.
- nil `*Metrics` anywhere -> all mutators no-op; the middleware and auth calls stay safe.
- Concurrent requests -> mutex guards the label maps; atomics for scalar counters/in-flight; `Render` takes the same mutex for a consistent snapshot. Verified `-race`.
- Histogram: a duration above the top finite bucket still increments `+Inf`, `_sum`, `_count` (standard Prometheus semantics).

## Testing Strategy

- **`metrics_test.go`:** `ObserveHTTP` accumulates the request counter and the correct cumulative histogram buckets/sum/count; `statusClass`/`normMethod`/`normRoute` mappings (incl. the "other" fallbacks); `Render` output is valid Prometheus text (has `# TYPE`, expected series lines, escaped labels, `+Inf` bucket, deterministic order); nil-`*Metrics` mutators no-op; a `Handler` token test (200 with correct bearer, 401 without/with wrong); build_info + pool gauges (via a fake `SetPoolSource`) render; a concurrent-`ObserveHTTP` test that is `-race` clean.
- **httpapi:** a `MetricsMiddleware` test - drive a request through a tiny chi router with the middleware + a real `Metrics`, then assert the counter/in-flight moved and the rendered output names the route pattern (not the raw path); a 404 records `route="other"`.
- **auth:** `Refresh` reuse increments `refresh_reuse_total` and success increments `rotations_total` (real `Metrics`, assert via `Render`); nil Metrics path still green.
- **`-race`** (Docker Postgres) green; `wails build` compiles.
- **Adversarial review** (like prior backend slices): probe cardinality bombs (hostile method/path -> series growth?), the token compare (timing/oracle), race-freedom of the registry + `Render` snapshot, Prometheus-format correctness (a real parser would accept it), and nil-safety.

## Out of Scope (YAGNI)

- Distributed tracing / OpenTelemetry spans (heavy, needs a collector, adds deps).
- A metrics push gateway or a bundled Prometheus/Grafana.
- Per-endpoint SLO/alerting rules.
- Exemplars, native histograms, or OpenMetrics extensions (stick to Prometheus text 0.0.4).
- Process/Go runtime metrics (GC, goroutines, memory) - could add later; not now.

## File Structure

- **Create:** `internal/server/metrics/metrics.go`, `internal/server/metrics/metrics_test.go`.
- **Modify:** `internal/server/http/middleware.go` (`MetricsMiddleware`), `internal/server/http/router.go` (`Options.Metrics`/`MetricsToken`, wire middleware + `/metrics`), `internal/server/http/router_test.go` or `middleware_test.go` (middleware test), `internal/server/pgstore/pg.go` (`Stat()`), `internal/server/auth/handlers.go` (`Config.Metrics` + counter bumps), `internal/server/auth/handlers_test.go` (reuse-counter test), `cmd/fleetd/main.go` (create Metrics, pool source, wire, `METRICS_TOKEN`).
