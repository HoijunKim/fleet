# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Releases are cut by pushing a `v*` tag; `release.yml` pulls that version's
section from this file into the GitHub release body, so a section must exist
before its tag is pushed.

## [Unreleased]

_Nothing yet._

## [0.1.1] - 2026-08-14

The author's GitHub account was renamed from `hoijun-kim` to `hoijunkim`. The
app is unchanged; everything that named the old account moved.

### Changed

- The help overlay, the README and the landing page link to
  `github.com/hoijunkim/fleet` and `hoijunkim.github.io/fleet`.
- The winget package is `hoijunkim.fleet`.

### Note

Download URLs under `github.com/hoijun-kim/fleet` survive only while GitHub
redirects the old account name, which stops if someone else claims it. Prefer
the new ones.

## [0.1.0] - 2026-07-27

First tagged release: the whole app as it stands, published for Windows, macOS
and Linux.

### Added

- **Fleet dashboard** - every git repo under the configured roots in one window:
  dirty/behind/stale at a glance, with fetch, pull, open-editor, open-terminal
  and run-command from the row.
- **Git depth** - staging per file, commit, amend, push, pull, merge and rebase
  against a diverged upstream, combined diff view, stash apply/drop, branch
  create/delete, history and blame-adjacent detail panes.
- **Conflict recovery** - a conflicted merge or rebase is kept, not thrown away:
  resolve each file to either side (rebase-aware) or by hand, then continue or
  abort - all from the commit panel, without leaving for a terminal.
- **Project management** - manual projects, tasks with status/progress/order,
  deadlines, tags, a cross-project Agenda, and a Today view.
- **Project intelligence** - an AI brief per project weighing GitHub CI/PR/issue
  signals, a per-repo chat, and an agentic mode that can read, grep and run
  commands under per-action approval.
- **Cross-repo agent** - one question answered across every project at the root.
- **Search** - file-name and content search across repos, with per-project filter
  chips and a Content/Files toggle.
- **Cloud sync** (`cmd/fleetd`) - GitHub OAuth identity and a per-user versioned
  document store with Last-Write-Wins sync, deployed on Fly.io + Neon Postgres.
  Clone a synced-but-uncloned project, export local data, delete the account.
- **Editor picker** - known editors auto-detected on `PATH`, with a Custom
  command fallback.
- **Discoverability** - light/dark theme, a command palette, first-run
  onboarding, tag autocomplete, and a settings validation pass.
- **Server operability** - Prometheus metrics on a token-gated `/metrics`, Go
  runtime metrics, `/healthz` and a rate-limited `/readyz` wired as the Fly
  readiness check, graceful shutdown, request ids and panic recovery.
- **Backup restore** - a record sync overwrote or deleted can be restored from
  its backup in Settings; the restore re-pushes and wins on every device.
- **Import** - an exported data file can be imported back: projects and intel are
  upserted (local-only records are never deleted) and re-pushed to win on every
  device.
- **Force-delete a branch** - when git refuses to delete an unmerged branch, the
  branch menu offers a force delete behind a confirm.
- **Case-insensitive search** - content search now ignores case by default, with
  an "Aa" toggle to match case.
- **Sort persistence** - the project-table sort you pick is remembered across
  restarts.
- **Cherry-pick** - apply a commit from another branch onto the current one, from
  the detail panel; a conflict is handled by the same resolve/continue/abort
  panel as merge and rebase.
- **Reflog recovery** - a "History" picker moves the current branch back to a
  previous HEAD (after a bad reset/rebase); it refuses on a dirty tree so
  uncommitted work is never discarded.
- **In-app help** - a "?" button in the toolbar opens an overlay explaining what
  each part of the app does, for new users.
- **Fuzzy file search** - file-name search now matches like an editor's
  quick-open (type "cbsv" for "CommitBox.svelte") and ranks the best matches
  first across all repos.
- **Search modes** - content search now treats the query as literal text by
  default, with opt-in regex (.*) and whole-word (\b) toggles.
- **Rewrite history (interactive rebase)** - reorder, drop, or fixup recent
  local commits from the detail panel without opening an editor: fleet drives
  `git rebase -i` as its own sequence editor. A conflict during replay is kept
  and handed to the existing conflict panel.

### Changed

- **`fleetd` is env-tunable** - the HTTP timeouts (`FLEET_READ_HEADER_TIMEOUT`,
  `FLEET_READ_TIMEOUT`, `FLEET_WRITE_TIMEOUT`, `FLEET_IDLE_TIMEOUT`), the
  graceful-shutdown budget (`FLEET_SHUTDOWN_TIMEOUT`), the refresh-token GC
  cadence (`FLEET_GC_INTERVAL`), and the Postgres pool (`FLEET_DB_MAX_CONNS`,
  `FLEET_DB_MIN_CONNS`, `FLEET_DB_MAX_CONN_LIFETIME`, `FLEET_DB_MAX_CONN_IDLE_TIME`)
  can be set via environment variables. Unset reproduces the previous
  compiled-in defaults; an invalid value is ignored with a warning.
- fleet is now source-available under the PolyForm Noncommercial License 1.0.0
  (previously unlicensed): free for noncommercial use, commercial use not
  permitted. See `LICENSE`.

### Changed

- The AI brief and chat transcripts are stored in the local data directory (and
  included in the export), keyed by a stable repo identity, instead of the
  browser's localStorage - so they survive a cleared browser store.
- The AI brief and chat transcripts sync across your devices (last-write-wins),
  alongside projects. A source whose local file is unreadable is skipped and its
  sync paused, without stopping the others.

### Security

- The agent's `Grep`/`Glob` run through the approval hook, with secret-path denial
  and auto-allow only for searches classified safe; a default-branch push is
  always denied.
- Refresh tokens rotate as a family, with reuse revocation and periodic GC of
  expired rows.
- `fleetd` refuses to boot on a `JWT_SIGNING_KEY` shorter than 32 bytes or with
  fewer than 8 distinct byte values.

### Fixed

- A second app launch can no longer clobber the first launch's local data - the
  second instance hands off and exits.
- Unreadable local data is quarantined and sync pauses instead of propagating the
  damage; fleet never aborts a merge or rebase it did not start.
- A clobbered unsynced local edit is backed up rather than silently lost, and the
  backups are listable from Settings.
