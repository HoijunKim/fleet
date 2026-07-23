# Tier 4c - Git Conflict Recovery Implementation Plan

**Goal:** a conflicted merge or rebase can be finished inside fleet.

**Architecture:** four new functions in `internal/git` (inspect, resolve,
continue, abort), one behavior change in `integrateUpstream` (keep a real
conflict instead of aborting it), four bindings, and a conflict block in
`CommitBox`. The one rule that must not be got wrong lives in Go, tested:
during a rebase `--ours`/`--theirs` are swapped relative to a merge.

**Tech Stack:** Go 1.22, Svelte 5, wails v2.12.0.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-24-tier4c-conflict-recovery-design.md`.
- `Runner.Run(dir, args...)` has no env hook, so suppress editors with
  `-c core.editor=true` before the subcommand. Do not change `Runner`.
- Every new git function takes `r Runner` first, matching `ops.go`.
- Tests use `ExecRunner` against real repos built by the existing helpers in
  `internal/git/integrate_test.go` - no mock runner for conflict behavior.
- Do NOT run `gofmt -w` across the tree (CRLF working copy). Check touched files
  with `git show HEAD:<file> | gofmt -d` and expect zero bytes.
- Regenerate wails bindings with `wails generate module` after adding bindings;
  `frontend/wailsjs/` is committed.
- Conventional Commits, no trailers. Keep the branch green on `desktop.yml`.

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/git/conflict.go` (create) | `Conflict`, `Conflicts`, `ResolveConflict`, `ContinueOperation`, `AbortOperation`, `ErrConflict`. |
| `internal/git/conflict_test.go` (create) | Real-repo tests for all of the above. |
| `internal/git/ops.go` (modify) | `integrateUpstream` keeps a real conflict; returns `ErrConflict`. |
| `internal/git/integrate_test.go` (modify) | Rewrite the two "conflict aborts" tests; keep the hook-rollback case. |
| `app.go` (modify) | `Conflicts`/`ResolveConflict`/`ContinueOperation`/`AbortOperation` bindings + `ConflictView`. |
| `app_test.go` (modify) | Binding-level test that a conflicted repo round-trips. |
| `frontend/src/lib/CommitBox.svelte` (modify) | Conflict banner, per-row resolution, Continue/Abort. |
| `frontend/src/lib/CommitBox.test.ts` (modify) | Continue disabled while a conflict remains. |
| `frontend/src/lib/DetailPanel.svelte` (modify) | The diverged hint no longer claims conflicts are rolled back. |
| `CHANGELOG.md` (modify) | `[Unreleased]` gains the capability. |

---

### Task 1: Conflict inspection

- [ ] **Step 1: Failing test** - `internal/git/conflict_test.go`, using a helper
that builds a repo with a chosen conflict shape. Cases: `both-modified`,
`both-added`, `deleted-by-us`, `deleted-by-them`. Assert one entry per path (not
three, one per stage) and the paths sorted.

- [ ] **Step 2: Run it** - fails to build (`undefined: Conflicts`).

- [ ] **Step 3: Implement** `Conflicts` in `internal/git/conflict.go`: run
`ls-files -u`, parse `<mode> <sha> <stage>\t<path>`, group stages per path, map
the stage set to a kind per the spec table.

- [ ] **Step 4: Green** - `go test ./internal/git/ -run TestConflicts -v`

- [ ] **Step 5: Commit** - `feat(git): report unmerged paths with their conflict kind`

### Task 2: Resolution

- [ ] **Step 1: Failing test** - the important one first:
`TestResolveConflictMineInRebaseKeepsLocalWork` builds a rebase conflict,
resolves `side="mine"`, and asserts the file holds the LOCAL commit's content.
A naive `--ours` mapping passes the merge test and fails this one.
Also: the merge counterpart both ways, `"worktree"` staging a hand-edited file,
and `deleted-by-us` + `"mine"` leaving the path staged as a deletion.

- [ ] **Step 2: Run it** - fails to build.

- [ ] **Step 3: Implement** `ResolveConflict(r, dir, mode, file, side)`:
look up the kind via `Conflicts`, pick the flag from the mode/side table, and
either `checkout <flag> -- file` + `add -- file`, or `rm -- file` when the chosen
side is the deleting one. `side="worktree"` is `add -- file` alone.

- [ ] **Step 4: Green** - `go test ./internal/git/ -run TestResolve -v`

- [ ] **Step 5: Commit** - `feat(git): resolve a conflicted file to either side, rebase-aware`

### Task 3: Continue / abort

- [ ] **Step 1: Failing test** - `ContinueOperation` errors while a conflict
remains and names the file; after resolving it finishes the merge and leaves a
two-parent commit; `AbortOperation` restores the pre-merge HEAD and clears
`OperationInProgress`; both error with "no merge or rebase in progress" on a
clean repo.

- [ ] **Step 2: Run it** - fails to build.

- [ ] **Step 3: Implement** both, reading the mode from `OperationInProgress`
and passing `-c core.editor=true` on the continue path.

- [ ] **Step 4: Green** - `go test ./internal/git/ -run "TestContinue|TestAbort" -v`

- [ ] **Step 5: Commit** - `feat(git): continue or abort an in-progress merge/rebase`

### Task 4: Stop rolling back real conflicts

- [ ] **Step 1: Rewrite the existing tests** - `TestMergeUpstreamConflictAborts`
and its rebase twin become `...LeavesConflict`: after the call,
`OperationInProgress()` is the mode, `Conflicts()` is non-empty, and the error
satisfies `errors.Is(err, ErrConflict)`. Keep
`TestMergeUpstreamRollsBackOnHookFailure` (the no-unmerged-paths case) asserting
the rollback still happens - that is the regression this task can cause.

- [ ] **Step 2: Run** - the rewritten tests fail; the hook test passes.

- [ ] **Step 3: Implement** - in `integrateUpstream`, branch on `ls-files -u`:
non-empty means leave the tree and return `ErrConflict` (wrapped with the mode);
empty means keep today's `<mode> --abort` + git's diagnostic.

- [ ] **Step 4: Green** - `go test ./internal/git/ -v` in full.

- [ ] **Step 5: Commit** - `feat(git): keep a real merge conflict instead of aborting it`

### Task 5: Bindings

- [ ] **Step 1: Failing test** in `app_test.go` - a conflicted temp repo:
`Conflicts(path)` returns one view carrying the mode and kind; `ResolveConflict`
then `ContinueOperation` return `""` and leave the repo clean.

- [ ] **Step 2: Implement** the four bindings and `ConflictView` (`Path`, `Kind`,
`Mode`, plus the two button labels computed in Go so the frontend never
re-derives the ours/theirs swap).

- [ ] **Step 3: Regenerate bindings** - `wails generate module`, commit the
`frontend/wailsjs/` diff.

- [ ] **Step 4: Green** - `go test .` and `go vet ./...`

- [ ] **Step 5: Commit** - `feat(app): bindings for conflict resolution and finishing a merge`

### Task 6: The UI

- [ ] **Step 1: Failing frontend test** - extend `CommitBox.test.ts`: with a
conflicted file present the Continue button is disabled; with none it is enabled.

- [ ] **Step 2: Implement** in `CommitBox.svelte` per spec section 6 - banner
with file count, Continue (disabled while any conflict remains) and Abort,
per-row Mine/Incoming/Resolved using the labels the binding supplies, and a
reload after each resolution.

- [ ] **Step 3: Fix the stale claim** - `DetailPanel.svelte:416` no longer says a
conflict is rolled back; it says fleet leaves it and offers resolution.

- [ ] **Step 4: Green** - `npm run check && npm test` in `frontend/`.

- [ ] **Step 5: Commit** - `feat(ui): resolve conflicts and finish a merge from the commit panel`

### Task 7: Verify and ship

- [ ] **Step 1** - `cd frontend && npm ci && npm run build && cd .. && go build ./... && go vet ./... && go test ./...`
- [ ] **Step 2** - gofmt diff on every touched Go file's LF blob: zero bytes.
- [ ] **Step 3** - `CHANGELOG.md` `[Unreleased]` gains a line under Added.
- [ ] **Step 4** - push, confirm the three `desktop` checks are green, open a PR,
merge once green.
