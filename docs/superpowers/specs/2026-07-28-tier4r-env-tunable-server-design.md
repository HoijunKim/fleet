# Tier 4r - Env-tunable server timeouts, GC interval, and DB pool sizing

**Goal:** let a `fleetd` operator tune the HTTP timeouts, graceful-shutdown
budget, refresh-token GC cadence, and Postgres pool size via environment
variables, instead of the compiled-in constants.

Backlog Ops item, deferred explicitly by three slices as "constants now,
env-tunable later if needed" (`server-lifecycle-resilience-design.md:77-82`,
`refresh-token-gc-design.md:62-66`). Now needed: Fly/Neon sizing differs from the
dev defaults, and a redeploy to change a timeout is heavy-handed.

## Principle: same defaults, opt-in overrides

Every current constant becomes the default. With no new env set, behaviour is
byte-for-byte what it is today. An override is read once at startup; an unset or
unparseable value logs a warning (for the unparseable case) and keeps the
default - a typo must never silently disable a timeout or shrink the pool.

## 1. Env parsing helpers (`cmd/fleetd`)

Alongside the existing `envOr`/`envBool`:

```go
// envDuration reads key as a Go duration ("15s", "2m", "1h"). Unset -> def.
// Unparseable or <= 0 -> def, with a warning: a bad value must not disable a
// timeout or make an interval nonpositive.
func envDuration(key string, def time.Duration) time.Duration
// envInt reads key as an int. Unset -> def. Unparseable or <= 0 -> def + warn.
func envInt(key string, def int) int
```

Both are pure functions of the environment; unit-tested directly.

## 2. Server config (`cmd/fleetd`)

A `serverConfig` gathers the six tunables; `loadServerConfig()` builds it from
env with today's constants as defaults:

| Field | Env | Default (today's constant) |
| --- | --- | --- |
| `ReadHeaderTimeout` | `FLEET_READ_HEADER_TIMEOUT` | `5s` |
| `ReadTimeout` | `FLEET_READ_TIMEOUT` | `15s` |
| `WriteTimeout` | `FLEET_WRITE_TIMEOUT` | `30s` |
| `IdleTimeout` | `FLEET_IDLE_TIMEOUT` | `120s` |
| `ShutdownTimeout` | `FLEET_SHUTDOWN_TIMEOUT` | `10s` |
| `GCInterval` | `FLEET_GC_INTERVAL` | `1h` |

`run` uses the config for the `http.Server` timeout fields, the `runGC`
interval, and the `serve` shutdown budget. `shutdownTimeout` stops being a
package const (it becomes `cfg.ShutdownTimeout`, still kept under fly.toml's
`kill_timeout` by the operator); `gcInterval` likewise. `serve` gains a
`shutdownTimeout time.Duration` parameter so it no longer reads a global.

## 3. DB pool sizing (`internal/server/pgstore`)

```go
type PoolConfig struct {
    MaxConns        int32
    MinConns        int32
    MaxConnLifetime time.Duration
    MaxConnIdleTime time.Duration
}
// NewWithPool opens a pool, applying any non-zero PoolConfig field over pgx's
// defaults (a zero field leaves pgx's default untouched).
func NewWithPool(ctx context.Context, databaseURL string, pc PoolConfig) (*Pg, error)
```

`applyPoolConfig(cfg *pgxpool.Config, pc PoolConfig)` is a pure helper: for each
non-zero field it sets the matching `pgxpool.Config`/`ConnConfig` field, so it is
unit-testable without a live DB. `New` becomes
`NewWithPool(ctx, url, PoolConfig{})` - a zero config applies nothing, so it is
identical to today's `pgxpool.New`. `cmd/fleetd` reads `FLEET_DB_MAX_CONNS`,
`FLEET_DB_MIN_CONNS` (ints), `FLEET_DB_MAX_CONN_LIFETIME`,
`FLEET_DB_MAX_CONN_IDLE_TIME` (durations) and passes them to `NewWithPool`.

## Testing

- **`cmd/fleetd` (`config_test.go`):** `envDuration`/`envInt` return the default
  when unset, the parsed value when valid, and the default when garbage (via
  `t.Setenv`); `loadServerConfig` reflects an override and otherwise the
  defaults.
- **`pgstore` (`pg_test.go`, no DB):** `applyPoolConfig` sets exactly the
  non-zero fields and leaves zero fields at pgx's defaults; `NewWithPool` with a
  malformed URL returns an error. The existing e2e test keeps calling `New`.
- Existing suites stay green; `go test -race ./cmd/fleetd/... ./internal/server/...`;
  `wails build` unaffected (desktop path untouched).

## Out of scope

Per-endpoint or per-route timeouts, statement/lock timeouts on the DB,
hot-reload of config without restart, and validating overrides against fly.toml
`kill_timeout` (the operator's responsibility, as the shutdown const already
notes). Config stays flat env vars - no file or flags.
