# Cross-project Agenda + PM depth - Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]` checkboxes.

**Goal:** Aggregate every project's deadlines and due tasks into a dashboard Agenda, and
deepen per-project tasks with todo/doing/done status, a progress bar, and drag-reordering.

**Architecture:** `store.Task` gains a `Status` field (migrated from `Done`); `app.go`
gets per-project PM bindings and a store-only `Agenda()` aggregator; the DetailPanel Tasks
tab and the Overview dashboard render them.

**Tech Stack:** Go 1.22 (Wails backend), Svelte-TS front end. Standard library only.

## Global Constraints

- ASCII-only source, plain "-" only. `go.mod` stays `go 1.22.0`. gofmt-clean; `go vet ./...` clean.
- List-returning bindings return non-nil slices; front end defends nullable arrays with `|| []`.
- Crash-safe: one project's data never breaks a view. Store access mutex-synchronized via `store.Update`/`Snapshot`.
- Backward compatible: legacy `projects.json` (tasks with only `done`) loads and migrates; `Done` stays a valid mirror of `Status`.
- Task status values, exact set: `todo`, `doing`, `done`. `Done == (Status == "done")` always.
- Project id is the repo path for code projects (Name empty; use `filepath.Base` for display) and an opaque id for manual projects (Name authoritative).

---

### Task 1: (C-T1) store Task.Status + migration

**Files:**
- Modify: `internal/store/store.go` (Task struct; migration in `Open`)
- Test: `internal/store/store_test.go`

**Interfaces:**
- Produces: `store.Task` gains `Status string json:"status"`. `Open` migrates legacy tasks.

**Design notes:**
- Add `Status string json:"status"` to `Task` (after `Done`).
- In `Open`, after `json.Unmarshal` populates `recs` (and before assigning to `s.records`),
  run a migration over every record's every task:
  - if `t.Status == ""` -> `t.Status = "done"` if `t.Done` else `"todo"`.
  - then `t.Done = (t.Status == "done")` (re-mirror, covers a file where the two disagree).
  - write the mutated task back into the slice (index assignment, since range copies).
- Migration must run whether or not `recs` is nil-checked; apply it to `recs` then assign.
  Keep it a small helper `migrate(recs map[string]Record)` for testability, or inline - your call.

- [ ] **Step 1: Write failing tests** (`internal/store/store_test.go`) - add:

```go
func TestOpenMigratesTaskStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")
	// legacy file: tasks carry only "done", no "status"
	raw := `{"p1":{"tasks":[{"id":"a","title":"x","done":true},{"id":"b","title":"y","done":false},{"id":"c","title":"z","done":false,"status":"doing"}]}}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	tasks := s.Get("p1").Tasks
	if len(tasks) != 3 {
		t.Fatalf("want 3 tasks, got %d", len(tasks))
	}
	if tasks[0].Status != "done" || !tasks[0].Done {
		t.Errorf("done task -> status done, got %+v", tasks[0])
	}
	if tasks[1].Status != "todo" || tasks[1].Done {
		t.Errorf("undone task -> status todo, got %+v", tasks[1])
	}
	if tasks[2].Status != "doing" || tasks[2].Done {
		t.Errorf("existing status preserved, done re-mirrored, got %+v", tasks[2])
	}
}
```
(Use `s.Get("p1")` - the existing accessor. If `Get` returns `(Record, bool)` adapt the call. Confirm the signature by reading store.go.)

- [ ] **Step 2: Run test, verify fail** - `go test ./internal/store/ -run TestOpenMigrates`.
- [ ] **Step 3: Implement** the field + migration.
- [ ] **Step 4: Run tests, verify pass**; `gofmt -l internal/store/` empty, `go vet ./internal/store/` clean; the existing store tests still pass.
- [ ] **Step 5: Commit** - `feat(store): add task status (todo/doing/done) with legacy migration`.

---

### Task 2: (C-T2) app.go PM bindings + progress counts

**Files:**
- Modify: `app.go`
- Modify: `app_test.go`

**Interfaces:**
- Consumes: `store.Task.Status`, `store.Update`.
- Produces:
  ```go
  // TaskView gains: Status string `json:"status"`
  // ProjectView gains: DoneCount int `json:"doneCount"`; TaskCount int `json:"taskCount"`
  func (a *App) SetTaskStatus(projectID, taskID, status string) string // "" ok, else error
  func (a *App) ReorderTasks(projectID string, orderedIDs []string) string
  ```

**Design notes:**
- `TaskView` (app.go ~259) gains `Status string json:"status"`; `toTaskViews` maps `t.Status`.
- `ProjectView` (~268) gains `DoneCount int json:"doneCount"` and `TaskCount int json:"taskCount"`;
  in `recordToView`, `TaskCount = len(r.Tasks)` and `DoneCount = count of tasks with Status=="done"`.
- `AddTask` (~365): set `Status: "todo"` on the new `store.Task` (Done stays false).
- `ToggleTask` (~373): after flipping `Done`, set `Status = "done"` if Done else `"todo"` (keep in sync).
- New `SetTaskStatus(projectID, taskID, status)`: reject a status not in {todo,doing,done}
  (return a non-nil error message, no mutation); else `store.Update` the matching task:
  `r.Tasks[i].Status = status; r.Tasks[i].Done = (status == "done")`. A missing taskID is a
  no-op success (mirrors the tolerant style) OR return an error - pick no-op success for
  consistency with ToggleTask's silent miss; note the choice.
- New `ReorderTasks(projectID, orderedIDs)`: in `store.Update`, rebuild `r.Tasks` in the
  order of `orderedIDs` (look each id up in the current slice); append any current task whose
  id is NOT in `orderedIDs` at the end in original order (never drop a task). Ignore ids in
  `orderedIDs` that don't match a task.

- [ ] **Step 1: Write failing tests** (`app_test.go`):

```go
func TestSetTaskStatus(t *testing.T) {
	a := newTestApp(t)
	id := "m1"
	a.AddProject(id) // or the existing manual-project creator; adapt to the real signature
	a.AddTask(id, "task one", "")
	tv := a.GetProject(id).Tasks
	if len(tv) != 1 || tv[0].Status != "todo" {
		t.Fatalf("new task should be todo, got %+v", tv)
	}
	if msg := a.SetTaskStatus(id, tv[0].ID, "doing"); msg != "" {
		t.Fatalf("SetTaskStatus doing: %s", msg)
	}
	tv = a.GetProject(id).Tasks
	if tv[0].Status != "doing" || tv[0].Done {
		t.Errorf("want doing/undone, got %+v", tv[0])
	}
	if msg := a.SetTaskStatus(id, tv[0].ID, "done"); msg != "" {
		t.Fatalf("SetTaskStatus done: %s", msg)
	}
	if pv := a.GetProject(id); pv.Tasks[0].Status != "done" || !pv.Tasks[0].Done || pv.DoneCount != 1 || pv.TaskCount != 1 {
		t.Errorf("want done+mirror+counts, got %+v", pv)
	}
	if msg := a.SetTaskStatus(id, tv[0].ID, "bogus"); msg == "" {
		t.Error("invalid status must be rejected")
	}
}

func TestReorderTasks(t *testing.T) {
	a := newTestApp(t)
	id := "m2"
	a.AddProject(id)
	a.AddTask(id, "A", "")
	a.AddTask(id, "B", "")
	a.AddTask(id, "C", "")
	ts := a.GetProject(id).Tasks
	ids := []string{ts[2].ID, ts[0].ID, ts[1].ID} // C, A, B
	if msg := a.ReorderTasks(id, ids); msg != "" {
		t.Fatalf("ReorderTasks: %s", msg)
	}
	got := a.GetProject(id).Tasks
	if got[0].Title != "C" || got[1].Title != "A" || got[2].Title != "B" {
		t.Errorf("bad order: %v", []string{got[0].Title, got[1].Title, got[2].Title})
	}
	// omitting an id keeps it (appended), never drops
	if msg := a.ReorderTasks(id, []string{got[2].ID}); msg != "" {
		t.Fatal(msg)
	}
	if len(a.GetProject(id).Tasks) != 3 {
		t.Error("reorder must never drop a task")
	}
}
```
(READ app.go/app_test.go first to use the REAL manual-project creation binding and its signature - the plan's `AddProject(id)` is a placeholder for whatever the codebase actually exposes, e.g. `AddProject(name)` returning an id. Adapt so the test compiles and is hermetic.)

- [ ] **Step 2: Run tests, verify fail** - `go test . -run 'TestSetTaskStatus|TestReorderTasks'`.
- [ ] **Step 3: Implement** the view fields, the two bindings, and the Add/Toggle sync.
- [ ] **Step 4: Run `go test .` + `go vet .` + `gofmt -l app.go app_test.go`** all clean.
- [ ] **Step 5: Commit** - `feat(app): task status + reorder bindings and progress counts`.

---

### Task 3: (C-T3) DetailPanel Tasks tab depth

**Files:**
- Modify: `frontend/src/lib/DetailPanel.svelte` (or its Tasks sub-component if one exists - read first)

**Interfaces:**
- Consumes: `SetTaskStatus`, `ReorderTasks` (regenerated bindings), plus existing
  `AddTask`/`ToggleTask`/`DeleteTask`; `ProjectView.doneCount`/`taskCount`, `TaskView.status`.

**Design notes:**
- Progress bar at the top of the Tasks content: `doneCount/taskCount` with a percentage
  (guard divide-by-zero -> show 0% / hide bar when taskCount is 0). Prefer the backend
  counts; if simpler, compute from the tasks array client-side - either is fine, keep one source.
- Per task: a status control (a small button/pill) showing the current status, colored
  todo/doing/done, that on click cycles todo -> doing -> done -> todo via
  `await SetTaskStatus(projectId, taskId, next)` then refreshes the project
  (use the existing task-mutation refresh path - find how AddTask/ToggleTask refresh today
  and reuse it). Keep the existing done checkbox OR replace it with the status control -
  your call, but the two must not contradict (both drive the same Status; if you keep the
  checkbox, wire it through the same refresh).
- Drag-to-reorder the task list: on drop, compute the new ordered id array and call
  `await ReorderTasks(projectId, ids)` then refresh. Use native HTML5 drag
  (`draggable`, `on:dragstart`/`on:dragover|preventDefault`/`on:drop`) - no external lib.
  Keep it crash-safe and ASCII-only.
- Show each task's due date when present. Empty task list renders cleanly (no bar, existing empty state).

- [ ] **Step 1:** Add the progress bar.
- [ ] **Step 2:** Add the per-task status cycle control (wired to SetTaskStatus + refresh).
- [ ] **Step 3:** Add drag-reorder (ReorderTasks + refresh).
- [ ] **Step 4:** Show due dates; verify zero-task and refresh paths.
- [ ] **Step 5:** `wails build` passes; ASCII check empty; commit real wailsjs binding changes if any.
- [ ] **Step 6: Commit** - `feat(frontend): task status, progress bar, and drag-reorder in Tasks tab`.

---

### Task 4: (D-T1) Agenda() binding

**Files:**
- Modify: `app.go`
- Modify: `app_test.go`

**Interfaces:**
- Consumes: `store.Snapshot`, `store.Record`/`Task` incl. `Status`.
- Produces:
  ```go
  type AgendaItem struct {
      ProjectID   string `json:"projectId"`
      ProjectName string `json:"projectName"`
      Kind        string `json:"kind"` // "deadline" | "task"
      Title       string `json:"title"`
      Due         string `json:"due"`  // may be ""
      Status      string `json:"status"`
  }
  func (a *App) Agenda() []AgendaItem // non-nil, sorted by Due asc, empty dues last
  ```

**Design notes:**
- Build from `a.store.Snapshot()` ONLY (no scan, no git). Start `out := []AgendaItem{}`.
- Project display name: `r.Name` if non-empty, else `filepath.Base(id)`.
- For each record: if `r.Deadline != ""` AND `r.Status != "done"`: append a `deadline` item
  (Title = display name, Due = r.Deadline, Status = r.Status).
- For each task `t` in the record: skip if `t.Status == "done"`. Include when `t.Due != ""`
  OR `t.Status == "doing"`. Append a `task` item (Title = t.Title, Due = t.Due, Status = t.Status).
- Sort `out` by `Due` ascending; items with `Due == ""` sort AFTER all dated items
  (use a stable sort; a simple comparator: empty-due -> treat as greater than any date).
  Dates are `YYYY-MM-DD` strings, so lexical compare works for non-empty dues.
- Return non-nil.

- [ ] **Step 1: Write failing test** (`app_test.go`):

```go
func TestAgenda(t *testing.T) {
	a := newTestApp(t)
	p := "mA"
	a.AddProject(p) // adapt to real creator
	a.SetDeadline(p, "2026-08-01") // adapt: use the real binding that sets Record.Deadline; if none, set via the store directly in the test
	a.AddTask(p, "due task", "2026-07-10")
	a.AddTask(p, "no-due doing", "")
	a.AddTask(p, "done task", "2026-07-05")
	ts := a.GetProject(p).Tasks
	// mark "no-due doing" doing and "done task" done
	for _, tk := range ts {
		if tk.Title == "no-due doing" {
			a.SetTaskStatus(p, tk.ID, "doing")
		}
		if tk.Title == "done task" {
			a.SetTaskStatus(p, tk.ID, "done")
		}
	}
	items := a.Agenda()
	if items == nil {
		t.Fatal("Agenda must be non-nil")
	}
	var kinds, titles []string
	for _, it := range items {
		kinds = append(kinds, it.Kind)
		titles = append(titles, it.Title)
	}
	// done task excluded; deadline + due task + doing-no-due included
	for _, tt := range titles {
		if tt == "done task" {
			t.Error("done task must be excluded")
		}
	}
	// dated items sorted before the empty-due doing item
	// (2026-07-10 due task and the 2026-08-01 deadline come before "no-due doing")
	if last := items[len(items)-1]; last.Due != "" {
		t.Errorf("empty-due item should sort last, got %+v", last)
	}
}
```
(READ the codebase for the REAL bindings to create a manual project and set a deadline. If
no `SetDeadline`-style binding exists, set the deadline by seeding the store through the same
store the app uses in `newTestApp` before calling Agenda - keep the test hermetic and compiling.)

- [ ] **Step 2: Run test, verify fail** - `go test . -run TestAgenda`.
- [ ] **Step 3: Implement** `AgendaItem` + `Agenda()`.
- [ ] **Step 4: `go test .` + `go vet .` + `gofmt -l app.go app_test.go`** clean.
- [ ] **Step 5: Commit** - `feat(app): cross-project Agenda aggregator (deadlines + due tasks)`.

---

### Task 5: (D-T2) Overview Agenda card

**Files:**
- Modify: `frontend/src/lib/Overview.svelte`
- Maybe create: `frontend/src/lib/AgendaCard.svelte` (keep Overview lean)

**Interfaces:**
- Consumes: `Agenda()` (regenerated binding) -> `AgendaItem[]`; the existing `daysUntil` from `pm.ts`; `onOpen`.

**Design notes:**
- On mount (and when the dashboard is shown), call `await Agenda()` guarded `|| []`.
- Bucket client-side using `daysUntil(item.due)` (from `./pm`):
  - `In progress`: `item.due === ""` (these are the doing-no-due tasks) - render in their own group, last.
  - `Overdue`: `daysUntil < 0`. `Today`: `=== 0`. `This week`: `1..7`. `Later`: `> 7`.
- Render each non-empty bucket with a heading and its items; each item shows
  `projectName` and `title` (and a small kind indicator for deadlines). Clicking an item
  calls `onOpen(item.projectId)`. Guard every array with `|| []`; crash-safe on empty.
- Place the Agenda card in the Overview grid (next to / above Needs-attention). Clean empty
  state ("Nothing scheduled") when all buckets are empty. ASCII-only.

- [ ] **Step 1:** Load `Agenda()` (guarded) on the dashboard.
- [ ] **Step 2:** Bucket by `daysUntil` incl. the no-due In-progress group.
- [ ] **Step 3:** Render grouped, click-through via `onOpen`, empty state.
- [ ] **Step 4:** `wails build` passes; ASCII check empty; commit real wailsjs changes if any.
- [ ] **Step 5: Commit** - `feat(frontend): dashboard Agenda card (fleet-wide deadlines and due tasks)`.

---

## Self-review notes

- Spec coverage: C-T1/C-T2/C-T3 cover PM depth (status model, bindings+progress, UI);
  D-T1/D-T2 cover the Agenda (aggregator, dashboard card). All spec sections mapped.
- Type consistency: `Status` values {todo,doing,done} used identically across store/app/UI;
  `Done == Status=="done"` invariant maintained in migration, AddTask, ToggleTask,
  SetTaskStatus. `AgendaItem` field names (ProjectID/ProjectName/Kind/Title/Due/Status) and
  `ProjectView.doneCount/taskCount`, `TaskView.status` are consistent between producer and consumers.
- Placeholders: the plan flags every spot where a test must be adapted to the REAL existing
  binding signatures (manual-project creation, deadline setting) - the implementer reads the
  code and adapts rather than inventing.
