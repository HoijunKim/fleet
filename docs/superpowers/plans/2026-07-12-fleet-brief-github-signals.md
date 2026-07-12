# Fleet Brief GitHub Signals Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Put each repo's GitHub status (CI result, open PRs, open issues) into the morning-brief prompt so the AI weighs a failing CI / waiting PRs as priorities.

**Architecture:** A `GitHubSignals()` binding bulk-fetches per-repo GitHub status (bounded parallel, reusing the badge cache); `Today.svelte` merges those signals into `projectLine` and adds a prompt instruction that CI/PRs are priority factors.

**Tech Stack:** Go 1.22 (stdlib `sync`), Wails v2, Svelte-TS, vitest, Go testing.

## Global Constraints

From the spec `docs/superpowers/specs/2026-07-12-fleet-brief-github-signals-design.md`:
- **No new runtime dependencies** (Go stdlib + existing `internal/gh`/`git`/`scan`; `frontend/package.json` unchanged).
- **Reuse the existing GitHub cache** (`ghCache`/`ghTTL`); the bulk call shares it with the badges.
- **Best-effort/non-blocking:** no-remote / non-GitHub / `gh`-failure repos are omitted, never an error; empty signals leave the brief unchanged.
- **Bounded parallelism** for the bulk fetch (no unbounded `gh` process spawn).
- **No behavior change to `GitHubInfo`/badges** beyond extracting a shared helper (its existing tests stay green).
- **Green gates:** `go build`/`go vet`/`go test ./...`, `npx svelte-check` 0 errors, `npx vitest run`, `wails build`.

## Commit authorship (all tasks)
`git -c user.name=hoijun -c user.email=hoijun.kim00@gmail.com commit -m "..."` - NO Co-Authored-By/Claude trailer.

---

## Task 1: Bulk GitHub signals (backend)

**Files:**
- Modify: `app.go` (extract `githubInfoForRemote`; add `RepoGHSignal` + `GitHubSignals`)
- Modify: `app_test.go` (GitHubSignals test)

**Interfaces:**
- Produces: `RepoGHSignal{RepoPath, Name, CI string; PRs, Issues int}` (json `repoPath`/`name`/`ci`/`prs`/`issues`) and `GitHubSignals() []RepoGHSignal`. Internal: `githubInfoForRemote(remote string) GitHubView` (extracted from `GitHubInfo`).

- [ ] **Step 1: Extract the shared helper (no behavior change).** In `app.go`, move the body of `GitHubInfo` into `func (a *App) githubInfoForRemote(remote string) GitHubView { ... }` (the owner/repo parse -> cache read -> `gh.Fetch` -> cache write logic, verbatim), and make `GitHubInfo` delegate: `func (a *App) GitHubInfo(remote string) GitHubView { return a.githubInfoForRemote(remote) }`. Run `go test ./ -run TestGitHubInfo` (and the full `go test ./...`) to confirm existing GitHub tests still pass - this refactor must be green before adding new code.

- [ ] **Step 2: Write the failing test** - in `app_test.go` add `TestGitHubSignals`. Mirror the `SearchAll`/`SearchFiles` test setup (fake runner(s) + temp git repos under a discovered root). Assert: only repos with a GitHub remote are included; a repo with no remote (or a non-GitHub remote) is omitted; CI/PRs/Issues are populated from the (faked) gh output; when gh is unavailable the result is an empty (non-nil) slice, not an error. Read the existing `SearchAll`/`GitHubInfo` tests first for the exact fake `runner`/`ghRunner` types and how a temp git repo with a remote is created.

```go
// Shape (adapt fakes/setup to what app_test.go already provides):
func TestGitHubSignals(t *testing.T) {
	// set up an App whose Discover finds two temp git repos: one with a
	// github.com remote, one with no remote; ghRunner returns a known
	// CI/PR/issue payload for the github one.
	got := app.GitHubSignals()
	if len(got) != 1 {
		t.Fatalf("only the github-remote repo should be included: %+v", got)
	}
	s := got[0]
	if s.CI == "" || s.PRs < 0 {
		t.Fatalf("signal not populated: %+v", s)
	}
}
```

Run: `go test ./ -run TestGitHubSignals`. Expected: FAIL (undefined: GitHubSignals).

- [ ] **Step 3: Implement `GitHubSignals`** - in `app.go`:

```go
// RepoGHSignal is one repo's GitHub status for the brief.
type RepoGHSignal struct {
	RepoPath string `json:"repoPath"`
	Name     string `json:"name"`
	CI       string `json:"ci"`
	PRs      int    `json:"prs"`
	Issues   int    `json:"issues"`
}

// GitHubSignals bulk-fetches GitHub status for every discovered repo that has a
// GitHub remote, bounded-parallel and cache-backed (shares the badge cache).
// Repos with no/non-GitHub remote or an unavailable result are omitted; a
// gh-less environment returns an empty slice, never an error.
func (a *App) GitHubSignals() []RepoGHSignal {
	out := []RepoGHSignal{}
	cfg := a.cfgSnapshot()
	repos := scan.Discover(cfg.Roots, cfg.ScanDepth, false)

	type job struct {
		path, name, remote string
	}
	var jobs []job
	for _, r := range repos {
		remote, err := git.RemoteURL(a.runner, r.Path)
		if err != nil || strings.TrimSpace(remote) == "" {
			continue
		}
		if _, _, ok := gh.OwnerRepo(remote); !ok {
			continue // not a GitHub remote
		}
		jobs = append(jobs, job{path: r.Path, name: r.Name, remote: remote})
	}

	results := make([]RepoGHSignal, len(jobs))
	found := make([]bool, len(jobs))
	sem := make(chan struct{}, 6) // bounded worker pool
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func(i int, j job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			v := a.githubInfoForRemote(j.remote)
			if !v.Available {
				return
			}
			results[i] = RepoGHSignal{RepoPath: j.path, Name: j.name, CI: v.CI, PRs: v.PRs, Issues: v.Issues}
			found[i] = true
		}(i, j)
	}
	wg.Wait()
	for i := range jobs {
		if found[i] {
			out = append(out, results[i])
		}
	}
	return out
}
```

(Ensure `sync` is imported in `app.go`.)

- [ ] **Step 4: Run test, verify pass** - `go test ./ -run TestGitHubSignals`. Expected: PASS.

- [ ] **Step 5: Verify + commit** - `go build ./... && go vet ./... && go test ./...` (green, incl. the untouched GitHubInfo tests). Then:

```bash
git add app.go app_test.go
git commit -m "feat(intel): GitHubSignals - bulk per-repo CI/PR/issue status for the brief (bounded parallel, cached)"
```

---

## Task 2: GitHub signals in the brief (frontend)

**Files:**
- Create: `frontend/src/lib/ciBit.ts`, `frontend/src/lib/ciBit.test.ts`
- Modify: `frontend/src/lib/Today.svelte`
- Modify (generated): `frontend/wailsjs/**` (regenerate for `GitHubSignals`/`RepoGHSignal`)

**Interfaces:**
- Consumes: `GitHubSignals` (Task 1).
- Produces: `ciBit(ci: string): string` - maps a CI conclusion to a brief bit: `""` for empty/`success`/`neutral`/`skipped`; `"CI running"` for `in_progress`/`queued`/`pending`; `"CI failing"` for any other non-empty conclusion (`failure`/`timed_out`/`cancelled`/`action_required`/etc).

- [ ] **Step 1: Write the failing test** - `frontend/src/lib/ciBit.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { ciBit } from "./ciBit";

describe("ciBit", () => {
  it("no bit for success/neutral/empty", () => {
    for (const c of ["", "success", "neutral", "skipped"]) expect(ciBit(c)).toBe("");
  });
  it("running for in-progress/queued", () => {
    for (const c of ["in_progress", "queued", "pending"]) expect(ciBit(c)).toBe("CI running");
  });
  it("failing for failure/timed_out/cancelled/action_required", () => {
    for (const c of ["failure", "timed_out", "cancelled", "action_required"]) expect(ciBit(c)).toBe("CI failing");
  });
});
```

- [ ] **Step 2: Run it, verify it fails** - `cd frontend && npx vitest run src/lib/ciBit.test.ts`. Expected: FAIL (module not found).

- [ ] **Step 3: Implement** - `frontend/src/lib/ciBit.ts`:

```ts
// ciBit maps a GitHub CI conclusion/status to a compact brief bit. Only
// problems/in-flight surface; success/neutral produce no bit (keep lines tight).
export function ciBit(ci: string): string {
  const c = (ci ?? "").trim().toLowerCase();
  if (c === "" || c === "success" || c === "neutral" || c === "skipped") return "";
  if (c === "in_progress" || c === "queued" || c === "pending" || c === "requested" || c === "waiting") return "CI running";
  return "CI failing";
}
```

- [ ] **Step 4: Run tests, verify pass** - `cd frontend && npx vitest run src/lib/ciBit.test.ts`. Expected: PASS (3/3).

- [ ] **Step 5: Wire into Today.svelte.** Read the CURRENT `frontend/src/lib/Today.svelte` (the `buildPrompt`/`projectLine`/`generate`/`maybeAutoBrief` flow). Then:
  - Import `GitHubSignals` from the bindings and `ciBit` from `./ciBit`. Add `let ghByPath = new Map<string, {ci:string;prs:number;issues:number}>();`.
  - Load signals once before a brief is built: in `generate()` (right before `buildPrompt()`), `try { const sigs = await GitHubSignals(); ghByPath = new Map(sigs.map((s:any) => [s.repoPath, s])); } catch { ghByPath = new Map(); }`. (Non-blocking: a failure leaves the map empty.)
  - In `projectLine(p)`, after the existing git bits, look up `ghByPath.get(p.repoPath || p.path)` and append: the `ciBit(g.ci)` if non-empty; `g.prs + " open PRs"` if `g.prs > 0`; `g.issues + " open issues"` if `g.issues > 0`.
  - In `buildPrompt()`, add one sentence to the instruction block: GitHub signals are priority factors - a failing CI likely blocks shipping (urgent), open PRs are waiting on review/merge - weigh them alongside uncommitted work and deadlines. Keep the existing ASCII-punctuation instruction.
  - If `ghByPath` is empty, `projectLine` adds nothing GitHub-related (graceful).

- [ ] **Step 6: Regenerate Wails bindings** - `wails generate module` (repo root) so `GitHubSignals`/`RepoGHSignal` appear in `frontend/wailsjs/go/main/App.{d.ts,js}` + `models.ts`; confirm additive.

- [ ] **Step 7: Verify** - `cd frontend && npx vitest run && npx svelte-check` (green, 0 errors), then `cd .. && wails build`. Manual-smoke note in the report: with a repo whose CI is failing / has open PRs, the generated brief mentions it; gh-unavailable leaves the brief unchanged.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/lib/ciBit.ts frontend/src/lib/ciBit.test.ts frontend/src/lib/Today.svelte frontend/wailsjs
git commit -m "feat(intel): brief weighs GitHub signals - CI failing / open PRs / issues per repo"
```

---

## Self-Review Notes (author)

- **Spec coverage:** W1 -> Task 1; W2 -> Task 2. Every File-Structure entry appears in a task.
- **Type consistency:** `RepoGHSignal{repoPath,name,ci,prs,issues}` (Task 1) is consumed by Today's `ghByPath` map + `projectLine` (Task 2). `ciBit(ci)` mapping (Task 2) matches the spec's CI-string handling.
- **No-regression / non-blocking:** the `githubInfoForRemote` extraction preserves `GitHubInfo` behavior (Task 1 Step 1 verifies existing tests); a failed/empty `GitHubSignals` leaves `projectLine` and the brief unchanged (Task 2 graceful path).
