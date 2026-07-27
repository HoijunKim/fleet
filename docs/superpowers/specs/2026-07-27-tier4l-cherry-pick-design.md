# Tier 4l - Cherry-pick a commit

**Goal:** apply a commit from another branch onto the current branch, from the
UI, with conflicts handled by the panel tier 4c already built.

Backlog item (git-recovery cluster, `specs/2026-07-16-tier3a-git-depth-design.md:84-87`).
The most self-contained of that cluster, and it reuses almost all of tier 4c's
conflict machinery.

## Why the conflict infra reuses cleanly

`git cherry-pick` leaves the same kind of in-progress state a merge does when it
conflicts, and its ours/theirs are oriented like a merge (HEAD is "ours", the
picked commit is "theirs"), not like a rebase. So:

- `OperationInProgress` (`ops.go`) recognizes MERGE_HEAD / rebase-merge /
  rebase-apply today. Adding `CHERRY_PICK_HEAD -> "cherry-pick"` makes the rest
  follow: `ContinueOperation`/`AbortOperation` already read the mode from that
  marker and run `<mode> --continue` / `<mode> --abort`, which are valid
  cherry-pick subcommands.
- `checkoutFlag` (`conflict.go`) swaps ours/theirs only when `mode == "rebase"`;
  cherry-pick falls through to the merge mapping, which is correct.
- `ContinueOperation` passes `-c core.editor=true`, so `cherry-pick --continue`
  keeps the picked commit's message without opening an editor.

The only frontend change the reuse needs is generalizing the CommitBox banner and
toasts, which hardcode "Merge"/"Rebase" (`CommitBox.svelte:76-97,232`), to show
the actual mode.

## 1. Go

- `git.CherryPick(r Runner, dir, hash string) error` runs `cherry-pick <hash>`.
  Like `integrateUpstream`, a real conflict is KEPT (state left in place) and
  returned as `ErrConflict`; any other failure that leaves a cherry-pick in
  progress is rolled back with `cherry-pick --abort`; a clean pick returns nil.
- `git.LogRef(r Runner, dir, ref string, n int) ([]repo.Commit, error)` - the
  existing `Log` restricted to a ref (`log <ref> -n ...`), so a source branch's
  recent commits can be listed. `Log` becomes `LogRef(r, dir, "HEAD", n)` or
  stays and shares the parse.
- `OperationInProgress` gains the `CHERRY_PICK_HEAD` marker.

## 2. Bindings

```go
func (a *App) CherryPick(path, hash string) string   // errMsg; ErrConflict maps to a message the panel recognizes
func (a *App) LogRef(path, ref string, n int) []CommitView
```

`CherryPick` returns `""` on a clean pick and an error string otherwise; a
conflict is not an error string the caller toasts - the UI re-checks
`GitOperation(path)` after the call and lets the conflict panel take over, the
same flow the diverged Merge/Rebase buttons already use.

## 3. UI

A **Cherry-pick** control in the DetailPanel git area opens a small picker:

- a branch dropdown (from `Branches`, excluding the current branch),
- that branch's recent commits (from `LogRef`), each showing short hash +
  subject,
- clicking a commit calls `CherryPick(path, hash)` and, on a conflict, the
  existing CommitBox conflict panel takes over (its banner now reads
  "Cherry-pick in progress").

`CommitBox` gets an `opLabel(mode)` helper (`merge`->"Merge", `rebase`->"Rebase",
`cherry-pick`->"Cherry-pick") used in the banner and the continue/abort toasts,
replacing the `mode === "rebase" ? ... : ...` ternaries.

## Testing

- **Go** (`internal/git`): a clean cherry-pick applies the commit (its file
  content appears on the current branch, HEAD advances); a conflicting
  cherry-pick returns `ErrConflict`, leaves `OperationInProgress() == "cherry-pick"`
  and unmerged paths; `ContinueOperation` after resolving finishes it;
  `AbortOperation` restores the pre-pick HEAD; `LogRef` lists a named branch's
  commits, not HEAD's.
- **Go** (`app_test.go`): `CherryPick` binding round-trips a clean pick; a bad
  hash returns an error string.
- **DOM** (tier-4h harness): the cherry-pick picker lists a branch's commits and
  clicking one calls `CherryPick` with that hash; a CommitBox conflict test that
  the banner reads "Cherry-pick in progress" when `GitOperation` returns
  `cherry-pick`.

## Out of scope

Range cherry-pick (`A..B`), `-x` provenance line, mainline selection (`-m`) for
merge commits, `cherry-pick --skip`, and cherry-picking across repositories. One
commit, onto the current branch, with continue/abort.
