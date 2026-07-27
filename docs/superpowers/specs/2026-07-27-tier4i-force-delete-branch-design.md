# Tier 4i - Force-delete an unmerged branch

**Goal:** delete a branch git's safe delete refuses, from the UI, without a
terminal.

Backlog item (`docs/superpowers/BACKLOG.md`: "`--force` branch delete from the
UI", deferred by tier 3a). Today `git.DeleteBranch` runs `branch -d`, which
refuses an unmerged branch; that refusal is a dead end - the only way forward is
a terminal.

## Design

- `git.DeleteBranchForce(r Runner, dir, name string) error` runs `branch -D`.
  It is separate from `DeleteBranch` (not a bool flag) so the two call sites read
  plainly and the force path is impossible to reach by accident.
- `App.DeleteBranchForce(path, name string) string` mirrors `DeleteBranch`
  (`app.go:692`): trims the name, refuses empty, returns `errMsg`.
- **UI (`BranchMenu.svelte`):** force is offered ONLY after the safe delete
  refuses because the branch is unmerged. `deleteBranch` already catches the
  error; when that error contains git's "not fully merged" wording, the row shows
  a **force delete** affordance instead of a bare toast. Clicking it calls
  `DeleteBranchForce` behind its own confirm ("Delete unmerged branch <b>? Its
  commits will be lost."). A force delete is never the default and never shown
  until git itself has refused the safe one.

## Testing

- **Go** (`internal/git`): create a branch with a commit not on the current
  branch; `DeleteBranch` returns an error (unmerged); `DeleteBranchForce` removes
  it and it is gone from `Branches`.
- **Go** (`app_test.go`): `DeleteBranchForce("", "")`-style guards - an empty name
  is refused without shelling out.
- **DOM** (`BranchMenu.dom.test.ts` under the tier-4h harness): a safe delete that
  returns a "not fully merged" error surfaces a force control; clicking it (confirm
  stubbed) calls `DeleteBranchForce` with the branch name.

## Out of scope

Deleting the current branch (git refuses it regardless), remote-branch deletion,
and a multi-select bulk delete. One unmerged local branch, forced, after the safe
path is refused.
