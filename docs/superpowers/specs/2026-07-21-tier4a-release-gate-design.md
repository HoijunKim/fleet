# Tier 4a - Release Gate Design

**Goal:** fleet must never publish a binary no test has touched, must never let a
second launch destroy the first launch's data, and must never boot a server
whose token signing key is guessable. Everything here is about the perimeter -
what gets shipped, what gets run twice, what gets deployed - not about features.

The four items share one shape: a promise the project already makes, with no
code behind it. `release.yml` publishes three platform binaries on a `v*` tag
and runs zero tests, because `server.yml`'s path filter scopes CI to
`cmd/fleetd` and `internal/server` only (`server.yml:4-19`). The desktop app
holds every local store fully in memory and rewrites each file whole
(`app.go:118-131`), with no lock stopping a second process from doing the same.
`cmd/fleetd/main.go:50` accepts any non-empty `JWT_SIGNING_KEY`. The README
documents Windows only and one of its two build commands names a directory that
does not exist.

## 1. `.github/workflows/desktop.yml` (new, reusable)

```yaml
on:
  push:
    branches: ["**"]
  pull_request:
  workflow_call:
```

No path filter: the whole point is that every push exercises the desktop tree.
`branches: ["**"]` matches branch pushes but not tags, so a `v*` tag does not
run this workflow twice - once directly and once through `release.yml`'s
`workflow_call`.

**Job `go`** - matrix `[windows-latest, ubuntu-latest]`:

- The frontend must be built before any Go step. `main.go:15-16` declares
  `//go:embed all:frontend/dist`, and `frontend/dist/` is gitignored
  (`.gitignore:4`), so on a fresh checkout every Go command that compiles
  package `main` - build, vet and test alike - fails with
  `pattern all:frontend/dist: no matching files found`. The job therefore runs
  `npm ci && npm run build` in `frontend/` first. `release.yml` does not need
  this only because `wails build` runs the frontend install/build itself
  (`wails.json`).
- `go build ./...`, `go vet ./...`, `go test ./...`.
- The ubuntu leg installs `libgtk-3-dev libwebkit2gtk-4.1-dev` and passes
  `-tags webkit2_41` to build, vet and test. Package `main` links wails, which
  is cgo/GTK on Linux; `release.yml:25-27` already does exactly this for the
  Linux build. The windows leg needs neither.
- `-race` on the ubuntu leg only. The windows race detector needs a cgo
  toolchain, and `internal/git` already takes ~14s there because it shells out
  to real git.
- `gofmt -l .` over the whole tree, ubuntu leg only - gofmt is
  platform-independent, so running it twice buys nothing. This is safe despite
  a local `gofmt -l .` listing 67 files: that is a CRLF artifact of
  `core.autocrlf=true` in the author's working tree, and `.gitattributes`
  (`* text=auto eol=lf`) forces LF on a fresh CI checkout on every platform.

**Job `frontend`** - ubuntu only, node 20: `npm ci` (a `package-lock.json` is
committed), then `npm run check` (svelte-check), `npm test` (vitest, 22 files /
87 tests), `npm run build`. Node output does not vary by runner OS, so there is
no matrix here.

`server.yml` is left exactly as it is. It needs a postgres service and owns the
Fly deploy on `server-v*` tags; folding it in would trade a clean split for one
larger file.

## 2. `release.yml` gains a test gate

```yaml
jobs:
  test:
    uses: ./.github/workflows/desktop.yml
  build:
    needs: test
    ...unchanged
```

GitHub Actions has no cross-workflow `needs:`, so a reusable `workflow_call` is
the only way to gate the release on the desktop suite without copying the job
into `release.yml`. `permissions: contents: write` stays declared in
`release.yml`; the called workflow only reads.

Cost: a `v*` tag now waits for the full matrix before building. That is the
point - the tag is the moment the artifact becomes real to someone else.

## 3. Single-instance lock (`main.go`)

Two fleet processes hold divergent full in-memory copies of every local store
and rewrite each file whole on mutation; last writer wins and silently destroys
the other's projects, tasks, tags and deadlines. `backupConflict` covers only
the cloud path, so nothing is preserved. Distribution is what makes a double
launch common.

```go
SingleInstanceLock: &options.SingleInstanceLock{
    UniqueId:               "fleet-desktop",
    OnSecondInstanceLaunch: func(options.SecondInstanceData) { app.focus() },
},
```

`App.focus()` is new: it no-ops when `a.ctx == nil` and otherwise calls
`runtime.WindowUnminimise` then `runtime.Show`. The nil guard is required, not
defensive - the callback can fire before `startup` has assigned `a.ctx`.

Three facts make this sufficient rather than merely convenient:

- The second instance exits inside `SetupSingleInstance`, called at the top of
  `Frontend.Run` (wails v2.12.0, `internal/frontend/desktop/windows/frontend.go:161`),
  which is before `OnStartup` is dispatched (same file, `:222-226`). So the
  second process never reaches `startup` -> `startSync` and never runs the 60s
  sync loop. Windows uses a named mutex plus `WM_COPYDATA`; darwin and linux
  have their own `single_instance.go`.
- `NewApp()` has already run by then, but `store.Open`, `edges.Open` and the
  keychain read are read-only. The one exception is `config.Load`, which writes
  defaults when the file is absent - identical bytes, only on first run, so it
  is harmless.
- The agent hook path returns before wails is touched at all
  (`main.go:22-26`), so a hook invocation never takes or trips the lock.

A data-directory lockfile was considered and rejected: it would re-implement by
hand what wails already provides on all three platforms, and it adds stale-lock
recovery UX that this tier does not need. The accepted consequence of the
mutex approach is that `wails dev` and an installed `fleet.exe` can no longer
run at the same time - which is the data-loss scenario, not a workflow to
protect.

## 4. Reject a weak `JWT_SIGNING_KEY` (`cmd/fleetd`)

A short or dictionary secret boots cleanly and looks healthy forever, and every
legitimately issued 15-minute token is then an offline cracking oracle for
minting `sub=<any user id>`.

New pure function in `cmd/fleetd`:

```go
func validateSigningKey(key []byte) error
```

- rejects `len(key) < 32`
- rejects fewer than 8 distinct byte values (catches `"aaaa..."`, `"changeme"`
  and other low-entropy strings that pass a length check)

Called in `run()` immediately after `mustEnv("JWT_SIGNING_KEY")` and returned as
an error rather than `os.Exit`, matching `run()`'s existing single-exit-point
contract (`cmd/fleetd/main.go:44-46`).

Deploy impact, accepted deliberately: if the live key is weak, the next deploy
refuses to boot until the secret is rotated. Rotation invalidates only issued
15-minute access tokens; refresh tokens live in Postgres and are unaffected, so
signed-in users refresh through it.

## 5. README corrections

- `## Run (Windows)` becomes platform-neutral. `release.yml:9-17` already ships
  windows/amd64, darwin/universal and linux/amd64; only the README says
  otherwise.
- Delete `go build ./cmd/fleet   # optional terminal UI (TUI) bonus binary`
  (`README.md:22`). That directory was removed in bd6f51e; `cmd/` holds
  `fleet-hook` and `fleetd`.
- Document the config path per OS from `internal/config/config.go:63-78`:
  `%APPDATA%\fleet\config.toml` on Windows, else `$XDG_CONFIG_HOME/fleet/` or
  `~/.config/fleet/`.
- Add the `claude` CLI as a prerequisite for the AI and agent features: it is
  the default provider (`config.go:59`) and is resolved with
  `exec.LookPath("claude")` (`internal/ai/ai.go:52`).
- In the server env table, state that `JWT_SIGNING_KEY` must be at least 32
  bytes, with `openssl rand -base64 48` as the example.

## Error handling & safety

- The signing-key check fails closed, before any listener binds or migration
  runs, so a weak key cannot serve a single request.
- `App.focus()` is total: a nil context returns immediately rather than calling
  into a runtime that has no window yet.
- No existing behavior changes on the normal single-launch path: the lock is
  inert when no other instance holds the mutex.

## Testing

- `validateSigningKey`: too short, exactly 32 bytes of low entropy (rejected),
  31 bytes of high entropy (rejected), 32 random bytes (accepted), empty
  (rejected - though `mustEnv` catches it first).
- `App.focus()` with `a.ctx == nil` returns without panicking.
- The workflows themselves are only verifiable by running them: after merge,
  confirm the first push shows `desktop / go (windows-latest)`,
  `desktop / go (ubuntu-latest)` and `desktop / frontend` green, and that a
  throwaway `v0.0.0-test` tag runs `test` before `build`.

## Out of scope

Code signing and notarization, an auto-update channel, version stamping via
ldflags, and a CHANGELOG. Each is its own piece of work; none is required for
the guarantee this tier makes, which is only that what ships was tested and
what runs is alone.
