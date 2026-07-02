# fleet - Unified Project Command Center (v1) - Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.

**Goal:** Turn fleet from a multi-repo git dashboard into a local-first command center for all projects: discovered git repos auto-register as code projects, non-code projects are added manually, and every project carries tasks, a deadline, notes, status, and priority, plus a git commit-activity heatmap.

**Architecture:** A new `internal/store` persists project-management data as one JSON file (`id -> Record`, atomic writes, no cgo). `app.go` assembles the unified project list from `scan.Discover` (code) UNION manual store records, merged with each record's project-management data, and exposes bindings for projects/tasks/activity. A new `internal/git` op returns per-day commit counts. The Svelte front end shows a unified list, a project-management detail section, a Today/Focus view, and commit-activity heatmaps. Live git status keeps using the existing `LoadRepo` flow.

**Tech Stack:** Go 1.22.0, Wails v2.12, Svelte-TS. No new external deps (JSON via stdlib).

## Global Constraints

- Reuse existing packages; only ADD. Do not break the existing git dashboard bindings or the live-load flow (`ScanRepos`/`LoadRepo` stay).
- Store is the source of truth for project-management data; git status is read live and never persisted.
- Store file: one JSON map `id -> Record`; atomic write (temp file + rename); on missing/corrupt load, start empty and do not overwrite until a change is made.
- Store and any shared `App` state are guarded by a mutex (as `app.cfg` already is).
- New git op goes through the `git.Runner` interface (testable with a fake).
- `go.mod` stays `go 1.22.0`, no `toolchain` line. Run go with `GOTOOLCHAIN=local`.
- ASCII-only in all user-facing text and code (never em-dash/en-dash/middle-dot/ellipsis/box-drawing); polish via CSS.
- Conventional Commits; NO Claude/AI co-author trailer.
- Env: Go `C:\Program Files\Go\bin`, wails `C:\Users\hoijun\go\bin` (prefix PATH + `GOTOOLCHAIN=local`). Never run `wails dev`. Verify front end with `wails build`.

## File Structure

```
internal/store/store.go        Record + Task types, Store (Open/Snapshot/Get/Put/Delete, atomic save)
internal/store/store_test.go
internal/git/activity.go       CommitActivity(runner, dir, weeks) []DayCount + DayCount type
internal/git/activity_test.go
app.go                         + store field, ProjectView/TaskView/DayCountView DTOs, project/task/activity bindings
app_test.go                    + tests for the new bindings (temp store dir + fake runner)
frontend/src/lib/ProjectTable.svelte   (replaces RepoTable usage: renders ProjectView rows)
frontend/src/lib/PMSection.svelte      task checklist + deadline/notes/status/priority
frontend/src/lib/AddProjectModal.svelte
frontend/src/lib/TodayView.svelte      aggregate deadlines + high-priority tasks + git attention
frontend/src/lib/Heatmap.svelte        commit-activity contribution grid
frontend/src/App.svelte                assemble projects (ListProjects + LoadRepo merge), views, wiring
frontend/src/lib/DetailPanel.svelte    + PMSection + Heatmap for code projects
frontend/src/app.css                   + styles
```

---

### Task 1: `internal/store` - local JSON store

**Files:**
- Create: `internal/store/store.go`, `internal/store/store_test.go`

**Interfaces produced:**
- `store.Task{ID, Title string; Done bool; Due string}` (json: id/title/done/due)
- `store.Record{Manual bool; Name, Status string; Priority int; Deadline, Notes string; Tags []string; Tasks []Task}` (json: manual/name/status/priority/deadline/notes/tags/tasks)
- `store.Open(path string) (*Store, error)` - loads, or empty store if file missing; on corrupt JSON returns an empty store AND a non-nil error (caller decides).
- `(*Store).Snapshot() map[string]Record` - copy of all records.
- `(*Store).Get(id string) (Record, bool)`
- `(*Store).Put(id string, r Record) error` - set + atomic save.
- `(*Store).Delete(id string) error` - remove + atomic save.

- [ ] **Step 1: Write the failing test**

Create `internal/store/store_test.go`:
```go
package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenMissingIsEmpty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "projects.json")
	s, err := Open(p)
	if err != nil {
		t.Fatalf("Open missing: %v", err)
	}
	if len(s.Snapshot()) != 0 {
		t.Errorf("expected empty store, got %v", s.Snapshot())
	}
}

func TestPutGetPersists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "projects.json")
	s, _ := Open(p)
	rec := Record{Manual: true, Name: "research", Status: "active", Priority: 2,
		Tasks: []Task{{ID: "t1", Title: "read paper", Done: false}}}
	if err := s.Put("m-1", rec); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// reload from disk
	s2, err := Open(p)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, ok := s2.Get("m-1")
	if !ok || got.Name != "research" || got.Priority != 2 || len(got.Tasks) != 1 {
		t.Errorf("reloaded record wrong: %+v", got)
	}
}

func TestDeleteRemoves(t *testing.T) {
	p := filepath.Join(t.TempDir(), "projects.json")
	s, _ := Open(p)
	_ = s.Put("x", Record{Manual: true, Name: "x"})
	if err := s.Delete("x"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("x"); ok {
		t.Error("expected x deleted")
	}
	s2, _ := Open(p)
	if _, ok := s2.Get("x"); ok {
		t.Error("delete did not persist")
	}
}

func TestOpenCorruptReturnsEmptyAndError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "projects.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(p)
	if err == nil {
		t.Error("expected error on corrupt file")
	}
	if s == nil || len(s.Snapshot()) != 0 {
		t.Error("expected usable empty store on corrupt file")
	}
}

func TestSnapshotIsCopy(t *testing.T) {
	p := filepath.Join(t.TempDir(), "projects.json")
	s, _ := Open(p)
	_ = s.Put("a", Record{Manual: true, Name: "a", Tags: []string{"x"}})
	snap := s.Snapshot()
	snap["a"] = Record{Name: "MUTATED"}
	if got, _ := s.Get("a"); got.Name != "a" {
		t.Error("Snapshot must not alias internal state")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -v`
Expected: FAIL - undefined `Open`/`Store`/`Record`.

- [ ] **Step 3: Write the implementation**

Create `internal/store/store.go`:
```go
// Package store persists fleet's project-management data (tasks, deadlines,
// notes, status, priority) as a single JSON file, keyed by project id.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Task is one checklist item on a project.
type Task struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
	Due   string `json:"due"`
}

// Record is the stored project-management data for one project id. For code
// projects the id is the repo path and Name is left empty (the scan supplies
// it); for manual projects Manual is true and Name is authoritative.
type Record struct {
	Manual   bool     `json:"manual"`
	Name     string   `json:"name"`
	Status   string   `json:"status"`
	Priority int      `json:"priority"`
	Deadline string   `json:"deadline"`
	Notes    string   `json:"notes"`
	Tags     []string `json:"tags"`
	Tasks    []Task   `json:"tasks"`
}

// Store is a concurrency-safe, file-backed map of id -> Record.
type Store struct {
	path    string
	mu      sync.RWMutex
	records map[string]Record
}

// Open loads the store at path. A missing file yields an empty store with no
// error. A corrupt file yields an empty (usable) store AND a non-nil error.
func Open(path string) (*Store, error) {
	s := &Store{path: path, records: map[string]Record{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}
	var recs map[string]Record
	if err := json.Unmarshal(data, &recs); err != nil {
		return s, err
	}
	if recs != nil {
		s.records = recs
	}
	return s, nil
}

// Snapshot returns a copy of all records (safe to mutate by the caller).
func (s *Store) Snapshot() map[string]Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]Record, len(s.records))
	for k, v := range s.records {
		out[k] = v
	}
	return out
}

// Get returns the record for id.
func (s *Store) Get(id string) (Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[id]
	return r, ok
}

// Put sets the record for id and saves atomically.
func (s *Store) Put(id string, r Record) error {
	s.mu.Lock()
	s.records[id] = r
	s.mu.Unlock()
	return s.save()
}

// Delete removes id and saves atomically.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	delete(s.records, id)
	s.mu.Unlock()
	return s.save()
}

// save writes the store to disk atomically (temp file + rename).
func (s *Store) save() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.records, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -v`
Expected: PASS (5 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat: add local JSON project-management store"
```

---

### Task 2: `internal/git` commit-activity

**Files:**
- Create: `internal/git/activity.go`, `internal/git/activity_test.go`

**Interfaces produced:**
- `git.DayCount{Date string; Count int}` (json: date/count)
- `git.CommitActivity(r Runner, dir string, weeks int) ([]DayCount, error)` - runs `git log --since=<weeks*7> days ago --date=short --pretty=format:%cd`, tallies commits per day, returns entries sorted ascending by date.

- [ ] **Step 1: Write the failing test**

Create `internal/git/activity_test.go`:
```go
package git

import "testing"

type actFake struct{ out string }

func (f actFake) Run(dir string, args ...string) (string, error) { return f.out, nil }

func TestCommitActivityTallies(t *testing.T) {
	f := actFake{out: "2026-07-02\n2026-07-02\n2026-06-30\n2026-07-02\n2026-06-30\n"}
	got, err := CommitActivity(f, "/x", 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 days, got %v", got)
	}
	// sorted ascending by date
	if got[0].Date != "2026-06-30" || got[0].Count != 2 {
		t.Errorf("day0=%+v", got[0])
	}
	if got[1].Date != "2026-07-02" || got[1].Count != 3 {
		t.Errorf("day1=%+v", got[1])
	}
}

func TestCommitActivityEmpty(t *testing.T) {
	got, err := CommitActivity(actFake{out: ""}, "/x", 8)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("want empty non-nil slice, got %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run TestCommitActivity -v`
Expected: FAIL - undefined `CommitActivity`.

- [ ] **Step 3: Write the implementation**

Create `internal/git/activity.go`:
```go
package git

import (
	"fmt"
	"sort"
	"strings"
)

// DayCount is the number of commits on a single day.
type DayCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// CommitActivity returns per-day commit counts for the last `weeks` weeks,
// sorted ascending by date, from the repo's git log.
func CommitActivity(r Runner, dir string, weeks int) ([]DayCount, error) {
	since := fmt.Sprintf("%d days ago", weeks*7)
	out, err := r.Run(dir, "log", "--since="+since, "--date=short", "--pretty=format:%cd")
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		d := strings.TrimSpace(line)
		if d != "" {
			counts[d]++
		}
	}
	dates := make([]string, 0, len(counts))
	for d := range counts {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	res := make([]DayCount, 0, len(dates))
	for _, d := range dates {
		res = append(res, DayCount{Date: d, Count: counts[d]})
	}
	return res, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/git/ -run TestCommitActivity -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/git/activity.go internal/git/activity_test.go
git commit -m "feat: add git commit-activity per-day tally"
```

---

### Task 3: `app.go` project / task / activity bindings

**Files:**
- Modify: `app.go`, `app_test.go`
- Regenerate: `frontend/wailsjs/*` via `wails build`

**Interfaces:**
- Consumes: `internal/store` (Task 1), `git.CommitActivity`/`DayCount` (Task 2), existing `scan`, `config`, `git`.
- Produces bindings + DTOs:
  - `TaskView{ID, Title string; Done bool; Due string}` (json id/title/done/due)
  - `ProjectView{ID, Name, Type, RepoPath, Status string; Priority int; Deadline, Notes string; Tags []string; Tasks []TaskView}` (json id/name/type/repoPath/status/priority/deadline/notes/tags/tasks)
  - `DayCountView{Date string; Count int}` (json date/count)
  - `ListProjects() []ProjectView`
  - `AddProject(name string) string` (returns new project id)
  - `UpdateProject(id, status string, priority int, deadline, notes string) string`
  - `DeleteProject(id string) string`
  - `AddTask(projectID, title, due string) string`
  - `ToggleTask(projectID, taskID string) string`
  - `DeleteTask(projectID, taskID string) string`
  - `CommitActivity(path string, weeks int) []DayCountView`
  - App gains `store *store.Store`; `NewApp` opens it next to the config file; store access via the store's own mutex.

- [ ] **Step 1: Write the failing test**

Add to `app_test.go`:
```go
func newTestApp(t *testing.T) *App {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &App{cfg: config.Default(), runner: fakeRunner{out: map[string]string{}}, store: st}
}

func TestAddAndListManualProject(t *testing.T) {
	a := newTestApp(t)
	id := a.AddProject("thesis")
	if id == "" {
		t.Fatal("AddProject returned empty id")
	}
	var found *ProjectView
	for i, p := range a.ListProjects() {
		if p.ID == id {
			found = &a.ListProjects()[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("manual project %q not in ListProjects", id)
	}
	if found.Name != "thesis" || found.Type != "manual" {
		t.Errorf("bad manual project: %+v", found)
	}
}

func TestTaskLifecycle(t *testing.T) {
	a := newTestApp(t)
	id := a.AddProject("p")
	tid := a.addTaskReturnID(t, id, "do X")
	// toggle
	if msg := a.ToggleTask(id, tid); msg != "" {
		t.Fatalf("ToggleTask: %s", msg)
	}
	p := a.projectByID(t, id)
	if len(p.Tasks) != 1 || !p.Tasks[0].Done {
		t.Errorf("task not toggled done: %+v", p.Tasks)
	}
	// delete
	if msg := a.DeleteTask(id, tid); msg != "" {
		t.Fatalf("DeleteTask: %s", msg)
	}
	if len(a.projectByID(t, id).Tasks) != 0 {
		t.Error("task not deleted")
	}
}

func TestUpdateProjectPersistsFields(t *testing.T) {
	a := newTestApp(t)
	id := a.AddProject("p")
	if msg := a.UpdateProject(id, "paused", 3, "2026-08-29", "defense prep"); msg != "" {
		t.Fatalf("UpdateProject: %s", msg)
	}
	p := a.projectByID(t, id)
	if p.Status != "paused" || p.Priority != 3 || p.Deadline != "2026-08-29" || p.Notes != "defense prep" {
		t.Errorf("update not applied: %+v", p)
	}
}

// helpers
func (a *App) addTaskReturnID(t *testing.T, projectID, title string) string {
	t.Helper()
	if msg := a.AddTask(projectID, title, ""); msg != "" {
		t.Fatalf("AddTask: %s", msg)
	}
	tasks := a.projectByID(t, projectID).Tasks
	if len(tasks) == 0 {
		t.Fatal("no task after AddTask")
	}
	return tasks[len(tasks)-1].ID
}

func (a *App) projectByID(t *testing.T, id string) ProjectView {
	t.Helper()
	for _, p := range a.ListProjects() {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("project %q not found", id)
	return ProjectView{}
}
```
Add imports `"path/filepath"` and `"github.com/hoijun/fleet/internal/store"` to `app_test.go` if missing.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run "TestAddAndList|TestTaskLifecycle|TestUpdateProject" -v`
Expected: FAIL - undefined `App.store`/`AddProject`/etc.

- [ ] **Step 3: Add the store field + NewApp wiring in `app.go`**

Add import `"github.com/hoijun/fleet/internal/store"` and `"path/filepath"`. Add field to the `App` struct: `store *store.Store`. Change `NewApp` to open the store beside the config file:
```go
func NewApp() *App {
	cfg, cfgPath, _ := config.Load()
	storePath := filepath.Join(filepath.Dir(cfgPath), "projects.json")
	st, _ := store.Open(storePath) // empty store on error; UI still works
	return &App{cfg: cfg, runner: git.ExecRunner{}, store: st}
}
```
(If `config.Load`'s current signature differs, adapt: it returns `(Config, string, error)`.)

- [ ] **Step 4: Add DTOs + bindings in `app.go`**

```go
// TaskView is the JS-facing task.
type TaskView struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
	Due   string `json:"due"`
}

// ProjectView is the JS-facing unified project (project-management fields only;
// live git status is merged in by the front end via LoadRepo for code projects).
type ProjectView struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Type     string     `json:"type"` // "code" | "manual"
	RepoPath string     `json:"repoPath"`
	Status   string     `json:"status"`
	Priority int        `json:"priority"`
	Deadline string     `json:"deadline"`
	Notes    string     `json:"notes"`
	Tags     []string   `json:"tags"`
	Tasks    []TaskView `json:"tasks"`
}

// DayCountView is the JS-facing per-day commit count.
type DayCountView struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

func toTaskViews(ts []store.Task) []TaskView {
	out := make([]TaskView, 0, len(ts))
	for _, t := range ts {
		out = append(out, TaskView{ID: t.ID, Title: t.Title, Done: t.Done, Due: t.Due})
	}
	return out
}

func recordToView(id, name, typ, repoPath string, r store.Record) ProjectView {
	tags := r.Tags
	if tags == nil {
		tags = []string{}
	}
	return ProjectView{
		ID: id, Name: name, Type: typ, RepoPath: repoPath,
		Status: r.Status, Priority: r.Priority, Deadline: r.Deadline,
		Notes: r.Notes, Tags: tags, Tasks: toTaskViews(r.Tasks),
	}
}

// ListProjects assembles discovered repos (code) plus manual store records.
func (a *App) ListProjects() []ProjectView {
	cfg := a.cfgSnapshot()
	snap := a.store.Snapshot()
	out := []ProjectView{}
	// code projects from the scan
	for _, r := range scan.Discover(cfg.Roots, cfg.ScanDepth, cfg.ShowNonGit) {
		rec := snap[r.Path] // zero Record if none
		out = append(out, recordToView(r.Path, r.Name, "code", r.Path, rec))
	}
	// manual projects from the store
	for id, rec := range snap {
		if rec.Manual {
			out = append(out, recordToView(id, rec.Name, "manual", "", rec))
		}
	}
	return out
}

// AddProject creates a manual project and returns its id.
func (a *App) AddProject(name string) string {
	id := "m-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	_ = a.store.Put(id, store.Record{Manual: true, Name: name, Status: "active"})
	return id
}

// UpdateProject sets a project's status/priority/deadline/notes.
func (a *App) UpdateProject(id, status string, priority int, deadline, notes string) string {
	rec, _ := a.store.Get(id)
	rec.Status = status
	rec.Priority = priority
	rec.Deadline = deadline
	rec.Notes = notes
	return errMsg(a.store.Put(id, rec))
}

// DeleteProject removes a project's stored data (manual project disappears; a
// code project loses its project-management data but is still discovered).
func (a *App) DeleteProject(id string) string { return errMsg(a.store.Delete(id)) }

// AddTask appends a task to a project.
func (a *App) AddTask(projectID, title, due string) string {
	rec, _ := a.store.Get(projectID)
	tid := "t-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	rec.Tasks = append(rec.Tasks, store.Task{ID: tid, Title: title, Due: due})
	return errMsg(a.store.Put(projectID, rec))
}

// ToggleTask flips a task's done state.
func (a *App) ToggleTask(projectID, taskID string) string {
	rec, _ := a.store.Get(projectID)
	for i := range rec.Tasks {
		if rec.Tasks[i].ID == taskID {
			rec.Tasks[i].Done = !rec.Tasks[i].Done
		}
	}
	return errMsg(a.store.Put(projectID, rec))
}

// DeleteTask removes a task from a project.
func (a *App) DeleteTask(projectID, taskID string) string {
	rec, _ := a.store.Get(projectID)
	kept := rec.Tasks[:0]
	for _, t := range rec.Tasks {
		if t.ID != taskID {
			kept = append(kept, t)
		}
	}
	rec.Tasks = kept
	return errMsg(a.store.Put(projectID, rec))
}

// CommitActivity returns per-day commit counts for the heatmap.
func (a *App) CommitActivity(path string, weeks int) []DayCountView {
	days, err := git.CommitActivity(a.runner, path, weeks)
	if err != nil {
		return []DayCountView{}
	}
	out := make([]DayCountView, 0, len(days))
	for _, d := range days {
		out = append(out, DayCountView{Date: d.Date, Count: d.Count})
	}
	return out
}
```
Add imports `"strconv"` and `"time"` to `app.go` if missing.

Note on `DeleteTask`'s `rec.Tasks[:0]` reuse: it rewrites the same backing array, which is fine here because `rec` is a copy returned by `Get` (value type), not shared state.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test . -v`
Expected: PASS (new project/task tests + existing app tests).

- [ ] **Step 6: Regenerate bindings, vet, build, commit**

```bash
export PATH="/c/Program Files/Go/bin:/c/Users/hoijun/go/bin:$PATH"; export GOTOOLCHAIN=local
go vet ./... && go test ./... && wails build
git add app.go app_test.go frontend/wailsjs/go/main/App.js frontend/wailsjs/go/main/App.d.ts frontend/wailsjs/go/models.ts
git commit -m "feat: add project/task/activity bindings backed by the store"
```
Confirm `App.d.ts` now exposes ListProjects/AddProject/UpdateProject/DeleteProject/AddTask/ToggleTask/DeleteTask/CommitActivity.

---

### Task 4: Front end - unified project list + project-management detail

**Files:** create `frontend/src/lib/ProjectTable.svelte`, `PMSection.svelte`, `AddProjectModal.svelte`; modify `App.svelte`, `DetailPanel.svelte`, `app.css`.

**Design contract (implementer writes the Svelte; verify with `wails build`):**
- **Assembly:** on mount, `projects = await ListProjects()`. For each project with `type === "code"`, call `LoadRepo(project.repoPath)` and merge the returned git fields (branch/dirty/modified/ahead/behind/hasUpstream/remote/dirtyFiles/lastHash/lastMsg/lastAuthor/lastWhen/language/todo/errMsg/loaded) onto that project row by id, in parallel (`Promise.all`) - same live-load pattern as today. Manual projects skip LoadRepo.
- **ProjectTable** (replaces the repo table as the main list): columns - Name, a Type badge (code/manual), git status pill (code only, from merged fields), task progress `done/total` (from `project.tasks`), deadline as `D-<n>` countdown (compute from `project.deadline`; blank if none; overdue styled red), status chip, priority indicator. Keep sort/filter; add sort/filter by status, priority, deadline. Reuse the existing premium row styling; manual rows show a distinct type badge and no git pill.
- **PMSection** (in the detail panel, for BOTH code and manual projects): a task checklist (each row: checkbox -> `ToggleTask(project.id, task.id)`, title, optional due, delete button -> `DeleteTask`); an "add task" input -> `AddTask(project.id, title, due)`; a deadline input, a notes textarea, a status selector (active/paused/done), and a priority selector (0..3) that call `UpdateProject(project.id, status, priority, deadline, notes)` on change (debounced for notes). After any mutation, re-fetch that project (re-run ListProjects or a targeted refresh) and toast the result. For code projects, PMSection sits alongside the existing git actions; for manual projects, the detail panel shows only PMSection (no git actions/branch/commit/etc).
- **AddProjectModal:** a "+ Project" button in the toolbar opens a modal with a name field -> `AddProject(name)` -> refresh list, select the new project. `DeleteProject(id)` available for manual projects (with confirm).
- Keep ASCII-only text; reuse toasts and design tokens. Do not break the existing git actions for code projects.

- [ ] **Step 1:** Build the components + wiring per the contract.
- [ ] **Step 2:** `wails build` succeeds; `go test ./...` still passes.
- [ ] **Step 3:** Commit `frontend/src`.

---

### Task 5: Front end - Today/Focus view + commit-activity heatmap

**Files:** create `frontend/src/lib/TodayView.svelte`, `Heatmap.svelte`; modify `App.svelte`, `DetailPanel.svelte`, `app.css`.

**Design contract:**
- **Heatmap** (`Heatmap.svelte`): render a GitHub-style contribution grid from a `DayCountView[]` prop - columns are weeks, rows are the 7 weekdays, each square shaded in 5 buckets by that day's count (0 = faint, higher = brighter accent), with a small legend. Given the sparse `[{date,count}]` list, build a dense grid for the last ~16 weeks (fill missing days with 0) using the dates present; do not fabricate "today" via Date.now on the Go side - the frontend may use the browser's current date for grid alignment. Purely presentational (takes data via prop).
- **Per-project heatmap:** in the detail panel for code projects, call `CommitActivity(project.repoPath, 16)` (lazily when the project is selected) and pass the result to `<Heatmap>`.
- **Aggregate heatmap:** in the Today view, sum `CommitActivity` across all code projects by date (fetch per code project, merge counts per date) and render one aggregate `<Heatmap>`.
- **TodayView** (a top-level view toggled from the toolbar, e.g. tabs "Projects" / "Today"): aggregates across all projects -
  - Upcoming deadlines: projects with a `deadline` within, say, 14 days (and overdue), sorted soonest-first, each showing D-countdown.
  - Open high-priority tasks: tasks where the project priority is high (2-3) and `!task.done`, or tasks with a near `due`, grouped by project.
  - Git attention: code projects that are dirty / behind / ahead (unpushed), as quick links.
  - The aggregate commit-activity heatmap.
  Clicking any item selects that project (and switches back to the Projects view / opens its detail).
- Keep ASCII-only; reuse tokens/toasts. `wails build` must succeed; `go test ./...` still passes.

- [ ] **Step 1:** Build TodayView + Heatmap + view toggle + wiring.
- [ ] **Step 2:** `wails build` succeeds; `go test ./...` passes.
- [ ] **Step 3:** Commit `frontend/src`.

---

## Self-Review

**Spec coverage:**
- Local-first store -> Task 1. ✓
- Discovered repos auto = code projects; manual projects; unified list -> Task 3 `ListProjects`, Task 4 ProjectTable. ✓
- Per-project tasks/deadline/notes/status/priority/tags -> Task 1 Record, Task 3 bindings, Task 4 PMSection. ✓
- Unified dashboard with git status + task progress + deadline -> Task 4. ✓
- Today/Focus view -> Task 5. ✓
- Commit-activity heatmap (per project + aggregate) -> Task 2 (git op), Task 3 (CommitActivity binding), Task 5 (Heatmap). ✓
- Atomic write / empty-on-missing / corrupt handling -> Task 1 (`save` temp+rename, `Open`). ✓
- Store access synchronized -> Task 1 (`sync.RWMutex` in Store). ✓
- Deferred (Notion / GitHub CI / bulk) -> not in any task, by design. ✓

**Placeholder scan:** No TBD/`add error handling`/vague steps; backend steps carry complete code; frontend tasks are design contracts with explicit binding names/fields and a build-verify gate (consistent with how the existing front-end tasks in this project were specified and executed).

**Type consistency:**
- `store.Record`/`store.Task` json tags (manual/name/status/priority/deadline/notes/tags/tasks; id/title/done/due) match `ProjectView`/`TaskView` (Task 3) and the fields the Svelte reads (Task 4/5). ✓
- Binding names produced in Task 3 (ListProjects/AddProject/UpdateProject/DeleteProject/AddTask/ToggleTask/DeleteTask/CommitActivity) are exactly those consumed in Tasks 4-5. ✓
- `git.CommitActivity` returns `[]git.DayCount{Date,Count}` (Task 2); `app.CommitActivity` maps to `[]DayCountView{date,count}` (Task 3); `Heatmap` consumes `DayCountView[]` (Task 5). ✓
- `App.cfgSnapshot()` (existing) reused in `ListProjects`; store methods (`Snapshot/Get/Put/Delete`) match Task 1. ✓
