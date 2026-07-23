# Tier 4c - Git Conflict Recovery Design

**Goal:** a conflicted merge or rebase can be finished inside fleet.

This is the anchor tier 3d named: "EXPLICITLY NOT IN SCOPE:
`MergeContinue`/`MergeAbort`/`RebaseContinue`/`RebaseAbort` bindings, per-file
ours/theirs resolution, the `CommitBox` conflict UI. That L is the anchor of a
later git-recovery tier."
(`specs/2026-07-18-tier3d-local-data-integrity-design.md:158-160`)

Backlog item 2 (`docs/superpowers/BACKLOG.md`).

## What the app does today

Three surfaces, all of them exits:

- `internal/git/ops.go:179-183` - when fleet's own `merge`/`rebase @{u}`
  conflicts, fleet runs `<mode> --abort` and reports "resolve in a terminal".
  The user never even lands in the conflict.
- `internal/git/ops.go:160` - when an operation fleet did NOT start is already in
  progress, integration refuses: "finish or abort it in a terminal first".
- `CommitBox.svelte:169-172` - a conflicted file is listed with a `!` and the
  title "resolve it in your editor or terminal before staging". There is no
  control on it.

`GitOperation(path)` (`app.go:367`) already reports `"merge"`, `"rebase"` or
`""`, and `StatusFiles` already flags conflicted paths (`ops.go:379`). The state
is visible; only the verbs are missing.

## 1. Conflict inspection: `Conflicts`

`git ls-files -u` lists unmerged paths as stage entries: stage 1 is the merge
base, stage 2 "ours", stage 3 "theirs". Which stages are present is what
distinguishes the conflict kinds, and the kinds are not cosmetic - they decide
what "keep mine" even means:

| Stages present | Kind | What "mine"/"incoming" mean |
| --- | --- | --- |
| 1,2,3 | `both-modified` | both sides have content |
| 2,3 (no 1) | `both-added` | both sides created the file |
| 1,2 (no 3) | `deleted-by-them` | mine has content, incoming deleted it |
| 1,3 (no 2) | `deleted-by-us` | mine deleted it, incoming has content |

```go
type Conflict struct {
	Path string `json:"path"`
	Kind string `json:"kind"` // both-modified | both-added | deleted-by-them | deleted-by-us
}

func Conflicts(r Runner, dir string) ([]Conflict, error)
```

Parsed from `ls-files -u`, whose lines are `<mode> <sha> <stage>\t<path>`. Paths
are deduplicated (three stages, one entry) and sorted, because `ls-files` emits
one line per stage.

## 2. Resolution: `ResolveConflict`

```go
func ResolveConflict(r Runner, dir, mode, file, side string) error
```

`side` is `"mine"` or `"incoming"` - the user's words, not git's. The translation
is the whole point of this function, and it is where a naive implementation is
wrong:

**During a rebase, `--ours` and `--theirs` are swapped.** A rebase checks out the
upstream and replays your commits onto it, so `HEAD` - what git calls "ours" -
is the upstream, and the commit being applied - "theirs" - is yours. A UI that
maps "keep mine" to `--ours` is correct for a merge and silently discards the
user's work in a rebase.

| mode | `side="mine"` | `side="incoming"` |
| --- | --- | --- |
| merge | `--ours` | `--theirs` |
| rebase | `--theirs` | `--ours` |

The resolution itself depends on whether the chosen side has content:

- The side has content: `git checkout --ours|--theirs -- <file>` then
  `git add -- <file>`.
- The chosen side is the one that deleted the file (`deleted-by-us` with
  `side="mine"`, or `deleted-by-them` with `side="incoming"`): there is nothing
  to check out, so it is `git rm -- <file>`. `checkout --ours` on a missing
  stage fails with a bare error, which is exactly the dead end this tier exists
  to remove.

`ResolveConflict` therefore calls `Conflicts` first to learn the kind. Marking a
manually-edited file resolved is the same call path with a third side value,
`"worktree"`, which only runs `git add -- <file>` - the user edited the file in
their editor and wants what is on disk.

## 3. Finishing: `ContinueOperation` / `AbortOperation`

```go
func ContinueOperation(r Runner, dir string) error
func AbortOperation(r Runner, dir string) error
```

Both read the mode from `OperationInProgress` rather than taking it as an
argument: the on-disk marker is the truth, and passing a stale mode from the UI
is a way to run `rebase --abort` on a merge.

`Runner.Run(dir, args...)` has no environment hook (`runner.go:22`), so an editor
cannot be suppressed with `GIT_EDITOR`. Instead both continue paths pass
`-c core.editor=true` before the subcommand - a "editor" that exits 0 without
touching the message, leaving git's default merge/rebase message. This is why
`Runner` needs no change.

- merge: `git -c core.editor=true merge --continue`
- rebase: `git -c core.editor=true rebase --continue`
- abort: `git <mode> --abort`

`ContinueOperation` refuses with a clear error when unmerged paths remain, rather
than letting git's own "you must edit all merge conflicts" through: fleet knows
the list and can say which files.

An empty `OperationInProgress` is an error for both ("no merge or rebase in
progress"), not a silent no-op - a UI that shows the buttons when nothing is in
progress is a bug worth surfacing.

## 4. `integrateUpstream` stops rolling back conflicts

`ops.go:176-184` currently aborts on every failure. That was correct when abort
was the only unwind fleet had; with sections 1-3 it is no longer. New rule:

- Unmerged paths exist (`ls-files -u` non-empty) - this is a real content
  conflict. **Leave it in place** and return a sentinel error the UI recognizes,
  so the conflict panel takes over.
- No unmerged paths but an operation is still in progress - a hook rejected the
  commit, `commit.gpgsign` had no signer, no user identity. Nothing to resolve
  by hand, so keep today's behavior: `<mode> --abort` and surface git's own
  diagnostic. This is the case tier 3a's comment was protecting, and it is
  preserved exactly.

The distinction is `ls-files -u`, which the function already runs.

`ErrConflict` is a package-level sentinel so `app.go` can branch with
`errors.Is` rather than matching a message. The guard at `:160` that refuses to
start on top of a foreign operation stays as is - starting a second operation is
still wrong; the difference is that the UI can now finish the first one.

`DetailPanel.svelte:416` says "A conflict is rolled back, not left half-applied."
That sentence becomes false with this change and is rewritten.

## 5. Bindings and views

```go
func (a *App) Conflicts(path string) []ConflictView
func (a *App) ResolveConflict(path, file, side string) string
func (a *App) ContinueOperation(path string) string
func (a *App) AbortOperation(path string) string
```

The mutating three return `errMsg(...)` like every other git binding in
`app.go:239-241`, so the frontend keeps one error convention.

`ConflictView` carries `Path`, `Kind`, and the two resolved labels the UI shows
("Keep mine" / "Keep incoming" are constant, but which is upstream differs by
mode, so the view also carries `Mode`). Computing the labels in Go keeps the
swap rule in one place - the frontend must not re-derive it.

## 6. UI

`CommitBox.svelte`, which already lists conflicted files:

- A banner above the file list when `GitOperation(path) != ""`: "Merge in
  progress - 3 files conflict", with **Continue** (disabled while any conflict
  remains) and **Abort**.
- Each conflicted row gains three controls: **Mine**, **Incoming**, **Resolved**
  (the last one after editing by hand), plus the existing open-in-editor path.
  For `deleted-by-us`/`deleted-by-them` the buttons read **Keep deleted** /
  **Keep file** so the user is choosing an outcome, not a git stage.
- After every resolution the file list reloads, so a row leaves the conflict
  group the moment it is staged.

The commit message box stays disabled while a conflict remains - `CommitStaged`
already refuses (`ops_test.go:88`), and Continue is the correct verb here, not
Commit.

## Testing

`internal/git/integrate_test.go` already builds real conflicting repos with
`ExecRunner` (`:208-232`), so the new tests extend that fixture rather than
inventing a mock:

- `Conflicts` reports `both-modified` for overlapping edits, `deleted-by-us` and
  `deleted-by-them` for a delete/modify pair, `both-added` for two new files at
  the same path.
- `ResolveConflict` with `side="mine"` in a **rebase** keeps the local commit's
  content - the test that fails if `--ours`/`--theirs` are mapped naively. The
  merge counterpart asserts the other direction.
- Resolving a `deleted-by-us` to `"mine"` leaves the path absent and staged as a
  deletion.
- `ContinueOperation` finishes a merge after every conflict is resolved and
  leaves a commit whose parents are both heads; it errors while a conflict
  remains, naming the file.
- `AbortOperation` restores the pre-merge HEAD and clears `OperationInProgress`.
- `integrateUpstream` on a conflict now leaves `OperationInProgress() == "merge"`
  and returns `ErrConflict` - the inverse of today's
  `TestMergeUpstreamConflictAborts`, which is rewritten rather than deleted so
  the non-conflict rollback case stays covered.
- A hook-rejected merge (a `pre-merge-commit` hook exiting 1) still rolls back:
  the case that must not regress.

Frontend: a `CommitBox` test that the Continue button is disabled while a
conflict row is present and enabled once the list is empty.

## Out of scope

Interactive rebase, cherry-pick conflicts, `rebase --skip`, three-way merge
tooling (`git mergetool`), a diff3 conflict-hunk editor inside fleet, and
conflict resolution for stash-apply conflicts. The tier finishes what fleet
itself can start - `merge @{u}` and `rebase @{u}` - plus any operation the user
started elsewhere and left in the tree, which fleet can now continue or abort
rather than only describe.
