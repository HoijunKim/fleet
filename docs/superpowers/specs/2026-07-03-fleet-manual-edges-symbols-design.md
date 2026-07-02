# fleet - Manual repo edges + per-repo Symbols - Design

**Date:** 2026-07-03
**Status:** Approved (scope confirmed by user)
**Builds on** the connected control panel (auto dependency graph, GitHub mission control).

## Motivation

The dependency graph currently shows only relationships fleet can auto-derive from
`go.mod` / `package.json` imports. Real repo relationships that never appear in code
(an HTTP/gRPC caller, a shared DB, a deploy-order constraint, "just related") are
invisible. And a repo node is opaque: you cannot see what is inside it.

Two additions, both fleet-native and non-stale:

1. **Manual repo edges** - the user draws edges the code cannot express.
2. **Per-repo Symbols** - read-only, auto-extracted from code (never hand-maintained,
   so never stale): Go main packages + exported top-level names, npm scripts + bin.

Explicitly NOT built (rejected as scope traps): manual function-level graph editing
(an Obsidian-style knowledge graph that goes stale on every edit) and an auto call
graph (IDE territory, off fleet's control-panel axis).

## Feature A: Manual repo edges

- **Store `internal/edges`:** a JSON file `edges.json` in the same directory as
  `projects.json` (`filepath.Dir(cfgPath)`). Atomic write (temp + rename), mutex-guarded.
  - `Edge{ID, From, To, Kind, Note string}`. `From`/`To` are repo IDs, which are repo
    paths (the graph already uses `RepoRef.ID = r.Path`).
  - `Open(path) (*Store, error)` - loads, empty store if the file is missing.
  - `List() []Edge` - non-nil copy.
  - `Add(from, to, kind, note string) (Edge, error)` - validates, generates an ID,
    persists. Rejects: empty from/to, `from == to` (no self-edge), a `kind` not in the
    allowed set. Allowed kinds: `http`, `db`, `deploy-after`, `related`.
  - `Remove(id string) error` - drops the edge, persists. Removing a missing ID is a
    no-op (no error).
  - ID generation: `crypto/rand` hex (collision-free, no wall-clock dependency).
- **Bindings (`app.go`):**
  - `ListEdges() []EdgeView` where `EdgeView{ID, From, To, Kind, Note string}`.
  - `AddEdge(from, to, kind, note string) string` - "" on success, else the error.
  - `RemoveEdge(id string) string`.
  - `RepoGraph` merges manual edges: `GraphEdge` gains `Manual bool` and `Kind string`.
    Auto edges keep `Manual:false, Kind:""`. A manual edge is included ONLY when both
    its `From` and `To` match a node in the graph (dangling edges - e.g. a deleted repo -
    are filtered out, never rendered).
- **Front end (Graph view):**
  - A "Connect" mode toggle in the graph toolbar. In connect mode: click a source node
    (highlighted as pending), then a different target node - a small kind picker appears
    (`http` / `db` / `deploy-after` / `related`); choosing one calls `AddEdge` and
    reloads the graph. Clicking the same node again, or toggling off, cancels.
  - Normal (non-connect) node click keeps the existing select-and-open behavior.
  - Manual edges render dashed with a kind color/label; auto edges stay solid. Clicking a
    manual edge (in connect mode) prompts to remove it (`RemoveEdge`, then reload).
  - Crash-safe on zero nodes/edges; `|| []` defense on the returned arrays.

## Feature B: Per-repo Symbols (read-only)

- **`internal/symbols`:** `Extract(dir string) SymbolSet` where
  `SymbolSet{GoMainPkgs, GoExported, NpmScripts, NpmBin []string; Truncated bool}`.
  - Go: walk `dir`, skipping `.git/`, `vendor/`, `node_modules/`, and `_test.go` files.
    Parse each `.go` file with `go/parser` (package clause + top-level decls only). A file
    whose package is `main` contributes its directory (relative to `dir`, `/`-slashed,
    "." for root) to `GoMainPkgs` (deduped). Exported top-level `func` and `type` names
    go to `GoExported`. Cap at 400 parsed `.go` files; if more exist, set `Truncated=true`
    and stop.
  - npm: read `package.json` at `dir` root only. `scripts` object keys -> `NpmScripts`.
    `bin` (string form -> the package `name`; object form -> its keys) -> `NpmBin`.
  - All slices non-nil and sorted (deterministic). Missing files / parse errors on a
    single file are skipped, never fatal.
- **Binding (`app.go`):** `RepoSymbols(path string) SymbolsView` where `SymbolsView`
  mirrors `SymbolSet`. Cached in-memory per path under a dedicated mutex; the extraction
  runs once per repo and is reused. Lazy - only the selected repo is extracted, no fan-out.
- **Front end:** a new "Symbols" tab in the detail panel for code (git) repos. Lazily
  calls `RepoSymbols(path)` with a path stale-drop guard (mirrors the GitHub badge / lazy
  panels). Renders the four lists read-only; shows a "showing first 400 files" note when
  `Truncated`. Empty groups are hidden; a repo with nothing extracted shows a neutral
  "no symbols found" line.

## Architecture / quality (global constraints)

- New Go through existing seams; `internal/edges` and `internal/symbols` are standalone,
  unit-tested packages (no runner needed - they touch the filesystem directly, tested
  against temp dirs).
- List-returning bindings return non-nil slices; the front end defends nullable arrays
  with `|| []`. Crash-safe: one repo's failure never kills the app or Svelte render.
- ASCII-only source, plain "-" only. `go.mod` stays `go 1.22.0`. gofmt-clean Go.
- No unbounded fan-out: both new reads are lazy per-selected-repo. Symbol extraction is
  bounded by the 400-file cap. Store/cache access is mutex-synchronized.
- Manual-edge integrity: dangling edges (endpoint no longer a node) are filtered at
  graph-build time so a deleted repo never breaks the graph.

## Build order (two SDD cycles, reviews between tasks)

Cycle A (manual edges):
- A-T1: `internal/edges` store (TDD).
- A-T2: `app.go` edge bindings + `RepoGraph` merge + `GraphEdge` fields (TDD).
- A-T3: Graph view connect mode + manual-edge render/delete (build-verified).

Cycle B (symbols):
- B-T1: `internal/symbols` `Extract` (TDD).
- B-T2: `app.go` `RepoSymbols` + cache (TDD).
- B-T3: detail-panel Symbols tab (build-verified).

## Testing

- `internal/edges`: TDD against a temp file - add/list/remove round-trip, invalid kind
  rejected, self-edge rejected, empty endpoints rejected, remove-missing is a no-op,
  persistence across reopen, IDs unique.
- `app.go` edges: `AddEdge`/`ListEdges`/`RemoveEdge` round-trip via a temp store;
  `RepoGraph` includes a manual edge between two real nodes and filters a dangling one.
- `internal/symbols`: TDD against temp dirs - a `main` package dir detected, an exported
  func/type collected, an unexported name and a `_test.go` file excluded, `vendor/`
  skipped, `package.json` scripts/bin parsed, the 400-file cap sets `Truncated`.
- `app.go` symbols: `RepoSymbols` returns extracted data and caches (second call does not
  re-extract - proven via a sentinel or timing-independent counter).
- Front end: build-verified (`wails build`).
