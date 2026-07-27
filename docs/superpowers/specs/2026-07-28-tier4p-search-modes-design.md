# Tier 4p - Content search modes (fixed / regex / whole-word)

**Goal:** content search treats the query as literal text by default, with opt-in
regex and whole-word modes.

Backlog item (`specs/2026-07-11-fleet-search-enhancements-design.md:63-68`:
"regex ... modes"; whole-word too). Today `git.Grep` runs `git grep -e <query>`,
which is a **basic regex** - so searching `foo.bar` unexpectedly matches
`fooXbar`, and `a[b` is a syntax error. Literal text is what an interactive
search should default to.

## Design

- `git.GrepOpts{IgnoreCase, Regex, WholeWord bool}` and
  `Grep(r Runner, dir, query string, opts GrepOpts)`. Flags assembled as
  `git grep -n -I` then, from the options: `-i` (ignore case), `-w` (whole word),
  and either `-F` (fixed string, the default) or `-E` (extended regex when
  `Regex`), then `-e <query>`. The options struct replaces the lone `ignoreCase`
  bool - three bools as positional args would be unreadable.
- **Default is fixed-string** (`-F`): a deliberate change from today's basic
  regex, so typing `config.yaml` matches the literal, not a regex.
- `App.SearchAll(query string, ignoreCase, regex, wholeWord bool)` threads the
  three toggles through.
- `App.RepoGrep` (the agent's grep) passes `GrepOpts{Regex: true}`: the agent
  supplies regex patterns, so it keeps regex-capable grep (extended, a minor
  change from today's basic).
- An invalid regex in regex mode makes `git grep -E` exit non-zero with a
  diagnostic on stderr rather than empty stdout; `Grep` already returns
  `nil, nil` only when stdout is empty, so a bad pattern surfaces as the error
  git gives (which `SearchAll` currently drops, yielding no hits - acceptable,
  same as any zero-match query).

## UI

The content-search overlay already has an `Aa` case toggle
(`SearchOverlay.svelte`). It gains two more, in the same button group:

- **`.*`** - regex mode (extended). When on, the query is an ERE.
- **`\b`** - whole-word mode.

Each toggle re-runs the live query immediately (like the case toggle). All three
compose. They apply only in content mode (file-name search is unaffected). The
handler passes `SearchAll(term, !matchCase, regex, wholeWord)`.

## Testing

- **Go unit** (`internal/git/grep_test.go`): an args-capturing fake asserts the
  flag for each option - default carries `-F` and not `-E`; `Regex` carries `-E`
  and not `-F`; `WholeWord` carries `-w`; `IgnoreCase` carries `-i`; the existing
  parse tests pass `GrepOpts{}`.
- **Go unit** (`app_test.go`): `SearchAll` over a temp repo containing a line
  `value a.c here` and a line `value abc here` - a fixed-string query `a.c`
  matches only the literal line; a regex query `a.c` matches both. A whole-word
  query `cat` does not match `category`.
- **DOM** (tier-4h harness): toggling `.*` then searching calls `SearchAll` with
  `regex=true`; toggling `\b` passes `wholeWord=true`; the existing case test
  still holds.

## Out of scope

Search history, multiline/`-z` matching, replace, a "match count" readout, and
per-mode syntax help. Three modes on the content search, nothing more.
