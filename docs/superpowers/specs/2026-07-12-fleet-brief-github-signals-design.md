# Fleet Brief - GitHub Signals - Design Spec (Intel Slice 2)

**Date:** 2026-07-12
**Status:** Approved for planning
**Topic:** Feed each repo's GitHub status (CI result, open PR count, open issue count) - already fetched for the per-repo badges - into the morning-brief prompt, so the AI's priorities factor in a failing CI or waiting PRs, not just local git state.

## Goal

The brief today reasons over local git state (dirty, behind/ahead, deadlines, tasks, TODOs) + Notion tasks, but ignores GitHub signals that fleet already knows: CI pass/fail, open PRs, open issues. This slice gathers those signals for all repos and puts them in the brief prompt so a failing CI or a stack of open PRs surfaces as a priority.

## Context

- `internal/gh` `Fetch(r Runner, owner, repo) (Info{CI, PRs, Issues, Available}, error)` queries the `gh` CLI; `OwnerRepo(remote)` parses a remote into owner/repo.
- `app.go` `GitHubInfo(remote) GitHubView{ci,prs,issues,available}` wraps `gh.Fetch` behind a cache (`ghCache`/`ghMu`/`ghTTL`), and is called per-repo, lazily, by `GitHubBadge.svelte`. Its cache+fetch body is the logic we want to reuse for a bulk call.
- `git.RemoteURL(r, dir) (string, error)` returns a repo's remote URL; `scan.Discover(roots, depth, false)` enumerates repos.
- The brief is built in `Today.svelte` `buildPrompt()`: `projectLine(p)` renders a compact per-project state string from the shared `projects` array's git fields. The `projects` array does NOT carry GitHub info (badges fetch it separately), so the brief can't see it today.

## Global Constraints

- **No new runtime dependencies.** Go stdlib (`sync` for bounded parallelism) + existing `internal/gh`/`internal/git`/`internal/scan`; `frontend/package.json` unchanged.
- **Reuse the existing GitHub cache** (`ghCache`/`ghTTL`) - the bulk call must share the badge cache so a repo isn't fetched twice, and a cold bulk fetch warms the cache the badges then reuse.
- **Best-effort + non-blocking:** a repo with no remote / a non-GitHub remote / a `gh` failure is simply omitted from the signals (never an error that blocks the brief); the brief still builds from git+Notion if GitHub is entirely unavailable.
- **Bounded parallelism:** the bulk fetch runs a bounded worker pool (do not spawn one `gh` process per repo unbounded); reuse of the cache keeps repeat calls instant.
- **No behavior change to `GitHubInfo`/badges** beyond extracting a shared helper.
- **Green gates:** `go build`/`go vet`/`go test ./...` green, `npx svelte-check` 0 errors, `npx vitest run` green, `wails build` succeeds.

## Workstream 1 - Bulk GitHub signals (backend)

- **Extract a shared helper** `githubInfoForRemote(remote string) GitHubView` from the current `GitHubInfo` body (owner/repo parse -> cache read -> `gh.Fetch` -> cache write). `GitHubInfo` becomes a one-line delegate. No behavior change.
- **New binding `GitHubSignals() []RepoGHSignal`** where `RepoGHSignal{RepoPath, Name, CI string; PRs, Issues int}` (json `repoPath`/`name`/`ci`/`prs`/`issues`). It:
  - Iterates `scan.Discover(cfg.Roots, cfg.ScanDepth, false)`; for each repo resolves the remote via `git.RemoteURL(a.runner, r.Path)`; skips repos with no remote or a non-GitHub remote (`gh.OwnerRepo` returns `ok=false`).
  - Fetches each via `githubInfoForRemote` (cache-backed) inside a **bounded worker pool** (e.g. 6 concurrent) using `sync.WaitGroup` + a buffered channel semaphore.
  - Includes only repos where the result is `Available`; returns a `RepoGHSignal` per included repo (order not important). A `gh`-unavailable environment returns an empty slice, not an error.
- The bounded fetch + shared cache keep it responsive; a warm cache (badges already loaded) makes it near-instant.

## Workstream 2 - GitHub signals in the brief (frontend)

- **`Today.svelte`:** before building the brief (in the same load path that gathers project state / just before `buildPrompt`), call `GitHubSignals()` once and index the results by `repoPath` into a `Map`. Store on a component variable so `projectLine` can read it.
- **`projectLine(p)`:** look up the repo's GitHub signal by its path (`p.repoPath || p.path`). Append bits:
  - CI: when the CI result indicates failure (e.g. `ci === "failure"` / `"failing"` / non-success non-empty), add `CI failing` (a high-signal bit); optionally `CI passing` is omitted to keep the line tight (only surface problems). In-progress -> `CI running`.
  - `N open PRs` when `prs > 0`.
  - `N open issues` when `issues > 0` (only if meaningfully > 0 to avoid noise; keep the threshold simple, e.g. > 0).
- **Prompt copy** (`buildPrompt`): add a short instruction that GitHub signals are priority factors - a failing CI is urgent (likely blocks shipping), open PRs are waiting on review/merge - so the AI weighs them alongside uncommitted work and deadlines. Keep the ASCII-punctuation rule already in the prompt.
- If `GitHubSignals()` returns empty (gh unavailable), `projectLine` adds nothing GitHub-related and the brief is unchanged - fully graceful.

## Data Flow

Brief load -> `GitHubSignals()` (bounded parallel, cached) -> Map by repoPath -> `projectLine` merges CI/PR/issue bits -> `buildPrompt` (with the GitHub-priority instruction) -> AI brief that weighs GitHub status. No change to how badges fetch `GitHubInfo` (now via the shared helper).

## Error Handling / Edge Cases

- gh missing / unauthenticated / a repo's fetch fails -> that repo omitted; empty signals -> brief unaffected.
- Repo with no remote or non-GitHub remote -> skipped.
- CI string variants: treat empty/`success` as "no problem" (omit or "passing"), any non-success non-empty conclusion as a failure/attention bit; `in_progress`/`queued` -> running. Keep the mapping in a small tested helper.
- Cache: bulk and per-badge share `ghCache`; TTL expiry unchanged.

## Testing Strategy

- Backend: `GitHubSignals` with a fake `runner`/`ghRunner` + a temp-dir repo set (mirroring the `SearchAll`/`SearchFiles` test pattern): includes only GitHub-remote repos, omits no-remote/non-github, populates CI/PRs/Issues, and returns empty (not error) when gh fails. The shared `githubInfoForRemote` extraction must keep `GitHubInfo`'s existing tests green.
- Frontend: a small pure helper `ciBit(ci: string): string` (failure/running/none) unit-tested; and the `projectLine` GitHub merge is exercised via the helper (extract the signal-merge into a tiny tested function if it keeps `projectLine` testable, else assert via the helper).
- Existing suites green; `wails build` succeeds.

## Out of Scope (YAGNI)

- Per-project drill-down UI, one-click actions, CI logs - later slices ("brief 심화" UI).
- PR/issue titles or details (counts only, matching the existing badge data).
- Non-GitHub CI providers.
- Refetching on a timer (the existing cache/TTL governs freshness).

## File Structure

- **Modify:** `app.go` (extract `githubInfoForRemote`; add `RepoGHSignal` + `GitHubSignals` + a small `ciAttention` mapping if backend-side), `app_test.go` (GitHubSignals test), `frontend/src/lib/Today.svelte` (fetch + merge + prompt copy), regenerate `frontend/wailsjs/**` for `GitHubSignals`/`RepoGHSignal`.
- **Create:** `frontend/src/lib/ciBit.ts` (+ `ciBit.test.ts`) for the CI-string -> brief-bit mapping.
