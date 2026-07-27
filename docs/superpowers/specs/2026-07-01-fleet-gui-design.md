# fleet GUI - Wails Desktop App (design addendum)

**Date:** 2026-07-01
**Status:** Design approved
**Supersedes the presentation layer of** `2026-07-01-fleet-tui-design.md`. The TUI
remains available as a bonus binary under `cmd/fleet`; the GUI becomes the primary
front end.

## Why

The original build produced a terminal UI (TUI). The user wanted a windowed
graphical desktop application. All backend logic already lives in UI-agnostic Go
packages, so only the presentation layer changes.

## Approach

A [Wails v2](https://wails.io) desktop app: Go backend + a Svelte-TS web front end
rendered in the OS native webview, packaged as a single executable per OS.

- **Reused as-is:** `internal/repo`, `internal/config`, `internal/scan`,
  `internal/git`, `internal/meta`, `internal/action`.
- **Replaced/added:** a Wails entry (`main.go`), a binding layer (`app.go`) exposing
  Go methods to JavaScript, and a `frontend/` Svelte-TS app.
- **Kept:** `cmd/fleet` (the TUI) still builds and runs.

## Binding layer (`app.go`)

The `App` struct holds a `git.Runner` (real `ExecRunner` in production, a fake in
tests) and the loaded `config.Config`. It exposes these methods to the front end
(names are the JS binding names):

- `ScanRepos() []RepoView` - discover repos under the configured roots; returns
  skeleton views (Name/Path/IsGit set, not yet loaded).
- `LoadRepo(path string) RepoView` - load one repo's git + meta data, return the
  full view. The front end calls this per repo in parallel, so rows fill in live.
- `Fetch(path string) string` / `Pull(path string) string` - run git fetch / pull
  --ff-only; return an error message or "".
- `OpenEditor(path string) string` / `OpenTerminal(path string) string` - launch the
  configured editor/terminal at the repo; return an error message or "".
- `RunCommand(path, line string) string` - run a command line in the repo, return
  combined output.
- `GetConfig() config.Config` / `SaveConfig(c config.Config) string` - read/write
  config.

`repo.Repo` carries an `error` field that does not serialize to JS, so a `RepoView`
DTO mirrors it with `ErrMsg string` instead, plus flattened last-commit fields.

Live loading is JavaScript-driven (front end calls `LoadRepo` per repo via
`Promise`), not Go-event-driven - simpler and equally concurrent, since each Wails
binding call runs in its own goroutine.

## Front end (`frontend/`, Svelte-TS)

- `App.svelte` - top-level state (repos, filter, sort, selection); on mount calls
  `ScanRepos` then `LoadRepo` for each; renders Toolbar + RepoTable + DetailPanel.
- `lib/Toolbar.svelte` - filter input, aggregate counts (repos/dirty/behind),
  Fetch-All and Refresh buttons.
- `lib/RepoTable.svelte` - the repo rows: name, branch, status pill (colored),
  ahead/behind, last commit, language, TODO; row click selects; per-row loading
  spinner.
- `lib/DetailPanel.svelte` - selected repo's path/commit/remote/dirty files, with
  per-repo action buttons (Fetch, Pull, Editor, Terminal, Run command).
- `app.css` - dark theme, colored status pills, clean modern layout (the "premium"
  look). ASCII-only text in code; no `-`/`·`/`…`/`─`.

## Build & distribution

`wails build` bundles the front end and produces a single native executable.
Cross-platform via `wails build` per OS. README and the release pipeline are updated
for Wails.

## Testing

- Backend packages: already covered.
- `app.go`: unit-test `toView` mapping and the action methods with a fake
  `git.Runner` (no real git/network); `ScanRepos` against a temp directory.
- Front end: build-verified (`wails build` succeeds); visual behavior is a manual
  check by the maintainer (the windowed app needs a real display).

## Out of scope (carried over)

Same deferrals as the TUI spec: `auto_fetch_minutes` timer, sort persistence,
language extension-heuristic fallback, bounded worker pool, CJK column-width
alignment.
