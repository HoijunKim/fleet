# Manual repo edges + per-repo Symbols - Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]` checkboxes.

**Goal:** Let the user draw manual repo-to-repo edges the code cannot express, and view
per-repo auto-extracted symbols (Go main pkgs / exported names, npm scripts / bin).

**Architecture:** Two standalone Go packages (`internal/edges`, `internal/symbols`) behind
thin `app.go` bindings; the Graph view gains a connect mode, the detail panel a Symbols tab.
Both reads are lazy per selected repo. No auto call graph, no manual function graph.

**Tech Stack:** Go 1.22 (Wails backend), Svelte-TS front end. Standard library only
(`go/parser`, `crypto/rand`, `encoding/json`).

## Global Constraints

- ASCII-only source. Plain "-" only, never a special dash.
- `go.mod` stays `go 1.22.0`. Go code gofmt-clean; `go vet ./...` clean.
- List-returning bindings return non-nil slices; front end defends nullable arrays with `|| []`.
- Crash-safe: one repo's failure/absence never kills the app or Svelte render.
- No unbounded fan-out: both new reads are lazy per selected repo; symbol extraction capped at 400 `.go` files.
- Shared state (edge store, symbol cache) mutex-synchronized.
- Node/edge repo IDs are repo paths (the graph uses `RepoRef.ID = r.Path`).
- Allowed edge kinds, exact set: `http`, `db`, `deploy-after`, `related`.

---

### Task 1: (A-T1) internal/edges store

**Files:**
- Create: `internal/edges/edges.go`
- Test: `internal/edges/edges_test.go`

**Interfaces:**
- Produces (later tasks rely on these exact signatures):
  ```go
  package edges
  type Edge struct {
      ID   string `json:"id"`
      From string `json:"from"`
      To   string `json:"to"`
      Kind string `json:"kind"`
      Note string `json:"note"`
  }
  func Open(path string) (*Store, error)
  func (s *Store) List() []Edge                                  // non-nil copy
  func (s *Store) Add(from, to, kind, note string) (Edge, error) // validates, persists, returns stored edge
  func (s *Store) Remove(id string) error                        // persists; missing id is a no-op
  func AllowedKind(kind string) bool
  ```

**Design notes:**
- `Store{ path string; mu sync.Mutex; edges []Edge }`. `Open` reads the file (JSON array);
  a missing file yields an empty store with no error; a malformed file yields an empty
  store with no error (do not crash the app on a corrupt scratch file).
- Atomic persistence: marshal, write to `path+".tmp"`, `os.Rename` over `path`, all under `mu`.
- `Add`: trim inputs; reject empty `from`/`to`, reject `from == to`, reject a kind for which
  `AllowedKind` is false - each returns a non-nil error and does NOT persist. On success
  generate `ID` via `crypto/rand` (8 bytes -> hex), append, persist, return the stored Edge.
- `List` returns a fresh slice copy (never the internal backing array, never nil).
- `AllowedKind`: true only for the four kinds in Global Constraints.

- [ ] **Step 1: Write failing tests** (`internal/edges/edges_test.go`)

```go
package edges

import (
	"path/filepath"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "edges.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAddListRoundTrip(t *testing.T) {
	s := newStore(t)
	e, err := s.Add("/a", "/b", "http", "calls it")
	if err != nil {
		t.Fatal(err)
	}
	if e.ID == "" || e.From != "/a" || e.To != "/b" || e.Kind != "http" || e.Note != "calls it" {
		t.Fatalf("bad edge %+v", e)
	}
	list := s.List()
	if len(list) != 1 || list[0].ID != e.ID {
		t.Fatalf("list=%+v", list)
	}
}

func TestAddRejectsInvalid(t *testing.T) {
	s := newStore(t)
	if _, err := s.Add("", "/b", "http", ""); err == nil {
		t.Error("empty from must error")
	}
	if _, err := s.Add("/a", "", "http", ""); err == nil {
		t.Error("empty to must error")
	}
	if _, err := s.Add("/a", "/a", "http", ""); err == nil {
		t.Error("self-edge must error")
	}
	if _, err := s.Add("/a", "/b", "bogus", ""); err == nil {
		t.Error("invalid kind must error")
	}
	if len(s.List()) != 0 {
		t.Error("no invalid edge should persist")
	}
}

func TestRemove(t *testing.T) {
	s := newStore(t)
	e, _ := s.Add("/a", "/b", "db", "")
	if err := s.Remove("nope"); err != nil {
		t.Errorf("removing missing id must be a no-op, got %v", err)
	}
	if err := s.Remove(e.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 0 {
		t.Error("edge not removed")
	}
}

func TestPersistAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edges.json")
	s1, _ := Open(path)
	e, _ := s1.Add("/a", "/b", "related", "note")
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	list := s2.List()
	if len(list) != 1 || list[0].ID != e.ID || list[0].Note != "note" {
		t.Fatalf("reopened list=%+v", list)
	}
}

func TestUniqueIDs(t *testing.T) {
	s := newStore(t)
	a, _ := s.Add("/a", "/b", "http", "")
	b, _ := s.Add("/a", "/c", "http", "")
	if a.ID == b.ID {
		t.Error("ids must be unique")
	}
}

func TestMalformedFileIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edges.json")
	if err := writeFile(path, "{not json"); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("corrupt file must not error: %v", err)
	}
	if len(s.List()) != 0 {
		t.Error("corrupt file must yield empty store")
	}
}

// writeFile is a tiny local helper (implementer may use os.WriteFile directly instead).
func writeFile(path, s string) error { return osWriteFile(path, s) }
```
(Implementer: replace the `writeFile`/`osWriteFile` shim with a direct `os.WriteFile(path, []byte(s), 0o644)` call in the test.)

- [ ] **Step 2: Run tests, verify they fail** - `go test ./internal/edges/` -> build/undefined errors.
- [ ] **Step 3: Implement `internal/edges/edges.go`** per the design notes (atomic write, crypto/rand ID, validation, non-nil List, corrupt-file tolerance).
- [ ] **Step 4: Run tests, verify pass** - `go test ./internal/edges/`.
- [ ] **Step 5: gofmt + vet** - `gofmt -l internal/edges/` empty, `go vet ./internal/edges/` clean.
- [ ] **Step 6: Commit** - `feat(edges): manual repo-edge store (add/list/remove, atomic, validated)`.

---

### Task 2: (A-T2) edge bindings + RepoGraph merge

**Files:**
- Modify: `app.go` (add `edges` field + `NewApp` wiring + `newTestApp` wiring in test; bindings; `GraphEdge` fields; `RepoGraph` merge)
- Modify: `app_test.go`

**Interfaces:**
- Consumes: `edges.Open`, `(*edges.Store).List/Add/Remove` from A-T1.
- Produces:
  ```go
  type EdgeView struct {
      ID   string `json:"id"`
      From string `json:"from"`
      To   string `json:"to"`
      Kind string `json:"kind"`
      Note string `json:"note"`
  }
  func (a *App) ListEdges() []EdgeView            // non-nil
  func (a *App) AddEdge(from, to, kind, note string) string // "" ok, else error
  func (a *App) RemoveEdge(id string) string
  // GraphEdge gains: Manual bool `json:"manual"`; Kind string `json:"kind"`
  ```

**Design notes:**
- Add `edges *edges.Store` to `App`. In `NewApp`, open it beside `projects.json`:
  `edgesPath := filepath.Join(filepath.Dir(cfgPath), "edges.json")`. Tolerate error (nil-safe).
- Guard every binding against a nil `a.edges` (return empty/`"edges unavailable"`), so a
  store-open failure never panics.
- `AddEdge` returns `errMsg(err)` from `edges.Add`; `RemoveEdge` from `edges.Remove`;
  `ListEdges` maps `List()` to `[]EdgeView` (start from `[]EdgeView{}`).
- `RepoGraph`: after building auto `edges` slice (each `GraphEdge{From,To,Manual:false,Kind:""}`),
  build a `nodeIDs := map[string]bool` from `nodes`; for each `edges.Store.List()` entry, append
  `GraphEdge{From:e.From, To:e.To, Manual:true, Kind:e.Kind}` ONLY when `nodeIDs[e.From] && nodeIDs[e.To]`.
- Update the existing auto-edge append to set the two new fields explicitly.
- `newTestApp(t)` must construct the App with a temp edges store so tests are hermetic. Wire a
  temp `edges.json` in the same temp dir the test store uses.

- [ ] **Step 1: Write failing tests** (`app_test.go`)

```go
func TestEdgeBindingsRoundTrip(t *testing.T) {
	a := newTestApp(t)
	if msg := a.AddEdge("/a", "/b", "http", "n"); msg != "" {
		t.Fatalf("AddEdge: %s", msg)
	}
	list := a.ListEdges()
	if len(list) != 1 || list[0].From != "/a" || list[0].Kind != "http" {
		t.Fatalf("list=%+v", list)
	}
	if msg := a.AddEdge("/a", "/a", "http", ""); msg == "" {
		t.Error("self-edge must be rejected")
	}
	if msg := a.RemoveEdge(list[0].ID); msg != "" {
		t.Fatalf("RemoveEdge: %s", msg)
	}
	if len(a.ListEdges()) != 0 {
		t.Error("edge not removed")
	}
}

func TestListEdgesNonNil(t *testing.T) {
	a := newTestApp(t)
	if a.ListEdges() == nil {
		t.Error("ListEdges must be non-nil")
	}
}
```
(RepoGraph merge/dangling-filter: the implementer may add a focused test only if the graph
build can be driven from the test roots the harness already sets up. If `RepoGraph` cannot be
exercised hermetically without real repos on disk, note that in the report and rely on the
binding round-trip test plus manual verification; do not fabricate a vacuous assertion.)

- [ ] **Step 2: Run tests, verify fail** - `go test . -run TestEdge`.
- [ ] **Step 3: Implement** the field, `NewApp`/`newTestApp` wiring, three bindings, `GraphEdge` fields, and the `RepoGraph` merge.
- [ ] **Step 4: Regenerate bindings if needed** - not required for `go test`; the front end task regenerates via `wails build`.
- [ ] **Step 5: Run tests + vet** - `go test .` and `go vet .` clean.
- [ ] **Step 6: Commit** - `feat(app): edge bindings + manual edges merged into RepoGraph (dangling filtered)`.

---

### Task 3: (A-T3) Graph connect mode + manual-edge render/delete

**Files:**
- Modify: `frontend/src/lib/Graph.svelte`
- Modify: `frontend/src/App.svelte` (only if the graph reload path needs it - prefer self-contained in Graph.svelte)

**Interfaces:**
- Consumes: `ListEdges`, `AddEdge`, `RemoveEdge` (regenerated `wailsjs/go/main/App`), and the
  `RepoGraph` result whose edges now carry `manual` and `kind`.

**Design notes:**
- Graph already loads `RepoGraph()` into nodes/edges. Render an edge dashed when `edge.manual`,
  solid otherwise; color/label manual edges by `kind` (a small fixed map:
  http/db/deploy-after/related -> distinct hues). Defend `edges = res.edges || []`.
- Add a "Connect" toggle button to the graph toolbar. State: `connectMode` (bool),
  `pendingFrom` (node id | null).
  - Node click when `!connectMode`: existing select/open behavior, unchanged.
  - Node click when `connectMode`: if no `pendingFrom`, set it (render that node highlighted);
    else if the clicked node id differs, open a small kind picker (http/db/deploy-after/related);
    choosing a kind calls `await AddEdge(pendingFrom, id, kind, "")`, then reloads the graph and
    clears `pendingFrom`. Clicking the same node, pressing Escape, or toggling off clears
    `pendingFrom`.
  - A manual edge, when `connectMode` is on, is clickable: confirm, then `await RemoveEdge(edge.id)`
    and reload. (Edges need an `id`; ensure `EdgeView.id` flows through - the graph reload uses
    `RepoGraph` edges which carry `manual`/`kind` but NOT `id`. To delete, match the manual edge
    back to `ListEdges()` by from+to+kind, or extend the render to fetch `ListEdges()` alongside
    the graph and key deletions by that id. Choose one and keep it crash-safe.)
- ASCII-only. Keep the existing force-simulation, seeding, and rAF-cancel behavior intact.
- Zero nodes / zero edges must not throw.

- [ ] **Step 1:** Add connect-mode state + toolbar toggle; keep normal-click behavior when off.
- [ ] **Step 2:** Implement source->target->kind-pick->`AddEdge`->reload; Escape/toggle cancels.
- [ ] **Step 3:** Render manual edges dashed + kind color/label; auto edges solid.
- [ ] **Step 4:** Manual-edge delete in connect mode (`RemoveEdge` + reload), crash-safe id resolution.
- [ ] **Step 5:** `wails build` passes; verify ASCII (`grep -nP "[^\x00-\x7F]"` empty).
- [ ] **Step 6: Commit** - `feat(frontend): graph connect mode - draw/delete manual repo edges`.

---

### Task 4: (B-T1) internal/symbols Extract

**Files:**
- Create: `internal/symbols/symbols.go`
- Test: `internal/symbols/symbols_test.go`

**Interfaces:**
- Produces:
  ```go
  package symbols
  type SymbolSet struct {
      GoMainPkgs []string `json:"goMainPkgs"`
      GoExported []string `json:"goExported"`
      NpmScripts []string `json:"npmScripts"`
      NpmBin     []string `json:"npmBin"`
      Truncated  bool     `json:"truncated"`
  }
  func Extract(dir string) SymbolSet
  ```

**Design notes:**
- Walk `dir` with `filepath.WalkDir`, skipping directories named `.git`, `vendor`, `node_modules`.
- For each `*.go` file that is not `*_test.go`: parse with
  `parser.ParseFile(fset, path, nil, parser.PackageClauseOnly|parser.SkipObjectResolution)` first
  to get the package name cheaply; if you need decls, parse with `parser.SkipObjectResolution`
  (no `PackageClauseOnly`) and walk `file.Decls`.
  - If package name is `main`: add the file's directory relative to `dir`
    (`filepath.ToSlash`, "." when it is the root) to `GoMainPkgs` (deduped via a set).
  - For top-level `*ast.FuncDecl` with an exported name (`ast.IsExported`) and NO receiver
    (free functions, not methods): add `name` to a `GoExported` set. For `*ast.GenDecl` of
    `token.TYPE`, each `*ast.TypeSpec` with an exported name: add `name`.
- Cap: count parsed `.go` files; at 400 stop walking and set `Truncated = true`.
- npm: `os.ReadFile(filepath.Join(dir, "package.json"))`; if present and valid JSON, collect
  `scripts` object keys into `NpmScripts`; `bin`: if a JSON string, add the top-level `name`
  field's value (if any); if a JSON object, add its keys, into `NpmBin`.
- All four slices: initialize to `[]string{}`, fill from the sets, then `sort.Strings`.
  Never nil. Parse errors on any single file are ignored (continue).

- [ ] **Step 1: Write failing tests** (`internal/symbols/symbols_test.go`) - build a temp tree:
  - `main/main.go` (`package main; func Run(){}; func unexported(){}`),
  - `lib/lib.go` (`package lib; func Exported(){}; type Widget struct{}; func (Widget) M(){}`),
  - `lib/lib_test.go` (`package lib; func TestX(t *testing.T){}` - must be ignored),
  - `vendor/x/v.go` (`package x; func Vendored(){}` - must be skipped),
  - `package.json` (`{"name":"pkg","scripts":{"build":"...","dev":"..."},"bin":{"pkg":"cli.js"}}`).
  Assert: `GoMainPkgs` contains `main`; `GoExported` contains `Run`, `Exported`, `Widget` and
  NOT `unexported`, NOT `M` (method), NOT `Vendored`, NOT `TestX`; `NpmScripts` == `[build dev]`
  (sorted); `NpmBin` contains `pkg`. Add a second test writing 401 tiny `.go` files and asserting
  `Truncated` is true.

- [ ] **Step 2: Run tests, verify fail** - `go test ./internal/symbols/`.
- [ ] **Step 3: Implement** `Extract` per notes.
- [ ] **Step 4: Run tests, verify pass**; gofmt + vet clean.
- [ ] **Step 5: Commit** - `feat(symbols): extract go main pkgs/exported names + npm scripts/bin`.

---

### Task 5: (B-T2) RepoSymbols binding + cache

**Files:**
- Modify: `app.go` (add `symCache` + `symMu`; `SymbolsView`; `RepoSymbols`)
- Modify: `app_test.go`

**Interfaces:**
- Consumes: `symbols.Extract` from B-T1.
- Produces:
  ```go
  type SymbolsView struct {
      GoMainPkgs []string `json:"goMainPkgs"`
      GoExported []string `json:"goExported"`
      NpmScripts []string `json:"npmScripts"`
      NpmBin     []string `json:"npmBin"`
      Truncated  bool     `json:"truncated"`
  }
  func (a *App) RepoSymbols(path string) SymbolsView // cached per path; slices non-nil
  ```

**Design notes:**
- Add `symCache map[string]SymbolsView` + `symMu sync.RWMutex` to `App`; init the map in `NewApp`.
- `RepoSymbols`: RLock-check cache; on hit return it. On miss call `symbols.Extract(path)`, map to
  `SymbolsView`, Lock-store, return. Guarantee non-nil slices (Extract already does).
- To make caching testable without wall-clock: the test extracts once, then deletes the on-disk
  source dir (or a key file), calls again, and asserts the second result still has the symbols -
  proving it came from cache, not a re-extract of the now-empty dir. (Alternative: seam Extract
  behind a package-level func var and count calls. Prefer the delete-source approach; if that is
  flaky on Windows file locks, use the func-var seam.)

- [ ] **Step 1: Write failing test** - `TestRepoSymbolsCaches`: create a temp repo with an exported
  func, call `RepoSymbols`, assert it is found; remove the `.go` file; call again; assert the func
  is STILL present (served from cache).
- [ ] **Step 2: Run test, verify fail** - `go test . -run TestRepoSymbols`.
- [ ] **Step 3: Implement** field, init, `RepoSymbols`.
- [ ] **Step 4: Run test + vet** clean.
- [ ] **Step 5: Commit** - `feat(app): RepoSymbols binding with per-path cache`.

---

### Task 6: (B-T3) detail-panel Symbols tab

**Files:**
- Modify: `frontend/src/lib/DetailPanel.svelte`
- Create (optional): `frontend/src/lib/SymbolsTab.svelte` (keep DetailPanel lean)

**Interfaces:**
- Consumes: `RepoSymbols(path)` (regenerated `wailsjs/go/main/App`) returning `SymbolsView`.

**Design notes:**
- Add a "Symbols" tab alongside the existing Overview/Git/Tasks tabs, shown only for code (git)
  repos (`isCode && project.isGit`), matching how the GitHub badge is gated.
- Lazily call `RepoSymbols(project.path)` when the Symbols tab is active, with a path stale-drop
  guard (capture `p = project.path` before the await; drop if `p !== project.path` after). Defend
  every array with `|| []`.
- Render four labeled groups (Go main packages, Exported, npm scripts, npm bin); hide a group when
  its array is empty. If all are empty, show a neutral "no symbols found" line. When `truncated`,
  show a small "showing first 400 files" note.
- ASCII-only. No new bindings. Must not throw on null/missing fields.

- [ ] **Step 1:** Add the Symbols tab button (code+git only) and active-tab wiring.
- [ ] **Step 2:** Lazy `RepoSymbols` load with path stale-drop + `|| []` defense.
- [ ] **Step 3:** Render the four groups read-only; empty-group hide; truncated note; all-empty fallback.
- [ ] **Step 4:** `wails build` passes; ASCII check empty.
- [ ] **Step 5: Commit** - `feat(frontend): per-repo Symbols tab (read-only)`.

---

## Self-review notes

- Spec coverage: A-T1/A-T2/A-T3 cover manual edges (store, bindings+merge, UI); B-T1/B-T2/B-T3
  cover symbols (extract, binding+cache, UI). All spec sections mapped.
- Type consistency: `EdgeView`/`Edge` fields (ID/From/To/Kind/Note) and `SymbolsView`/`SymbolSet`
  fields (GoMainPkgs/GoExported/NpmScripts/NpmBin/Truncated) match across tasks.
- Node IDs are repo paths in both the graph and the edges - `AddEdge` From/To must be node ids
  (paths), which the Graph view supplies from the clicked nodes.
