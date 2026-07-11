# Fleet Editor Picker - Design Spec

**Date:** 2026-07-12
**Status:** Approved for planning
**Topic:** Replace the bare "Editor" text field in Settings with an auto-detecting picker - a dropdown of common editors that marks which are installed, plus a Custom option that keeps the free-form command entry.

## Goal

Make "which editor opens my files" (used by the search overlay's open-in-editor and other open actions) easy to configure: detect the editors actually installed on the machine and let the user pick one from a labeled dropdown, while still allowing a custom command for anything not in the list.

## Context

- `Config.Editor` (`internal/config/config.go`) is a string - the command run by `action.EditorCmd(editor, path) = exec.Command(editor, path)` (via `OpenEditorAt`, which the search overlay and other UI use to open a file). Today Settings exposes it as a bare `<input bind:value={cfg.Editor}>` (`SettingsModal.svelte:187`), placeholder "code" - the user has to know the CLI command.
- `exec.LookPath(cmd)` resolves a command on PATH cross-platform (on Windows it honors PATHEXT, so `code` -> `code.cmd`), returning an error if absent.

## Global Constraints

- **No new runtime dependencies.** Go stdlib (`os/exec`) only; frontend adds no packages.
- **No behavior change to how the editor is launched** - `OpenEditorAt`/`EditorCmd` stay as-is; this only changes how `Config.Editor` is set in the UI. A custom command still works exactly as before.
- **Detection is best-effort and never blocks Settings** - if detection fails or finds nothing, the picker still offers the known list + Custom, and the current value is preserved.
- **Green gates:** `go build ./...` + `go vet ./...` clean, `go test ./...` green, `npx svelte-check` 0 errors, `npx vitest run` green, `wails build` succeeds.

## Workstream 1 - Editor detection (backend)

- **New `app.go` binding `DetectEditors() []EditorOption`** where `EditorOption{Name, Command string; Installed bool}` (json `name`/`command`/`installed`). It checks a curated list of known editors with `exec.LookPath(command)`; `Installed` is true when found. Returns the full known list (so the UI can show all options), installed ones sorted first, each in a stable order otherwise.
- **Known editor list** (name -> command): VS Code -> `code`, Cursor -> `cursor`, Windsurf -> `windsurf`, Sublime Text -> `subl`, Zed -> `zed`, Neovim -> `nvim`, Vim -> `vim`, IntelliJ IDEA -> `idea`, WebStorm -> `webstorm`, Emacs -> `emacs`, Notepad++ -> `notepad++`. (Order in the file is the display fallback order.)
- Detection is a pure-ish helper (`internal/action` or inline in app.go) unit-tested with an injectable `lookPath func(string) (string,error)` seam so tests don't depend on the machine's real PATH.

## Workstream 2 - Editor picker (Settings UI)

- **Modify `SettingsModal.svelte`:** on open, call `DetectEditors()` once. Replace the Editor `<input>` with:
  - A `<select>` listing the known editors by Name, each labeled `Name` + " (installed)" when `installed`, plus a trailing **"Custom..."** option.
  - The selected option is derived from the current `cfg.Editor`: if it exactly matches a known `command`, that editor is selected; otherwise "Custom..." is selected and the free-form text input is shown (pre-filled with `cfg.Editor`).
  - Picking a known editor sets `cfg.Editor = command`. Picking "Custom..." reveals the existing text `<input bind:value={cfg.Editor}>` (unchanged binding) so any command still works.
- The Terminal field and the rest of Settings are untouched. Keep the `field`/`field-label`/`input` styling; the `<select>` reuses the existing `Select.svelte` component if one is already used elsewhere in Settings, else a plain styled `<select>`.

## Data Flow

Settings opens -> `DetectEditors()` -> known list + installed flags -> `<select>` (current value maps to a known editor or Custom) -> user picks -> `cfg.Editor` updated -> saved with the rest of Settings as today. No change to how `cfg.Editor` is consumed.

## Error Handling / Edge Cases

- `DetectEditors` failure / empty PATH: the list still renders (all `installed:false`); Custom always available; current value preserved.
- `cfg.Editor` empty: default to "Custom..." (empty text) or the first installed editor - keep it non-destructive (do not overwrite a blank with a guess on open; only change on an explicit pick).
- `cfg.Editor` is a known command not currently installed: still select that editor in the dropdown (show it without "(installed)"), so a valid config on another machine isn't lost.

## Testing Strategy

- Backend: `DetectEditors` (or its helper) with an injected `lookPath` stub - returns the full known list, marks the stubbed-installed ones `Installed:true`, installed-first ordering. (Table test.)
- Frontend: a small pure helper `editorSelection(cfgEditor, options) -> { selected: string /* command or "custom" */, customValue: string }` extracted and unit-tested (known match -> that command; unknown/blank -> custom + the raw value). Render assertion for the select + custom-field toggle if the SSR pattern fits.
- Existing suites green; `wails build` succeeds.

## Out of Scope (YAGNI)

- Launch-argument templates per editor (e.g. `code -g file:line`) - EditorCmd stays `editor path`. (A `:line` open is a possible later enhancement.)
- Editor version detection, icons, or download links.
- Detecting editors not on PATH (GUI-only installs without a CLI shim).

## File Structure

- **Create:** `frontend/src/lib/editorSelection.ts` + `frontend/src/lib/editorSelection.test.ts`; backend detection test (in `app_test.go` or `internal/action`).
- **Modify:** `app.go` (`DetectEditors` + `EditorOption`; the detection helper + its `lookPath` seam), `frontend/src/lib/SettingsModal.svelte` (picker), `frontend/src/app.css` if a picker style is needed.
