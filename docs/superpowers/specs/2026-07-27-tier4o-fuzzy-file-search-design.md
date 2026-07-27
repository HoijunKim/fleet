# Tier 4o - Fuzzy file-name search

**Goal:** file-name search matches like an editor's quick-open - type `cbsv` and
`CommitBox.svelte` comes up, ranked best-first.

Backlog item (`specs/2026-07-11-fleet-search-enhancements-design.md:63-68`:
"Fuzzy file matching / ranking"). Today `SearchFiles` (`app.go:1676`) is a
case-insensitive substring `Contains` with no ranking - it misses `cbsv` for
`CommitBox.svelte` and returns matches in arbitrary file order.

## Design

- **`internal/fuzzy`** - `Match(query, candidate string) (score int, ok bool)`:
  a case-insensitive **subsequence** match (query characters appear in order,
  not necessarily contiguous; `ok=false` when they do not). The score rewards:
  - **contiguous runs** (adjacent query chars matching adjacent candidate chars),
  - **boundary hits** (a match at the start, or right after `/`, `.`, `_`, `-`,
    a space, or a lowercase->uppercase camelCase transition),
  - **earliness** (matches nearer the start score slightly higher).

  Greedy left-to-right matching with these bonuses; deterministic. This is the
  fzf/Ctrl-P heuristic, kept small - not the full optimal-alignment algorithm.

- **`SearchFiles`** replaces `Contains` with `fuzzy.Match`, collects every
  matching file with its score across all repos, and sorts **globally** by score
  descending (tie-break: shorter path first, then path ascending for a stable
  order), then caps at `searchGlobalCap`. The per-repo round-robin is dropped for
  file search: a quality score is the right fairness for quick-open (the best
  match should surface regardless of which repo holds it), unlike content search
  where there is no score.

`SearchAll` (content grep) is unchanged - fuzzy applies to file names, not to
line-content matching, and content keeps its round-robin.

## Testing

- **Go unit** (`internal/fuzzy/fuzzy_test.go`): a subsequence matches
  (`cbsv` -> `CommitBox.svelte`) and a non-subsequence does not (`xyz` ->
  `CommitBox.svelte` is `ok=false`); a contiguous match outscores a scattered one
  (`commit` in `CommitBox.svelte` beats `cmt`); a boundary match outscores a
  mid-word one (`box` scores higher against `CommitBox` than against `iconboxed`
  where the match is mid-token); an exact substring still matches and ranks well.
- **Go unit** (`app_test.go`): `SearchFiles` over temp repos returns the
  best-scoring file first across repos (a file named to match tightly outranks a
  loose match in a different repo) and excludes non-subsequence files.
- No frontend test: the overlay already renders `SearchFiles` output; only the
  order improves, and that order is Go-tested.

## Out of scope

Highlighting the matched characters in the UI, fuzzy matching for content
(line-grep) search, regex/whole-word file search modes, and a configurable
ranking weight. Subsequence match + a fixed score, best-first.
