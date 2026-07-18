# Tier 3d - Local Data Integrity Design

**Goal:** fleet must never destroy work it did not create. A file it could not
parse is quarantined rather than overwritten; a store that failed to load can
never tombstone the cloud; a record deleted by a remote tombstone is backed up
the way a clobbered edit already is; every backup it writes is reachable; and a
merge the user started in a terminal is never aborted by fleet.

The five items are one causal chain. A corrupt `projects.json` today loads as an
empty-but-writable store (`store.go:53,57`, error discarded at `app.go:110`); the
first user edit calls `saveLocked` and destroys the on-disk copy; within 60s the
sync engine reads that empty snapshot and tombstones every doc
(`engine.go:158-163`), wiping the server and every other device while the pill
still shows green. Fixing the store alone leaves the tombstone amplifier; fixing
the engine alone still loses the local file.

## 1. Degraded-load detection: quarantine, refuse to overwrite, say so

`internal/store/store.go`:
- Add `loadErr error` to `Store`, set on the two failure returns in `Open`
  (`:53` non-NotExist read error, `:57` unmarshal error).
- `saveLocked` (`:159`) returns `loadErr` immediately when set, so `Put`/`Update`/
  `Delete` fail loudly instead of writing an empty map over real data. It is
  ALREADY atomic (temp+rename at `:167-171`) - gate it, do not rewrite it.
- Add `Degraded() error` and `DiscardAndReset() error` (clears `loadErr` after an
  explicit user opt-in, then saves the empty map).

`internal/fileguard` (new, tiny): `Quarantine(path) (string, error)` renames a
file to `<name>.corrupt-<RFC3339 with : stripped>` and returns the new path. One
implementation shared by all three loaders so the bytes are never lost.

- `store.Open` quarantines on unmarshal failure.
- `edges.Open` (`internal/edges/edges.go:36-51`) stops returning `(s, nil)` on
  both error paths: quarantine and return the error.
- `config.loadFrom` (`:99-101`) quarantines before returning `Default()`, so the
  file holding `OpenAIKey`/`GeminiKey`/`NotionToken` - which exist nowhere else,
  not in the keychain (`app.go:115` stores only the sync refresh token), not in
  `ExportData` (`app.go:740`) - survives a decode error.

`internal/config/config.go`: `Save` (`:120-130`) is the one persister that is not
atomic - `os.Create(p)` (`:124`) truncates the live file in place, so an unclean
shutdown mid-save is itself a way to produce the corrupt file. Encode to
`p+".tmp"`, `Sync()`, `os.Rename` - matching `store.go:167-171`,
`edges.go:155-159`, `syncengine/state.go:61-65`.

`app.go`: stop discarding the loader errors at `:107`, `:110`, `:112`. Keep them
on the App struct (set in `NewApp`) and add bound methods:
- `StartupHealth() []HealthIssue` - `{Scope, Path, Error, Quarantined}` per
  failed loader.
- `DiscardCorruptStore() string` - explicit opt-in reset.
- `RevealDataDir() string` - wraps the existing `RevealInExplorer` (`:469`) on
  `a.dataDir` (`:127`), which is Go-only today.

`frontend/src/lib/StartupBanner.svelte` (new): non-dismissible banner above the
toolbar when `StartupHealth()` is non-empty, naming the quarantined file, with
"Reveal folder" and an armed-confirm "Start fresh".

## 2. Sync refuses to tombstone from a degraded or implausibly empty store

`internal/syncengine/engine.go`:
- Add a `degraded func() error` field to `Engine` and to `New`, wired at
  `app.go:117` from `st.Degraded`.
- In `SyncOnce`, immediately after the snapshot (`:121`) and before the dirty and
  tombstone loops, return `ErrLocalDataUnsafe` when `degraded() != nil`, or when
  `len(snap) == 0 && len(e.state.Docs) > 0` - the implausible-wipe heuristic
  covers the empty-but-not-corrupt case. Abort the whole cycle before any Push;
  do not attempt a partial push/pull.

`authsync.go`:
- `runSync` (`:376-387`): map `ErrLocalDataUnsafe` to a new `"paused"` pill state
  with the reason text, checked BEFORE the `isOffline` branch.
- `syncLoop` (`:458`): add it to the reset-backoff classification alongside
  `errNotSignedIn`/`ErrRefreshFailed`. Otherwise the capped exponential backoff
  (`:463-464`) hammers a condition that cannot resolve without user action.

`frontend/src/lib/SyncPill.svelte`: a `paused` branch placed BEFORE the `{:else}`
at `:45` - that fallback renders "Sign in to sync", which would be actively
wrong. Copy: "Sync paused: local data unreadable", `dot-warn`, no Retry.

## 3. Back up records deleted by a remote tombstone

`engine.go:216-224` hashes the local record against `state.Docs[].Hash`, calls
`backupConflict`, and sets `lostLocalEdit` before overwriting. The sibling branch
at `:207-210` is a bare `e.store.Delete(localID)` with no hash check and no
backup, so a project deleted on device B silently erases notes and tasks written
offline on device A. `store.Delete` (`:150-155`) is a hard delete with no
tombstone and no undo path anywhere in the repo.

In the `d.Deleted` branch, look up `snap[localID]`; when present, marshal it,
call the existing `e.backupConflict(localID, local, lp)` (`:86-102`) and set
`e.lostLocalEdit = true` before `e.store.Delete` - reusing the exact machinery
from `:219-224`.

`authsync.go:391-398` already converts `lostLocalEdit` into the `sync:conflict`
event, so the toast fires for free. Pass a discriminator on the payload
(currently `nil` at `:395`) so `App.svelte:701-702` can say "deleted on another
device" rather than "overwritten by".

## 4. Make sync-conflicts.jsonl reachable and bounded

`backupConflict` writes a genuinely useful backup to a path derived at
`engine.go:79-81`, but the only surface is a `toastError` naming a bare filename
with a 5s ttl, in a directory the app never discloses. Items 1 and 3 both make
this file more important.

- `engine.go:95-101`: `os.Stat` before the append; rotate to
  `sync-conflicts.1.jsonl` past ~1MB.
- `app.go`: `ConflictBackups() []ConflictView{Index, LocalID, Name, When}`
  reading the JSONL from `a.dataDir`; `RevealDataDir()` shared with item 1.
- frontend: sticky ttl on the `sync:conflict` toast with a "Show backup" action
  calling `RevealDataDir`; an "Overwritten copies" list under the Export data
  button in `SettingsModal.svelte`.

Restoring a backup into the store is deliberately out of scope: it needs a
re-push story. Reveal + list is what removes the dead end.

## 5. Stop destroying an in-progress merge or rebase fleet did not start

`internal/git/ops.go:139` runs `<mode> --abort` on EVERY non-conflict failure of
`git <mode> @{u}`, and the comment at `:136-137` calls this "harmless when
nothing is in progress" - untrue when something IS. A user who started a merge in
a terminal and fully resolved it still sees the Diverged banner (ahead/behind
come from `# branch.ab`, unchanged mid-merge), is one click from
`DetailPanel.svelte:371`, and their resolved-but-uncommitted merge is thrown
away. The fully-resolved case takes the empty `ls-files -u` path, so it is `:139`
that does the damage.

- Add `OperationInProgress(r Runner, dir string) (string, error)` reading
  `.git/MERGE_HEAD`, `.git/rebase-merge`, `.git/rebase-apply` -> `"merge"` /
  `"rebase"` / `""`.
- `integrateUpstream` checks it FIRST and returns a clear error without running
  git at all; then delete the unconditional `--abort` at `:139`, keeping only the
  conflict-path abort at `:133` (fleet's own operation, safe to unwind). Rewrite
  the doc comment at `:119-124`, which documents the removed behavior as
  intentional.
- `app.go`: bind `GitOperation(path) string` next to `MergeUpstream`/
  `RebaseUpstream`.
- `DetailPanel.svelte`: gate the diverged banner on no operation in progress,
  replacing it with a "Merge in progress - finish it in a terminal" notice, and
  fix the hint that currently reads "Conflicts abort safely".

EXPLICITLY NOT IN SCOPE: `MergeContinue`/`MergeAbort`/`RebaseContinue`/
`RebaseAbort` bindings, per-file ours/theirs resolution, the `CommitBox` conflict
UI. That L is the anchor of a later git-recovery tier.

## Testing

- **Go unit**: `store_test.go` - `TestOpenCorruptReturnsEmptyAndError` (`:58`)
  currently locks in the lossy behavior and is rewritten to assert quarantine +
  save refusal; `config_test.go` - corrupt-file quarantine and atomic-save cases;
  `edges_test.go` - corrupt file returns an error and is quarantined;
  `engine_test.go` - degraded store and empty-snapshot-with-tracked-docs both
  push zero `Deleted` docs; a tombstone over a locally-edited record writes a
  backup line; `integrate_test.go` - a pre-existing `MERGE_HEAD` is NOT aborted.
- **Frontend unit**: `StartupBanner` renders the issue and both actions; the
  `paused` pill branch renders its copy and no Retry button.
- **GUI**: corrupt `projects.json` by hand, launch, confirm the banner, the
  paused pill, and that editing a task fails loudly rather than writing.

## Out of scope

Restoring a conflict backup into the store; importing an export; a general undo/
trash for deleted projects; the git conflict-resolution UI (item 5's deferred L);
self-healing `loadState`; a force-full-resync repair button.
