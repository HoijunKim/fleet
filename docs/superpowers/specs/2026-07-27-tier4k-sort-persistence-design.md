# Tier 4k - Persist the project-table sort

**Goal:** the column sort a user picks survives an app restart.

Backlog item (`specs/2026-07-01-fleet-gui-design.md:80-84`: "sort persistence").
Today `sortKey`/`sortDir` are in-memory App state (`App.svelte:36-37`), reset to
unsorted on every launch.

## Design

- A small helper `frontend/src/lib/sortPref.ts`, matching the codebase's
  `editorSelection.ts` / `ciBit.ts` pattern, owns the localStorage read/write:
  - `loadSortPref(): { key: string; dir: "asc" | "desc" }` - reads `fleet.sort`,
    returns `{ key: "", dir: "asc" }` when absent or malformed.
  - `saveSortPref(key, dir): void` - writes it; a `""` key (unsorted) is stored
    too, so clearing the sort also persists.
  - Both are SSR-safe (`typeof localStorage === "undefined"` guard) and never
    throw.
- `App.svelte` initializes `sortKey`/`sortDir` from `loadSortPref()` and calls
  `saveSortPref` at the end of `onSort` (`App.svelte:499-503`).
- The sort lives in `localStorage`, not the synced store: it is a per-device view
  preference, like `fleet.briefAutoDate`, and syncing it would fight a user who
  wants a different sort on each machine.

## Testing

- **Unit** (`sortPref.test.ts`, node env): round-trips a key/dir; a missing key
  yields the unsorted default; a malformed value yields the default without
  throwing; an empty key persists (clearing the sort is remembered).

## Out of scope

Persisting the filter chips, the selected row, scroll position, or column widths.
One preference: the sort key and direction.
