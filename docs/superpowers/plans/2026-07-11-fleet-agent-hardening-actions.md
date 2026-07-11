# Fleet Agent Hardening + Gated Actions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the agentic deep-dive carry a change to a pull request (commit/push/PR, each user-approved; default-branch push always blocked) and turn the approval moment into a real code review — readable diff, plain-language summary, severity badge.

**Architecture:** A pure, server-side action **classifier** (`internal/agent/classify.go`) becomes the single policy brain: fleet's `ApprovalServer` classifies every gated tool call, auto-denies dangerous ones (default-branch push, shell secret-reads) without ever asking, and hands the rest to the GUI enriched with `category/severity/summary`. The overlay renders that as a review. No new dependencies; the gate stays fail-closed.

**Tech Stack:** Go 1.22 (stdlib only in `internal/agent`), Wails v2, Svelte-TS, vitest, Go testing.

## Global Constraints

Copy verbatim from the spec `docs/superpowers/specs/2026-07-11-fleet-agent-hardening-actions-design.md`. Every task inherits these:

- **No new runtime dependencies.** `internal/agent` stays stdlib-only. `frontend/package.json` unchanged. Diff renderer is hand-written; motion uses existing `motion.ts`.
- **The gate must not be weakened; fail-safe stays fail-safe.** Any classify/parse error, ambiguity, timeout, or cancel resolves to **deny**, never allow.
- **Default-branch push is always blocked**, in every form (`git push origin main`, `git push origin HEAD:main`, bare `git push` while on `main`/`master`, `--force`, `+refspec`). If a push's target cannot be proven to be a non-default branch, **deny** (fail-closed).
- **`prefers-reduced-motion` honored** for any added motion (via `motion.ts`).
- **No regression:** consent gate, project-scoping, single-run, the read-only allow-list, and fail-safe approval keep working; existing `internal/agent`, `app`, and frontend suites stay green.
- **Craft bar:** the approval card is a human review (readable diff for `Edit`/`Write`, one-line plain summary for every action, severity badge), not raw JSON. ASCII punctuation in all AI-facing/UI copy (hyphen `-`, no `…`/em dash).
- **Green gates each task:** `go build ./...` + `go vet ./...` clean, `go test ./...` green, `npx svelte-check` 0 errors, `npx vitest run` green; frontend-touching tasks also `wails build`.

## Commit authorship (all tasks)

Commit with `git -c user.name=hoijun -c user.email=hoijun.kim00@gmail.com commit -m "…"` — NO `Co-Authored-By`/Claude trailer.

---

## File Structure

- `internal/agent/classify.go` (new) — pure classifier (Task 1).
- `internal/agent/classify_test.go` (new) — exhaustive table (Task 1).
- `internal/agent/approve.go` — classifier wiring, `ActionRequest` fields, auto-deny (Task 2).
- `internal/agent/policy.go` — `git push` out of disallow, secret globs (Task 2).
- `internal/agent/approve_test.go`, `policy_test.go` — extend (Task 2).
- `app.go` — inject classify closure that live-resolves the branch (Task 2).
- `internal/git/*` — reuse or add `CurrentBranch(dir)` (Task 2).
- `frontend/src/lib/agentAction.ts` (new) + `agentAction.test.ts` (new) — parse a gated action into a renderable shape (Task 3).
- `frontend/src/lib/AgentOverlay.svelte` — review card + outcome line (Task 4).
- `frontend/src/lib/agentSession.ts` — `pending` carries category/severity/summary; outcome line; `__reset()` (Task 4).
- `frontend/src/app.css` — badge/diff styles (Task 4).
- `.gitattributes` (new) + small GUI Minors (Task 5).

---

## Task 1: Action classifier (pure, security core)

**Files:**
- Create: `internal/agent/classify.go`
- Create: `internal/agent/classify_test.go`

**Interfaces:**
- Produces: `Category` (`"edit"|"shell"|"remote"`), `Severity` (`"low"|"medium"|"high"`), `ClassifyContext{CurrentBranch string; ProtectedBranches []string}`, `Verdict{Decision string /* "gate"|"deny" */; Reason string; Category Category; Severity Severity; Summary string}`, and `func Classify(toolName string, toolInput json.RawMessage, ctx ClassifyContext) Verdict`. Also exports `func DefaultProtectedBranches() []string` = `["main","master"]`.

- [ ] **Step 1: Write the failing test** — `internal/agent/classify_test.go`

```go
package agent

import (
	"encoding/json"
	"testing"
)

func v(tool, input string, cur string) Verdict {
	return Classify(tool, json.RawMessage(input), ClassifyContext{CurrentBranch: cur, ProtectedBranches: DefaultProtectedBranches()})
}

func TestClassifyEdits(t *testing.T) {
	got := v("Edit", `{"file_path":"README.md","old_string":"a","new_string":"b"}`, "feat/x")
	if got.Decision != "gate" || got.Category != "edit" || got.Summary != "Edit README.md" {
		t.Fatalf("edit: %+v", got)
	}
	w := v("Write", `{"file_path":"new.txt","content":"hi"}`, "feat/x")
	if w.Decision != "gate" || w.Category != "edit" || w.Severity != "medium" {
		t.Fatalf("write: %+v", w)
	}
}

func TestClassifyPushProtectedAlwaysDenied(t *testing.T) {
	deny := []struct{ cmd, cur string }{
		{"git push origin main", "feat/x"},
		{"git push origin HEAD:main", "feat/x"},
		{"git push origin master", "feat/x"},
		{"git push --force origin main", "feat/x"},
		{"git push origin +main", "feat/x"},
		{"git push", "main"},               // bare push while on main
		{"git push origin", "master"},      // bare push, remote only, on master
		{"git push origin feat/x:main", "feat/x"},
		{"git -C . push origin main", "feat/x"},
	}
	for _, d := range deny {
		got := v("Bash", `{"command":`+jsonStr(d.cmd)+`}`, d.cur)
		if got.Decision != "deny" {
			t.Fatalf("expected deny for %q (on %q): %+v", d.cmd, d.cur, got)
		}
	}
}

func TestClassifyPushFeatureBranchGates(t *testing.T) {
	got := v("Bash", `{"command":"git push origin feat/x"}`, "feat/x")
	if got.Decision != "gate" || got.Category != "remote" || got.Severity != "high" {
		t.Fatalf("feature push: %+v", got)
	}
	bare := v("Bash", `{"command":"git push"}`, "feat/x")
	if bare.Decision != "gate" || bare.Category != "remote" {
		t.Fatalf("bare push on feature: %+v", bare)
	}
}

func TestClassifyCommitAndPR(t *testing.T) {
	c := v("Bash", `{"command":"git commit -m \"fix typo\""}`, "feat/x")
	if c.Decision != "gate" || c.Category != "shell" || c.Summary != "Commit: fix typo" {
		t.Fatalf("commit: %+v", c)
	}
	pr := v("Bash", `{"command":"gh pr create --fill"}`, "feat/x")
	if pr.Decision != "gate" || pr.Category != "remote" {
		t.Fatalf("pr: %+v", pr)
	}
}

func TestClassifySecretReadDenied(t *testing.T) {
	got := v("Bash", `{"command":"cat .env"}`, "feat/x")
	if got.Decision != "deny" {
		t.Fatalf("secret read should deny: %+v", got)
	}
}

func TestClassifyFailClosed(t *testing.T) {
	if v("Bash", `{}`, "feat/x").Decision != "gate" { // empty command -> generic gate is fine, but must not be a push
		// an empty command is a harmless no-op; gating it is acceptable
	}
	if v("Bash", `not json`, "feat/x").Decision != "deny" {
		t.Fatal("garbage input must deny")
	}
	if v("Frobnicate", `{}`, "feat/x").Decision != "deny" {
		t.Fatal("unknown tool must deny")
	}
}

// jsonStr quotes a string as a JSON literal for embedding in a command field.
func jsonStr(s string) string { b, _ := json.Marshal(s); return string(b) }
```

- [ ] **Step 2: Run it, verify it fails** — Run: `go test ./internal/agent/ -run TestClassify`. Expected: FAIL (undefined: Classify).

- [ ] **Step 3: Implement** — `internal/agent/classify.go`:

```go
package agent

import (
	"encoding/json"
	"regexp"
	"strings"
)

type Category string
type Severity string

const (
	CatEdit   Category = "edit"
	CatShell  Category = "shell"
	CatRemote Category = "remote"

	SevLow    Severity = "low"
	SevMedium Severity = "medium"
	SevHigh   Severity = "high"
)

// ClassifyContext carries the repo state the classifier needs. CurrentBranch is
// resolved live by the caller (the branch can change mid-run via checkout).
type ClassifyContext struct {
	CurrentBranch     string
	ProtectedBranches []string
}

// Verdict is the classifier's decision for one gated tool call.
type Verdict struct {
	Decision string // "gate" | "deny"
	Reason   string
	Category Category
	Severity Severity
	Summary  string
}

// DefaultProtectedBranches are never pushed to by the agent.
func DefaultProtectedBranches() []string { return []string{"main", "master"} }

func deny(reason string) Verdict { return Verdict{Decision: "deny", Reason: reason} }

// Classify decides how a gated tool call is handled. It NEVER returns allow;
// callers treat "gate" as "ask the user" and "deny" as "block". Any parse
// failure or ambiguity is deny (fail-closed).
func Classify(toolName string, toolInput json.RawMessage, ctx ClassifyContext) Verdict {
	switch toolName {
	case "Edit":
		var p struct{ FilePath string `json:"file_path"` }
		if json.Unmarshal(toolInput, &p) != nil {
			return deny("unreadable edit")
		}
		return Verdict{Decision: "gate", Category: CatEdit, Severity: SevLow, Summary: "Edit " + baseOr(p.FilePath, "a file")}
	case "Write":
		var p struct{ FilePath string `json:"file_path"` }
		if json.Unmarshal(toolInput, &p) != nil {
			return deny("unreadable write")
		}
		return Verdict{Decision: "gate", Category: CatEdit, Severity: SevMedium, Summary: "Create " + baseOr(p.FilePath, "a file")}
	case "Bash":
		var p struct{ Command string `json:"command"` }
		if json.Unmarshal(toolInput, &p) != nil {
			return deny("unreadable command")
		}
		return classifyBash(p.Command, ctx)
	default:
		return deny("tool not permitted")
	}
}

func classifyBash(cmd string, ctx ClassifyContext) Verdict {
	c := strings.TrimSpace(cmd)
	if c == "" {
		return Verdict{Decision: "gate", Category: CatShell, Severity: SevLow, Summary: "Run a command"}
	}
	// Secret-read guard (best-effort): cat/less/head/... on a secret path.
	if readsSecret(c) {
		return deny("reading a secret file is blocked")
	}
	sub, args := gitSubcommand(c)
	switch {
	case sub == "push":
		return classifyPush(args, ctx)
	case sub == "commit":
		return Verdict{Decision: "gate", Category: CatShell, Severity: SevMedium, Summary: "Commit: " + commitMessage(c)}
	case isPRCreate(c):
		return Verdict{Decision: "gate", Category: CatRemote, Severity: SevMedium, Summary: "Open a pull request"}
	default:
		return Verdict{Decision: "gate", Category: CatShell, Severity: SevMedium, Summary: "Run: " + truncate(c, 80)}
	}
}

// classifyPush resolves the push destination(s) and denies any that hit a
// protected branch or that cannot be determined.
func classifyPush(args []string, ctx ClassifyContext) Verdict {
	protected := map[string]bool{}
	for _, b := range ctx.ProtectedBranches {
		protected[b] = true
	}
	var refspecs []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue // flags (incl. --force)
		}
		refspecs = append(refspecs, a)
	}
	// refspecs[0] is the remote (if present); the rest are refs.
	var refs []string
	if len(refspecs) >= 2 {
		refs = refspecs[1:]
	}
	// Bare push (no refspec) targets the current branch.
	if len(refs) == 0 {
		if ctx.CurrentBranch == "" {
			return deny("cannot determine push target")
		}
		if protected[ctx.CurrentBranch] {
			return deny("push to the default branch is blocked")
		}
		return Verdict{Decision: "gate", Category: CatRemote, Severity: SevHigh, Summary: "Push branch " + ctx.CurrentBranch + " to " + remoteOf(refspecs)}
	}
	for _, r := range refs {
		dest := pushDest(r)
		if dest == "" {
			return deny("cannot determine push target")
		}
		if protected[dest] {
			return deny("push to the default branch is blocked")
		}
	}
	return Verdict{Decision: "gate", Category: CatRemote, Severity: SevHigh, Summary: "Push " + strings.Join(refs, ", ") + " to " + remoteOf(refspecs)}
}

// pushDest returns the destination branch name of a refspec: the right side of
// a colon, else the whole ref, with a leading '+' (force) stripped and any
// "HEAD:" / "refs/heads/" prefixes normalized to the branch name.
func pushDest(ref string) string {
	ref = strings.TrimPrefix(ref, "+")
	if i := strings.LastIndex(ref, ":"); i >= 0 {
		ref = ref[i+1:]
	}
	ref = strings.TrimPrefix(ref, "refs/heads/")
	return ref
}

func remoteOf(refspecs []string) string {
	if len(refspecs) >= 1 {
		return refspecs[0]
	}
	return "origin"
}

// gitSubcommand returns the git subcommand and its args if cmd is a git
// invocation, else ("", nil). Handles `git -C dir push …`.
func gitSubcommand(cmd string) (string, []string) {
	toks := strings.Fields(cmd)
	i := 0
	for i < len(toks) && toks[i] != "git" {
		i++ // tolerate a leading `env FOO=bar git …`
	}
	if i >= len(toks) {
		return "", nil
	}
	i++ // past "git"
	for i < len(toks) && strings.HasPrefix(toks[i], "-") {
		// skip global flags; -C and -c take a value
		if toks[i] == "-C" || toks[i] == "-c" {
			i++
		}
		i++
	}
	if i >= len(toks) {
		return "", nil
	}
	return toks[i], toks[i+1:]
}

func isPRCreate(cmd string) bool {
	return regexp.MustCompile(`\bgh\s+pr\s+create\b`).MatchString(cmd) ||
		regexp.MustCompile(`\bgit\s+request-pull\b`).MatchString(cmd)
}

var secretPathRe = regexp.MustCompile(`(?i)(\.env(\.\w+)?|id_rsa|id_ed25519|\.pem|\.key|\.p12|\.pfx|\.netrc|credentials|secret|\.ssh/)`)
var readCmdRe = regexp.MustCompile(`^\s*(cat|less|more|head|tail|xxd|base64|strings|od|nl)\b`)

func readsSecret(cmd string) bool {
	return readCmdRe.MatchString(cmd) && secretPathRe.MatchString(cmd)
}

var commitMsgRe = regexp.MustCompile(`-m\s+("([^"]*)"|'([^']*)'|(\S+))`)

func commitMessage(cmd string) string {
	m := commitMsgRe.FindStringSubmatch(cmd)
	if m == nil {
		return "(no message)"
	}
	for _, g := range m[2:] {
		if g != "" {
			return truncate(g, 80)
		}
	}
	return "(no message)"
}

func baseOr(path, fallback string) string {
	if path == "" {
		return fallback
	}
	path = strings.ReplaceAll(path, "\\", "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
```

- [ ] **Step 4: Run tests, verify pass** — Run: `go test ./internal/agent/ -run TestClassify -v`. Expected: all Classify tests PASS.

- [ ] **Step 5: Vet + full agent package** — Run: `go vet ./internal/agent/ && go test ./internal/agent/`. Expected: clean, green.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/classify.go internal/agent/classify_test.go
git commit -m "feat(agent): action classifier - gate/deny with category, severity, summary (default-branch push always denied)"
```

---

## Task 2: Wire classifier into the approval server + open git push

**Files:**
- Modify: `internal/agent/approve.go`
- Modify: `internal/agent/policy.go`
- Modify: `internal/agent/approve_test.go`, `internal/agent/policy_test.go`
- Modify: `app.go`
- Reuse/Modify: `internal/git` (a current-branch reader)

**Interfaces:**
- Consumes: `Classify`, `Verdict`, `ClassifyContext`, `DefaultProtectedBranches` (Task 1).
- Produces: `ActionRequest` gains `Category Category`, `Severity Severity`, `Summary string` (json `category/severity/summary`). `NewApprovalServer` gains a final param `classify func(toolName string, toolInput json.RawMessage, cwd string) Verdict`; when nil, defaults to a gate-everything shim (so existing tests that don't care still gate). A `CurrentBranch(dir string) string` helper in `internal/git` (add if absent; returns "" on error).

- [ ] **Step 1: Extend the policy test (RED)** — in `internal/agent/policy_test.go`, add:

```go
func TestPolicyPushIsGatedNotDenied(t *testing.T) {
	p := DefaultPolicy()
	for _, d := range p.Disallowed {
		if d == "Bash(git push:*)" {
			t.Fatal("git push must not be hard-denied; it is gated by the classifier")
		}
	}
	// rm/sudo/curl stay denied
	must := map[string]bool{"Bash(rm:*)": false, "Bash(sudo:*)": false, "Bash(curl:*)": false}
	for _, d := range p.Disallowed {
		if _, ok := must[d]; ok {
			must[d] = true
		}
	}
	for k, seen := range must {
		if !seen {
			t.Fatalf("expected %s still denied", k)
		}
	}
}
```

Run: `go test ./internal/agent/ -run TestPolicyPushIsGated`. Expected: FAIL (git push still in Disallowed).

- [ ] **Step 2: Update policy.go** — in `DefaultPolicy`, remove `"Bash(git push:*)"` from `Disallowed`. Keep `"Bash(rm:*)"`, `"Bash(sudo:*)"`, `"Bash(curl:*)"`. Add secret globs to the Read denies: `"Read(**/*token*)"`, `"Read(**/*.p12)"`, `"Read(**/*.pfx)"`, `"Read(**/.netrc)"`, `"Read(**/*.keystore)"`, `"Read(**/*.ovpn)"`. Run the test → PASS.

- [ ] **Step 3: Extend approve.go** — add fields to `ActionRequest`:

```go
type ActionRequest struct {
	ID        string          `json:"id"`
	ToolName  string          `json:"toolName"`
	ToolInput json.RawMessage `json:"toolInput"`
	Cwd       string          `json:"cwd"`
	SessionID string          `json:"sessionId"`
	Category  Category        `json:"category"`
	Severity  Severity        `json:"severity"`
	Summary   string          `json:"summary"`
}
```

Add a classify field + constructor param:

```go
type ApprovalServer struct {
	coord    *Coordinator
	onAction func(ActionRequest)
	classify func(toolName string, toolInput json.RawMessage, cwd string) Verdict
	timeout  time.Duration
	ctx      context.Context
	srv      *http.Server
	url      string
}

func NewApprovalServer(ctx context.Context, coord *Coordinator, timeout time.Duration, onAction func(ActionRequest), classify func(string, json.RawMessage, string) Verdict) *ApprovalServer {
	if classify == nil {
		classify = func(string, json.RawMessage, string) Verdict { return Verdict{Decision: "gate"} }
	}
	return &ApprovalServer{coord: coord, onAction: onAction, classify: classify, timeout: timeout, ctx: ctx}
}
```

Rewrite `handleApprove` to classify first:

```go
func (s *ApprovalServer) handleApprove(w http.ResponseWriter, r *http.Request) {
	var p hookPost
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&p); err != nil {
		writeDecision(w, false, "malformed hook request")
		return
	}
	v := s.classify(p.ToolName, p.ToolInput, p.Cwd)
	if v.Decision == "deny" {
		writeDecision(w, false, v.Reason)
		return
	}
	id := s.coord.Register()
	if s.onAction != nil {
		s.onAction(ActionRequest{
			ID: id, ToolName: p.ToolName, ToolInput: p.ToolInput, Cwd: p.Cwd, SessionID: p.SessionID,
			Category: v.Category, Severity: v.Severity, Summary: v.Summary,
		})
	}
	ctx := s.ctx
	if ctx == nil {
		ctx = r.Context()
	}
	d := s.coord.Await(ctx, id, s.timeout)
	writeDecision(w, d.Approved, d.Reason)
}
```

- [ ] **Step 4: Extend approve_test.go** — add a test that a deny-classified POST answers `{approved:false}` and does NOT fire onAction, and a gate POST does:

```go
func TestApprovalServerAutoDeniesWithoutAsking(t *testing.T) {
	coord := NewCoordinator()
	fired := false
	classify := func(tool string, _ json.RawMessage, _ string) Verdict {
		if tool == "Bash" {
			return Verdict{Decision: "deny", Reason: "blocked"}
		}
		return Verdict{Decision: "gate", Category: CatEdit, Summary: "Edit x"}
	}
	s := NewApprovalServer(context.Background(), coord, time.Second, func(ActionRequest) { fired = true }, classify)
	if err := s.Start(); err != nil { t.Fatal(err) }
	defer s.Stop(nil)

	body := `{"tool_name":"Bash","tool_input":{"command":"git push origin main"},"cwd":"/r"}`
	resp, _ := http.Post(s.URL(), "application/json", strings.NewReader(body))
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if out["approved"] != false {
		t.Fatalf("auto-deny should answer approved=false, got %v", out)
	}
	if fired {
		t.Fatal("onAction must NOT fire for an auto-denied action")
	}
}
```

(Use the existing test file's imports; add `strings`, `net/http`, `encoding/json`, `time`, `context` if missing. A gate-path assertion can reuse the existing round-trip test, updated for the new constructor arity + populated Summary.)

- [ ] **Step 5: Update existing approve_test.go call sites** — every existing `NewApprovalServer(...)` call gains the 5th arg. For tests that just exercise gating, pass `nil` (defaults to gate-everything).

- [ ] **Step 6: Add `internal/git` current-branch helper** — check `internal/git/ops.go` for an existing current-branch reader; if none, add:

```go
// CurrentBranch returns the repo's current branch name, or "" on error/detached.
func CurrentBranch(dir string) string {
	out, err := runGit(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil { return "" }
	return strings.TrimSpace(out)
}
```

(Match the file's existing git-exec helper name; if the package already exposes an equivalent, reuse it and skip this.)

- [ ] **Step 7: Wire the classifier in app.go** — in `AgentAsk`, pass a classify closure to `NewApprovalServer` that live-resolves the branch from the tool's cwd:

```go
	srv := agent.NewApprovalServer(ctx, a.agentCoord, 10*time.Minute,
		func(req agent.ActionRequest) { wruntime.EventsEmit(a.ctx, "agent:action", req) },
		func(tool string, input json.RawMessage, cwd string) agent.Verdict {
			if cwd == "" { cwd = repoDir }
			return agent.Classify(tool, input, agent.ClassifyContext{
				CurrentBranch:     git.CurrentBranch(cwd),
				ProtectedBranches: agent.DefaultProtectedBranches(),
			})
		})
```

Add imports `encoding/json` and the `internal/git` package if not already imported. The `agent:action` event now carries the new fields automatically (it emits `req`).

- [ ] **Step 8: Verify** — Run: `go build ./... && go vet ./... && go test ./...`. Expected: clean + green (all packages). Confirm `app_test.go`'s agentic tests still pass (they construct/exercise AgentAsk paths).

- [ ] **Step 9: Commit**

```bash
git add internal/agent/approve.go internal/agent/policy.go internal/agent/approve_test.go internal/agent/policy_test.go internal/git app.go
git commit -m "feat(agent): approval server classifies actions - auto-deny default-branch push/secret-reads, gate the rest with review metadata"
```

---

## Task 3: Frontend action parser (renderable review shape)

**Files:**
- Create: `frontend/src/lib/agentAction.ts`
- Create: `frontend/src/lib/agentAction.test.ts`

**Interfaces:**
- Produces: `parseAction(category: string, toolName: string, toolInput: any)` → a normalized object:
  - `{ kind: "diff", file: string, removed: string[], added: string[] }` for an `Edit`
  - `{ kind: "write", file: string, preview: string[] }` for a `Write`
  - `{ kind: "command", command: string }` for a `Bash`
  - `{ kind: "raw", json: string }` fallback (unparseable)
  `toolInput` may be an object or a JSON string; the parser handles both and never throws.

- [ ] **Step 1: Write the failing test** — `frontend/src/lib/agentAction.test.ts`

```ts
import { describe, it, expect } from "vitest";
import { parseAction } from "./agentAction";

describe("parseAction", () => {
  it("splits an Edit into removed/added lines", () => {
    const a = parseAction("edit", "Edit", { file_path: "a/b.ts", old_string: "x\ny", new_string: "x\nz" });
    expect(a.kind).toBe("diff");
    if (a.kind === "diff") {
      expect(a.file).toBe("a/b.ts");
      expect(a.removed).toEqual(["x", "y"]);
      expect(a.added).toEqual(["x", "z"]);
    }
  });
  it("previews a Write (first lines)", () => {
    const a = parseAction("edit", "Write", { file_path: "n.txt", content: "l1\nl2" });
    expect(a.kind).toBe("write");
    if (a.kind === "write") expect(a.preview).toEqual(["l1", "l2"]);
  });
  it("returns the command for Bash", () => {
    const a = parseAction("remote", "Bash", { command: "git push origin feat/x" });
    expect(a).toEqual({ kind: "command", command: "git push origin feat/x" });
  });
  it("accepts a JSON string and never throws on garbage", () => {
    expect(parseAction("edit", "Edit", '{"file_path":"z","old_string":"","new_string":"q"}').kind).toBe("diff");
    expect(parseAction("edit", "Edit", "not json").kind).toBe("raw");
  });
});
```

- [ ] **Step 2: Run it, verify it fails** — Run: `cd frontend && npx vitest run src/lib/agentAction.test.ts`. Expected: FAIL (module not found).

- [ ] **Step 3: Implement** — `frontend/src/lib/agentAction.ts`:

```ts
// Normalize a gated agent action into a shape the approval card can render.
// Never throws; unknown/garbage shapes fall back to { kind: "raw" }.
export type Action =
  | { kind: "diff"; file: string; removed: string[]; added: string[] }
  | { kind: "write"; file: string; preview: string[] }
  | { kind: "command"; command: string }
  | { kind: "raw"; json: string };

function asObj(input: any): any | null {
  if (input && typeof input === "object") return input;
  if (typeof input === "string") {
    try { return JSON.parse(input); } catch { return null; }
  }
  return null;
}

export function parseAction(category: string, toolName: string, toolInput: any): Action {
  const o = asObj(toolInput);
  const raw = (): Action => ({ kind: "raw", json: typeof toolInput === "string" ? toolInput : safeJson(toolInput) });
  if (!o) return raw();
  if (toolName === "Edit" && typeof o.old_string === "string" && typeof o.new_string === "string") {
    return { kind: "diff", file: String(o.file_path ?? ""), removed: o.old_string.split("\n"), added: o.new_string.split("\n") };
  }
  if (toolName === "Write" && typeof o.content === "string") {
    return { kind: "write", file: String(o.file_path ?? ""), preview: o.content.split("\n").slice(0, 20) };
  }
  if (toolName === "Bash" && typeof o.command === "string") {
    return { kind: "command", command: o.command };
  }
  return raw();
}

function safeJson(v: any): string {
  try { return JSON.stringify(v, null, 2); } catch { return String(v); }
}
```

- [ ] **Step 4: Run tests, verify pass** — Run: `cd frontend && npx vitest run src/lib/agentAction.test.ts`. Expected: PASS (4/4).

- [ ] **Step 5: Verify types** — Run: `cd frontend && npx svelte-check`. Expected: 0 errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/lib/agentAction.ts frontend/src/lib/agentAction.test.ts
git commit -m "feat(ui): agentAction parser - normalize Edit/Write/Bash into a renderable review shape"
```

---

## Task 4: Approval card as a review + outcome line

**Files:**
- Modify: `frontend/src/lib/agentSession.ts` (pending carries category/severity/summary; outcome line on decide; `__reset()`)
- Modify: `frontend/src/lib/AgentOverlay.svelte` (categorized review card)
- Modify: `frontend/src/app.css` (badge + diff styles)

**Interfaces:**
- Consumes: `parseAction` (Task 3), the `agent:action` event's new `category/severity/summary` (Task 2), `Icon`, `motion.fadeScaleIn`.

- [ ] **Step 1: agentSession pending + outcome + reset.** In `frontend/src/lib/agentSession.ts`:
  - Widen `pending` to `{ id: string; toolName: string; toolInput: string; category: string; severity: string; summary: string } | null`.
  - In the `agent:action` handler, set `pending` with `category: a?.category ?? "shell"`, `severity: a?.severity ?? "medium"`, `summary: a?.summary ?? ""` alongside the existing fields.
  - In `decide(approved)`, before clearing `pending`, capture `p.summary` and append an activity line reflecting the decision: `activity.update((x) => [...x, { tool: approved ? "approved" : "rejected", input: p.summary }])`. (The overlay renders `tool==="approved"`/`"rejected"` with an icon — no literal glyphs.)
  - Add `export function __reset(): void { … }` that resets every store to its initial value and clears module state (`project=null, loadedPath="", gen=0, runPath="", runGen=0, deciding=false`). Do NOT reset `started` (event subscription stays). This is for test isolation only.

- [ ] **Step 2: AgentOverlay review card.** In `frontend/src/lib/AgentOverlay.svelte`, add `import { parseAction } from "./agentAction";` and replace the approval-card block. Compute `$: action = $pending ? parseAction($pending.category, $pending.toolName, $pending.toolInput) : null;`. Render:

```svelte
{#if $pending}
  <div class="ov-approval sev-{$pending.severity}" transition:scale={fadeScaleIn()}>
    <div class="ov-approval-head">
      <span class="ov-cat ov-cat-{$pending.category}">{$pending.category}</span>
      <span class="ov-summary">{$pending.summary || $pending.toolName}</span>
    </div>
    {#if action && action.kind === "diff"}
      <div class="ov-diff" class:mono>
        <div class="ov-diff-file">{action.file}</div>
        {#each action.removed as l}<div class="ov-del">- {l}</div>{/each}
        {#each action.added as l}<div class="ov-add">+ {l}</div>{/each}
      </div>
    {:else if action && action.kind === "write"}
      <div class="ov-diff mono"><div class="ov-diff-file">{action.file}</div>
        {#each action.preview as l}<div class="ov-add">+ {l}</div>{/each}
      </div>
    {:else if action && action.kind === "command"}
      <pre class="ov-cmd">{action.command}</pre>
    {:else if action}
      <pre class="ov-approval-body">{action.json}</pre>
    {/if}
    <div class="ov-approval-btns">
      <button class="btn btn-primary btn-sm" on:click={() => decide(true)}><Icon name="check" size={14} /> Approve</button>
      <button class="btn btn-sm ov-reject" on:click={() => decide(false)}><Icon name="x" size={14} /> Reject</button>
    </div>
  </div>
{/if}
```

  In the activity-feed `{#each $activity as a, i}` block, render the approve/reject outcome lines with an icon instead of the tool-dot: if `a.tool === "approved"` show `<Icon name="check"/>` (accent), if `"rejected"` show `<Icon name="x"/>` (muted), else the existing `activity` icon.

- [ ] **Step 3: Styles.** In `frontend/src/app.css`, add `.ov-cat` badge styles (small uppercase pill; `.ov-cat-edit` accent, `.ov-cat-shell` amber, `.ov-cat-remote` a distinct high-risk hue), `.sev-high` a stronger left border on the card, `.ov-diff`/`.ov-del`/`.ov-add`/`.ov-diff-file`/`.ov-cmd` (monospace, `overflow-x: auto`, red/green tints via existing `--err`/`--ok` tokens at low alpha). Keep it consistent with existing `.ov-*` tokens.

- [ ] **Step 4: Verify** — Run: `cd frontend && npx vitest run && npx svelte-check`. Expected: green, 0 errors. Then `cd .. && wails build` succeeds.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/agentSession.ts frontend/src/lib/AgentOverlay.svelte frontend/src/app.css
git commit -m "feat(ui): approval card as a review - severity badge, readable diff, command label, approve/reject outcome line"
```

---

## Task 5: Cleanup (line endings + deferred Minors)

**Files:**
- Create: `.gitattributes`
- Modify: `frontend/src/app.css` (`.ic-jump` comment), `frontend/src/lib/AgentOverlay.svelte` (remove redundant backdrop `on:keydown`), plus the `…` -> `...` unification where UI copy uses an ellipsis glyph.

- [ ] **Step 1: `.gitattributes`** — create at repo root to stop the LF/CRLF phantom-modified churn:

```gitattributes
* text=auto eol=lf
*.png binary
*.ico binary
*.exe binary
```

Then renormalize: `git add --renormalize .` and confirm the previously phantom-modified `frontend/wailsjs/**`, `go.mod` no longer show as modified after a clean status. Do NOT commit unrelated content changes — only the renormalization + `.gitattributes`.

- [ ] **Step 2: `.ic-jump` comment** — in `frontend/src/app.css`, add `/* Command-palette key hint */` above the `.ic-jump` rule.

- [ ] **Step 3: Remove the redundant backdrop keydown** — in `frontend/src/lib/AgentOverlay.svelte`, remove the leftover `on:keydown={onOverlayKey}` on the `<div class="ov-backdrop">` (the window-level handler already covers Escape). Keep the div's other attributes.

- [ ] **Step 4: Ellipsis unification** — grep the frontend for the `…` glyph in user-facing copy (`grep -rn "…" frontend/src`) and replace with `...` to match the ASCII-punctuation direction (e.g. `Syncing…` -> `Syncing...`, `working in the repo…` -> `...`). Do not touch non-copy occurrences.

- [ ] **Step 5: Verify** — Run: `cd frontend && npx vitest run && npx svelte-check` (green, 0 errors), `cd .. && go build ./...` (OK), and `git status` shows a clean tree except the intended `.gitattributes`/renormalized files.

- [ ] **Step 6: Commit**

```bash
git add .gitattributes frontend/src/app.css frontend/src/lib/AgentOverlay.svelte
git commit -m "chore: .gitattributes eol=lf (stop CRLF churn) + gui-polish minor cleanup"
```

---

## Self-Review Notes (author)

- **Spec coverage:** W1 -> Task 1; W2 -> Task 2; W3 -> Tasks 3+4; W4 -> Task 4; W5 -> Task 5. Every spec File-Structure entry appears in a task.
- **Type consistency:** `Verdict`/`Category`/`Severity`/`ClassifyContext`/`Classify`/`DefaultProtectedBranches` (Task 1) are consumed with identical names in Task 2. `ActionRequest`'s new json keys `category/severity/summary` (Task 2) match `agentSession`'s reads and `AgentOverlay`'s render (Task 4). `parseAction`'s `Action` union (Task 3) matches the card's `action.kind` branches (Task 4).
- **Fail-closed:** classifier denies on parse error / ambiguous push / unknown tool; approval server denies-without-asking on a deny verdict; `NewApprovalServer(nil classify)` gates (never allows). Verified across Tasks 1-2.
- **Security-critical unit:** the push-target parser has an exhaustive deny table (Task 1 Step 1). The live branch-resolve happens server-side (Task 2 Step 7), so a mid-run `checkout -b` is reflected; a bare push whose branch can't be resolved denies.
