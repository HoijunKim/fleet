# Tier 4g - Export Import Design

**Goal:** an exported fleet data file can be imported back, merging its projects
and intel into the local stores, and the import sticks across devices.

This is the second half of backlog item 4 (the first, restoring a single conflict
backup, shipped in tier 4f). It is the inverse of the existing `ExportData`
(`app.go`), which writes `{projects, intel}` to a JSON file the user picks.

## Merge policy: upsert, never delete, re-stamped

Import **upserts** by id: each record in the file overwrites the local record
with the same id, or is added when absent. A local record whose id is NOT in the
file is left untouched - import never deletes, so it can never tombstone data on
this device or, through sync, on any other. This is the least destructive policy
that still recovers overwritten data, and it matches the primary use case:
restoring a backup onto a fresh or damaged machine.

As in tier 4f'"'"'s restore, imported records are **re-stamped** to now (they are
written through `store.Update` / `intel.SetBrief` / `intel.SetChat`, all of which
set `UpdatedAt = now`). Without the re-stamp, an old export'"'"'s timestamps would
lose last-write-wins to whatever is on the server and the import would be silently
undone on the next sync. Re-stamping makes the imported copies win and re-push, so
the import becomes authoritative - which is what importing a backup means.

Because a re-stamped import re-pushes and can overwrite records edited on another
device since the export, the flow is gated by a confirm that shows counts.

## 1. Two-step flow

Bindings are stateless per call, so the pick+preview and the commit are separate,
with the path carried between them:

```go
type ImportSummary struct {
	Path              string `json:"path"`              // "" when the user cancelled the dialog
	Projects          int    `json:"projects"`          // records in the file
	ProjectsOverwrite int    `json:"projectsOverwrite"` // of those, ids already present locally
	Chats             int    `json:"chats"`
	ChatsOverwrite    int    `json:"chatsOverwrite"`
	Brief             bool   `json:"brief"`             // the file carries a non-empty brief
	Error             string `json:"error"`             // parse/shape failure, else ""
}

func (a *App) ImportPreview() ImportSummary          // OpenFileDialog, parse, count - NO writes
func (a *App) ImportCommit(path string) string       // re-read, upsert, trigger sync; errMsg
```

- **`ImportPreview`** opens a native open-file dialog (mirroring `ExportData`'"'"'s
  save dialog), reads and parses the chosen file, and returns the counts. A
  cancelled dialog returns `{Path: ""}` with no error. A file that is not valid
  JSON, or does not have the `{projects, intel}` shape, returns `Error`. It never
  writes.
- **`ImportCommit`** re-reads `path`, refuses up front if either store is degraded
  (so a half-import cannot happen), then upserts every project and chat and the
  brief (if present), and calls `triggerSync()`. Re-reading rather than caching
  the parse keeps the bindings stateless; the file is a local file the user just
  picked, so the TOCTOU window is irrelevant.

The shape parsed is exactly what `writeExport` writes:

```go
struct {
	Projects map[string]store.Record `json:"projects"`
	Intel    intel.Data              `json:"intel"`
}
```

## 2. Commit semantics

- **Degraded guard first.** If `a.store.Degraded() != nil` or
  `a.intel.Degraded() != nil`, return an error and write nothing. Importing into a
  read-only store would fail partway and leave a mix.
- **Projects:** for each `id, rec` in `Projects`,
  `a.store.Update(id, func(r *store.Record){ *r = rec })`. `Update` re-stamps and
  creates the key if absent. Local ids not in the file are untouched.
- **Brief:** import only when the file carries one (`Text != "" || UpdatedAt != ""`),
  via `a.intel.SetBrief(brief)` (re-stamps). An empty brief in the file does not
  wipe a local brief.
- **Chats:** for each `id, chat` in `Intel.Chats`, `a.intel.SetChat(id, chat.Turns)`
  (re-stamps, caps to 20). Upsert: overwrites a matching chat, adds a new one,
  leaves local-only chats alone.
- **Then `triggerSync()`** so the imported, re-stamped records re-push promptly.

`ImportPreview`'"'"'s `ProjectsOverwrite`/`ChatsOverwrite` counts are computed by
checking each file id against `a.store.Snapshot()` / the intel chat ids, so the
confirm can say how many existing records the import will replace.

## 3. UI

A "Import data (JSON)" button beside Export in Settings'"'"' Data section. Its
handler:

1. `const s = await ImportPreview()`.
2. If `s.path === ""` return (cancelled); if `s.error` toast it and return.
3. `confirm` with counts:
   > Import <projects> projects (<projectsOverwrite> replace existing) and
   > <chats> chats<, and the brief> from this file? Replaced records win on all
   > your devices.
4. On confirm, `ImportCommit(s.path)`, toast the outcome, and `onSaved()` so the
   parent rescans and the imported records appear.

## Testing

- **Go unit** (`app_test.go`):
  - `ImportCommit` upserts a project (an existing id'"'"'s record is replaced and its
    `UpdatedAt` is re-stamped newer than the file'"'"'s), adds a new id, and leaves a
    local-only id untouched - the no-delete guarantee.
  - Chats and a brief import; an empty brief in the file does not wipe a local one.
  - A degraded store makes `ImportCommit` return an error and write nothing (assert
    a local record is unchanged).
  - A malformed / wrong-shape file: `ImportPreview` on a temp file (via a seam that
    lets the test supply a path without the dialog - see below) returns `Error`;
    `ImportCommit` on it errors.
  - The preview counts: `ProjectsOverwrite` equals the number of file ids already
    in the store.
  - After an import, the engine'"'"'s next `SyncOnce` re-pushes an imported record
    (re-stamp proof), mirroring tier 4f'"'"'s re-push test.
- **Test seam:** the dialog cannot run headless, so factor the parse+count into an
  unexported `importSummary(path string) ImportSummary` and the write into an
  unexported `importCommit(path string) error`; `ImportPreview` calls
  `OpenFileDialog` then `importSummary`, and the tests drive `importSummary` /
  `importCommit` directly with a temp path.
- **Frontend** (`vitest`): the Data section renders an Import button; the handler
  calls `ImportPreview` then, on a stubbed confirm, `ImportCommit` with the
  previewed path. (SSR cannot reach the async-loaded Settings body, so this is a
  handler-level test if achievable, else the gate is `npm run check` + the Go
  tests, as in tier 4f.)

## Out of scope

Whole-store replacement (delete local ids absent from the file), field-level or
turn-level merge, an import that does not re-push (local-only resurrection),
undo/redo, and importing across accounts with id remapping. Import is upsert +
re-stamp, nothing more.
