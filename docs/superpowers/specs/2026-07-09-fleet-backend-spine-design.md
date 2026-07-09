# fleet Backend Spine (accounts + PM sync) - Design

> Slice 0 of turning fleet from a single-user local desktop tool into a hybrid
> service (local desktop + cloud backend). This spec covers ONLY the backend
> spine: identity, cloud sync of project-management data, and the desktop GUI
> for both. Later slices (intel sync, web dashboard, billing, teams) are out of
> scope and listed at the end.

## Goal and context

fleet today is a Wails desktop app: it scans local git repos under the user's
project roots and layers project-management (PM) data, GitHub, Notion, and AI
"intel" briefings on top. All state is local - `projects.json` (PM), `edges.json`,
config in `%APPDATA%\fleet`, and intel in the webview's `localStorage`.

The product direction is to run fleet as a real service and acquire customers
later. Because fleet's core value is inherently local (scan the user's disk, open
their editor/terminal, read local `git status`), a pure web SaaS cannot host it.
The chosen shape is therefore **hybrid**: the desktop keeps all local powers; a
cloud backend adds accounts and cross-device sync. This slice builds the spine
everything else depends on.

## Direction decisions (settled during brainstorming)

- Direction: hybrid service (local desktop + cloud backend).
- Driver: servicing / future customers (not just a portfolio piece).
- First slice: backend spine = accounts + sync.
- Architecture: self-built **Go backend** + managed **Postgres** + **GitHub OAuth**
  + **REST** sync (chosen over BaaS and CRDT: reuses the Go stack and internal/*,
  maximizes demonstrable backend depth, right complexity for v0).
- v0 sync scope: **PM data only**. Intel (briefings, per-repo chats) is deferred
  to slice 0.5 because it currently lives in the frontend `localStorage` and must
  first be relocated into a Go-backed store.
- Hosting: **Fly.io** (app, region `nrt`) + **Neon** serverless Postgres.

## Global principles

- **Default-to-local (minimal server footprint).** Anything not strictly required
  to sync stays on the device. The server holds ONLY identity plus the synced PM
  documents (opaque payloads). No config, no secrets, no derived scan/git state,
  no telemetry of local activity. When in doubt, it stays local.
- **Offline-first.** The desktop works fully with no network. Sync happens in the
  background when connected. The service never adds latency to local work.
- **Dumb server, smart client.** The server is a per-user, isolated, versioned
  document store. All domain logic stays in the desktop client.
- **Per-user isolation.** Every server query is scoped by `user_id`.
- **Crash-safe / no data loss.** A failed sync never corrupts or drops local data;
  local edits are the source of truth until acknowledged by the server.

## Architecture overview

Monorepo, single Go module. New packages are Wails-free on the server side.

```
fleet/
  app.go, internal/*        # existing desktop (Wails) - minimal changes
  internal/cloud/           # NEW: backend API client (auth calls, sync calls, token mgmt)
  internal/syncengine/      # NEW: offline-first sync loop (dirty tracking, push/pull, LWW)
  cmd/fleetd/               # NEW: backend server main
  internal/server/          # NEW: HTTP handlers, auth, Postgres store (no Wails import)
    http/                   #   router, middleware (auth, rate limit, logging)
    auth/                   #   GitHub OAuth, token issue/refresh/revoke
    pgstore/                #   Postgres-backed users + documents repositories
  frontend/src/lib/         # existing Svelte - add auth + sync-status UI
```

Boundary summary:

- Local-only (server never sees): repo scan, git ops, editor/terminal launch,
  local file reads, secrets (Notion/GitHub/AI tokens), config (roots, editor...),
  and **AI calls** (v0: the desktop calls the AI provider directly and syncs only
  resulting text later; server-side AI is a billing-era concern).
- Server-owned (synced): account identity + PM documents.

## Authentication - GitHub OAuth native flow (RFC 8252)

Desktop apps must not embed a browser for OAuth; use the system browser with a
loopback redirect and PKCE.

Flow:

1. Desktop starts a temporary loopback listener on `127.0.0.1:<ephemeral>` and
   generates a PKCE verifier/challenge and a random `state`.
2. Desktop opens the system browser to the backend `GET /auth/github/login`
   (carrying `state`, PKCE `code_challenge`, and the loopback `redirect`).
3. Backend redirects to GitHub's authorize URL (scopes: `read:user`, `user:email`).
4. GitHub redirects back to `GET /auth/github/callback`; the backend exchanges the
   code for a GitHub token (client secret held server-side), fetches the user,
   upserts the `users` row, mints a short-lived one-time `link_code`, and
   redirects the browser to the desktop's loopback URL with `link_code` + `state`.
5. Desktop's listener captures `link_code`, verifies `state`, and calls
   `POST /auth/exchange {link_code, code_verifier}`; the backend validates PKCE and
   returns the fleet session tokens.

Tokens:

- **Access token**: short-lived JWT (~15 min), HS256 signed with a server secret,
  carries `user_id`. Held in desktop memory only.
- **Refresh token**: opaque, long-lived, **rotating**, stored server-side as a
  hash. The desktop stores the raw refresh token in the **Windows Credential
  Manager** (OS keychain), never on disk in plaintext.
- `POST /auth/refresh {refresh}` rotates and returns a new access+refresh pair;
  the old refresh is revoked. `POST /auth/logout {refresh}` revokes.
- The client refreshes proactively and on any `401`.

`users` table:

```
users(
  id          uuid primary key,
  github_id   bigint unique not null,
  login       text not null,
  email       text,
  avatar_url  text,
  created_at  timestamptz not null default now(),
  updated_at  timestamptz not null default now()
)
refresh_tokens(
  id          uuid primary key,
  user_id     uuid not null references users(id),
  token_hash  text not null,
  expires_at  timestamptz not null,
  revoked     boolean not null default false,
  created_at  timestamptz not null default now()
)
```

## Sync model

Sync unit is a **document**. In v0 the only kind is `project`; `briefing` and
`chat` arrive in slice 0.5.

Server schema:

```
documents(
  user_id    uuid not null references users(id),
  kind       text not null,          -- 'project' (v0)
  doc_id     text not null,          -- stable identity (see below)
  payload    jsonb not null,         -- opaque to the server (a store.Record for 'project')
  updated_at timestamptz not null,   -- client logical time, used for LWW
  deleted    boolean not null default false,
  version    bigint not null,        -- per-user monotonic, server-assigned on each write
  primary key (user_id, kind, doc_id)
)
-- per-user monotonic counter for the pull cursor
user_versions(user_id uuid primary key, current bigint not null default 0)
```

Conflict resolution: **per-document Last-Write-Wins** by `updated_at` (client
logical timestamp, ms) plus soft-delete **tombstones**. On push, the server
accepts a document only if its `updated_at` is newer than the stored one (or the
doc is absent); otherwise it rejects that doc and the client pulls the newer
version. Field-level merge is explicitly out of scope (single-user multi-device
makes LWW sufficient). Clock-skew across one user's own devices is acceptable and
noted as a known limitation.

Pull cursor: a per-user monotonic `version`. Each accepted write bumps
`user_versions.current` (same transaction) and stamps the document's `version`.
Clients pull `where user_id=? and version > ?`.

Protocol (REST, auth required):

- `GET /sync?since=<version>` -> `{ docs: [{kind, doc_id, payload, updated_at, deleted, version}], cursor }`
- `POST /sync { docs: [...] }` -> `{ results: [{doc_id, accepted, version}], cursor }`

Client sync engine (`internal/syncengine`):

- Keeps a local sync-state file `sync.json`: last pull `cursor`, and per-doc
  `{doc_id, last_synced_hash, updated_at}` used to detect dirty docs.
- On every local PM mutation, the store stamps `UpdatedAt` and the engine marks
  the doc dirty (its payload hash differs from `last_synced_hash`).
- Background loop (interval + on-change + on-reconnect): push dirty docs, then
  pull since `cursor`, then apply LWW to the local store (a pulled doc overwrites
  the local one only if its `updated_at` is newer; tombstones delete locally).
- Offline: network/5xx errors queue the work and retry with exponential backoff;
  local edits keep accumulating and flush on recovery.

## Document identity and cross-machine mapping

The local project id is the repo path for code projects and an opaque id for
manual projects. Paths are machine-specific, so a raw path cannot be the sync
`doc_id` (the whole point of sync is multiple machines). Rules:

- **Manual project**: `doc_id` = its existing opaque id (already portable).
- **Code project**: `doc_id` = a stable identity derived from the repo's git
  remote: `git:<normalized-remote>` (lowercased, scheme/credentials stripped,
  trailing `.git` removed, host+path only). If the repo has no remote, fall back
  to `local:<hash(folder-name)>` - these degrade gracefully and simply do not
  match across machines.
- The client maintains a mapping `local project id (path) <-> doc_id` so pulled
  docs apply to the right local project and local edits push under the right id.
- A pulled code-project doc with no matching local repo on this machine is stored
  as a **detached record** and reconciled when a scan later surfaces a repo whose
  stable id matches. Detached records are retained, never dropped.

`store.Record` gains `UpdatedAt string` (RFC3339), set on every mutation (a small
migration mirroring the existing `Status` migration). This is the LWW timestamp.

## Desktop GUI (must be fully polished)

Implemented in Svelte, consistent with the existing dark theme and the
Today/Overview/Projects/Graph layout; built with the frontend-design skill.

- **Onboarding / sign-in**: non-blocking. A clean "Sign in with GitHub" entry
  with copy that makes offline-first explicit ("works without signing in; sign in
  to sync across devices"). Never forces login.
- **Account chip**: top bar, avatar + login; dropdown with Sign out and Sync now.
- **Sync status pill** (top bar) with clear, distinct states: Offline /
  Syncing... / Synced (n sec ago) / Error (with reason + retry) / Not signed in
  (dismissible nudge). No layout shift when the state changes.
- **Conflict feedback**: LWW is silent by default; when a remote change overwrites
  a local edit, show a subtle non-blocking toast ("updated on another device").
- **Errors**: all network/auth errors surface as non-blocking toasts, never block
  local work, and auto-retry with backoff.
- **Polish bar**: loading skeletons, clean empty states, keyboard accessible.

New Wails bindings on `App`: `AuthStart()`, `AuthStatus()`, `SignOut()`,
`SyncNow()`, `SyncState()`; sync/auth state changes emit Wails runtime events the
frontend subscribes to for the pill.

## Error handling

- Local mutations never fail on sync problems; the store write succeeds and the
  engine retries sync independently.
- Auth: `401` triggers a refresh; a failed refresh drops to signed-out state and
  the pill nudges re-login, without losing local data.
- Sync push rejection (stale doc) triggers a pull-and-reapply, not an error.
- Server 5xx / network down: exponential backoff, capped; the pill shows Offline.

## Infrastructure and deployment

- **Deploy**: Fly.io app (Go binary + Dockerfile), region `nrt`; Neon Postgres.
  TLS terminated by Fly.
- **Server stack**: `net/http` + a light router (chi), `pgx` for Postgres,
  `golang-jwt` for access tokens, `golang.org/x/oauth2` for GitHub. Dependency
  policy: the existing "standard library only" rule stays for the **desktop**; the
  **server module** may use these vetted, minimal dependencies (a real service
  needs them). The desktop stays stdlib-lean.
- **Secrets (server)**: Fly secrets - `DATABASE_URL`, `GITHUB_OAUTH_CLIENT_ID`,
  `GITHUB_OAUTH_CLIENT_SECRET`, `JWT_SIGNING_KEY`, allowed loopback redirect
  prefix.
- **Migrations**: embedded SQL run by golang-migrate at startup.
- **Observability**: `slog` structured logging + a `GET /healthz` endpoint. v0
  keeps this minimal.
- **Rate limiting**: simple middleware (per-IP on auth routes, per-user on sync).
- **CI**: GitHub Actions builds and tests both the desktop and the server; a tag
  triggers a Fly deploy.

## Testing strategy

- **Server**:
  - `pgstore`: repository tests against a real Postgres (via a `DATABASE_URL_TEST`
    or dockertest; skipped when unset).
  - handlers: `httptest` against a stub store.
  - auth: the GitHub client is an interface; tests use a fake GitHub.
  - sync: table-driven LWW tests (newer wins, tombstones, cursor monotonicity,
    per-user isolation).
- **Client**:
  - `syncengine` against an in-process `httptest` fake server: LWW apply, dirty
    detection, offline queue + backoff, cursor persistence.
  - `cloud`: refresh-on-401 and token rotation.
  - `store`: the `UpdatedAt` migration (mirrors the existing `Status` migration test).
- Existing desktop tests stay green; `go vet ./...` clean; desktop packages
  remain gofmt-clean and ASCII-only.

## Security

- TLS everywhere (Fly).
- Refresh tokens: hashed at rest, rotating, revocable, expiring.
- Access tokens: short-lived signed JWTs.
- PKCE binds the loopback exchange; `state` guards CSRF on the callback.
- Per-user row isolation on every query.
- No user dev-secrets (Notion/GitHub/AI tokens) ever leave the device.

## Out of scope for v0 (later slices)

- Intel sync (briefings, per-repo chats) - slice 0.5, after relocating intel from
  `localStorage` into a Go-backed store keyed by a stable repo identity.
- Web dashboard, billing/licensing, teams/sharing, server-side AI, field-level
  merge, real-time push (v0 uses interval/on-change polling).

## Success criteria

- Sign in with GitHub from the desktop; the session survives restarts (refresh
  token in the Credential Manager).
- A PM edit on one machine appears on another after sync: manual projects always;
  code projects when both machines have the same git remote.
- Fully usable offline; syncs on reconnect with no data loss; LWW resolves
  concurrent edits deterministically.
- The GUI shows clear auth and sync status; errors never block local use.
- Server and client tests are green; deployed to Fly.io with Neon Postgres.
