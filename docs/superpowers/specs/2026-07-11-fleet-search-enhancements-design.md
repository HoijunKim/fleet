# Fleet Search Enhancements - Design Spec

**Date:** 2026-07-11
**Status:** Approved for planning
**Topic:** Add file-name search (a Content/Files mode toggle) to the cross-repo search overlay, and add per-project filter chips above the results so a long result list can be narrowed to specific projects with one click.

## Goal

Make the cross-repo search overlay do two things it can't today: (1) find files by name/path, not just by content, via a Content/Files mode toggle; (2) let the user narrow a long result list to specific projects with on/off filter chips at the top - so scanning results across many repos is fast.

## Context

- The overlay is `frontend/src/lib/SearchOverlay.svelte`. Today it calls `SearchAll(query)` (git grep across all discovered repos) and renders the flat `Hit{repo,repoPath,file,line,text}` list grouped by repo. Keyboard nav (`selIndex`) walks the flat `hits`; Enter opens the highlighted hit via `OpenEditorAt(repoPath, file)`; 250ms debounce; Esc closes.
- Backend `SearchAll` (`app.go:856`) iterates `scan.Discover(cfg.Roots, cfg.ScanDepth, false)` and calls `git.Grep(a.runner, r.Path, query)` per repo, capping at 500 hits. `git.Grep(r Runner, dir, query)` runs `git grep` over tracked files. The `Runner` seam is `r.Run(dir, args...) (string, error)`.
- There is no file-listing git helper yet; `git ls-files` gives tracked files.

## Global Constraints

- **No new runtime dependencies.** `internal/agent`-style stdlib discipline; `frontend/package.json` unchanged.
- **Keyboard nav operates over the VISIBLE (filtered) hits only** - after project chips hide some repos, ArrowUp/Down and Enter act on what is shown, and `selIndex` never points at a hidden hit.
- **`prefers-reduced-motion` honored** for any chip/toggle motion (via `motion.ts` or an instant fallback).
- **No regression:** existing content search (grep, grouping, keyboard nav, open, debounce, blank-query clear, request-generation guard) keeps working exactly as today.
- **Green gates:** `go build ./...` + `go vet ./...` clean, `go test ./...` green, `npx svelte-check` 0 errors, `npx vitest run` green, `wails build` succeeds.

## Workstream 1 - File search (backend + mode toggle)

### Backend
- **New `git.ListFiles(r Runner, dir string) ([]string, error)`** (in `internal/git`) - runs `git ls-files` in `dir`, returns the tracked repo-relative paths (split on newline, blanks dropped). A non-zero exit with empty output is not an error (matches `Grep`'s tolerance).
- **New `app.go` binding `SearchFiles(query string) []FileHit`** where `FileHit{Repo, RepoPath, File string}` (json `repo`/`repoPath`/`file`). It mirrors `SearchAll`: iterate `scan.Discover(...)`, and per repo include tracked files whose repo-relative path contains `query` **case-insensitively** (substring match on `strings.ToLower`). Cap the total at 500. Blank query returns `[]FileHit{}`.

### Frontend (SearchOverlay.svelte)
- Add `mode: "content" | "files"` (default `"content"`). A small segmented Content / Files toggle sits in the overlay header (near the input). Clicking a segment switches mode; **Tab** (when the input is focused) also toggles between the two. Switching mode re-runs the current query in the new mode (reusing the debounce/generation machinery).
- In `files` mode, the search calls `SearchFiles(query)` and maps results into the same grouped-by-repo shape, but each item shows only the file path (no line/text). Opening a file item calls `OpenEditorAt(repoPath, file)` (the editor opens the file; no line offset).
- A unified internal hit type carries an optional `line`/`text` (present for content, absent for files) so the grouping, keyboard nav, count, and open logic are shared across modes rather than duplicated.
- The header/footer hints reflect the mode (e.g. placeholder "Search file names" in files mode).

## Workstream 2 - Project filter chips

- Above the result list (below the count line), render a **chip row**: one chip per repo present in the current results, showing the repo name and its hit count (e.g. `fleet 12`). Chips are toggle buttons; **all are ON by default**. Toggling a chip OFF hides that repo's group from the results.
- The filter is derived state: from the raw `hits`, compute the set of repos and per-repo counts; a `hidden: Set<string>` tracks toggled-off repos; the rendered `groups` (and the flat list keyboard nav walks) include only non-hidden repos. The count line reflects visible hits (e.g. "8 of 12 results" when a filter is active, plain "12 results" when none are hidden).
- Selecting a chip does not lose the others; a small "show all" affordance (or clicking the last-off chip) restores everything. When a new query runs, the chip set is rebuilt from the new results; a repo that reappears defaults to ON (the `hidden` set is cleared on a new query, so filters don't silently persist across unrelated searches).
- Chips apply in BOTH content and files mode.
- `selIndex` is kept valid against the visible list: after a chip toggle, if the highlighted hit is now hidden, clamp `selIndex` to the nearest visible hit (or 0).

## Data Flow

Query -> (mode) -> `SearchAll` or `SearchFiles` -> raw hits -> derive chips (repos + counts) -> apply `hidden` filter -> grouped visible hits -> render + keyboard nav + open. Chip toggles and mode switches are pure frontend re-derivations except mode-switch, which re-queries the backend.

## Error Handling / Edge Cases

- Blank query: clears hits, chips, keeps mode (existing blank-query guard extended to chips).
- Mode switch with an in-flight search: the request-generation guard (`reqGen`) drops the stale response so a content result can't land into files mode or vice-versa.
- All chips off: results area shows an empty "all projects hidden" state (not "No results"), with the chips still visible to turn back on.
- Reduced motion: chip toggle has no motion or an instant one.

## Testing Strategy

- `git.ListFiles`: returns tracked paths for a repo; tolerates the no-files/non-zero case. (Table test against a fake `Runner`, matching `grep_test.go` style.)
- `app.go` `SearchFiles`: case-insensitive substring match; caps at 500; blank query -> empty. (Test against the app's fake runner/discover like the existing `SearchAll` test, if present.)
- SearchOverlay (vitest, SSR-render where feasible + logic helpers): the chip-derivation + `hidden`-filter + `selIndex`-clamp logic extracted into a small tested helper (`searchFilter.ts`) so it's unit-tested without a DOM; render assertions for the mode toggle + chip row.
- Existing suites stay green; `wails build` succeeds.

## Out of Scope (YAGNI)

- Fuzzy file matching / ranking (substring only for v1).
- Regex/content-search options, case sensitivity toggles.
- Persisting mode or chip state across overlay opens.
- Searching untracked/ignored files (tracked files only, matching grep's scope).

## File Structure

- **Create:** `internal/git/lsfiles.go` (`ListFiles`) + `internal/git/lsfiles_test.go`; `frontend/src/lib/searchFilter.ts` (chip/filter/clamp helpers) + `frontend/src/lib/searchFilter.test.ts`.
- **Modify:** `app.go` (`SearchFiles` binding + `FileHit`), `app_test.go` (SearchFiles test if the SearchAll test pattern exists), `frontend/src/lib/SearchOverlay.svelte` (mode toggle + files rendering + chip row + filtered nav), `frontend/src/app.css` (mode-toggle + chip styles).
