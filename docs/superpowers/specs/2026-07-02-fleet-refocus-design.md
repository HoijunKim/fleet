# fleet - Refocus to a Multi-Repo Control Panel - Design

**Date:** 2026-07-02
**Status:** Design approved, pending spec review
**Supersedes emphasis of** the "unified project command center" direction. Driven by a
cold three-lens evaluation (UX/IA, product, architecture) that found fleet had become
four half-products (multi-repo git glance + shallow git client + task manager + vanity
charts) with no spine. This refocuses fleet on its one differentiated job.

## Identity

**fleet is a control panel for many local git repos:** see the state of all your repos
at once, know what needs attention, and act - on one or in bulk - without cd-ing
around. Everything else is secondary and must not crowd this out.

## Scope

**In (this refocus):**
1. **Overview as the default landing view:** portfolio stat tiles + one ranked "Needs
   attention" queue + one aggregate commit-activity heatmap.
2. **Bulk actions:** Pull all, Push all (repos with unpushed commits), and multi-select
   to run a bulk action on the selected repos; progress via toasts.
3. **Detail panel restructured into tabs** (Overview / Git / Tasks) with the inline
   arbitrary-command runner removed.
4. **Architecture fixes** that are prerequisite for bulk and fix real bugs (see below).

**Demoted (kept, de-emphasized):**
- Project-management (tasks/notes/deadline/status/priority) moves into a light **Tasks
  tab** on the detail panel. Manual (non-code) projects remain but are no longer a
  headline; they show only the Tasks tab.

**Cut:**
- The language-distribution bar (decorative).
- The `tags` field from the UI (unused; the backend field may remain but nothing reads
  or writes it).
- The **per-project** commit heatmap (kept only as the single aggregate one in
  Overview).
- The standalone **Today** view (its content is absorbed into Overview).

**Explicitly still deferred:** Notion integration, GitHub CI/PR mission control.

## Views

Two top-level views (toolbar toggle), Overview default:
- **Overview** (default): the fleet-scale answer to "what needs me across everything."
- **Projects**: the existing sortable/filterable list (unchanged in spirit).

### Overview contents
- **Stat tiles:** total repos, active, dirty, behind, **unpushed (ahead>0)**, **overdue**
  (deadline in the past among projects that have one). Computed once, client-side.
- **Needs-attention queue:** a single ranked list merging, per repo/project: dirty,
  behind, unpushed, overdue-deadline, and stale (no commit in N days). Each row states
  why it needs attention and is clickable (selects it / opens detail). This replaces
  the three separate Today cards.
- **Aggregate commit-activity heatmap:** the one heatmap (summed across code repos).

## Bulk actions
- **Pull all** and **Push all** operate over the relevant repos (all git repos for
  pull; repos with `ahead > 0` for push), fan out through the existing `Pull`/`Push`
  bindings, and report per-repo success/failure via toasts, then refresh.
- **Multi-select:** the Projects table gains selection (checkbox / modifier-click); a
  bulk action bar runs Fetch/Pull/Push over the selected set.
- These reuse existing bindings; the backend adds no new git op for bulk (orchestrated
  client-side), but see the architecture fixes for the concurrency cap.

## Detail panel (tabs)
- **Overview tab:** identity + a read-only status summary (branch, dirty/ahead/behind,
  last commit, deadline, task progress). Glanceable.
- **Git tab:** branch switcher, changed-files -> diff, commit box, and the
  fetch/pull/editor/terminal action row; stash and history behind collapsed sections.
- **Tasks tab:** the light task checklist + deadline + notes + status/priority.
- The inline arbitrary-command runner is **removed** (redundant with Open Terminal and a
  footgun). The per-project heatmap is removed.
- Manual projects show only the Tasks tab.

## Architecture fixes (prerequisite)

These fix real defects the evaluation found and are required before bulk actions:

1. **Atomic read-modify-write in the store.** Add `store.Update(id string, fn func(*Record)) error`
   that takes the write lock, reads, applies `fn`, and saves - all under one lock. Move
   the task/project mutation bodies in `app.go` onto it, replacing the non-atomic
   `Get`-then-`Put` pairs (which can silently lose an update when two mutations overlap,
   e.g. a debounced notes save vs a task toggle).
2. **Stop re-scanning the filesystem on every edit.** Project/task mutation bindings
   return the updated `ProjectView` so the front end patches the one changed row instead
   of calling `ListProjects()` (which re-walks all roots) after every checkbox toggle.
3. **Cap the `LoadRepo` fan-out** with a small concurrency limit (e.g. 6-8) so N repos do
   not spawn ~4N git subprocesses at once; the Overview aggregate heatmap uses the same
   cap.
4. **Merge robustness.** Add `.catch` to every `LoadRepo` call (a rejected IPC must reset
   that row's skeleton, not leave a permanent "loading"), and apply the `reqGen`
   stale-drop guard consistently to `fetchAll`/refresh/auto-fetch, not only the initial
   load.

(Stable store-assigned ids for code projects - so a moved repo keeps its data - are
noted as a follow-up, not in this refocus.)

## Error handling
- Bulk actions never abort the batch on one repo's failure; each result is independent,
  failures are counted and toasted.
- All list-returning bindings stay non-nil (already the case); front-end renders remain
  null-safe for manual rows lacking git fields.

## Testing
- `store.Update`: TDD - concurrent overlapping updates do not lose data (a
  read-modify-write test that would fail with the old Get/Put pair).
- Mutation bindings returning the updated `ProjectView`: unit-tested against a temp store.
- Front end: build-verified (`wails build`); visual behavior is a manual check.

## Distribution
Unchanged (single Wails binary, no new deps).
