# fleet Refocus (Multi-Repo Control Panel) - Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.

**Goal:** Refocus fleet on its differentiated job (a control panel for many local git repos): an Overview landing view + bulk actions, a tabbed detail panel, a demoted project-management layer, cut vanity, and the prerequisite architecture fixes.

**Architecture:** Backend first fixes the store's non-atomic read-modify-write (`store.Update`) and adds a targeted `GetProject` refresh; the front end then adds an Overview view, tabs the detail panel, adds bulk actions, caps the `LoadRepo` fan-out, and cuts the language bar / per-project heatmap / Today view / tags UI.

**Tech Stack:** Go 1.22.0, Wails v2.12, Svelte-TS. No new deps.

## Global Constraints

- Only ADD/CHANGE as specified; keep the app building at each task. Existing git actions for code projects stay.
- Store mutations must be atomic per operation (one lock across read-modify-write).
- ASCII-only in all user-facing text/code (never em-dash/en-dash/middle-dot/ellipsis/box-drawing; polish via CSS; no emoji).
- `go.mod` stays `go 1.22.0`, no `toolchain` line. Run go with `GOTOOLCHAIN=local`.
- Conventional Commits; NO Claude/AI co-author trailer.
- Env: Go `C:\Program Files\Go\bin`, wails `C:\Users\hoijun\go\bin` (prefix PATH + `GOTOOLCHAIN=local`). Never run `wails dev`. Verify front end with `wails build`, backend with `go test ./...`.

## File Structure

```
internal/store/store.go        + Update(id, fn) atomic read-modify-write
internal/store/store_test.go   + concurrent-no-lost-update test
app.go                         mutators use store.Update; + GetProject(id) binding
app_test.go                    + GetProject + atomic-mutation tests
frontend/src/App.svelte        targeted refresh (GetProject), LoadRepo concurrency cap, .catch, reqGen everywhere; Overview default; view toggle; bulk; remove Today
frontend/src/lib/Overview.svelte      NEW: stat tiles + needs-attention queue + aggregate heatmap
frontend/src/lib/DetailPanel.svelte   tabs (Overview/Git/Tasks); remove RunCommand runner + per-project heatmap
frontend/src/lib/Toolbar.svelte       Overview/Projects toggle; bulk action bar; remove tags/lang references
frontend/src/lib/ProjectTable.svelte  multi-select (checkbox); remove tag column if any
frontend/src/lib/TodayView.svelte     DELETED (absorbed into Overview)
frontend/src/lib/StatsHeader.svelte   remove language bar (or fold tiles into Overview)
```

---

### Task 1: Backend - atomic store update + GetProject

**Files:**
- Modify: `internal/store/store.go`, `app.go`
- Test: `internal/store/store_test.go`, `app_test.go`
- Regenerate: `frontend/wailsjs/*` via `wails build`

**Interfaces:**
- Produces: `store.Update(id string, fn func(*Record)) error`; `app.GetProject(id string) ProjectView`; the existing mutators (`UpdateProject`/`AddTask`/`ToggleTask`/`DeleteTask`/`AddProject`) reimplemented on `store.Update` (same signatures).

- [ ] **Step 1: Write the failing store test**

Add to `internal/store/store_test.go`:
```go
func TestUpdateAtomicNoLostUpdate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "projects.json")
	s, _ := Open(p)
	_ = s.Put("a", Record{Manual: true, Name: "a"})
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Update("a", func(r *Record) {
				r.Tasks = append(r.Tasks, Task{ID: "t"})
			})
		}()
	}
	wg.Wait()
	got, _ := s.Get("a")
	if len(got.Tasks) != 100 {
		t.Errorf("lost updates: want 100 tasks, got %d", len(got.Tasks))
	}
}

func TestUpdateOnMissingIdCreates(t *testing.T) {
	p := filepath.Join(t.TempDir(), "projects.json")
	s, _ := Open(p)
	_ = s.Update("new", func(r *Record) { r.Manual = true; r.Name = "n" })
	got, ok := s.Get("new")
	if !ok || !got.Manual || got.Name != "n" {
		t.Errorf("Update should create a record for a missing id: %+v", got)
	}
}
```
(`sync` is already imported by the store's concurrency test.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestUpdate -v`
Expected: FAIL - `Update` undefined.

- [ ] **Step 3: Add `Update` to `internal/store/store.go`**

```go
// Update atomically reads the record for id, applies fn to a copy of it, and
// saves - all under the write lock - so overlapping updates cannot lose each
// other's changes. fn receives a pointer to the current record (a zero Record
// if id is new).
func (s *Store) Update(id string, fn func(*Record)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := cloneRecord(s.records[id])
	fn(&rec)
	s.records[id] = rec
	return s.saveLocked()
}
```

- [ ] **Step 4: Run store test to verify it passes**

Run: `go test ./internal/store/ -run TestUpdate -v`
Expected: PASS.

- [ ] **Step 5: Write the failing app test**

Add to `app_test.go`:
```go
func TestGetProjectManual(t *testing.T) {
	a := newTestApp(t)
	id := a.AddProject("thesis")
	p := a.GetProject(id)
	if p.ID != id || p.Name != "thesis" || p.Type != "manual" {
		t.Errorf("GetProject manual wrong: %+v", p)
	}
}

func TestMutationsPersistViaUpdate(t *testing.T) {
	a := newTestApp(t)
	id := a.AddProject("p")
	if msg := a.AddTask(id, "do X", ""); msg != "" {
		t.Fatalf("AddTask: %s", msg)
	}
	tid := a.GetProject(id).Tasks[0].ID
	if msg := a.ToggleTask(id, tid); msg != "" {
		t.Fatalf("ToggleTask: %s", msg)
	}
	p := a.GetProject(id)
	if len(p.Tasks) != 1 || !p.Tasks[0].Done {
		t.Errorf("task not toggled via GetProject: %+v", p.Tasks)
	}
	if msg := a.UpdateProject(id, "paused", 3, "2026-08-29", "n"); msg != "" {
		t.Fatalf("UpdateProject: %s", msg)
	}
	p = a.GetProject(id)
	if p.Status != "paused" || p.Priority != 3 || p.Deadline != "2026-08-29" {
		t.Errorf("update not applied: %+v", p)
	}
}
```

- [ ] **Step 6: Run app test to verify it fails**

Run: `go test . -run "TestGetProject|TestMutationsPersist" -v`
Expected: FAIL - `GetProject` undefined.

- [ ] **Step 7: Reimplement mutators on `store.Update` + add `GetProject` in `app.go`**

Replace the mutator bodies:
```go
// GetProject returns the current view for one project id (targeted refresh after
// a mutation, avoiding a full rescan). Code projects (id == repo path) carry only
// their project-management fields here; the front end keeps the live git fields.
func (a *App) GetProject(id string) ProjectView {
	rec, _ := a.store.Get(id)
	if rec.Manual {
		return recordToView(id, rec.Name, "manual", "", rec)
	}
	return recordToView(id, filepath.Base(id), "code", id, rec)
}

// AddProject creates a manual project and returns its id, or "" on failure.
func (a *App) AddProject(name string) string {
	id := "m-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := a.store.Update(id, func(r *store.Record) {
		r.Manual = true
		r.Name = name
		r.Status = "active"
	}); err != nil {
		return ""
	}
	return id
}

func (a *App) UpdateProject(id, status string, priority int, deadline, notes string) string {
	return errMsg(a.store.Update(id, func(r *store.Record) {
		r.Status = status
		r.Priority = priority
		r.Deadline = deadline
		r.Notes = notes
	}))
}

func (a *App) DeleteProject(id string) string { return errMsg(a.store.Delete(id)) }

func (a *App) AddTask(projectID, title, due string) string {
	tid := "t-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	return errMsg(a.store.Update(projectID, func(r *store.Record) {
		r.Tasks = append(r.Tasks, store.Task{ID: tid, Title: title, Due: due})
	}))
}

func (a *App) ToggleTask(projectID, taskID string) string {
	return errMsg(a.store.Update(projectID, func(r *store.Record) {
		for i := range r.Tasks {
			if r.Tasks[i].ID == taskID {
				r.Tasks[i].Done = !r.Tasks[i].Done
			}
		}
	}))
}

func (a *App) DeleteTask(projectID, taskID string) string {
	return errMsg(a.store.Update(projectID, func(r *store.Record) {
		kept := r.Tasks[:0]
		for _, t := range r.Tasks {
			if t.ID != taskID {
				kept = append(kept, t)
			}
		}
		r.Tasks = kept
	}))
}
```
Delete the old `Get`-then-`Put` mutator bodies. `filepath` and `strconv`/`time`/`store` imports already present from prior work.

- [ ] **Step 8: Test, regenerate bindings, commit**

```bash
export PATH="/c/Program Files/Go/bin:/c/Users/hoijun/go/bin:$PATH"; export GOTOOLCHAIN=local
go test ./... && go vet ./... && wails build
git add internal/store/ app.go app_test.go frontend/wailsjs/go/main/App.js frontend/wailsjs/go/main/App.d.ts frontend/wailsjs/go/models.ts
git commit -m "feat: atomic store.Update and GetProject targeted refresh"
```
Confirm `App.d.ts` now exposes `GetProject`.

---

### Task 2: Front end - data-layer robustness

**Files:** modify `frontend/src/App.svelte`.

**Design contract (verify with `wails build`):**
- **Targeted refresh, no rescan:** the function called after a project-management mutation (currently re-runs `ListProjects()` -> full filesystem rescan) must instead call `GetProject(id)` and patch ONLY the project-management fields onto the existing row (keep the live git fields already merged). Keep the initial `ListProjects()` on mount and on manual Refresh / settings-save; do NOT call it after a task toggle / status / notes edit.
- **Cap the LoadRepo fan-out:** replace the unbounded `Promise.all(projects.map(LoadRepo))` with a small concurrency pool (limit 6). Write a tiny inline `pLimit`-style helper (no new dependency) or process in chunks of 6. Applies to `loadAll`, `fetchAll`, and any aggregate load.
- **`.catch` on every `LoadRepo`:** a rejected IPC must reset that row to a non-loading state (mark it loaded with an error indicator) so `loadingCount` can reach zero and no unhandled rejection occurs.
- **Consistent `reqGen` guard:** apply the existing generation/stale-drop guard to `fetchAll`, `refreshRepo`/`refreshProject`, and the auto-fetch timer callback - not only `loadAll`/`reconcile` - so a slow in-flight load cannot overwrite newer state.
- Keep all existing behavior otherwise. ASCII-only.

- [ ] **Step 1:** Implement the four changes.
- [ ] **Step 2:** `wails build` succeeds; `go test ./...` passes.
- [ ] **Step 3:** Commit `frontend/src`.

---

### Task 3: Front end - Overview view (default) + cuts

**Files:** create `frontend/src/lib/Overview.svelte`; modify `App.svelte`, `Toolbar.svelte`, `StatsHeader.svelte`; delete `frontend/src/lib/TodayView.svelte`.

**Design contract:**
- **View toggle Overview / Projects** in the Toolbar; **Overview is the default** on launch. Remove the old "Today" view/toggle.
- **Overview.svelte** (takes the assembled `projects` list):
  - **Stat tiles:** total repos, active, dirty, behind, unpushed (`ahead > 0`), overdue (projects with a `deadline` in the past). Computed client-side. (Reuse/move the counts from `StatsHeader`; drop the language-distribution bar entirely.)
  - **Needs-attention queue:** ONE ranked list. For each code project, include it if dirty OR `behind > 0` OR `ahead > 0` OR (has a deadline that is overdue) OR stale (last commit older than 14 days). Each row shows the project name + a short reason tag (e.g. "3 changed", "behind 2", "unpushed 1", "overdue 5d", "stale"). Rank overdue/behind/unpushed above stale. Clicking a row selects that project and switches to the Projects view (opening its detail).
  - **Aggregate commit-activity heatmap:** the single aggregate `<Heatmap>` (summed `CommitActivity` across code repos), moved here from the deleted Today view. Reuse the existing aggregate-load logic (with the concurrency cap from Task 2).
- **StatsHeader:** remove the language bar; either fold its tiles into Overview or keep a slim strip only in Projects view - do not duplicate the counts' computation (compute once, pass down).
- ASCII-only; reuse tokens/toasts. `wails build` must succeed; `go test ./...` passes.

- [ ] **Step 1:** Build Overview + toggle + delete Today + remove language bar.
- [ ] **Step 2:** `wails build` succeeds; `go test ./...` passes.
- [ ] **Step 3:** Commit `frontend/src`.

---

### Task 4: Front end - detail tabs + bulk actions + remaining cuts

**Files:** modify `frontend/src/lib/DetailPanel.svelte`, `App.svelte`, `Toolbar.svelte`, `ProjectTable.svelte`, `PMSection.svelte`.

**Design contract:**
- **Tab the DetailPanel** into three tabs for code projects: **Overview | Git | Tasks**.
  - Overview tab: identity + read-only status summary (branch, dirty/ahead/behind, last commit, deadline, task progress `done/total`). Glanceable, no heavy controls.
  - Git tab: BranchMenu, changed-files -> DiffModal, CommitBox, and the Fetch/Pull/Editor/Terminal action row; StashPanel and HistoryList behind collapsed sections/accordions.
  - Tasks tab: the PMSection (task checklist + deadline + notes + status + priority).
  - Manual projects: show ONLY the Tasks tab (no tabs bar or a disabled Git tab; no git actions).
- **Remove** the inline arbitrary-command runner (the `RunCommand` input + output pane) from DetailPanel entirely. **Remove** the per-project heatmap from DetailPanel (the aggregate one in Overview remains).
- **Remove tags from the UI:** drop any tag display/column in ProjectTable and any tag control in PMSection (the backend `Tags` field may remain, unread).
- **Bulk actions:**
  - Toolbar buttons **Pull all** (over all code/git projects) and **Push all** (over code projects with `ahead > 0`), fanning out through the existing `Pull`/`Push` bindings with the Task-2 concurrency cap, counting failures and toasting a summary, then refreshing.
  - **Multi-select in ProjectTable:** a per-row checkbox (and a header select-all); when any rows are selected, show a bulk action bar with Fetch / Pull / Push that runs over the selected code projects and toasts a summary.
- Keep every remaining existing feature (Ctrl+K palette, settings, context menu, filters, keyboard, auto-fetch, toasts). ASCII-only. `wails build` must succeed; `go test ./...` passes.

- [ ] **Step 1:** Tab the detail panel; remove RunCommand runner + per-project heatmap + tags UI.
- [ ] **Step 2:** Add bulk Pull-all/Push-all + multi-select bulk bar.
- [ ] **Step 3:** `wails build` succeeds; `go test ./...` passes.
- [ ] **Step 4:** Commit `frontend/src`.

---

## Self-Review

**Spec coverage:**
- Overview default view (tiles + needs-attention + aggregate heatmap) -> Task 3. ✓
- Bulk actions (pull-all/push-all/multi-select) -> Task 4. ✓
- Detail panel tabs; remove RunCommand + per-project heatmap -> Task 4. ✓
- Demote PM to a Tasks tab; manual projects show only Tasks -> Task 4. ✓
- Cuts: language bar (Task 3), tags UI (Task 4), per-project heatmap (Task 4), Today view (Task 3). ✓
- Arch fix 1 atomic RMW -> Task 1 (`store.Update`). ✓
- Arch fix 2 no rescan on edit -> Task 1 (`GetProject`) + Task 2 (use it). ✓
- Arch fix 3 LoadRepo concurrency cap -> Task 2. ✓
- Arch fix 4 `.catch` + consistent `reqGen` -> Task 2. ✓
- Deferred (Notion, CI, stable ids) -> not in any task, by design. ✓

**Placeholder scan:** backend task carries complete code; frontend tasks are design contracts with explicit binding names/fields + build-verify gates (consistent with how this project's front-end tasks were specified and executed throughout).

**Type consistency:** `store.Update(id, func(*Record))` (Task 1) used by all mutators (Task 1); `GetProject(id) ProjectView` (Task 1) consumed by the targeted refresh (Task 2); `ProjectView`/`Heatmap`/`CommitActivity`/`DayCountView` names unchanged from prior tasks and reused in Tasks 3-4.
