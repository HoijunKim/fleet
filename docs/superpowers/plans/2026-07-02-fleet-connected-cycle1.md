# fleet Connected - Cycle 1 (groups/tags + dependency graph) - Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.

**Goal:** Wire tags end-to-end (grouping/coloring/filter) and add a repo dependency Graph view (nodes = repos, edges = real go.mod/package.json dependencies).

**Architecture:** New `internal/deps` parses go.mod/package.json across discovered repos and builds a dependency graph; `app.go` adds `SetTags` and `RepoGraph` bindings; the front end adds tag chips/filter/color and an interactive force-directed Graph view.

**Tech Stack:** Go 1.22.0, Wails v2.12, Svelte-TS. No new deps.

## Global Constraints
- Only ADD; keep the app building each task. `internal/deps` is pure (stdlib); testable without git.
- List-returning bindings non-nil; ASCII-only; `go.mod` stays `go 1.22.0` no toolchain; run go with `GOTOOLCHAIN=local`.
- Conventional Commits; NO Claude/AI co-author trailer.
- Env: Go `C:\Program Files\Go\bin`, wails `C:\Users\hoijun\go\bin` (PATH prefix + GOTOOLCHAIN=local). Never `wails dev`. Verify FE with `wails build`, BE with `go test ./...`.

## File Structure
```
internal/deps/deps.go       Produces/Requires parsers + BuildGraph
internal/deps/deps_test.go
app.go                      + SetTags(id,tags) + RepoGraph() + GraphView/GraphNode/GraphEdge DTOs
app_test.go                 + SetTags round-trip + RepoGraph tests
frontend/src/lib/TagChips.svelte     add/remove tag chips (in detail)
frontend/src/lib/Graph.svelte        force-directed SVG graph view
frontend/src/App.svelte              tag filter, tag color helper, Graph view + toggle, wiring
frontend/src/lib/Toolbar.svelte      Overview/Projects/Graph toggle + tag filter
frontend/src/lib/pm.ts               + tagColor(tag) helper
```

---

### Task 1: Backend - SetTags + internal/deps + RepoGraph

**Files:** create `internal/deps/deps.go`, `internal/deps/deps_test.go`; modify `app.go`, `app_test.go`; regenerate bindings.

**Interfaces produced:**
- `deps.RepoRef{ID, Path, Name string}`, `deps.Node{ID, Name string; Produces []string}`, `deps.Edge{From, To string}`, `deps.Graph{Nodes []Node; Edges []Edge}`
- `deps.Produces(dir string) (goModule, jsName string)`, `deps.Requires(dir string) []string`, `deps.BuildGraph(repos []RepoRef) Graph`
- `app.SetTags(id string, tags []string) string`; `app.RepoGraph() GraphView`; `GraphView{Nodes []GraphNode; Edges []GraphEdge}`, `GraphNode{ID, Name string; Tags []string; IsGit bool}`, `GraphEdge{From, To string}`

- [ ] **Step 1: Write `internal/deps/deps_test.go`**
```go
package deps

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProduces(t *testing.T) {
	d := t.TempDir()
	write(t, d, "go.mod", "module github.com/me/a\n\ngo 1.22\n")
	write(t, d, "package.json", `{"name":"pkg-a","version":"1.0.0"}`)
	gm, js := Produces(d)
	if gm != "github.com/me/a" || js != "pkg-a" {
		t.Errorf("Produces=%q,%q", gm, js)
	}
}

func TestRequires(t *testing.T) {
	d := t.TempDir()
	write(t, d, "go.mod", "module x\n\nrequire (\n\tgithub.com/me/b v1.2.3\n\tgithub.com/other/c v0.1.0\n)\n")
	write(t, d, "package.json", `{"dependencies":{"pkg-b":"^1.0.0"},"devDependencies":{"vite":"^5"}}`)
	got := Requires(d)
	want := map[string]bool{"github.com/me/b": true, "github.com/other/c": true, "pkg-b": true, "vite": true}
	if len(got) != 4 {
		t.Fatalf("Requires=%v", got)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected require %q", g)
		}
	}
}

func TestBuildGraphEdges(t *testing.T) {
	da := t.TempDir()
	write(t, da, "go.mod", "module github.com/me/a\nrequire github.com/me/b v1.0.0\n")
	db := t.TempDir()
	write(t, db, "go.mod", "module github.com/me/b\n")
	g := BuildGraph([]RepoRef{{ID: "a", Path: da, Name: "a"}, {ID: "b", Path: db, Name: "b"}})
	if len(g.Nodes) != 2 {
		t.Fatalf("nodes=%v", g.Nodes)
	}
	if len(g.Edges) != 1 || g.Edges[0].From != "a" || g.Edges[0].To != "b" {
		t.Errorf("edges=%v (want a->b)", g.Edges)
	}
}
```

- [ ] **Step 2: Run to fail** - `go test ./internal/deps/ -v` -> FAIL (undefined).

- [ ] **Step 3: Write `internal/deps/deps.go`**
```go
// Package deps derives dependency relationships between local repos from their
// go.mod / package.json manifests.
package deps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type RepoRef struct{ ID, Path, Name string }
type Node struct {
	ID       string
	Name     string
	Produces []string
}
type Edge struct{ From, To string }
type Graph struct {
	Nodes []Node
	Edges []Edge
}

// Produces returns the Go module path (go.mod) and npm package name
// (package.json) this directory publishes; either may be "".
func Produces(dir string) (goModule, jsName string) {
	if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "module ") {
				goModule = strings.TrimSpace(strings.TrimPrefix(line, "module "))
				break
			}
		}
	}
	if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		var pkg struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(data, &pkg) == nil {
			jsName = pkg.Name
		}
	}
	return
}

// Requires returns dependency names declared here: go.mod require module paths
// + package.json dependencies/devDependencies keys.
func Requires(dir string) []string {
	var out []string
	if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
		out = append(out, parseGoRequires(string(data))...)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		var pkg struct {
			Dependencies    map[string]string `json:"dependencies"`
			DevDependencies map[string]string `json:"devDependencies"`
		}
		if json.Unmarshal(data, &pkg) == nil {
			for k := range pkg.Dependencies {
				out = append(out, k)
			}
			for k := range pkg.DevDependencies {
				out = append(out, k)
			}
		}
	}
	return out
}

func parseGoRequires(gomod string) []string {
	var out []string
	inBlock := false
	for _, line := range strings.Split(gomod, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "require ("):
			inBlock = true
		case inBlock && line == ")":
			inBlock = false
		case inBlock && line != "":
			out = append(out, firstField(line))
		case strings.HasPrefix(line, "require "):
			out = append(out, firstField(strings.TrimPrefix(line, "require ")))
		}
	}
	return out
}

func firstField(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i]
	}
	return s
}

// BuildGraph maps each repo's produced module/name, then emits an edge from a
// repo to another repo whose produced name it requires. Nodes are returned for
// every input repo; edges are de-duplicated.
func BuildGraph(repos []RepoRef) Graph {
	owner := map[string]string{} // produced name -> repo id
	nodes := make([]Node, 0, len(repos))
	for _, r := range repos {
		gm, js := Produces(r.Path)
		var prod []string
		if gm != "" {
			owner[gm] = r.ID
			prod = append(prod, gm)
		}
		if js != "" {
			owner[js] = r.ID
			prod = append(prod, js)
		}
		nodes = append(nodes, Node{ID: r.ID, Name: r.Name, Produces: prod})
	}
	seen := map[string]bool{}
	edges := []Edge{}
	for _, r := range repos {
		for _, dep := range Requires(r.Path) {
			if to, ok := owner[dep]; ok && to != r.ID {
				key := r.ID + "\x00" + to
				if !seen[key] {
					seen[key] = true
					edges = append(edges, Edge{From: r.ID, To: to})
				}
			}
		}
	}
	return Graph{Nodes: nodes, Edges: edges}
}
```

- [ ] **Step 4: Run to pass** - `go test ./internal/deps/ -v` -> PASS.

- [ ] **Step 5: Write failing app tests** (add to `app_test.go`)
```go
func TestSetTagsRoundTrip(t *testing.T) {
	a := newTestApp(t)
	id := a.AddProject("p")
	if msg := a.SetTags(id, []string{"work", "urgent"}); msg != "" {
		t.Fatalf("SetTags: %s", msg)
	}
	tags := a.GetProject(id).Tags
	if len(tags) != 2 || tags[0] != "work" {
		t.Errorf("tags not persisted: %v", tags)
	}
}

func TestSetTagsNilCoerced(t *testing.T) {
	a := newTestApp(t)
	id := a.AddProject("p")
	if msg := a.SetTags(id, nil); msg != "" {
		t.Fatalf("SetTags nil: %s", msg)
	}
	if a.GetProject(id).Tags == nil {
		t.Error("Tags should never be nil in the view")
	}
}
```

- [ ] **Step 6: Run to fail** - `go test . -run TestSetTags -v` -> FAIL (undefined `SetTags`).

- [ ] **Step 7: Add `SetTags` + `RepoGraph` to `app.go`** (import `"github.com/hoijun/fleet/internal/deps"`)
```go
// SetTags sets a project's tags.
func (a *App) SetTags(id string, tags []string) string {
	if tags == nil {
		tags = []string{}
	}
	return errMsg(a.store.Update(id, func(r *store.Record) { r.Tags = tags }))
}

// GraphNode/GraphEdge/GraphView are the JS-facing dependency graph.
type GraphNode struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Tags  []string `json:"tags"`
	IsGit bool     `json:"isGit"`
}
type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}
type GraphView struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// RepoGraph builds the dependency graph over discovered git repos (nodes) and
// their go.mod/package.json cross-dependencies (edges). Tags come from the store.
func (a *App) RepoGraph() GraphView {
	cfg := a.cfgSnapshot()
	repos := scan.Discover(cfg.Roots, cfg.ScanDepth, false) // git repos only
	refs := make([]deps.RepoRef, 0, len(repos))
	for _, r := range repos {
		refs = append(refs, deps.RepoRef{ID: r.Path, Path: r.Path, Name: r.Name})
	}
	g := deps.BuildGraph(refs)
	snap := a.store.Snapshot()
	nodes := make([]GraphNode, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		tags := snap[n.ID].Tags
		if tags == nil {
			tags = []string{}
		}
		nodes = append(nodes, GraphNode{ID: n.ID, Name: n.Name, Tags: tags, IsGit: true})
	}
	edges := make([]GraphEdge, 0, len(g.Edges))
	for _, e := range g.Edges {
		edges = append(edges, GraphEdge{From: e.From, To: e.To})
	}
	return GraphView{Nodes: nodes, Edges: edges}
}
```

- [ ] **Step 8: Run to pass** - `go test . -v` -> PASS.

- [ ] **Step 9: Regenerate, vet, build, commit**
```bash
export PATH="/c/Program Files/Go/bin:/c/Users/hoijun/go/bin:$PATH"; export GOTOOLCHAIN=local
go vet ./... && go test ./... && wails build
git add internal/deps/ app.go app_test.go frontend/wailsjs/go/main/App.js frontend/wailsjs/go/main/App.d.ts frontend/wailsjs/go/models.ts
git commit -m "feat: add dependency graph (internal/deps + RepoGraph) and SetTags binding"
```
Confirm `App.d.ts` exposes `SetTags` and `RepoGraph`.

---

### Task 2: Front end - tags (chips + filter + color)

**Files:** create `frontend/src/lib/TagChips.svelte`; modify `frontend/src/lib/pm.ts` (add `tagColor`), `DetailPanel.svelte` (or PMSection) to show TagChips, `Toolbar.svelte` (tag filter), `App.svelte` (tag filter state + apply).

**Design contract (verify with `wails build`):**
- **`tagColor(tag: string): string`** in `pm.ts`: deterministic hue from the tag string (e.g. hash the chars -> hue 0..360 -> `hsl(...)`), so the same tag is always the same color. ASCII-only.
- **TagChips.svelte:** given a project, render its `tags` as small colored chips (color via `tagColor`), each with a remove (x); an input to add a tag (Enter). Add/remove call `SetTags(project.id, newTagsArray)` (compute the full array and send it), then refresh that project (via the existing `GetProject`/onChanged path) and toast on error. Shown in the project's detail (Tasks tab or a small area of the Overview tab), for both code and manual projects.
- **Tag filter (Toolbar + App.svelte):** the toolbar offers a way to filter the Projects list to a chosen tag (a select or chips of all distinct tags across `projects`); App.svelte composes it with the existing name/status/priority filters (AND). "All" = no tag filter.
- Reuse tokens/toasts; ASCII-only; keep existing features intact.

- [ ] **Step 1-3:** implement; `wails build` succeeds; `go test ./...` passes; commit `frontend/src`.

---

### Task 3: Front end - dependency Graph view

**Files:** create `frontend/src/lib/Graph.svelte`; modify `App.svelte` (Graph view + wiring), `Toolbar.svelte` (Overview/Projects/Graph toggle).

**Design contract:**
- Add **Graph** as a third top-level view (toolbar toggle becomes Overview / Projects / Graph).
- **Graph.svelte:** on show, call `RepoGraph()` -> `{nodes, edges}`. Render an interactive force-directed graph in SVG with a small self-contained simulation (no external library): repulsion between nodes, spring attraction along edges, a few dozen iterations on a timer (or a fixed layout then draggable). Nodes are circles labeled with the repo name, colored by their first tag via `tagColor` (fallback neutral); edges are lines/arrows (from -> to = "depends on"). Support: drag a node (updates its fixed position), pan + zoom (wheel + drag background), click a node to select that repo and switch to the Projects view opening its detail (reuse the Overview `onOpen(id)` path). Crash-safe on 0 nodes / 0 edges (show an empty hint like "No repositories" / "No dependencies detected"). Cap simulation cost for large N (e.g. stop after K iterations).
- The Graph view must not break keyboard/overlay guards; disable list-nav shortcuts while on Graph (like the existing `view !== "projects"` gate).
- ASCII-only; reuse tokens. `wails build` must succeed; `go test ./...` passes.

- [ ] **Step 1-3:** implement; `wails build` succeeds; commit `frontend/src`.

---

## Self-Review
- Tags end-to-end: `SetTags` (T1) + TagChips/filter/color (T2). ✓
- Dependency graph: `internal/deps`+`RepoGraph` (T1) + Graph view (T3). ✓
- `deps` pure + TDD (Produces/Requires/BuildGraph); `SetTags` round-trip tested; RepoGraph assembly. ✓
- Non-nil views (Tags coerced, nodes/edges `make(...,0)`); ASCII-only; graph crash-safe on empty. ✓
- **Type consistency:** `deps.BuildGraph`->`Graph{Nodes,Edges}` (T1) consumed by `RepoGraph` (T1); `GraphView{nodes,edges}`/`GraphNode{id,name,tags,isGit}`/`GraphEdge{from,to}` json tags (T1) consumed by `Graph.svelte` (T3); `SetTags(id, string[])` + `tagColor` used by TagChips (T2) and Graph (T3).
- Deferred (org coloring, groups-as-separate-entity): not built; tags are the grouping concept. Cross-repo search + GitHub mission control are Cycle 2. ✓
