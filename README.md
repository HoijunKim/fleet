# fleet

**Every git repo you own, at a glance.**

fleet is a desktop dashboard for every git repository under your project roots.
See which are dirty, behind, or stale - then fetch, commit, resolve conflicts,
cherry-pick, search across all of them, and track tasks and deadlines, from one
window. No terminal required.

[![release](https://img.shields.io/github/v/release/hoijun-kim/fleet?color=5b8cff)](https://github.com/hoijun-kim/fleet/releases/latest)
[![platforms](https://img.shields.io/badge/platforms-Windows%20%7C%20macOS%20%7C%20Linux-8b97a7)](https://github.com/hoijun-kim/fleet/releases/latest)
[![license](https://img.shields.io/badge/license-PolyForm%20Noncommercial-3fb950)](LICENSE)

**[Download](https://github.com/hoijun-kim/fleet/releases/latest)** &nbsp;·&nbsp;
**[Landing page](https://hoijun-kim.github.io/fleet/)** &nbsp;·&nbsp;
**[Changelog](CHANGELOG.md)**

<!-- Add a 10-30s demo GIF here - it is the single highest-leverage asset for
     promotion. Record the real app (ScreenToGif / Kap), save as docs/demo.gif,
     then uncomment: -->
<!-- ![fleet in action](docs/demo.gif) -->

## Why fleet

Most git GUIs open one repository at a time. fleet is built for the whole
**fleet** of them: dozens of repos across your project roots, monitored like a
status board.

- **Live status** - dirty / behind / ahead / stale / CI-failing across every repo; fetch, pull or push in bulk.
- **Git without a terminal** - per-file staging, commit, amend, merge/rebase a diverged upstream, cherry-pick, reflog recovery, conflicts resolved inline.
- **Project management** - tasks, deadlines, tags, a cross-project Agenda and a Today view.
- **AI brief & agent** - a brief that weighs GitHub CI/PR/issue signals, per-repo chat, and an agent that runs under per-action approval (defaults to the `claude` CLI; OpenAI/Gemini optional).
- **Search** - fuzzy file quick-open and case-insensitive content search across every repo, plus a command palette.
- **Optional sync** - sign in with GitHub to sync projects and intel across devices; export/import everything as JSON.

## Run

Download the binary for your platform from Releases - `fleet.exe` (Windows),
`fleet-macos.zip` (macOS), `fleet` (Linux) - or build from source:

    wails build
    ./build/bin/fleet.exe

First run writes a config and creates the data directory next to it:

| OS | Path |
| --- | --- |
| Windows | `%APPDATA%\fleet\config.toml` |
| macOS / Linux | `$XDG_CONFIG_HOME/fleet/config.toml`, else `~/.config/fleet/config.toml` |

`roots` defaults to `~/Projects`. Edit it and restart.

## Build from source

Requires Go 1.22+, Node 18+, and Wails v2 (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`).
On Linux also install `libgtk-3-dev` and `libwebkit2gtk-4.1-dev`.

    wails build            # desktop app -> build/bin/fleet.exe

The AI brief, repo chat and agent features shell out to the `claude` CLI, which
must be on `PATH` (it is the default AI provider). The OpenAI and Gemini
providers are configured in Settings instead and need no CLI.

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
| `JWT_SIGNING_KEY` | HS256 secret for access tokens. At least 32 bytes with real entropy - the server refuses to boot otherwise. Generate with `openssl rand -base64 48` |
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

## License

Copyright (c) 2026 H.K. All rights reserved.

fleet is **source-available**, not open source. It is licensed under the
**PolyForm Noncommercial License 1.0.0** (see [LICENSE](LICENSE)): you may use,
study, modify and share it for any **noncommercial** purpose - personal use,
research, education, hobby projects, and nonprofit/government organizations.
**Commercial use is not permitted** under this license.

The copyright holder reserves all commercial rights. A commercial license may be
offered separately in the future; contact the author for commercial use.

Third-party dependencies keep their own (permissive / MPL-2.0) licenses and are
unaffected by fleet's license.
