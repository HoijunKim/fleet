# fleet

A desktop dashboard for every git repo under your project roots. See at a glance
which are dirty, behind, or stale, then fetch, pull, open an editor/terminal, or run
a command - from a single window.

## Run (Windows)

Download `fleet.exe` from Releases and run it, or build from source:

    wails build
    ./build/bin/fleet.exe

First run writes a config at `%APPDATA%\fleet\config.toml` (roots default to
`~/Projects`). Edit `roots` and restart.

## Build from source

Requires Go 1.22+, Node 18+, and Wails v2 (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`).

    wails build            # desktop app -> build/bin/fleet.exe
    go build ./cmd/fleet   # optional terminal UI (TUI) bonus binary

## Config

    roots = ["C:/Users/you/Projects"]
    scan_depth = 2
    editor = "code"
    terminal = "wt"
    auto_fetch_minutes = 0
    show_non_git = true

## Backend server (fleetd)

`cmd/fleetd` is the fleet cloud backend: GitHub OAuth identity plus a per-user
versioned document store with Last-Write-Wins sync. It is deployed to Fly.io
(region `nrt`) on Neon serverless Postgres. The desktop app never imports it.

### Environment

| Variable | Purpose |
| --- | --- |
| `DATABASE_URL` | Neon Postgres connection string (`postgres://...?sslmode=require`) |
| `JWT_SIGNING_KEY` | HS256 secret for access tokens |
| `GITHUB_OAUTH_CLIENT_ID` | GitHub OAuth app client id |
| `GITHUB_OAUTH_CLIENT_SECRET` | GitHub OAuth app client secret |
| `GITHUB_OAUTH_CALLBACK_URL` | This server's public callback, e.g. `https://fleet-api.fly.dev/auth/github/callback` |
| `FLEET_ALLOWED_REDIRECT` | Allowed loopback redirect prefix (default `http://127.0.0.1`) |
| `PORT` | Listen port (default `8080`) |
| `TRUST_PROXY` | Trust `Fly-Client-IP`/`X-Forwarded-For` for the client IP (default `false`) |

`TRUST_PROXY` is **required on Fly.io**. Fly terminates TLS and proxies every
request to the app, so without `TRUST_PROXY=true` the server sees only Fly's
proxy address as the client IP and the per-IP rate limiter on `/auth`
collapses onto a single bucket for all users. It is set in `fly.toml`'s
`[env]` block, alongside `PORT`, since it is a deploy-topology flag rather
than a secret.

### Neon

1. Create a Neon project and open its dashboard's "Connection Details".
2. Copy the **pooled** connection string (the one using the PgBouncer host,
   not the direct host) and make sure `sslmode=require` is on it, e.g.
   `postgres://user:pass@ep-xxx-pooler.region.aws.neon.tech/fleet?sslmode=require`.
   Use this as `DATABASE_URL`.
3. Migrations (golang-migrate, embedded) run automatically at startup.

### GitHub OAuth app

1. Create an OAuth app at https://github.com/settings/developers (or in an
   org's settings) for `fleet-api`.
2. Set the **Authorization callback URL** to
   `https://fleet-api.fly.dev/auth/github/callback` (substitute your app's
   `<app>.fly.dev` hostname if different from `fleet-api`). This must match
   `GITHUB_OAUTH_CALLBACK_URL` exactly.
3. The server requests scopes `read:user user:email` during the OAuth
   handshake; no other scopes are needed.
4. Copy the generated client id/secret into `GITHUB_OAUTH_CLIENT_ID` /
   `GITHUB_OAUTH_CLIENT_SECRET` (set as Fly secrets, below).

### Fly.io secrets

```bash
fly apps create fleet-api
fly secrets set \
  DATABASE_URL="postgres://user:pass@ep-xxx-pooler.region.aws.neon.tech/fleet?sslmode=require" \
  JWT_SIGNING_KEY="$(openssl rand -base64 48)" \
  GITHUB_OAUTH_CLIENT_ID="..." \
  GITHUB_OAUTH_CLIENT_SECRET="..." \
  GITHUB_OAUTH_CALLBACK_URL="https://fleet-api.fly.dev/auth/github/callback"
fly deploy
```

`PORT` and `TRUST_PROXY` are non-secret and already set in `fly.toml`'s
`[env]` block, so they do not need `fly secrets set`. Set the GitHub OAuth
app's Authorization callback URL to
`https://fleet-api.fly.dev/auth/github/callback` (see above). `fly.toml`
health-checks `GET /healthz` on `internal_port = 8080`, matching the
server's default `PORT`; response body is `ok`.

### Run tests locally

```bash
# Unit/handler tests (no database needed):
go test ./cmd/fleetd/... ./internal/server/...
# Postgres repo tests (start a throwaway Postgres, then):
DATABASE_URL_TEST="postgres://postgres:postgres@localhost:5432/fleet_test?sslmode=disable" \
  go test ./internal/server/pgstore/...
```

Local dev/e2e: `docker compose up -d postgres` then `go test ./...` with
`DATABASE_URL_TEST=...` (runs the pgstore and `internal/server/e2e` suites
against it); `docker compose up -d --build fleetd` runs the server via the
repo's `docker-compose.yml`.
