# Tier 4m - Reflog recovery

**Goal:** get back to a previous HEAD - after a bad reset, rebase, or a deleted
branch - by picking a reflog entry, from the UI.

Backlog item (git-recovery cluster, `specs/2026-07-16-tier3a-git-depth-design.md:84-87`).
The reflog is git's safety net; fleet has had no surface for it.

## Safety: refuse a destructive restore, don't force it

Restoring to a reflog entry means moving the current branch to that commit with
`git reset --hard`, which discards the working tree. The reversible half is the
branch move: the commits left behind stay reachable through the reflog, so a user
who jumps back too far can jump forward again. The irreversible half is losing
UNCOMMITTED changes to `--hard`.

So the restore **refuses when the working tree is dirty** ("commit or stash your
changes first"), and on a clean tree proceeds behind a confirm. This makes the
one destructive outcome impossible while keeping the common recovery case (a
clean tree, lost commits) one click away.

## 1. Go

- `git.Reflog(r Runner, dir string, n int) ([]ReflogEntry, error)` parses
  `git reflog -n <n> --format=%H%x1f%gd%x1f%gs%x1f%cI` into
  `ReflogEntry{Hash, Ref, Subject, When}` (Ref is `HEAD@{k}`, Subject is git's
  action text like "reset: moving to HEAD~2").
- `git.ResetHard(r Runner, dir, ref string) error` runs `reset --hard <ref>`.

## 2. Bindings

```go
func (a *App) Reflog(path string, n int) []ReflogView
func (a *App) RestoreReflog(path, ref string) string // errMsg
```

`RestoreReflog` checks the working tree first via `git.StatusFiles`: if any file
is listed (staged, unstaged, or conflicted) it returns an error and does NOT
reset, so uncommitted work is never discarded. On a clean tree it runs
`ResetHard(ref)` and returns `errMsg`.

## 3. UI

A **History** control in the DetailPanel git area (beside Cherry-pick), a picker
modelled on `CherryPickMenu`:

- lists the reflog entries (each: the `HEAD@{k}` selector + short hash +
  subject),
- clicking one confirms ("Move the current branch to this point? Commits after it
  stay recoverable in the reflog.") then calls `RestoreReflog(path, ref)`,
- a dirty-tree refusal surfaces as an error toast telling the user to stash or
  commit first; the picker stays open.

Restoring by the `HEAD@{k}` ref (not the bare hash) is what the entry means -
"put me back where I was k moves ago" - and reads correctly even when several
entries share a hash.

## Testing

- **Go** (`internal/git`): `Reflog` returns the HEAD-movement list newest-first
  after a couple of commits + a reset; `ResetHard` moves the branch (a file from
  a later commit disappears, HEAD matches the target); a commit "lost" to a reset
  is reachable again after restoring its reflog entry.
- **Go** (`app_test.go`): `RestoreReflog` refuses on a dirty tree (an uncommitted
  file is left untouched and the error is returned) and succeeds on a clean one.
- **DOM** (tier-4h harness): the History picker lists reflog entries and clicking
  one (confirm stubbed) calls `RestoreReflog` with that entry's ref.

## Out of scope

`reset --soft` / `--mixed`, reflog expiry/gc, another branch's reflog (HEAD only),
restoring a single file from a reflog entry, and an "undo the last fleet git
action" shortcut. One reflog entry, `--hard`, refused when dirty.
