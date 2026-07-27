# Tier 4j - Case-insensitive content search

**Goal:** content search finds matches regardless of case, with an opt-in
"match case" toggle for when case matters.

Backlog item (`specs/2026-07-11-fleet-search-enhancements-design.md:63-68`: "case
sensitivity toggles"). Today `git.Grep` runs a case-SENSITIVE `git grep`, so
searching `todo` misses `TODO` - the opposite of what an interactive search
should do.

## Design

- `git.Grep(r Runner, dir, query string, ignoreCase bool)` prepends `-i` to the
  `git grep` args when `ignoreCase` is true. The one caller (`SearchAll`,
  `app.go:1579` - the only `git.Grep` call in the tree) threads the flag through.
- `App.SearchAll(query string, ignoreCase bool)` gains the parameter.
- **Default is case-INSENSITIVE.** Interactive search matching an editor's
  convention (VS Code, ripgrep's smart default) is the enhancement's whole point;
  `ignoreCase` defaults to true in the UI. A "match case" toggle (an `Aa`
  control, like the existing Content/Files toggle) turns it off when the user
  wants exact case. This changes the prior case-sensitive default deliberately.
- File-name search (`SearchFiles`) is already case-insensitive
  (`app.go:1617-1618`) and is unchanged.

## Testing

- **Go** (`internal/git/grep_test.go`): `Grep(..., "todo", true)` matches a line
  containing `TODO`; `Grep(..., "todo", false)` does not. The existing
  case-sensitive tests pass `false`.
- **Go** (`app_test.go`): `SearchAll` with `ignoreCase=true` finds a mixed-case
  match across a temp repo; with `false` it does not.
- **DOM** (`SearchOverlay.dom.test.ts`, tier-4h harness): the match-case toggle
  flips, and a search after toggling calls `SearchAll` with the expected
  `ignoreCase` value.

## Out of scope

Regex vs fixed-string mode, whole-word matching, fuzzy file ranking, and search
history - the other search-enhancement backlog items, each its own change. This
tier is case sensitivity only.
