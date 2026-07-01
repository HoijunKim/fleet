# fleet GUI Feature Expansion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.

**Goal:** Add a broad set of features to the Wails desktop app across four groups: richer git actions, a flashy dashboard, settings & automation, and integrations/quick actions.

**Architecture:** New git operations in `internal/git` (behind the `Runner` interface, TDD with a fake runner), exposed through new `app.go` bindings plus two OS-action bindings (open-in-browser, reveal-in-explorer). The Svelte front end grows new components consuming those bindings. `wails build` regenerates the JS bindings.

**Tech Stack:** unchanged (Go 1.22.0, Wails v2.12, Svelte-TS).

## Global Constraints

- Reuse the `Runner` seam: every new git op is a `func(r Runner, dir string, ...)`; no direct `os/exec` for git outside `internal/git`.
- All new git ops testable with a fake runner keyed on `args[0]` (+ subcommand where needed).
- `app.go` bindings return `""` on success and error text on failure (via the existing `errMsg`), or a DTO for reads.
- ASCII-only user-facing text (no `—`, `–`, `·`, `…`, `─`); polish via CSS.
- Preserve the existing binding/field contract; only ADD.
- `go.mod` stays `go 1.22.0`, no `toolchain` line. Conventional Commits; NO Claude/AI co-author trailer.
- Env: Go `C:\Program Files\Go\bin`, wails `C:\Users\hoijun\go\bin` (prefix PATH + `GOTOOLCHAIN=local`). Do NOT run `wails dev`. Verify with `wails build` + `go test ./...`.

---

### Task F1: Backend git ops + app.go bindings + OS actions

**Files:**
- Create: `internal/git/ops.go`, `internal/git/ops_test.go`
- Modify: `app.go` (new bindings + DTOs), `paths.go` (remoteToHTTPS helper + reveal), `app_test.go` (remoteToHTTPS test)
- Regenerate: `frontend/wailsjs/*` via `wails build`

**Interfaces produced (git):**
- `Branches(r Runner, dir string) (current string, all []string, err error)`
- `Checkout(r Runner, dir, branch string) error`
- `CommitAll(r Runner, dir, msg string) error` (git add -A then commit -m)
- `Push(r Runner, dir string) error`
- `DiffFile(r Runner, dir, file string) (string, error)`
- `Log(r Runner, dir string, n int) ([]repo.Commit, error)`
- `StashList(r Runner, dir string) ([]string, error)`, `Stash`, `StashPop`

**Interfaces produced (app.go):** `Branches(path) BranchInfo`, `Checkout(path,branch) string`, `CommitAll(path,msg) string`, `Push(path) string`, `DiffFile(path,file) string`, `Log(path,n) []CommitView`, `StashList(path) []string`, `Stash(path) string`, `StashPop(path) string`, `OpenInBrowser(remote) string`, `RevealInExplorer(path) string`. DTOs: `BranchInfo{Current string; All []string}`, `CommitView{Hash,Message,Author,When string}`.

- [ ] **Step 1: Write `internal/git/ops_test.go`**

```go
package git

import (
	"testing"

	"github.com/hoijun/fleet/internal/repo"
)

type opFake struct {
	out  map[string]string
	last [][]string
}

func (f *opFake) Run(dir string, args ...string) (string, error) {
	f.last = append(f.last, args)
	key := args[0]
	if len(args) > 1 {
		key = args[0] + " " + args[1]
	}
	return f.out[key], nil
}

func TestBranches(t *testing.T) {
	f := &opFake{out: map[string]string{
		"branch --show-current": "main\n",
		"for-each-ref":          "main\ndev\nfeature/x\n",
	}}
	cur, all, err := Branches(f, "/x")
	if err != nil || cur != "main" {
		t.Fatalf("cur=%q err=%v", cur, err)
	}
	if len(all) != 3 || all[1] != "dev" {
		t.Errorf("all=%v", all)
	}
}

func TestCommitAllStagesFirst(t *testing.T) {
	f := &opFake{out: map[string]string{}}
	if err := CommitAll(f, "/x", "msg"); err != nil {
		t.Fatal(err)
	}
	if len(f.last) != 2 || f.last[0][0] != "add" || f.last[1][0] != "commit" {
		t.Errorf("expected add then commit, got %v", f.last)
	}
}

func TestLogParsesMultiple(t *testing.T) {
	f := &opFake{out: map[string]string{
		"log -n": "h1\x1fa\x1f2026-07-01T10:00:00+09:00\x1fmsg1\nh2\x1fb\x1f2026-06-30T10:00:00+09:00\x1fmsg2",
	}}
	commits, err := Log(f, "/x", 2)
	if err != nil || len(commits) != 2 {
		t.Fatalf("commits=%v err=%v", commits, err)
	}
	if commits[0].Hash != "h1" || commits[1].Author != "b" {
		t.Errorf("bad commits: %+v", commits)
	}
	_ = repo.Commit{}
}

func TestStashList(t *testing.T) {
	f := &opFake{out: map[string]string{"stash list": "stash@{0}: WIP\nstash@{1}: more\n"}}
	l, err := StashList(f, "/x")
	if err != nil || len(l) != 2 {
		t.Fatalf("l=%v err=%v", l, err)
	}
}
```

- [ ] **Step 2: Run `go test ./internal/git/ -run "TestBranches|TestCommitAll|TestLog|TestStash" -v`** — expect FAIL (undefined).

- [ ] **Step 3: Write `internal/git/ops.go`**

```go
package git

import (
	"strconv"
	"strings"

	"github.com/hoijun/fleet/internal/repo"
)

// Branches returns the current branch and all local branch names.
func Branches(r Runner, dir string) (current string, all []string, err error) {
	cur, err := r.Run(dir, "branch", "--show-current")
	if err != nil {
		return "", nil, err
	}
	current = strings.TrimSpace(cur)
	out, err := r.Run(dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return current, nil, err
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			all = append(all, line)
		}
	}
	return current, all, nil
}

// Checkout switches to branch.
func Checkout(r Runner, dir, branch string) error { _, err := r.Run(dir, "checkout", branch); return err }

// CommitAll stages everything then commits with msg.
func CommitAll(r Runner, dir, msg string) error {
	if _, err := r.Run(dir, "add", "-A"); err != nil {
		return err
	}
	_, err := r.Run(dir, "commit", "-m", msg)
	return err
}

// Push runs git push.
func Push(r Runner, dir string) error { _, err := r.Run(dir, "push"); return err }

// DiffFile returns the working-tree diff for a single file.
func DiffFile(r Runner, dir, file string) (string, error) { return r.Run(dir, "diff", "--", file) }

// Log returns the last n commits.
func Log(r Runner, dir string, n int) ([]repo.Commit, error) {
	out, err := r.Run(dir, "log", "-n", strconv.Itoa(n), "--format=%H%x1f%an%x1f%cI%x1f%s")
	if err != nil {
		return nil, err
	}
	var commits []repo.Commit
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		commits = append(commits, parseLastCommit(line))
	}
	return commits, nil
}

// StashList returns the stash entries.
func StashList(r Runner, dir string) ([]string, error) {
	out, err := r.Run(dir, "stash", "list")
	if err != nil {
		return nil, err
	}
	var list []string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line != "" {
			list = append(list, line)
		}
	}
	return list, nil
}

// Stash saves the working tree; StashPop restores the latest.
func Stash(r Runner, dir string) error    { _, err := r.Run(dir, "stash", "push"); return err }
func StashPop(r Runner, dir string) error { _, err := r.Run(dir, "stash", "pop"); return err }
```

- [ ] **Step 4: Run the git tests** — expect PASS.

- [ ] **Step 5: Add `remoteToHTTPS` to `paths.go` + reveal helper**

```go
// remoteToHTTPS converts a git remote URL (git@host:owner/repo.git or
// https://host/owner/repo.git or ssh://...) into a browsable https URL. Returns
// "" if it cannot.
func remoteToHTTPS(remote string) string {
	remote = strings.TrimSpace(remote)
	remote = strings.TrimSuffix(remote, ".git")
	switch {
	case strings.HasPrefix(remote, "https://"):
		return remote
	case strings.HasPrefix(remote, "http://"):
		return remote
	case strings.HasPrefix(remote, "git@"):
		// git@github.com:owner/repo -> https://github.com/owner/repo
		rest := strings.TrimPrefix(remote, "git@")
		rest = strings.Replace(rest, ":", "/", 1)
		return "https://" + rest
	case strings.HasPrefix(remote, "ssh://git@"):
		rest := strings.TrimPrefix(remote, "ssh://git@")
		return "https://" + rest
	default:
		return ""
	}
}
```
Add `"strings"` to `paths.go` imports.

- [ ] **Step 6: Add the app.go bindings**

Add to `app.go` (import `"os/exec"`, `"runtime"` (stdlib for GOOS), and the Wails runtime `wruntime "github.com/wailsapp/wails/v2/pkg/runtime"`):
```go
// BranchInfo is the current + all-local branches for a repo.
type BranchInfo struct {
	Current string   `json:"current"`
	All     []string `json:"all"`
}

// CommitView is a JS-serializable commit.
type CommitView struct {
	Hash    string `json:"hash"`
	Message string `json:"message"`
	Author  string `json:"author"`
	When    string `json:"when"`
}

func (a *App) Branches(path string) BranchInfo {
	c, all, err := git.Branches(a.runner, path)
	if err != nil {
		return BranchInfo{}
	}
	return BranchInfo{Current: c, All: all}
}

func (a *App) Checkout(path, branch string) string { return errMsg(git.Checkout(a.runner, path, branch)) }
func (a *App) CommitAll(path, msg string) string   { return errMsg(git.CommitAll(a.runner, path, msg)) }
func (a *App) Push(path string) string             { return errMsg(git.Push(a.runner, path)) }

func (a *App) DiffFile(path, file string) string {
	out, err := git.DiffFile(a.runner, path, file)
	if err != nil {
		return out + "\n[error: " + err.Error() + "]"
	}
	return out
}

func (a *App) Log(path string, n int) []CommitView {
	commits, err := git.Log(a.runner, path, n)
	if err != nil {
		return []CommitView{}
	}
	out := make([]CommitView, 0, len(commits))
	for _, c := range commits {
		w := ""
		if !c.When.IsZero() {
			w = c.When.Format("2006-01-02")
		}
		out = append(out, CommitView{Hash: c.Hash, Message: c.Message, Author: c.Author, When: w})
	}
	return out
}

func (a *App) StashList(path string) []string {
	l, err := git.StashList(a.runner, path)
	if err != nil {
		return []string{}
	}
	return l
}
func (a *App) Stash(path string) string    { return errMsg(git.Stash(a.runner, path)) }
func (a *App) StashPop(path string) string { return errMsg(git.StashPop(a.runner, path)) }

// OpenInBrowser opens the repo's remote (converted to https) in the default browser.
func (a *App) OpenInBrowser(remote string) string {
	url := remoteToHTTPS(remote)
	if url == "" {
		return "no browsable url for remote"
	}
	wruntime.BrowserOpenURL(a.ctx, url)
	return ""
}

// RevealInExplorer opens the OS file manager at path.
func (a *App) RevealInExplorer(path string) string {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return errMsg(cmd.Start())
}
```
(These wruntime/os/exec bindings are side-effecting; no unit test.)

- [ ] **Step 7: Add a `remoteToHTTPS` test to `app_test.go`**

```go
func TestRemoteToHTTPS(t *testing.T) {
	cases := map[string]string{
		"git@github.com:o/r.git":       "https://github.com/o/r",
		"https://github.com/o/r.git":   "https://github.com/o/r",
		"ssh://git@github.com/o/r.git": "https://github.com/o/r",
		"":                             "",
		"weird":                        "",
	}
	for in, want := range cases {
		if got := remoteToHTTPS(in); got != want {
			t.Errorf("remoteToHTTPS(%q)=%q want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 8: Test, regenerate bindings, commit**

```bash
export PATH="/c/Program Files/Go/bin:/c/Users/hoijun/go/bin:$PATH"; export GOTOOLCHAIN=local
go test ./... && go vet ./... && wails build
git add internal/git/ops.go internal/git/ops_test.go app.go paths.go app_test.go frontend/wailsjs/go/main/App.js frontend/wailsjs/go/main/App.d.ts frontend/wailsjs/go/models.ts
git commit -m "feat: add branch/commit/push/diff/log/stash git ops and browser/reveal bindings"
```

---

### Task F2: Front end - dashboard, settings, integrations, automation

**Files:** rewrite/extend `frontend/src/App.svelte`; new components under `frontend/src/lib/`: `StatsHeader.svelte`, `CommandPalette.svelte`, `Toasts.svelte`, `SettingsModal.svelte`, `ContextMenu.svelte`. Extend `frontend/src/lib/Toolbar.svelte`, `RepoTable.svelte`, `app.css`.

**Design contract (implementer writes the Svelte, build-verified):**
- **Stats header / language bar:** a compact summary strip (total repos, dirty, behind, clean) plus a horizontal stacked bar of language distribution (count repos by `language`, colored segments, legend). Client-side from the repos array.
- **Toasts:** a small toast store; every action (fetch/pull/checkout/commit/push/stash/open) shows a success or error toast (top-right, auto-dismiss ~3s, colored by outcome). Wrap the binding calls so results surface as toasts instead of only inline text.
- **Command palette (Ctrl+K):** a centered overlay with a fuzzy filter over (a) repos (jump/select) and (b) global actions (Refresh, Fetch All, Open Settings). Arrow keys + Enter; Esc closes. Keyboard-first.
- **Settings modal:** opened from a Toolbar gear button; loads `GetConfig()`, lets the user edit roots (add/remove list), editor, terminal, scan_depth (number), show_non_git (toggle), auto_fetch_minutes (number); Save calls `SaveConfig(cfg)` then re-runs the scan/load. Show the save error if any.
- **Sort + filters:** clickable table headers cycle sort (name/branch/status/last/todo, asc/desc); Toolbar status filter chips (All / Dirty / Behind) narrow the visible list (compose with the name filter).
- **Keyboard shortcuts:** `/` focus filter, `j`/`k` or arrows move selection, `r` refresh selected, `f` fetch selected, `Ctrl+K` palette, `Esc` closes overlays. Do not hijack typing inside inputs.
- **Auto-fetch:** if `auto_fetch_minutes > 0`, a `setInterval` fetches all git repos on that cadence and refreshes; cleared/reset on settings change and on unmount.
- **Context menu:** right-click a repo row -> menu with Open in Browser (`OpenInBrowser(repo.remote)`), Reveal in Explorer (`RevealInExplorer(repo.path)`), Copy Path (`navigator.clipboard`), Copy Remote (`navigator.clipboard`). Disable browser/remote entries when `repo.remote` is empty.
- Keep the existing premium visual language (design tokens, dots, mono accents), ASCII-only text, and the live-load contract. Verify with `wails build`.

- [ ] **Step 1:** Build the components + wiring per the contract above.
- [ ] **Step 2:** `wails build` succeeds; `go test ./...` still passes.
- [ ] **Step 3:** Commit `frontend/src` (+ regenerated `wailsjs` if changed).

---

### Task F3: Front end - git actions UI (branch, commit, diff, history, stash)

**Files:** extend `frontend/src/lib/DetailPanel.svelte`; new components: `BranchMenu.svelte`, `CommitBox.svelte`, `DiffModal.svelte`, `HistoryList.svelte`, `StashPanel.svelte`. Extend `app.css`.

**Design contract:**
- **Branch switcher:** in the detail panel, a dropdown (from `Branches(path)`, marking `current`); selecting one calls `Checkout(path, branch)` then refreshes the repo; toast the result.
- **Commit box:** a message textarea + "Commit all & push" (calls `CommitAll(path,msg)` then optionally `Push(path)`) and a "Commit all" button; shows the dirty file count; refresh + toast after. Disabled when the repo is clean.
- **Diff viewer:** clicking a dirty file (in the detail panel's dirty list) opens `DiffModal` showing `DiffFile(path,file)` in a monospace, syntax-plain code view with +/- line coloring (green add / red remove), Esc to close.
- **History:** a collapsible "Recent commits" section calling `Log(path, 15)`, each row: short hash (mono, accent), message, author, date.
- **Stash:** a small section: `StashList(path)`, a "Stash" button (`Stash`) and "Pop" (`StashPop`), refresh + toast.
- Keep premium visuals, ASCII-only, live-load contract. Verify with `wails build`.

- [ ] **Step 1:** Build the components + DetailPanel wiring per the contract.
- [ ] **Step 2:** `wails build` succeeds; `go test ./...` still passes.
- [ ] **Step 3:** Commit `frontend/src`.

---

## Self-Review

- Group A (git actions) -> F1 backend ops + F3 UI. Group B (dashboard) -> F2. Group C (settings/automation) -> F2. Group D (integrations) -> F1 OS actions + F2 context menu. All four groups covered.
- Every new git op goes through `Runner` and is unit-tested with a fake (F1). `remoteToHTTPS` is pure + tested. Side-effecting OS/browser bindings are thin and untested by design.
- Binding names added in F1 (`Branches`, `Checkout`, `CommitAll`, `Push`, `DiffFile`, `Log`, `StashList`, `Stash`, `StashPop`, `OpenInBrowser`, `RevealInExplorer`) are exactly what F2/F3 consume.
- ASCII-only, live-load contract, and premium visuals preserved; `wails build` gates each front-end task.
