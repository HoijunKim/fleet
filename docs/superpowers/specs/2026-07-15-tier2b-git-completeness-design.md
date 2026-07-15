# Tier 2b - Git Completeness Design

**Goal:** Close the three Git-tab gaps so the desktop UI covers the everyday
git loop without dropping to a terminal: push, integrate a diverged upstream,
and read the full working-tree diff.

## Scope

Three sub-features, all in the code-project Git tab (`DetailPanel.svelte`, the
`activeTab === "git"` branch) plus their backing bindings.

### F1 - Standalone Push

- Add a `Push` button to the Git-tab actions row (currently Fetch / Pull /
  Editor / Terminal). `App.Push` already exists; only the UI is missing.
- Enabled only when `project.hasUpstream`. When `project.ahead > 0` the label
  shows the count ("Push 3"); otherwise "Push" (a no-op push is harmless).
- Click -> `App.Push(path)` -> toast success/error -> `onRepoChanged(path)`.

### F2 - Diverged pull: Merge / Rebase

- Divergence is detected client-side: `hasUpstream && ahead > 0 && behind > 0`
  (both counts come from `git status` porcelain, refreshed by Fetch/Pull).
- When diverged, the Git tab shows an inline banner with the counts, a one-line
  explanation, and two buttons: **Merge** and **Rebase**. Pull stays `--ff-only`
  (unchanged) - the banner is the diverged path.
- New git ops (`internal/git/ops.go`):
  - `MergeUpstream(r, dir) error` -> `git merge @{u}`
  - `RebaseUpstream(r, dir) error` -> `git rebase @{u}`
  - Shared helper: on failure, if `git ls-files -u` reports unmerged paths it is
    a **conflict** -> run `<mode> --abort` and return a conflict error
    ("merge conflict: local and remote changes overlap; resolve in a terminal").
    Any other failure is surfaced verbatim after a defensive `--abort`
    (harmless when nothing is in progress). **Never leave a half-done
    merge/rebase** - a GUI user cannot resolve it in-app.
- New bindings: `App.MergeUpstream(path) string`, `App.RebaseUpstream(path) string`.
- UI handlers `doMerge` / `doRebase` -> binding -> toast -> `onRepoChanged`.

### F3 - View all changes (combined diff)

- `DiffModal` already renders a colored, hunk-aware per-file diff. Add a whole
  working-tree view reusing the same modal.
- New binding `App.DiffAll(path) string` -> `git.Diff(runner, path)`
  (`git diff HEAD`, already capped at 12000 bytes).
- `DiffModal` gains an `all` prop: when true it loads `DiffAll(path)` instead of
  `DiffFile(path, file)` and titles itself "All changes".
- `DetailPanel` replaces the `diffFile: string | null` state with
  `diffView: { file: string; all: boolean } | null`. A "View all changes"
  button appears next to the Changed-files list when `dirtyFiles.length > 0`.

## Error handling

- All bindings return `""` on success or an `error: ...` string (existing
  `errMsg` convention); the UI toasts non-empty results.
- Merge/rebase conflicts auto-abort and return a human message pointing at the
  Terminal button. Non-conflict git failures (dirty tree, no upstream) surface
  git's own diagnostic.

## Testing

- **Backend integration** (`internal/git/integrate_test.go`, skips without git):
  build a bare remote + clone, create divergence, assert `MergeUpstream` and
  `RebaseUpstream` succeed on a non-conflicting diverge, and that a conflicting
  diverge returns an error AND leaves the tree clean (no `ls-files -u` entries,
  no in-progress merge/rebase).
- **Backend unit** (`app_test.go`): `DiffAll` returns the runner's diff; `Push`
  returns "" on success and error text on failure (fakeRunner / errRunner).
- **Frontend** (`DiffModal.test.ts`): SSR asserts the title is "All changes"
  when `all` is set, and the file name otherwise.
- **GUI** (CopyFromScreen): Push button state, the diverged banner with
  Merge/Rebase, and the combined-diff modal.

## Out of scope (Tier 3)

Staging/unstaging individual hunks, amend, cherry-pick, interactive rebase,
conflict resolution inside the app.
