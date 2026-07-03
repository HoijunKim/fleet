# fleet - Cross-project Agenda + PM depth - Design

**Date:** 2026-07-03
**Status:** Approved (direction + scope confirmed by user)
**Builds on** the connected control panel. Turns fleet from a git-health monitor into a
"what should I work on now?" command center, and deepens per-project task management.

## Motivation

fleet is a strong git control panel, but project-management data (Status, Priority,
Deadline, Notes, Tasks) lives ONLY inside each project's detail panel. The dashboard is
git-centric; the only PM signal that surfaces is the project-level "overdue" count. You
cannot answer "what is due across all my projects this week?" without opening each repo.
And tasks themselves are shallow: a bare done/not-done checkbox, no progress, no
in-progress state, no ordering.

Two additions, sharing the same task data:

1. **PM depth** (per project): task status todo/doing/done, a progress bar, and
   drag-reordering (order = priority).
2. **Cross-project Agenda** (dashboard): every project's deadline and every incomplete
   task with a due date, aggregated fleet-wide and grouped by when it is due.

Explicitly NOT built (YAGNI, scope traps): nested subtasks, structured/rich notes, and
per-task numeric priority labels (drag order conveys priority). Deferred to a later pass.

## Data model (store)

`store.Task` gains `Status string` (`todo` | `doing` | `done`). The existing `Done bool`
is kept as a mirror so old readers and the existing checkbox keep working:
`Done == (Status == "done")`. On `Open`, migrate legacy data: for every task, if
`Status == ""` set it to `"done"` when `Done` else `"todo"`; then force `Done` to mirror
`Status`. Migration is idempotent and lossless.

## Feature 1: PM depth (per project)

- **Bindings (`app.go`):**
  - `SetTaskStatus(projectID, taskID, status string) string` - validates status in
    {todo,doing,done}; sets `Status` and `Done = (status=="done")` via `store.Update`.
  - `ReorderTasks(projectID string, orderedIDs []string) string` - reorders a project's
    tasks to match `orderedIDs`; any task id not present in the list is appended in its
    original relative order (never dropped).
  - `AddTask` sets new tasks to `Status:"todo"`. `ToggleTask` keeps `Status` in sync
    (done<->todo) so the checkbox and the status control agree.
  - `TaskView` gains `Status string`. `ProjectView` gains `DoneCount int` and
    `TaskCount int` (computed in `recordToView`) so tiles/rows/agenda can show progress
    without re-counting.
- **Front end (DetailPanel Tasks tab):** a progress bar (DoneCount/TaskCount, with %),
  a per-task status control that cycles todo -> doing -> done -> todo (calls
  `SetTaskStatus`, colored per state), drag-to-reorder the task list (calls
  `ReorderTasks` with the new id order), and each task's due date shown. The existing
  add/delete stay. Crash-safe on zero tasks.

## Feature 2: Cross-project Agenda (dashboard)

- **Binding (`app.go`):** `Agenda() []AgendaItem` where
  `AgendaItem{ProjectID, ProjectName, Kind, Title, Due, Status string}`
  (`Kind` is `"deadline"` or `"task"`). Built from the store snapshot ONLY (a pure PM
  read - no scan, no git, no fan-out):
  - For each record with a non-empty `Deadline` whose `Status != "done"`: one `deadline`
    item (Title = project display name, Due = Deadline, Status = record Status).
  - For each task that is not done (`Status != "done"`) AND has a non-empty `Due`, OR is
    `doing` (regardless of due): one `task` item (Title = task Title, Due = task Due which
    may be "", Status = task Status).
  - Project display name: `Record.Name` when set (manual projects), else
    `filepath.Base(projectID)` (code projects store an empty Name; the id is the repo
    path). Non-nil slice, sorted by `Due` ascending with empty dues last.
- **Front end (Overview dashboard):** a new "Agenda" card that buckets the items client
  side using the existing `daysUntil` helper: **Overdue** (due < today), **Today**,
  **This week** (<= +7 days), **Later** (> +7 days), and **In progress** (no due date,
  status doing). Each item shows its project name and title; clicking opens that project
  (`onOpen(projectId)`). Clean empty state when nothing is scheduled.

## Architecture / quality (global constraints)

- Store access stays mutex-synchronized via `store.Update` / `Snapshot`; migration runs
  once in `Open`. List-returning bindings non-nil; front end defends nullable arrays with
  `|| []`. ASCII-only, plain "-". `go.mod` stays `go 1.22.0`. gofmt-clean.
- Backward compatible: old `projects.json` (tasks with only `done`) loads and migrates
  cleanly; new files stay readable by the old `Done` field.
- No fan-out: Agenda is a single store read; PM-depth bindings are per-project `Update`s.

## Build order (two SDD cycles, reviews between tasks)

Cycle C (PM depth):
- C-T1: `store` Task.Status + migration (TDD).
- C-T2: `app.go` SetTaskStatus / ReorderTasks / Status sync / progress counts (TDD).
- C-T3: DetailPanel Tasks tab - progress bar, status cycle, drag reorder, due (build-verified).

Cycle D (Agenda):
- D-T1: `app.go` `Agenda()` binding (TDD).
- D-T2: Overview Agenda card - buckets + click-through (build-verified).

## Testing

- `store`: migration - a legacy task (`done:true`, no status) -> `done`; (`done:false`) ->
  `todo`; an existing `status` preserved; `Done` re-mirrored. Round-trips through Open.
- `app.go` PM: `SetTaskStatus` valid sets status+Done, invalid rejected; `ReorderTasks`
  reorders and never drops; `ToggleTask` keeps Status/Done in sync; `AddTask` -> todo;
  ProjectView DoneCount/TaskCount correct.
- `app.go` Agenda: seed the store with a deadline, a due task, a done task (excluded), a
  doing-no-due task (included); assert item kinds, exclusion of done, name fallback to
  basename, and Due-ascending order with empty dues last.
- Front end: build-verified (`wails build`).
