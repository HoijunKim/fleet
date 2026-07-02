# fleet - Unified Project Command Center (v1) - Design

**Date:** 2026-07-02
**Status:** Design approved, pending spec review
**Builds on:** the existing Wails desktop app (multi-repo git dashboard). This adds a
local-first project-management layer so fleet manages *all* of the user's projects,
not just code repos.

## Why

The git dashboard manages only the code-repo facet of the user's projects. Real
project management is broader: tasks, deadlines, status, priorities, notes - and many
of the user's projects are not code at all. This turns fleet into a single command
center for every project, with git as one facet of the code-backed ones.

## Scope

**In (v1):**
- A local-first store (source of truth for project-management data).
- Discovered git repos auto-register as "code" projects; non-code projects are added
  manually. Both live in one unified list.
- Per project: tasks (checklist), deadline, notes, status, priority, tags.
- A unified dashboard showing, per project: name, type, git status (code projects),
  task progress, deadline countdown, status, priority.
- A "Today / Focus" view aggregating across projects: upcoming deadlines, open
  high-priority tasks, and git attention -> what to work on now.
- A commit-activity heatmap (GitHub contribution-graph style: colored day squares)
  from local git history - per code project, plus an aggregate heatmap across all
  code projects in the dashboard. Purely from `git log` dates; no GitHub API.

**Deferred (later phases, explicitly out of v1):**
- Notion integration (the second axis of the hybrid model).
- GitHub mission control (CI status, PR/issue counts).
- Fleet-scale bulk actions (pull-all / push-all / cross-repo search).

## Data model

Local store is the source of truth for project-management data. Git status is read
live (as today) and merged in for code projects; it is never persisted.

```
Project {
  id        string        // code: repo path; manual: generated id
  name      string
  type      "code" | "manual"
  repoPath  string        // set for code projects
  status    "active" | "paused" | "done"
  priority  int           // 0..3 (0 = none, 3 = highest)
  deadline  string        // "YYYY-MM-DD" or ""
  notes     string
  tags      []string
  tasks     []Task
}

Task { id string; title string; done bool; due string /* YYYY-MM-DD or "" */ }
```

- **Code projects:** a discovered repo auto-registers with `id = repoPath`,
  `type = "code"`. Its git status/branch/dirty/ahead-behind is read live and shown;
  its project-management fields come from the local store (empty until edited).
- **Manual projects:** created by the user with a generated `id`, `type = "manual"`,
  no repo; they carry the same project-management fields.

## Architecture

- **`internal/store`** (new): persists project-management data as a single JSON file
  at the config directory (`%APPDATA%\fleet\projects.json` on Windows; XDG elsewhere).
  The file holds ONE map, `id -> Record`, where a `Record` carries the
  project-management fields (status, priority, deadline, notes, tags, tasks) plus
  `manual bool` and, for manual projects only, `name`. Code projects have a record
  only once they gain project-management data or are edited (their name/repoPath come
  from the scan, not the store); manual projects always have a record (it is their
  sole source). Pure Go, no cgo. (Alternative considered: pure-Go SQLite via modernc -
  overkill at this scale; JSON chosen for simplicity and zero cgo.)
- **Project assembly:** the unified project list = discovered repos (auto, git status
  read live, each merged with its store `Record` by id if one exists) UNION the manual
  projects (the store records where `manual == true`).
- **Bindings (app.go):** `ListProjects() []ProjectView`; `AddProject(name) ...`;
  `UpdateProject(...)` (status/priority/deadline/notes/tags); task ops
  `AddTask/ToggleTask/DeleteTask`; `CommitActivity(path, weeks) []DayCount` (for the
  heatmap); existing git bindings unchanged. Reads/writes go through `internal/store`;
  store access is synchronized (RWMutex) like `app.cfg`.
- **Commit activity:** a new git op in `internal/git` runs
  `git log --since=<weeks> --date=short --pretty=format:%cd` and tallies commits per
  day, returning `[]DayCount{date, count}`; the front end colors squares by count and
  sums per-date across code projects for the aggregate heatmap.
- The existing repo-scan/git/meta pipeline is reused for the git facet of code
  projects.

## UI

- **Unified project list:** columns for name, type badge, git status (code projects
  only), task progress (e.g. 3/7), deadline (D-N countdown), status, priority.
  Filters/sort extend the current ones with status / priority / deadline.
- **Today / Focus view:** a top-level view aggregating across all projects - imminent
  deadlines, open high-priority tasks, and git-attention repos - answering "what do I
  work on now."
- **Detail panel - project-management section:** task checklist (add / toggle /
  delete), deadline picker, notes, status and priority controls. For code projects
  this sits alongside the existing git actions (branch/commit/diff/history/stash) and
  a commit-activity heatmap; manual projects show only the project-management section.
- **Commit-activity heatmap:** a GitHub-style contribution grid (weeks x 7 days,
  squares shaded by that day's commit count, with a small legend). Shown per code
  project in its detail panel, and as an aggregate (summed across all code projects)
  in the dashboard / Today view.
- Keep the established premium visual language and ASCII-only text.

## Error handling

- Store load failure (missing/corrupt JSON): start from an empty store, surface a
  toast, and do not overwrite the file until the user makes a change (avoid clobbering
  a recoverable file).
- Writes are atomic (temp file + rename) so a crash mid-write cannot corrupt the store.
- A manual project referencing a deleted path, or a code project whose repo vanished,
  still renders (marked accordingly) rather than breaking the list.

## Testing

- `internal/store`: TDD - load/save round-trip, atomic write, default-on-missing,
  merge of stored project-management data with a discovered repo, task operations.
- Commit-activity git op: TDD - parse sample `git log` date output into per-day counts
  (via the `Runner` fake).
- Bindings: unit-tested against a temp store dir (as `SaveConfig` is), no real Notion
  or network.
- Front end: build-verified (`wails build`); visual behavior is a manual check.

## Distribution

Unchanged (single Wails binary). No new external dependencies (JSON store is stdlib;
no cgo).
