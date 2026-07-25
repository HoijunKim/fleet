# Tier 4f - Conflict Backup Restore Design

**Goal:** a record that sync overwrote or deleted can be restored from its backup,
in one click, and the restore sticks across devices.

This is backlog item 4 (the restore half; import stays backlogged). Tier 3d built
the backup and the reveal+list UI but stopped there: "Restoring a backup into the
store is deliberately out of scope: it needs a re-push story"
(`specs/2026-07-18-tier3d-local-data-integrity-design.md:126-127`). This tier is
that re-push story.

## The re-push problem, and its answer

`sync-conflicts.jsonl` holds, per clobbered record, `{at, localId, name, payload}`
where `payload` is the full `store.Record` as it was before sync overwrote or
deleted it. The naive restore - write the payload back with `store.Put` - fails:
`Put` keeps the payload's original `UpdatedAt`, which is OLDER than the server
copy that clobbered it. On the very next sync, LWW compares the two timestamps,
the server wins again, and the restore is silently re-clobbered.

The fix is to **re-stamp `UpdatedAt` to now** on restore. `store.Update` already
does exactly this (`store.go`: it sets `rec.UpdatedAt = now` after the mutator
runs), so restoring through `Update` makes the restored record newer than every
existing copy. The next sync sees a changed hash, pushes it, and LWW accepts it
on the server and on every other device. The restore becomes authoritative -
which is what "restore my version" means.

## 1. `RestoreBackup` binding

```go
func (a *App) RestoreBackup(localID, when string) string // errMsg
```

- Reads `sync-conflicts.jsonl` and finds the line whose `localId` and `at` both
  match the arguments. `(localId, at)` identifies one backup: the same record can
  be clobbered more than once, so each backup is a distinct line and the UI
  passes the pair it listed.
- Unmarshals that line's `payload` into a `store.Record`.
- Writes it with `a.store.Update(localID, func(r *store.Record){ *r = payload })`,
  which re-stamps `UpdatedAt`. `Update` creates the key if absent, so restoring a
  DELETED record re-creates it - the main use case.
- Calls `a.triggerSync()` so the restore propagates promptly rather than waiting
  for the next 60s tick.
- Returns an error string when no line matches, when the file is unreadable, or
  when the payload will not parse.

The backup log is append-only and is NOT modified by a restore: it stays a
historical record, and the restored line remains in the list (restoring twice is
harmless - it just re-stamps again).

`ConflictBackups` (the existing lister) is unchanged; it already returns
`localId` and `when` per row, which is exactly the pair `RestoreBackup` needs.

## 2. UI

In `SettingsModal.svelte`'s "Overwritten copies" list, each row gains a
**Restore** button next to the name/when. Because a restore replaces the current
record on every device, the button opens a confirm first:

> Restore "<name>"? This replaces the current version on all your devices with
> this saved copy.

On confirm it calls `RestoreBackup(b.localId, b.when)`, toasts the outcome, and
asks the parent to rescan so the restored record appears. The backups list is
left as is (the line is still a valid historical backup).

## Trade-off, stated

A restore overwrites the CURRENT record (the remote winner) without backing that
current version up in turn. This is deliberate: restore is an explicit action on
a clearly labelled "overwritten copies" list, gated by a confirm, and the whole
point is to make the saved copy win. Backing up the thing you are explicitly
discarding would only grow the log with copies no one asked to keep. A user who
restores the wrong line can restore a different one; the log is not consumed.

## Testing

- **Go unit** (`app_test.go`): write a `sync-conflicts.jsonl` with a known
  payload whose `UpdatedAt` is in the past; `RestoreBackup` writes the record and
  its `UpdatedAt` is now strictly newer than the backup's (the re-stamp - the
  correctness anchor). Restoring a `localId` absent from the store re-creates it.
  A `(localId, when)` that matches no line returns an error and writes nothing.
  Two backups for one `localId` at different `when`s restore independently.
- **Go unit**: after a restore, the engine's next `SyncOnce` pushes the restored
  record (a fake server records the push), proving the re-stamp actually makes it
  dirty and re-pushed rather than silently equal.
- **Frontend** (`vitest`): the Settings backup list renders a Restore button per
  row; clicking it (with the confirm stubbed) calls `RestoreBackup` with that
  row's `localId`/`when`.

## Out of scope

Importing an export (backlog item 4's other half - needs a merge policy and a
destructive-action confirmation of its own), backing up the current record a
restore discards, a general undo/redo, and restoring intel (brief/chat) backups -
intel is not backed up to `sync-conflicts.jsonl` (its sources set
`BacksUpClobbered() == false`), so there is nothing to restore there.
