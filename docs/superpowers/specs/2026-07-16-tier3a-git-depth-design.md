# Tier 3a - Git Depth Design

**Goal:** Bring the Git tab up to an everyday-complete workflow: stage/unstage
individual files, amend the last commit, apply/drop individual stashes, and
create/delete branches - without dropping to a terminal.

## Sub-slice 1: Per-file staging + amend (CommitBox)

**Status model.** A new binding surfaces per-file staged/unstaged state, kept
separate from the existing repo status (which stays a flat dirty list):

- `git.StatusFiles(r, dir) ([]FileStatus, error)` runs `git status --porcelain=v2`
  and returns, per changed file, `{Path, Staged, Unstaged}`:
  - `1 XY`/`2 XY`: `Staged = X != '.'`, `Unstaged = Y != '.'` (rename keeps the
    new path).
  - `? path`: untracked -> `Unstaged: true`.
  - `u ...`: unmerged -> `Unstaged: true, Staged: false` (must be resolved+staged
    before it can be committed).
- `App.StatusFiles(path) []FileStatusView` wraps it.

**Staging ops:**
- `git.StageFile(r, dir, file)` -> `git add -- <file>`
- `git.UnstageFile(r, dir, file)` -> `git restore --staged -- <file>`
- `git.CommitStaged(r, dir, msg)` -> `git commit -m <msg>` (index only; errors
  when nothing is staged)
- `git.CommitAmend(r, dir, msg)` -> `git commit --amend -m <msg>`
- `git.LastCommitMessage(r, dir) (string, error)` -> `git log -1 --pretty=%B`
- App bindings: `StageFile`, `UnstageFile`, `CommitStaged`, `CommitAmend`,
  `LastCommitMessage` (each returns "" / "error: ..."; LastCommitMessage returns
  the message string).

**UI (CommitBox):** the changed list becomes two groups - Staged and Unstaged -
each file with a toggle (unstaged: "+"/stage; staged: "-"/unstage). Buttons:
`Commit staged` (uses CommitStaged, enabled when something is staged), the
existing `Commit all` / `Commit all & push` (unchanged: CommitAll stages
everything), and an `Amend last commit` checkbox that, when ticked, prefills the
message from `LastCommitMessage` and routes commit through `CommitAmend`. Amend
carries a one-line "rewrites the last commit" caution.

## Sub-slice 2: Stash apply / drop (StashPanel)

- `git.StashApply(r, dir, i int)` -> `git stash apply stash@{i}` (keeps the
  entry, unlike pop)
- `git.StashDrop(r, dir, i int)` -> `git stash drop stash@{i}` (destructive)
- App bindings `StashApply(path, i)`, `StashDrop(path, i)`.
- UI: each listed entry gets `Apply` and `Drop` buttons; `Drop` needs a
  per-entry two-step confirm. The index is the entry's position in StashList
  (stash@{0} is newest).

## Sub-slice 3: Branch create / delete (BranchMenu)

- `git.CreateBranch(r, dir, name)` -> `git checkout -b <name>` (create + switch)
- `git.DeleteBranch(r, dir, name)` -> `git branch -d <name>` (safe delete; git
  refuses an unmerged branch and that error is surfaced verbatim)
- App bindings `CreateBranch(path, name)`, `DeleteBranch(path, name)`.
- UI: the branch popover gains a "+ New branch" row (text input -> create+switch)
  and a delete "x" on every non-current branch, with a per-branch confirm.

## Error handling & safety

- All bindings use the `errMsg` convention. File/branch/stash arguments are
  passed as argv elements to the runner (no shell), so no metacharacter risk.
- Destructive actions (stash drop, branch delete, amend) require an explicit
  confirm or checkbox; git's own refusals (unmerged branch, nothing staged) are
  surfaced as toasts.
- After any mutation the component reloads its own state and calls onChanged so
  the repo's git fields refresh.

## Testing

- **Backend integration** (real git, skips without git): StatusFiles reports
  staged vs unstaged correctly for a partially-staged file; StageFile/UnstageFile
  round-trip; CommitStaged commits only staged; CommitAmend rewrites HEAD;
  StashApply keeps the entry while StashDrop removes it; CreateBranch switches,
  DeleteBranch refuses an unmerged branch.
- **Backend unit**: LastCommitMessage via a fake runner; the porcelain v2 XY
  parse via a table test.
- **Frontend**: SSR tests that CommitBox renders staged/unstaged groups and the
  amend checkbox; StashPanel renders per-entry Apply/Drop; BranchMenu renders the
  new-branch row.
- **GUI**: stage a file and Commit staged; amend; stash apply + drop; create and
  delete a branch.

## Out of scope (later)

Interactive hunk staging, `--force` branch delete from the UI, cherry-pick,
interactive rebase, reflog.
