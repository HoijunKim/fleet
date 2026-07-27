# Tier 4q - Interactive rebase (reorder / drop / fixup)

**Goal:** clean up local commit history before pushing - reorder commits, drop
one, or fixup a commit into its parent - from the UI, without an editor.

Backlog item (git-recovery cluster; cherry-pick 4l and reflog 4m shipped, this is
the hardest remaining git item). Scoped deliberately: **reorder, drop, fixup**.
`reword` (message edit) and `edit` (mid-rebase amend) need a message editor and a
stateful pause and are out of scope.

## The mechanism: fleet as its own sequence editor

`git rebase -i <base>` opens a "todo" list in an editor (`GIT_SEQUENCE_EDITOR`)
and replays what the edited list says. To drive it non-interactively fleet must
BE that editor. The `Runner` has no env hook (see tier 4c), and embedding the
todo path literally in a `sequence.editor` config is quoting-fragile across
Windows/macOS/Linux. Instead:

- The rebase runs with `GIT_SEQUENCE_EDITOR="<fleet-exe> --rebase-seq"` and
  `FLEET_REBASE_TODO=<path to the todo fleet wants>`.
- git invokes `<fleet-exe> --rebase-seq <git's-todo-file>`. `main.go` recognizes
  the `--rebase-seq` sentinel (exactly like the existing agent-hook dispatch),
  reads `FLEET_REBASE_TODO`, writes its contents over git's todo file, and exits
  0 - all in Go, so no reliance on `cp`/`sh` or path quoting.
- `GIT_EDITOR=true` is set too: a safety no-op editor so no unexpected message
  prompt can hang the rebase (fixup discards messages, so none is expected).

Because this needs env and the executable path, the rebase runs via `exec` in a
dedicated `internal/git` function, not through `Runner`.

## 1. Go

- `git.RebaseAction{Hash, Op string}` where `Op` is `pick` | `fixup` | `drop`.
- `git.BuildRebaseTodo(actions []RebaseAction) string` (pure, unit-tested): emits
  one `<op> <hash>` line per non-drop action in order (`pick`/`fixup`), omits
  `drop`. Returns `""` when nothing survives (an all-drop list is invalid).
- `git.InteractiveRebase(dir, base, seqEditor string, actions []RebaseAction) error`:
  builds the todo, writes it to a temp file, runs
  `git -c ... rebase -i <base>` with the env above, and - like `integrateUpstream`
  / `CherryPick` - KEEPS a real conflict (returns `ErrConflict`, state left for
  the panel) while rolling back any other in-progress failure. `seqEditor` is the
  `GIT_SEQUENCE_EDITOR` command; production passes the fleet sentinel, tests pass
  a portable copy command so the driving mechanics are testable without the real
  binary.
- `main.go`: an `isRebaseSeq()` check before wails init; when set, copy
  `FLEET_REBASE_TODO` -> `os.Args[2]` and exit.

## 2. Bindings

```go
// RebaseCommits lists the last n commits (newest first) as candidates, plus the
// base sha (parent of the oldest) to rebase onto.
func (a *App) RebaseCommits(path string, n int) RebaseView
// InteractiveRebase applies the reordered/marked actions onto base.
func (a *App) InteractiveRebase(path, base string, actions []git.RebaseAction) string
```

`RebaseView{Base string; Commits []CommitView}`. `InteractiveRebase` builds the
sequence-editor command from `os.Executable()` + `--rebase-seq`, calls
`git.InteractiveRebase`, and returns `errMsg`; a conflict leaves the rebase in
progress and the CommitBox panel (tier 4c, already labels "rebase") takes over.

## 3. UI

A **Rewrite** control in the detail panel (beside Cherry-pick / History), a
picker modelled on the others:

- lists the last ~10 commits, newest at top, each with short hash + subject;
- per row: **up / down** to reorder, and an op selector **keep / fixup / drop**
  (fixup folds the row into the one above it);
- a confirm before running ("Rewrite these N commits? This changes history -
  only do it on commits you haven't pushed.") then `InteractiveRebase(path, base,
  actions)` where actions are in the displayed order;
- a conflict during replay hands off to the existing conflict panel.

Reorder is up/down buttons, not drag - reliable and testable. `base` is
`RebaseView.Base` (parent of the oldest listed commit).

## Testing

- **Go unit** (`internal/git/rebase_test.go`): `BuildRebaseTodo` emits the right
  lines in order, omits drop, returns "" for all-drop; a `pick` then `fixup`
  produces two lines.
- **Go unit** (`main_test.go` or a helper): the `--rebase-seq` copy - given
  `FLEET_REBASE_TODO` and a destination path, the contents are written.
- **Go integration** (`internal/git/rebase_test.go`, real git): with `seqEditor`
  set to a portable copy command, reordering two commits swaps them on HEAD;
  dropping a commit removes it; fixup squashes two into one (HEAD count drops by
  one, the fixup'd file content is present). A conflicting reorder returns
  `ErrConflict` and leaves `OperationInProgress() == "rebase"`.
- **Go** (`app_test.go`): `RebaseCommits` returns the last n commits and a base;
  `InteractiveRebase` binding round-trips a clean reorder.
- **DOM** (tier-4h harness): the Rewrite picker lists commits, up/down reorders,
  and confirming calls `InteractiveRebase` with the actions in display order.

## Out of scope

`reword` and `edit` (need the message editor / a mid-rebase pause), squashing
that keeps a combined message (fixup discards messages), rebasing onto a
different branch, `--onto`, autosquash, and drag-to-reorder. Reorder + drop +
fixup on the recent local commits, conflicts via the existing panel.
