# fleet Connected - Cycle 2 (cross-repo search) - Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.

**Goal:** Search a term across every code repo at once (git grep), show results grouped by repo, and open a hit in the editor.

**Architecture:** A `git.Grep` op behind the `Runner` seam; `app.go` adds `SearchAll(query)` (iterates code repos, capped) and `OpenEditorAt(repoPath, file)`; the front end adds a search overlay (input + grouped results + open).

**Tech Stack:** Go 1.22.0, Wails v2.12, Svelte-TS. No new deps.

## Global Constraints
- Only ADD; keep building each task. List-returning bindings non-nil; ASCII-only; `go.mod` stays `go 1.22.0` no toolchain (`GOTOOLCHAIN=local`).
- Conventional Commits; NO Claude/AI co-author trailer.
- Env: Go `C:\Program Files\Go\bin`, wails `C:\Users\hoijun\go\bin` (PATH prefix + GOTOOLCHAIN=local). Never `wails dev`. FE via `wails build`, BE via `go test ./...`.

## File Structure
```
internal/git/grep.go        Grep(runner, dir, query) []GrepHit + GrepHit type
internal/git/grep_test.go
app.go                      + SearchAll(query) []SearchHit + OpenEditorAt(repoPath, file) + SearchHit DTO
app_test.go                 + Grep-assembly + SearchAll tests (fake runner)
frontend/src/lib/SearchOverlay.svelte   input + grouped results + open
frontend/src/App.svelte     search state + open overlay (toolbar button / shortcut) + wiring
frontend/src/lib/Toolbar.svelte         a Search button
```

---

### Task 1: Backend - git Grep + SearchAll + OpenEditorAt

**Files:** create `internal/git/grep.go`, `internal/git/grep_test.go`; modify `app.go`, `app_test.go`; regenerate bindings.

**Interfaces:**
- `git.GrepHit{File string; Line int; Text string}`; `git.Grep(r Runner, dir, query string) ([]GrepHit, error)` - `git grep -n -I -e <query>`; a no-match (git exit 1, empty output) returns `(nil, nil)`, not an error.
- `app.SearchHit{Repo, RepoPath, File string; Line int; Text string}`; `app.SearchAll(query string) []SearchHit`; `app.OpenEditorAt(repoPath, file string) string`.

- [ ] **Step 1: Write `internal/git/grep_test.go`**
```go
package git

import "testing"

type grepFake struct {
	out string
	err error
}

func (f grepFake) Run(dir string, args ...string) (string, error) { return f.out, f.err }

func TestGrepParses(t *testing.T) {
	f := grepFake{out: "src/a.go:12:func Foo() {\ninternal/b.go:3:// Foo helper\n"}
	hits, err := Grep(f, "/r", "Foo")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits=%v", hits)
	}
	if hits[0].File != "src/a.go" || hits[0].Line != 12 || hits[0].Text != "func Foo() {" {
		t.Errorf("hit0=%+v", hits[0])
	}
	if hits[1].File != "internal/b.go" || hits[1].Line != 3 {
		t.Errorf("hit1=%+v", hits[1])
	}
}

func TestGrepTextWithColons(t *testing.T) {
	f := grepFake{out: "cfg.yaml:5:url: http://x:8080/path\n"}
	hits, _ := Grep(f, "/r", "url")
	if len(hits) != 1 || hits[0].File != "cfg.yaml" || hits[0].Line != 5 {
		t.Fatalf("hit=%+v", hits)
	}
	if hits[0].Text != "url: http://x:8080/path" {
		t.Errorf("text=%q (must keep colons after the line number)", hits[0].Text)
	}
}

func TestGrepNoMatch(t *testing.T) {
	// git grep exits 1 with empty output when nothing matches.
	f := grepFake{out: "", err: &stubErr{msg: "exit status 1"}}
	hits, err := Grep(f, "/r", "zzz")
	if err != nil {
		t.Errorf("no-match must not be an error: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("hits=%v", hits)
	}
}
```
(`stubErr{msg}` already exists in the git package's tests.)

- [ ] **Step 2: Run to fail** - `go test ./internal/git/ -run TestGrep -v` -> FAIL.

- [ ] **Step 3: Write `internal/git/grep.go`**
```go
package git

import (
	"strconv"
	"strings"
)

// GrepHit is one matching line: the repo-relative file, its 1-based line
// number, and the matched line's text.
type GrepHit struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// Grep runs `git grep -n -I -e <query>` in dir over tracked files. git grep
// exits non-zero (1) with empty output when nothing matches; that is treated as
// no hits, not an error.
func Grep(r Runner, dir, query string) ([]GrepHit, error) {
	out, err := r.Run(dir, "grep", "-n", "-I", "-e", query)
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, nil // no matches (or a benign non-zero exit)
	}
	var hits []GrepHit
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		// format: <file>:<line>:<text> - split on the first two colons only,
		// so colons inside the text are preserved.
		i1 := strings.IndexByte(line, ':')
		if i1 < 0 {
			continue
		}
		rest := line[i1+1:]
		i2 := strings.IndexByte(rest, ':')
		if i2 < 0 {
			continue
		}
		n, e := strconv.Atoi(rest[:i2])
		if e != nil {
			continue
		}
		hits = append(hits, GrepHit{File: line[:i1], Line: n, Text: rest[i2+1:]})
	}
	return hits, nil
}
```

- [ ] **Step 4: Run to pass** - `go test ./internal/git/ -run TestGrep -v` -> PASS.

- [ ] **Step 5: Write failing app tests** (add to `app_test.go`)
```go
func TestSearchAllEmptyQuery(t *testing.T) {
	a := newTestApp(t)
	if got := a.SearchAll("   "); got == nil || len(got) != 0 {
		t.Errorf("blank query must return empty non-nil, got %v", got)
	}
}

func TestSearchAllAssembles(t *testing.T) {
	// A real git repo in a temp root so scan.Discover finds it; the fake runner
	// returns canned grep output for the "grep" subcommand.
	root := t.TempDir()
	repo := filepath.Join(root, "myrepo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Roots = []string{root}
	a := &App{cfg: cfg, runner: fakeRunner{out: map[string]string{"grep": "main.go:1:package main\n"}}, store: newTestStore(t)}
	hits := a.SearchAll("package")
	if len(hits) != 1 {
		t.Fatalf("hits=%v", hits)
	}
	if hits[0].Repo != "myrepo" || hits[0].File != "main.go" || hits[0].Line != 1 {
		t.Errorf("hit=%+v", hits[0])
	}
}
```
Add a `newTestStore(t)` helper if one is not already present (open a store in a temp dir):
```go
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}
```
(If `newTestApp` already builds an App with a temp store and a `fakeRunner`, you may instead extend it - but `TestSearchAllAssembles` needs a runner whose `grep` key returns the canned output, so construct the App inline as shown. `fakeRunner` keys on `args[0]`; `git.Grep` calls `Run(dir, "grep", ...)`, so `out["grep"]` is returned.)

- [ ] **Step 6: Run to fail** - `go test . -run TestSearchAll -v` -> FAIL (undefined `SearchAll`).

- [ ] **Step 7: Add `SearchAll` + `OpenEditorAt` to `app.go`** (imports `path/filepath` (present), `strings` (present))
```go
// SearchHit is one cross-repo search result.
type SearchHit struct {
	Repo     string `json:"repo"`
	RepoPath string `json:"repoPath"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Text     string `json:"text"`
}

// SearchAll runs git grep across all discovered git repos and returns the hits
// (capped to keep the UI responsive). A blank query returns no hits.
func (a *App) SearchAll(query string) []SearchHit {
	out := []SearchHit{}
	if strings.TrimSpace(query) == "" {
		return out
	}
	cfg := a.cfgSnapshot()
	for _, r := range scan.Discover(cfg.Roots, cfg.ScanDepth, false) {
		hits, _ := git.Grep(a.runner, r.Path, query)
		for _, h := range hits {
			out = append(out, SearchHit{Repo: r.Name, RepoPath: r.Path, File: h.File, Line: h.Line, Text: h.Text})
			if len(out) >= 500 {
				return out
			}
		}
	}
	return out
}

// OpenEditorAt opens a specific file (repo-relative) in the configured editor.
func (a *App) OpenEditorAt(repoPath, file string) string {
	return errMsg(action.EditorCmd(a.cfgSnapshot().Editor, filepath.Join(repoPath, file)).Start())
}
```

- [ ] **Step 8: Run to pass** - `go test . -v` -> PASS.

- [ ] **Step 9: Regenerate, vet, build, commit**
```bash
export PATH="/c/Program Files/Go/bin:/c/Users/hoijun/go/bin:$PATH"; export GOTOOLCHAIN=local
go vet ./... && go test ./... && wails build
git add internal/git/grep.go internal/git/grep_test.go app.go app_test.go frontend/wailsjs/go/main/App.js frontend/wailsjs/go/main/App.d.ts frontend/wailsjs/go/models.ts
git commit -m "feat: add cross-repo git grep search (SearchAll) and OpenEditorAt"
```
Confirm `App.d.ts` exposes `SearchAll` and `OpenEditorAt`.

---

### Task 2: Front end - search overlay

**Files:** create `frontend/src/lib/SearchOverlay.svelte`; modify `App.svelte`, `Toolbar.svelte`.

**Design contract (verify with `wails build`):**
- A **search overlay** (a modal/panel like the command palette), opened by a Toolbar "Search" button and a keyboard shortcut (e.g. Ctrl+Shift+F; and it must respect the existing typing/overlay guards - opening/closing integrates with Escape and does not conflict with Ctrl+K).
- The overlay has a text input; on submit (Enter) or debounced typing, call `SearchAll(query)` and render results **grouped by repo** (repo name header, then each hit as `file:line` + the matched line text in mono, ASCII-only). Show a count and a "no results" / "type to search" state. Handle an empty query.
- Clicking a hit calls `OpenEditorAt(hit.repoPath, hit.file)` (import from `../../wailsjs/go/main/App`), toasts on error, and closes the overlay.
- Keyboard: arrow keys move a highlight through the flat result list, Enter opens the highlighted hit, Escape closes. Do not hijack typing in the search input.
- Crash-safe: `SearchAll` returns a non-nil array; guard anyway. Long result lists are scrollable and capped display is fine.
- Reuse tokens/toasts; ASCII-only. Keep every existing feature working (Ctrl+K, settings, filters, views, etc.).

- [ ] **Step 1-3:** implement; `wails build` succeeds; `go test ./...` passes; commit `frontend/src`.

---

## Self-Review
- Cross-repo search backend: `git.Grep` (TDD, colon-in-text safe, no-match not an error) + `SearchAll` (capped, non-nil) + `OpenEditorAt` (T1). ✓
- Search UI: overlay + grouped results + open-in-editor + keyboard (T2). ✓
- Non-nil (`SearchAll` returns `[]SearchHit{}`); ASCII-only; git grep respects tracked files/.gitignore via `-I` binary skip. ✓
- **Type consistency:** `git.Grep`->`[]GrepHit{File,Line,Text}` (T1) consumed by `SearchAll`; `SearchHit{repo,repoPath,file,line,text}` json tags + `SearchAll`/`OpenEditorAt` bindings (T1) consumed by SearchOverlay (T2).
- Deferred: GitHub mission control is the next cycle. ✓
