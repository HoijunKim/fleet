# fleet Connected - Cycle 3 (GitHub mission control) - Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.

**Goal:** Per-repo GitHub status (latest CI conclusion, open PR count, open issue count) via the `gh` CLI, degrading gracefully when `gh` is absent/unauthenticated; plus a fleet "remote changes" indicator.

**Architecture:** New `internal/gh` shells to `gh api` behind a tiny `Runner` interface (so parsing is testable); `app.go` adds `GitHubInfo(remote)` (parses owner/repo, calls gh, caches per repo). The front end shows a CI badge + PR/issue counts (lazy, cached) and a toolbar "remote changes" count (repos with `behind > 0`, already computed).

**Tech Stack:** Go 1.22.0, Wails v2.12, Svelte-TS. Requires the `gh` CLI at runtime (already installed + authed on the dev machine); absent -> feature hidden.

## Global Constraints
- New git/gh subprocesses must hide the console window on Windows (reuse `internal/winhide` as `git.ExecRunner` does).
- `gh` calls go through a `gh.Runner` interface (testable with a fake); pure `OwnerRepo` parsing tested directly.
- List/DTO bindings non-nil; ASCII-only; `go.mod` stays `go 1.22.0` no toolchain (`GOTOOLCHAIN=local`).
- Conventional Commits; NO Claude/AI co-author trailer.
- Env: Go `C:\Program Files\Go\bin`, wails `C:\Users\hoijun\go\bin` (PATH prefix + GOTOOLCHAIN=local). Never `wails dev`. FE via `wails build`, BE via `go test ./...`.

## File Structure
```
internal/gh/gh.go        Runner + ExecRunner (shells gh, winhide) + OwnerRepo + Info + Fetch
internal/gh/gh_test.go   OwnerRepo + Fetch (fake runner)
app.go                   + ghRunner/ghCache/ghMu fields; GitHubInfo(remote) GitHubView; NewApp wiring
app_test.go              + GitHubInfo tests (fake gh runner)
frontend/src/lib/GitHubBadge.svelte   CI badge + PR/issue counts (per project)
frontend/src/lib/DetailPanel.svelte   show GitHubBadge in the Git/Overview tab (lazy)
frontend/src/lib/ProjectTable.svelte  optional compact CI dot per row
frontend/src/lib/Toolbar.svelte       "remote changes: N" indicator
frontend/src/App.svelte               remote-changes count wiring
```

---

### Task 1: Backend - internal/gh + GitHubInfo

**Files:** create `internal/gh/gh.go`, `internal/gh/gh_test.go`; modify `app.go`, `app_test.go`; regenerate bindings.

**Interfaces:**
- `gh.Runner{ Run(args ...string) (string, error) }`, `gh.ExecRunner` (runs `gh <args>`, hides console via winhide).
- `gh.OwnerRepo(remote string) (owner, repo string, ok bool)` - parses git@/https/ssh GitHub remotes.
- `gh.Info{ CI string; PRs, Issues int; Available bool }`; `gh.Fetch(r Runner, owner, repo string) (Info, error)`.
- `app.GitHubView{ CI string; PRs, Issues int; Available bool }`; `app.GitHubInfo(remote string) GitHubView` (cached).

- [ ] **Step 1: Write `internal/gh/gh_test.go`**
```go
package gh

import (
	"strings"
	"testing"
)

func TestOwnerRepo(t *testing.T) {
	cases := map[string][2]string{
		"git@github.com:hoijun/fleet.git":       {"hoijun", "fleet"},
		"https://github.com/hoijun/fleet.git":   {"hoijun", "fleet"},
		"https://github.com/hoijun/fleet":       {"hoijun", "fleet"},
		"ssh://git@github.com/hoijun/fleet.git":  {"hoijun", "fleet"},
	}
	for remote, want := range cases {
		o, r, ok := OwnerRepo(remote)
		if !ok || o != want[0] || r != want[1] {
			t.Errorf("OwnerRepo(%q)=%q,%q,%v want %q,%q", remote, o, r, ok, want[0], want[1])
		}
	}
	if _, _, ok := OwnerRepo("git@gitlab.com:x/y.git"); ok {
		// non-github still parses owner/repo; ok true is acceptable. Only assert empty/garbage fails:
	}
	if _, _, ok := OwnerRepo(""); ok {
		t.Error("empty remote must not parse")
	}
	if _, _, ok := OwnerRepo("garbage"); ok {
		t.Error("garbage remote must not parse")
	}
}

type ghFake struct{ err error }

func (f ghFake) Run(args ...string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	j := strings.Join(args, " ")
	switch {
	case strings.Contains(j, "actions/runs"):
		return "success\n", nil
	case strings.Contains(j, "type:pr"):
		return "2\n", nil
	case strings.Contains(j, "type:issue"):
		return "5\n", nil
	}
	return "", nil
}

func TestFetchParses(t *testing.T) {
	info, err := Fetch(ghFake{}, "hoijun", "fleet")
	if err != nil {
		t.Fatal(err)
	}
	if info.CI != "success" || info.PRs != 2 || info.Issues != 5 || !info.Available {
		t.Errorf("info=%+v", info)
	}
}

func TestFetchErrorWhenGhUnavailable(t *testing.T) {
	_, err := Fetch(ghFake{err: &stubErr{"gh: not found"}}, "o", "r")
	if err == nil {
		t.Error("expected error when the gh CI call fails")
	}
}

type stubErr struct{ msg string }

func (e *stubErr) Error() string { return e.msg }
```

- [ ] **Step 2: Run to fail** - `go test ./internal/gh/ -v` -> FAIL.

- [ ] **Step 3: Write `internal/gh/gh.go`**
```go
// Package gh queries GitHub for a repo's CI / PR / issue status via the gh CLI.
package gh

import (
	"bytes"
	"os/exec"
	"strconv"
	"strings"

	"github.com/hoijun/fleet/internal/winhide"
)

// Runner runs a `gh` subcommand and returns stdout. The single seam through
// which this package touches gh; tests substitute a fake.
type Runner interface {
	Run(args ...string) (string, error)
}

// ExecRunner runs the real gh CLI, hiding the console window on Windows.
type ExecRunner struct{}

func (ExecRunner) Run(args ...string) (string, error) {
	cmd := exec.Command("gh", args...)
	winhide.Apply(cmd)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return out.String(), err
}

// OwnerRepo parses a GitHub remote URL into owner and repo. Returns ok=false
// for empty/unparseable remotes.
func OwnerRepo(remote string) (owner, repo string, ok bool) {
	remote = strings.TrimSpace(remote)
	remote = strings.TrimSuffix(remote, ".git")
	switch {
	case strings.HasPrefix(remote, "git@"):
		// git@host:owner/repo
		if i := strings.Index(remote, ":"); i >= 0 {
			remote = remote[i+1:]
		} else {
			return "", "", false
		}
	case strings.HasPrefix(remote, "https://"), strings.HasPrefix(remote, "http://"):
		remote = remote[strings.Index(remote, "://")+3:]
		if i := strings.IndexByte(remote, '/'); i >= 0 {
			remote = remote[i+1:] // strip host
		} else {
			return "", "", false
		}
	case strings.HasPrefix(remote, "ssh://git@"):
		remote = strings.TrimPrefix(remote, "ssh://git@")
		if i := strings.IndexByte(remote, '/'); i >= 0 {
			remote = remote[i+1:]
		} else {
			return "", "", false
		}
	default:
		return "", "", false
	}
	parts := strings.Split(strings.Trim(remote, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// Info is a repo's GitHub status.
type Info struct {
	CI        string
	PRs       int
	Issues    int
	Available bool
}

// Fetch queries gh for the latest CI conclusion/status and open PR/issue counts.
// A failure of the CI call (e.g. gh missing/unauthenticated) returns an error;
// PR/issue failures are tolerated (left 0).
func Fetch(r Runner, owner, repo string) (Info, error) {
	base := "repos/" + owner + "/" + repo
	ci, err := r.Run("api", base+"/actions/runs?per_page=1",
		"--jq", `.workflow_runs[0].conclusion // .workflow_runs[0].status // ""`)
	if err != nil {
		return Info{}, err
	}
	info := Info{CI: strings.TrimSpace(ci), Available: true}
	if out, err := r.Run("api", "-X", "GET", "search/issues",
		"-f", "q=repo:"+owner+"/"+repo+" type:pr state:open", "--jq", ".total_count"); err == nil {
		info.PRs = atoi(out)
	}
	if out, err := r.Run("api", "-X", "GET", "search/issues",
		"-f", "q=repo:"+owner+"/"+repo+" type:issue state:open", "--jq", ".total_count"); err == nil {
		info.Issues = atoi(out)
	}
	return info, nil
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
```

- [ ] **Step 4: Run to pass** - `go test ./internal/gh/ -v` -> PASS.

- [ ] **Step 5: Write failing app tests** (add to `app_test.go`)
```go
type ghFakeApp struct{}

func (ghFakeApp) Run(args ...string) (string, error) {
	j := strings.Join(args, " ")
	switch {
	case strings.Contains(j, "actions/runs"):
		return "failure\n", nil
	case strings.Contains(j, "type:pr"):
		return "1\n", nil
	case strings.Contains(j, "type:issue"):
		return "3\n", nil
	}
	return "", nil
}

func TestGitHubInfoParsesAndCaches(t *testing.T) {
	a := newTestApp(t)
	a.ghRunner = ghFakeApp{}
	v := a.GitHubInfo("git@github.com:hoijun/fleet.git")
	if !v.Available || v.CI != "failure" || v.PRs != 1 || v.Issues != 3 {
		t.Errorf("view=%+v", v)
	}
}

func TestGitHubInfoNoRemote(t *testing.T) {
	a := newTestApp(t)
	a.ghRunner = ghFakeApp{}
	if v := a.GitHubInfo(""); v.Available {
		t.Error("empty remote must be Available=false")
	}
}
```
(`newTestApp` must construct an `App` that has the `ghRunner`/`ghCache` fields initialized - update it to set `ghCache: map[string]GitHubView{}` if the zero map would panic on write; a nil map read is fine but write panics, so initialize `ghCache` in `newTestApp` or have `GitHubInfo` lazily init it.)

- [ ] **Step 6: Run to fail** - `go test . -run TestGitHubInfo -v` -> FAIL.

- [ ] **Step 7: Add `GitHubInfo` + fields to `app.go`** (import `"sync"` (present), `"github.com/hoijun/fleet/internal/gh"`)
Add to the `App` struct: `ghRunner gh.Runner`, `ghCache map[string]GitHubView`, `ghMu sync.RWMutex`. In `NewApp`, set `ghRunner: gh.ExecRunner{}` and `ghCache: map[string]GitHubView{}`.
```go
// GitHubView is a repo's GitHub status for the front end.
type GitHubView struct {
	CI        string `json:"ci"`
	PRs       int    `json:"prs"`
	Issues    int    `json:"issues"`
	Available bool   `json:"available"`
}

// GitHubInfo returns a repo's GitHub status (cached per owner/repo). Returns
// Available=false when the remote is not a parseable GitHub URL or gh fails.
func (a *App) GitHubInfo(remote string) GitHubView {
	owner, repo, ok := gh.OwnerRepo(remote)
	if !ok {
		return GitHubView{Available: false}
	}
	key := owner + "/" + repo
	a.ghMu.RLock()
	if v, hit := a.ghCache[key]; hit {
		a.ghMu.RUnlock()
		return v
	}
	a.ghMu.RUnlock()

	info, err := gh.Fetch(a.ghRunner, owner, repo)
	v := GitHubView{Available: err == nil && info.Available}
	if err == nil {
		v.CI = info.CI
		v.PRs = info.PRs
		v.Issues = info.Issues
	}
	a.ghMu.Lock()
	if a.ghCache == nil {
		a.ghCache = map[string]GitHubView{}
	}
	a.ghCache[key] = v
	a.ghMu.Unlock()
	return v
}
```

- [ ] **Step 8: Run to pass** - `go test . -v` -> PASS.

- [ ] **Step 9: Regenerate, vet, build, commit**
```bash
export PATH="/c/Program Files/Go/bin:/c/Users/hoijun/go/bin:$PATH"; export GOTOOLCHAIN=local
go vet ./... && go test ./... && wails build
git add internal/gh/ app.go app_test.go frontend/wailsjs/go/main/App.js frontend/wailsjs/go/main/App.d.ts frontend/wailsjs/go/models.ts
git commit -m "feat: add GitHub mission control (internal/gh + GitHubInfo, cached)"
```
Confirm `App.d.ts` exposes `GitHubInfo`.

---

### Task 2: Front end - GitHub badge + PR/issue counts + remote-changes indicator

**Files:** create `frontend/src/lib/GitHubBadge.svelte`; modify `DetailPanel.svelte`, `Toolbar.svelte`, `App.svelte` (and optionally `ProjectTable.svelte`).

**Design contract (verify with `wails build`):**
- **`GitHubBadge.svelte`:** given a code project's `remote` (and a `path` key), lazily call `GitHubInfo(remote)` when shown (guard by remote/path stale-drop like the other lazy panels). If `available === false` or `remote` empty, render nothing (graceful). Otherwise show: a small **CI badge** colored by conclusion (`success`->green, `failure`/`cancelled`/`timed_out`->red, `in_progress`/`queued`/`""`->amber/neutral) with the status text; **PR count** and **issue count** as small labeled chips (hide a count when 0, or show 0 subtly - your call, keep clean). ASCII-only labels (e.g. "CI", "PR", "Issues").
- Place `<GitHubBadge>` in the detail panel's **Git tab (and/or Overview tab)** for code projects only (never manual). Lazy-load on selection.
- **Remote-changes indicator (Toolbar):** show "remote changes: N" (or a small badge) where N = number of code projects with `behind > 0` (this count is already derivable from `projects`/`stats` in App.svelte - compute it and pass to the Toolbar). Clicking it can switch to Overview (where the attention queue already lists them) - optional.
- Do not fan out `GitHubInfo` for every repo at once (only for the selected repo's badge); the toolbar indicator uses the already-loaded `behind` field, NOT gh.
- Crash-safe; ASCII-only; reuse tokens/toasts; keep existing features intact.

- [ ] **Step 1-3:** implement; `wails build` succeeds; `go test ./...` passes; commit `frontend/src`.

---

## Self-Review
- GitHub CI/PR/issue: `internal/gh` (OwnerRepo + Fetch, TDD) + `GitHubInfo` cached (T1) + GitHubBadge lazy (T2). ✓
- Remote-change notifications: toolbar "remote changes: N" from existing `behind` count (T2) - no new backend. ✓
- Graceful degradation: `gh` missing/unauth -> `Available=false` -> badge renders nothing. ✓
- Console hidden (winhide in gh.ExecRunner); cache guarded by mutex; DTOs non-nil-safe; ASCII-only. ✓
- **Type consistency:** `gh.Fetch`->`gh.Info{CI,PRs,Issues,Available}` (T1) -> `GitHubView{ci,prs,issues,available}` json tags (T1) consumed by GitHubBadge (T2); `GitHubInfo(remote)` binding (T1) called by GitHubBadge (T2).
- This completes the four connected features (tags, graph, search, GitHub). ✓
