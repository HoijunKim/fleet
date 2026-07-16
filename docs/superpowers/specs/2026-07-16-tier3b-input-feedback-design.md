# Tier 3b - Input & Feedback Polish Design

**Goal:** Three small correctness/UX gaps: suggest existing tags, validate
settings before they take effect, and let the user cancel (and see the progress
of) an in-flight GitHub sign-in.

## 1. Tags autocomplete

- App already holds every project (`projects`). Derive the sorted, de-duped set
  of all tags in use and thread it down as an `allTags: string[]` prop:
  App -> DetailPanel -> `TagChips` (and PMSection's TagChips).
- `TagChips` renders a `<datalist>` of `allTags` (minus the project's current
  tags) bound to its add-input via `list=`. Purely additive; no backend change.

## 2. Settings validation

- New binding `App.DirExists(path string) bool` (`os.Stat` + `IsDir`).
- `SettingsModal`:
  - Numeric fields (Scan depth, Auto-fetch minutes) are clamped to `>= 0` on
    save (a negative or blank value becomes 0).
  - Each configured Root is checked with `DirExists`; a missing one gets a
    "not found" badge. Missing roots do NOT block save (a root may be a drive
    that is temporarily absent) but are surfaced.
  - Saving with an empty Roots list shows an inline note (nothing to scan).

## 3. OAuth cancel + progress

- `App` gains an `authCancel chan struct{}` (buffered, size 1). `AuthStart`'s
  `select` adds a `case <-a.authCancel` arm that returns the sentinel
  `"cancelled"`. A fresh channel is installed at the top of `AuthStart` so a
  stale cancel can't abort a later attempt.
- New binding `App.CancelAuth()` does a non-blocking send on `authCancel`.
- `SignIn`/`App`: while `authBusy`, show a "Waiting for browser..." state with a
  Cancel button that calls `CancelAuth`. A `"cancelled"` result is treated as a
  soft outcome (no error toast).

## Error handling

- `DirExists` returns false for any stat error (missing, permission) - it is a
  hint, never a hard gate.
- `CancelAuth` is a no-op if no sign-in is in flight (non-blocking send).
- `"cancelled"` is distinguished from real errors so the UI stays quiet.

## Testing

- **Backend**: `DirExists` true for a temp dir, false for a missing path;
  `AuthStart` returns `"cancelled"` when `CancelAuth` fires before the callback
  (drive it with a goroutine that calls CancelAuth, asserting the sentinel
  without a real browser/loopback exchange - refactor the wait+cancel select
  into a testable helper if needed).
- **Frontend**: a `pm`-style unit test for the all-tags derivation; SSR test
  that `TagChips` emits a `<datalist>` with the suggested tags and that
  `SettingsModal`/`SignIn` render the new controls where feasible.
- **GUI**: tag suggestion dropdown; a missing-root badge; the Cancel button
  during sign-in.

## Out of scope

Tag rename/merge across projects, full config schema validation, remembering the
last-used sign-in method.
