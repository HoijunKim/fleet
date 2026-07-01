# fleet — Multi-repo Command Center TUI

**Date:** 2026-07-01
**Status:** Design approved, pending spec review
**Working name:** `fleet` (renameable)

## 1. Purpose

A single Go binary that shows every git repository under one or more configured
root directories on one screen, and lets the user manage them without leaving the
terminal: inspect status, run git actions, open an editor or terminal at a repo,
and run arbitrary commands. Zero runtime dependencies — download the binary and run
it on Windows, macOS, or Linux.

**Problem it kills:** the user keeps many projects side by side (arsi, emg,
PROJECT_Debt-collection, notion, unis-j-web, …). Checking which ones are dirty,
behind, or stale means `cd`-ing into each and running `git status`/`git log` by
hand. `fleet` collapses that into one live dashboard.

## 2. Architecture

Go + Bubbletea, following the Elm architecture (Model / Update / View). All slow
work (scanning, git calls, TODO counting, fetch/pull, arbitrary commands) runs as
asynchronous `tea.Cmd`s so the UI never blocks; results flow back in as messages.

```
cmd/fleet/main.go        Entry point: load config -> scan -> start TUI
internal/config/         Roots, editor/terminal commands, scan depth (TOML)
internal/scan/           Discover .git dirs under roots (depth-limited walk)
internal/git/            GitRunner interface + exec implementation; porcelain parsing
internal/meta/           Language detection, folder size, TODO/FIXME count
internal/tui/            Model / Update / View, components, keymap
internal/action/         fetch / pull / open-editor / open-terminal / run-command wrappers -> tea.Cmd
```

### Key decision: git access via the real `git` binary (shell-out), not go-git

`fleet` shells out to the user's installed `git` through an `os/exec` wrapper hidden
behind a `GitRunner` interface. Rationale:

- The user's git config and credential helpers work automatically, so `fetch`/`pull`
  authenticate with no extra handling.
- `--porcelain` output is stable and easy to parse.
- The `GitRunner` interface lets tests inject a fake runner, so parsing and model
  logic are testable without a real repo or network.

### Concurrency

Scanning uses a bounded worker pool. Each repo's status, last-commit, meta, and TODO
count are computed independently and streamed into the model as they finish, each
showing a spinner until ready. One slow or broken repo never blocks the others.

## 3. Configuration

Location: `%APPDATA%\fleet\config.toml` on Windows, `$XDG_CONFIG_HOME/fleet/config.toml`
(fallback `~/.config/fleet/config.toml`) elsewhere. If missing on first run, a default
config is written and the user is told where it is.

```toml
roots = ["C:/Users/hoijun/Projects"]   # one or more root directories
scan_depth = 2                          # max recursion depth when finding .git dirs
editor = "code"                         # command used to open a repo in an editor
terminal = "wt"                         # command used to open a new terminal at a repo
auto_fetch_minutes = 0                  # 0 = off; > 0 = periodic background fetch interval
show_non_git = true                     # show non-git folders (dimmed) or hide them
```

## 4. Per-repo data

- **Git core:** current branch, dirty/clean, number of modified files, ahead/behind
  counts vs upstream (↑/↓).
- **Recent activity:** last commit's relative time, message, and author.
- **Meta:** primary language (detected from `go.mod` / `package.json` / `pyproject.toml`
  markers plus a file-extension heuristic over tracked files), folder size, README presence.
- **TODO:** count of `TODO`/`FIXME` markers via `git grep -cE "TODO|FIXME"` over tracked
  files only (fast, and respects `.gitignore`).

Non-git folders under a root are shown dimmed (or skipped) — they still appear so the
user sees the whole directory, but carry no git data.

## 5. UI layout

```
+- fleet - roots 2 - repos 11 - dirty 3 - behind 1 -----------------+
| NAME            BR      ST    up/dn  LAST      LANG   TODO         |
| arsi            main    *3    dn1    2h ago    JS     4            |  <- selected
| emg             dev     ok           3d ago    Py     0            |
| PROJECT_Debt..  main    *1           5d ago    TS     12           |
| ...                                                               |
+- detail: arsi ----------------------------------------------------+
| path   C:/Users/hoijun/Projects/arsi                              |
| head   a1b2c3 "fix scaffold bug" - hoijun, 2h ago                 |
| remote origin git@.../arsi.git                                    |
| dirty  M src/app.ts   M README.md   ?? tmp.log                    |
+- [f]etch [F]all [p]ull [e]dit [t]erm [x]cmd [/]filter [?]help ----+
+-------------------------------------------------------------------+
```

- **Keys:** `j/k` or arrows to move, `enter` toggle detail, `f` fetch selected /
  `F` fetch all, `p` pull, `e` open editor, `t` open terminal, `x` run arbitrary
  command (input prompt -> run in repo -> output pane), `r/R` refresh selected/all,
  `/` fuzzy filter by name, `s` cycle sort key, `q` quit, `?` help.
- **Styling (Lipgloss):** dirty = yellow marker, clean = green marker, behind = red
  down-arrow, scanning = spinner. A top bar shows aggregate counts (repos, dirty,
  behind).

## 6. Actions (all async `tea.Cmd`)

- **fetch / fetch all:** run in the background, per-repo progress shown, status
  refreshed on completion.
- **pull:** pull the selected repo, then refresh its status.
- **edit:** run the configured `editor` command with the repo path.
- **term:** run the configured `terminal` command to open a new terminal at the repo path.
- **run command (`x`):** prompt for a command, run it in the selected repo's directory,
  stream stdout/stderr into an output pane.

## 7. Error handling

- Per-repo errors are isolated: a broken repo shows an error on its row and never
  crashes the app.
- Non-git folders are shown dimmed (or hidden when `show_non_git = false`), never treated as repos.
- Missing/invalid `editor` or `terminal` commands surface a friendly message in the
  status line.
- A config parse error shows a clear message and points the user at the default config.
- Command-exec failures show the exit code and stderr in the output pane.

## 8. Testing

- **scan:** table tests over temp directories containing fake `.git` folders at
  various depths.
- **git:** feed sample `--porcelain` / `log` / ahead-behind output into the pure
  parser functions and assert the parsed structs.
- **config:** load, default generation, and save round-trip.
- **tui:** send messages into `Model.Update` and assert state transitions (teatest).
- **GitRunner interface:** inject a fake runner so all of the above run without a real
  repo or network access.

## 9. Distribution

- `go install github.com/hoijun/fleet@latest` for Go users.
- GitHub Actions + goreleaser to publish prebuilt Windows/macOS/Linux binaries per
  release, so non-Go users just download and run.

## 10. Out of scope (YAGNI, later)

- Scaffolding / bootstrap of new projects (separate future tool).
- Session-handoff journal (separate future tool).
- Authoring tools.
- Remote/API integrations (GitHub issues, Notion). Config leaves room to add later.
