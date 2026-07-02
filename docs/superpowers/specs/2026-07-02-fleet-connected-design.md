# fleet - Connected Control Panel (feature expansion) - Design

**Date:** 2026-07-02
**Status:** Design approved, pending spec review
**Builds on** the refocused multi-repo control panel. Adds four fleet-native features -
groups/tags, a repo dependency graph, cross-repo search, and GitHub mission control -
all on fleet's own axis (code topology), NOT an Obsidian-style note graph. Built to a
production bar, not MVP.

## Guiding constraint

Every feature must reinforce the identity "control panel for many local git repos."
The "connected brain" feel is delivered by relationships that are REAL for code
(dependencies, shared org, groups), never by a generic note/knowledge graph.

## Feature 1: Groups / tags

Tags already exist on the store `Record` (`Tags []string`) but are unread. Wire them
end-to-end as the grouping mechanism.
- **Backend:** `SetTags(id string, tags []string) string` (persists via `store.Update`,
  returns "" or error). Tags are already carried in `ProjectView`.
- **Front end:** editable tag chips on a project (add/remove) in its detail; a tag
  filter in the toolbar (narrow to a tag); a stable per-tag color (derived from the tag
  name client-side - same tag always the same hue). Tags also color graph nodes.
- Tags are the single grouping concept (no separate "group" entity) - simpler, solid.

## Feature 2: Repo dependency graph (headline)

A new top-level **Graph** view: nodes are repos, edges are REAL relationships.
- **Backend `internal/deps`:**
  - `Produces(dir) (goModule, jsName string)` - the module path from `go.mod` and the
    `name` from `package.json` (either may be empty).
  - `Requires(dir) []string` - dependency names: `go.mod` `require` module paths +
    `package.json` `dependencies`/`devDependencies` keys.
  - `BuildGraph(repos []RepoRef) Graph` where `RepoRef{ID, Path, Name}` - builds a
    produced-name -> repo map, then for each repo's requires, emits an edge
    `from -> to` when a required name matches another repo's produced module/name.
  - Org is a node attribute: parse the git remote to an `owner/org` for optional
    coloring/clustering (reuse the existing remote-parsing helper).
- **Binding:** `RepoGraph() GraphView{Nodes []GraphNode, Edges []GraphEdge}` where
  `GraphNode{ID, Name, Org string, Tags []string, IsGit bool}` and
  `GraphEdge{From, To string}` (dependency direction).
- **Front end (Graph view):** an interactive force-directed SVG - nodes colored by tag
  (or status), sized by e.g. dependent count; edges are dependency arrows. Drag nodes,
  pan/zoom, click a node to select that repo (switch to Projects + open detail), filter
  by tag/org. Crash-safe on zero edges / zero repos. No external graph library - a small
  self-contained force simulation in Svelte.

## Feature 3: Cross-repo search

Search a term across every code repo at once.
- **Backend:** a git op `Grep(r Runner, dir, query string) []GrepHit{File string; Line int; Text string}`
  via `git grep -n -I -e <query>` (tracked files, respects .gitignore), parsed from
  `path:line:text`. `SearchAll(query string) []SearchHit{Repo, RepoPath, File string; Line int; Text string}`
  in `app.go` iterates code repos through the concurrency pool, collecting hits (cap the
  total per repo to keep it responsive).
- **Front end:** a search surface (a dedicated Search view or a mode in the command
  palette) - type a query, results grouped by repo with file:line and the matching line;
  clicking a hit opens that file in the configured editor (`OpenEditorAt(path, file)` -
  a new binding that runs the editor with the repo path and the file). Empty/no-match is
  handled cleanly.

## Feature 4: GitHub mission control (+ remote-change notifications)

Per-repo GitHub status, degrading gracefully when `gh` is absent.
- **Backend `internal/gh`:** `Info(remote string) (GHInfo, error)` shelling to `gh api`:
  CI = the latest Actions run's conclusion/status
  (`repos/OWNER/REPO/actions/runs?per_page=1`); open PR count
  (`repos/OWNER/REPO/pulls?state=open`); open issue count (issues, excluding PRs). Parse
  `owner/repo` from the remote (reuse the remote helper). Results cached in-memory per
  repo; `gh` missing or unauthenticated -> `Available=false`, no error surfaced to the UI.
- **Binding:** `GitHubInfo(remote string) GitHubView{CI string; PRs, Issues int; Available bool}`,
  loaded lazily/async per repo and cached.
- **Remote-change notifications:** a repo whose upstream is ahead of local (i.e. the
  existing `RepoView.Behind > 0` after a fetch) HAS remote changes - this is already
  computed. Surface it as a toolbar notification count (number of repos with
  `behind > 0`) and it already appears in the Overview attention queue. Auto-fetch keeps
  it fresh. No new git op needed for this part.
- **Front end:** a small CI badge (pass/fail/running) + PR/issue counts on the project
  row and in the detail Git/Overview tab (lazy, cached, hidden when `Available=false`);
  a toolbar "remote changes: N" indicator.

## Architecture / quality
- New backend goes through the `git.Runner` seam where it runs git; `internal/gh` shells
  to `gh` behind a tiny interface so its parsing is testable. Store access stays
  synchronized. List-returning bindings stay non-nil. ASCII-only. `go.mod` stays
  `go 1.22.0`.
- Fan-outs (graph build's per-repo parse, SearchAll, GitHubInfo) are bounded (the
  existing pool / a Go worker cap) - no unbounded subprocess storms.

## Build order (each a solid SDD cycle with reviews)
1. Groups/tags (backend `SetTags` + front-end chips/filter/color).
2. Dependency graph (`internal/deps` + `RepoGraph` + Graph view).
3. Cross-repo search (`Grep`/`SearchAll` + search UI).
4. GitHub mission control (`internal/gh` + badges + remote-change indicator).

## Testing
- `internal/deps`: TDD - `Produces`/`Requires` parse sample go.mod/package.json;
  `BuildGraph` emits the right edges from a set of fake repos.
- `Grep` parser and `SearchAll` assembly: TDD with a fake runner.
- `internal/gh`: TDD the owner/repo parse + JSON parse of `gh api` output via a fake
  command runner; the live `gh` call is untested by design.
- `SetTags`/tags round-trip: TDD against a temp store.
- Front end: build-verified (`wails build`).
