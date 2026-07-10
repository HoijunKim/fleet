# fleet Intel Agent (drive the `claude` CLI agentically) - Slice 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Upgrade fleet's per-repo AI deep-dive from a single-shot text call into a real agentic session by driving the local `claude` CLI in headless stream-json mode, gating every mutating tool call (Edit/Write/Bash) through fleet's own approval UI over a loopback HTTP endpoint.

**Architecture:** A new Wails-free `internal/agent` package builds the `claude` argv, spawns it (reusing fleet's `winhide` + `WaitDelay` spawn shape), streams and parses its NDJSON events, and coordinates approvals. A tiny stdlib-only `cmd/fleet-hook` binary is registered as claude's `PreToolUse` hook via a run-scoped `--settings` file; it POSTs each mutating tool call to a fleet loopback server (`FLEET_HOOK_URL`) and blocks for the user's decision. `app.go` adapts `internal/agent` to Wails events/bindings; `RepoChat.svelte` renders live activity, approval cards, cost, and cancel.

**Tech Stack:** Go 1.22 (stdlib only: `os/exec`, `bufio`, `net`, `net/http`, `encoding/json`, `context`, `sync`), Wails v2 runtime events, Svelte/TypeScript frontend, the local `claude` CLI (Claude Code) v2.1+.

## Global Constraints

- Module path: `github.com/hoijun/fleet`. Go version floor: `go 1.22.0`.
- Desktop code is **stdlib-only**: NO new third-party dependency may be added to `go.mod` (the whole point of Approach A' is no SDK). Allowed imports are the Go standard library, `internal/*` fleet packages, and the already-present `github.com/wailsapp/wails/v2/...` (in `app.go`/`main.go` only).
- All Go source is **ASCII-only**. Every package must be `gofmt`-clean and `go vet`-clean.
- All spawned CLI processes MUST use `winhide.Apply(cmd)` and set `cmd.WaitDelay = 10 * time.Second` (a Node CLI can leave a grandchild holding the stdout pipe past a kill), matching `internal/ai.ClaudeRunner`.
- All Wails events are emitted with `wruntime.EventsEmit(a.ctx, name, data)` where `wruntime "github.com/wailsapp/wails/v2/pkg/runtime"`; cancellation reuses the `aiMu`/`aiCancel`/`context.CancelFunc` pattern in `app.go`.
- Minimum supported `claude` CLI: v2.1 (stream-json + PreToolUse JSON decisions). Below the floor, degrade to the existing single-shot `internal/ai` deep-dive. The floor is enforced at runtime in `AgentAvailable` (Task 8), which runs `claude --version` and checks `MinVersionMet` (Task 1).
- The PreToolUse hook is **fail-safe deny**: any error, timeout, or unreachable endpoint resolves to `permissionDecision: "deny"`.
- At most **one** approval is outstanding at a time; the hook timeout is generous (human-scale). Auth is the user's existing Claude Code login (NO API key).
- Commit messages follow Conventional Commits; a Korean description is fine; **NO `Co-Authored-By` trailer**.

---

## File Map

- `internal/agent/capability.go` (Create) - `claude --version` parse + v2.1 floor check. Pure.
- `internal/agent/stream.go` (Create) - stream-json event types + tolerant `Parse(line) []Event`.
- `internal/agent/policy.go` (Create) - allow/deny tool policy + `Flags()`.
- `internal/agent/prompt.go` (Create) - `BuildSystemPrompt(name, Record)` for `--append-system-prompt`.
- `internal/agent/gate.go` (Create) - approval `Coordinator` (Register/Decide/Await, timeout+cancel), ordering-independent delivery.
- `internal/agent/driver.go` (Create) - `BuildArgs`, `WriteHookSettings`, `Driver.Run` (spawn/stream/cancel).
- `internal/agent/approve.go` (Create) - loopback HTTP `ApprovalServer` wrapping the Coordinator.
- `internal/agent/*_test.go` (Create) - table + integration tests, including a compiled fake `claude` stub and a driver+stream+gate end-to-end test.
- `cmd/fleet-hook/main.go` (Create) - PreToolUse hook helper: stdin JSON -> POST `FLEET_HOOK_URL` -> print decision.
- `cmd/fleet-hook/main_test.go` (Create) - httptest-stubbed decision tests.
- `app.go` (Modify) - new fields, `AgentAsk`/`ApproveAction`/`CancelAgent`/`AgentAvailable`/`AgentConsent`/`GiveAgentConsent` bindings + event wiring + runtime version gate.
- `app_test.go` (Create) - consent-marker state test.
- `frontend/src/lib/RepoChat.svelte` (Modify) - agentic mode: live activity, approval card, cost, cancel, consent notice, event subscriptions.

---

### Task 1: CLI capability spike + version floor

Documented spike plus a pure, testable version gate. `internal/agent` is created here. `ParseVersion`/`MinVersionMet` are consumed at runtime by `App.AgentAvailable` (Task 8) so the v2.1 floor actually gates the agentic path.

**Spike (run manually, record findings inline in a comment at the top of `capability.go`):**
- `claude --help` - confirm the flags this slice depends on exist: `--print`/`-p`, `--output-format stream-json`, `--include-partial-messages`, `--verbose` (required to enable stream-json under `-p` in current CLI builds), `--append-system-prompt`, `--allowedTools`, `--disallowedTools`, `--settings <file>`, `--max-turns`, `--resume`.
- `claude --version` - confirm the version string shape fed to `ParseVersion`.
- **Fallback recorded:** if `--settings <file>` is absent on the installed CLI, write a temporary `.claude/settings.json` in the repo cwd before the run and delete it after (non-invasive; documented in the driver comment). This plan assumes `--settings` exists (v2.1+).

**Files:**
- Create: `internal/agent/capability.go`
- Test: `internal/agent/capability_test.go`

**Interfaces:**
- Consumes: (nothing)
- Produces:
  - `func ParseVersion(out string) (major, minor int, ok bool)`
  - `func MinVersionMet(major, minor int) bool`
- Consumed by: `App.AgentAvailable` (Task 8).

- [ ] **Step 1: Write the failing test**

```go
package agent

import "testing"

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in         string
		maj, min   int
		ok         bool
	}{
		{"2.1.4 (Claude Code)", 2, 1, true},
		{"claude 2.3.0", 2, 3, true},
		{"v2.10.1", 2, 10, true},
		{"no version here", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, c := range cases {
		maj, min, ok := ParseVersion(c.in)
		if ok != c.ok || maj != c.maj || min != c.min {
			t.Errorf("ParseVersion(%q) = %d,%d,%v want %d,%d,%v", c.in, maj, min, ok, c.maj, c.min, c.ok)
		}
	}
}

func TestMinVersionMet(t *testing.T) {
	cases := []struct {
		maj, min int
		want     bool
	}{
		{2, 1, true}, {2, 3, true}, {3, 0, true}, {2, 0, false}, {1, 9, false},
	}
	for _, c := range cases {
		if got := MinVersionMet(c.maj, c.min); got != c.want {
			t.Errorf("MinVersionMet(%d,%d) = %v want %v", c.maj, c.min, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run 'TestParseVersion|TestMinVersionMet' -v`
Expected: FAIL - `undefined: ParseVersion` / `undefined: MinVersionMet` (package does not compile).

- [ ] **Step 3: Write minimal implementation**

```go
// Package agent drives the local `claude` CLI in headless agentic mode and
// gates its mutating tool calls through fleet's approval UI. It is Wails-free
// (stdlib + internal/store + internal/winhide only); app.go adapts it to Wails
// events/bindings.
//
// CLI capability spike (verified against claude v2.1+, --help / --version):
//   flags used: -p, --output-format stream-json, --include-partial-messages,
//   --verbose, --append-system-prompt, --allowedTools, --disallowedTools,
//   --settings <file>, --max-turns, --resume. If --settings is unavailable on
//   an older CLI, the fallback is a temporary .claude/settings.json in the repo
//   cwd (see driver.go). Below the v2.1 floor, callers degrade to single-shot.
package agent

import (
	"strconv"
	"strings"
)

// minMajor, minMinor is the claude CLI floor for agentic mode: stream-json with
// PreToolUse JSON decisions requires v2.1+.
const (
	minMajor = 2
	minMinor = 1
)

// ParseVersion extracts the major and minor version from `claude --version`
// output such as "2.1.4 (Claude Code)" or "claude 2.3.0" or "v2.10.1". ok is
// false when no dotted numeric token is found.
func ParseVersion(out string) (major, minor int, ok bool) {
	for _, f := range strings.Fields(out) {
		f = strings.TrimPrefix(strings.TrimSpace(f), "v")
		if f == "" || f[0] < '0' || f[0] > '9' {
			continue
		}
		parts := strings.SplitN(f, ".", 3)
		if len(parts) < 2 {
			continue
		}
		maj, err1 := strconv.Atoi(parts[0])
		min, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			continue
		}
		return maj, min, true
	}
	return 0, 0, false
}

// MinVersionMet reports whether major.minor satisfies the agentic floor (v2.1).
func MinVersionMet(major, minor int) bool {
	if major != minMajor {
		return major > minMajor
	}
	return minor >= minMinor
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run 'TestParseVersion|TestMinVersionMet' -v`
Expected: PASS (both tests ok).

- [ ] **Step 5: gofmt + vet**

Run: `gofmt -l internal/agent/ && go vet ./internal/agent/`
Expected: no output (clean).

- [ ] **Step 6: Commit**

```bash
git add internal/agent/capability.go internal/agent/capability_test.go
git commit -m "feat(agent): claude CLI 버전 파싱 + v2.1 floor 게이트"
```

---

### Task 2: stream-json parser

Tolerant NDJSON parser normalizing claude stream events. A single line may yield several events (an assistant message can carry text + tool_use blocks). Streamed `text_delta` chunks are marked `Partial:true` so the app layer can stream them without also re-emitting the complete assistant text block (which the CLI repeats when `--include-partial-messages` is on).

**Files:**
- Create: `internal/agent/stream.go`
- Test: `internal/agent/stream_test.go`

**Interfaces:**
- Consumes: (nothing)
- Produces:
  - `type EventKind string` with consts `KindInit`, `KindText`, `KindTool`, `KindResult`
  - `type Event struct { Kind EventKind; SessionID, Model, Text string; Partial bool; ToolName string; ToolInput json.RawMessage; Result string; CostUSD float64; InputTokens, OutputTokens int }`
  - `func Parse(line []byte) []Event`

- [ ] **Step 1: Write the failing test**

```go
package agent

import "testing"

func TestParseInit(t *testing.T) {
	evs := Parse([]byte(`{"type":"system","subtype":"init","session_id":"sess-1","model":"claude-x"}`))
	if len(evs) != 1 || evs[0].Kind != KindInit || evs[0].SessionID != "sess-1" || evs[0].Model != "claude-x" {
		t.Fatalf("init parse = %+v", evs)
	}
}

func TestParseAssistantTextAndTool(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"Looking"},{"type":"tool_use","name":"Read","input":{"file_path":"app.go"}}]}}`
	evs := Parse([]byte(line))
	if len(evs) != 2 {
		t.Fatalf("want 2 events, got %d: %+v", len(evs), evs)
	}
	// A complete assistant text block is NOT partial (deltas carry the stream).
	if evs[0].Kind != KindText || evs[0].Text != "Looking" || evs[0].Partial {
		t.Errorf("text event = %+v", evs[0])
	}
	if evs[1].Kind != KindTool || evs[1].ToolName != "Read" || string(evs[1].ToolInput) != `{"file_path":"app.go"}` {
		t.Errorf("tool event = %+v", evs[1])
	}
}

func TestParsePartialTextDelta(t *testing.T) {
	line := `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"abc"}}}`
	evs := Parse([]byte(line))
	if len(evs) != 1 || evs[0].Kind != KindText || evs[0].Text != "abc" || !evs[0].Partial {
		t.Fatalf("delta parse = %+v", evs)
	}
}

func TestParseResult(t *testing.T) {
	line := `{"type":"result","subtype":"success","result":"done","total_cost_usd":0.012,"session_id":"sess-1","usage":{"input_tokens":10,"output_tokens":20}}`
	evs := Parse([]byte(line))
	if len(evs) != 1 || evs[0].Kind != KindResult {
		t.Fatalf("result parse = %+v", evs)
	}
	if evs[0].Result != "done" || evs[0].CostUSD != 0.012 || evs[0].InputTokens != 10 || evs[0].OutputTokens != 20 {
		t.Errorf("result fields = %+v", evs[0])
	}
}

func TestParseTolerant(t *testing.T) {
	for _, bad := range []string{"", "   ", "not json", "{bad", `{"type":"unknown_thing"}`, `[]`} {
		if evs := Parse([]byte(bad)); evs != nil {
			t.Errorf("Parse(%q) = %+v, want nil", bad, evs)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestParse -v`
Expected: FAIL - `undefined: Parse` / `undefined: KindInit` (does not compile). (Note: the existing `TestParseVersion` from Task 1 shares the `TestParse` prefix; that one still passes - the new ones fail to compile.)

- [ ] **Step 3: Write minimal implementation**

```go
package agent

import (
	"bytes"
	"encoding/json"
)

// EventKind classifies a normalized stream event.
type EventKind string

const (
	KindInit   EventKind = "init"
	KindText   EventKind = "text"
	KindTool   EventKind = "tool_use"
	KindResult EventKind = "result"
)

// Event is one normalized item extracted from a stream-json line. A single line
// may yield several events (an assistant message can carry a text block and a
// tool_use block at once). Partial is true only for streamed text_delta chunks;
// a complete assistant "text" block is Partial=false, so a consumer streaming
// deltas can ignore the redundant complete block that --include-partial-messages
// also emits.
type Event struct {
	Kind         EventKind
	SessionID    string
	Model        string
	Text         string
	Partial      bool
	ToolName     string
	ToolInput    json.RawMessage
	Result       string
	CostUSD      float64
	InputTokens  int
	OutputTokens int
}

// block is one content block inside an assistant/user message.
type block struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// envelope is the tolerant shape every line is decoded into. Unknown fields are
// ignored; missing fields stay zero.
type envelope struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Session string `json:"session_id"`
	Model   string `json:"model"`
	Message struct {
		Content []block `json:"content"`
		Model   string  `json:"model"`
	} `json:"message"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
	Event   json.RawMessage `json:"event"`
	Result  string          `json:"result"`
	CostUSD float64         `json:"total_cost_usd"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Parse decodes one newline-delimited JSON line into zero or more normalized
// events. Blank lines, non-object lines, malformed JSON, and unrecognized
// shapes yield nil - it never panics, so a noisy stream cannot crash the driver.
func Parse(line []byte) []Event {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] != '{' {
		return nil
	}
	var e envelope
	if err := json.Unmarshal(line, &e); err != nil {
		return nil
	}
	switch e.Type {
	case "system":
		if e.Subtype == "init" {
			return []Event{{Kind: KindInit, SessionID: e.Session, Model: e.Model}}
		}
		return nil
	case "assistant":
		model := e.Message.Model
		if model == "" {
			model = e.Model
		}
		var out []Event
		for _, b := range e.Message.Content {
			switch b.Type {
			case "text":
				if b.Text != "" {
					out = append(out, Event{Kind: KindText, Text: b.Text, Model: model, SessionID: e.Session})
				}
			case "tool_use":
				out = append(out, Event{Kind: KindTool, ToolName: b.Name, ToolInput: b.Input, SessionID: e.Session})
			}
		}
		return out
	case "stream_event":
		if len(e.Event) > 0 {
			return Parse(e.Event)
		}
		return nil
	case "content_block_delta":
		if e.Delta.Type == "text_delta" && e.Delta.Text != "" {
			return []Event{{Kind: KindText, Text: e.Delta.Text, Partial: true, SessionID: e.Session}}
		}
		return nil
	case "result":
		return []Event{{
			Kind:         KindResult,
			Result:       e.Result,
			CostUSD:      e.CostUSD,
			SessionID:    e.Session,
			InputTokens:  e.Usage.InputTokens,
			OutputTokens: e.Usage.OutputTokens,
		}}
	default:
		return nil
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestParse -v`
Expected: PASS (all `TestParse*` tests ok).

- [ ] **Step 5: gofmt + vet**

Run: `gofmt -l internal/agent/ && go vet ./internal/agent/`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/stream.go internal/agent/stream_test.go
git commit -m "feat(agent): stream-json 이벤트 파서 (init/text/tool/result, partial 델타 표시, 관대한 파싱)"
```

---

### Task 3: tool policy

Read-only tools allow-listed; secret reads + destructive shell denied; mutators absent from both lists so they fall through to the PreToolUse gate.

**Files:**
- Create: `internal/agent/policy.go`
- Test: `internal/agent/policy_test.go`

**Interfaces:**
- Consumes: (nothing)
- Produces:
  - `type Policy struct { Allowed []string; Disallowed []string }`
  - `func DefaultPolicy() Policy`
  - `func (p Policy) Flags() []string`

- [ ] **Step 1: Write the failing test**

```go
package agent

import (
	"strings"
	"testing"
)

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestDefaultPolicyLists(t *testing.T) {
	p := DefaultPolicy()
	if !has(p.Allowed, "Read") || !has(p.Allowed, "Grep") || !has(p.Allowed, "Glob") {
		t.Errorf("read-only tools must be allowed: %+v", p.Allowed)
	}
	if !has(p.Disallowed, "Read(**/.env)") || !has(p.Disallowed, "Bash(rm:*)") || !has(p.Disallowed, "Bash(git push:*)") {
		t.Errorf("secret/destructive must be denied: %+v", p.Disallowed)
	}
	// Mutators are gated by the hook, so they are in NEITHER list.
	for _, m := range []string{"Edit", "Write"} {
		if has(p.Allowed, m) || has(p.Disallowed, m) {
			t.Errorf("%s must be gated (absent from both lists)", m)
		}
	}
}

func TestPolicyFlags(t *testing.T) {
	p := Policy{Allowed: []string{"Read", "Grep"}, Disallowed: []string{"Bash(rm:*)"}}
	got := strings.Join(p.Flags(), " ")
	want := "--allowedTools Read,Grep --disallowedTools Bash(rm:*)"
	if got != want {
		t.Errorf("Flags() = %q want %q", got, want)
	}
	if len(Policy{}.Flags()) != 0 {
		t.Error("empty policy must produce no flags")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run 'TestDefaultPolicyLists|TestPolicyFlags' -v`
Expected: FAIL - `undefined: DefaultPolicy` / `undefined: Policy` (does not compile).

- [ ] **Step 3: Write minimal implementation**

```go
package agent

import "strings"

// Policy is the tool allow/deny lists handed to the claude CLI. Read-only tools
// are allow-listed so they run without a prompt; secret reads and destructive
// shell commands are denied outright; mutating tools (Edit, Write, general
// Bash) are deliberately absent from both lists so they fall through to the
// PreToolUse approval hook.
type Policy struct {
	Allowed    []string
	Disallowed []string
}

// DefaultPolicy returns fleet's slice-1 tool policy.
func DefaultPolicy() Policy {
	return Policy{
		Allowed: []string{
			"Read", "Grep", "Glob",
			"Bash(git status)", "Bash(git status:*)",
			"Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)",
		},
		Disallowed: []string{
			"Read(**/.env)", "Read(**/.env.*)", "Read(**/*secret*)",
			"Read(**/id_rsa)", "Read(**/id_ed25519)", "Read(**/*.pem)",
			"Read(**/credentials)", "Read(**/.aws/**)",
			"Bash(rm:*)", "Bash(git push:*)", "Bash(sudo:*)", "Bash(curl:*)",
		},
	}
}

// Flags renders the policy as claude CLI flags: each non-empty list becomes a
// single comma-joined value (allow flag first, then deny).
func (p Policy) Flags() []string {
	var out []string
	if len(p.Allowed) > 0 {
		out = append(out, "--allowedTools", strings.Join(p.Allowed, ","))
	}
	if len(p.Disallowed) > 0 {
		out = append(out, "--disallowedTools", strings.Join(p.Disallowed, ","))
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run 'TestDefaultPolicyLists|TestPolicyFlags' -v`
Expected: PASS.

- [ ] **Step 5: gofmt + vet**

Run: `gofmt -l internal/agent/ && go vet ./internal/agent/`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/policy.go internal/agent/policy_test.go
git commit -m "feat(agent): 툴 정책 (읽기 허용, 비밀/파괴 거부, 변경은 게이트)"
```

---

### Task 4: system-prompt builder

Builds the `--append-system-prompt` text: fleet role + this project's PM context.

**Files:**
- Create: `internal/agent/prompt.go`
- Test: `internal/agent/prompt_test.go`

**Interfaces:**
- Consumes: `github.com/hoijun/fleet/internal/store` -> `store.Record{Status, Deadline, Notes string; Tasks []store.Task}`, `store.Task{Title, Status, Due string}`
- Produces: `func BuildSystemPrompt(name string, r store.Record) string`

- [ ] **Step 1: Write the failing test**

```go
package agent

import (
	"strings"
	"testing"

	"github.com/hoijun/fleet/internal/store"
)

func TestBuildSystemPrompt(t *testing.T) {
	r := store.Record{
		Status:   "active",
		Deadline: "2026-08-01",
		Notes:    "ship the labeling tool",
		Tasks: []store.Task{
			{Title: "wire EMG parser", Status: "todo", Due: "2026-07-20"},
			{Title: "old task", Status: "done"},
		},
	}
	out := BuildSystemPrompt("fleet", r)
	for _, want := range []string{
		"fleet's code-aware assistant",
		"\"fleet\"",
		"approved by the user",
		"Status: active",
		"Deadline: 2026-08-01",
		"wire EMG parser",
		"due 2026-07-20",
		"ship the labeling tool",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "old task") {
		t.Error("done tasks must be omitted")
	}
}

func TestBuildSystemPromptEmpty(t *testing.T) {
	out := BuildSystemPrompt("proj", store.Record{})
	if !strings.Contains(out, "\"proj\"") {
		t.Errorf("empty record still needs role framing: %s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestBuildSystemPrompt -v`
Expected: FAIL - `undefined: BuildSystemPrompt`.

- [ ] **Step 3: Write minimal implementation**

```go
package agent

import (
	"fmt"
	"strings"

	"github.com/hoijun/fleet/internal/store"
)

// BuildSystemPrompt builds the text passed to `claude --append-system-prompt`:
// fleet's role framing plus this project's PM context (status, deadline, open
// tasks, notes) so the agent's answers are grounded in what the user tracks.
// name is the display name (a code project's store Record.Name is empty, so the
// caller passes the repo folder name).
func BuildSystemPrompt(name string, r store.Record) string {
	var b strings.Builder
	b.WriteString("You are fleet's code-aware assistant for the project \"")
	b.WriteString(name)
	b.WriteString("\". You are working inside this project's repository with read tools; ")
	b.WriteString("propose concrete, file-grounded changes. Any edit, file write, or shell ")
	b.WriteString("command you run is reviewed and approved by the user before it takes effect.\n\n")
	b.WriteString("=== Project management context (from fleet) ===\n")
	if s := strings.TrimSpace(r.Status); s != "" {
		fmt.Fprintf(&b, "Status: %s\n", s)
	}
	if d := strings.TrimSpace(r.Deadline); d != "" {
		fmt.Fprintf(&b, "Deadline: %s\n", d)
	}
	open := 0
	for _, t := range r.Tasks {
		if t.Status != "done" {
			open++
		}
	}
	if len(r.Tasks) > 0 {
		fmt.Fprintf(&b, "Tasks: %d open of %d total\n", open, len(r.Tasks))
		for _, t := range r.Tasks {
			if t.Status == "done" {
				continue
			}
			line := "- " + t.Title
			if strings.TrimSpace(t.Due) != "" {
				line += " (due " + t.Due + ")"
			}
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	if n := strings.TrimSpace(r.Notes); n != "" {
		b.WriteString("Notes: ")
		b.WriteString(n)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestBuildSystemPrompt -v`
Expected: PASS.

- [ ] **Step 5: gofmt + vet**

Run: `gofmt -l internal/agent/ && go vet ./internal/agent/`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/prompt.go internal/agent/prompt_test.go
git commit -m "feat(agent): 프로젝트 PM 컨텍스트로 append-system-prompt 생성"
```

---

### Task 5: approval Coordinator (gate concurrency core)

Maps each pending approval to a buffered channel; delivery is **ordering-independent** - `Decide` can run before or after `Await` and the decision is still received (the buffered channel holds it and `Await` never fails just because a decision already landed). `Await` blocks with timeout->deny and cancel->deny, and is the sole owner of cleanup (it discards the entry on exit). No real CLI/HTTP.

> **Concurrency rationale (the bug this fixes):** an earlier draft had `Decide` delete the pending entry before sending; `Await` then re-looked-up that entry by id and returned `"unknown approval id"` whenever `Decide` won the race - which the loopback flow can trigger (the GUI's `ApproveAction` HTTP call can complete before the `Await` goroutine registers its receive) and which the plan's own `TestCoordinatorAllow` (Decide-before-Await) would fail. The fix: `Decide` does a guarded non-blocking send and does NOT delete; `Await` owns deletion via `defer discard`. The buffered channel (cap 1) guarantees the decision is always deliverable, a second `Decide` is a no-op (`false`), and timeout/cancel still deny fail-safe.

**Files:**
- Create: `internal/agent/gate.go`
- Test: `internal/agent/gate_test.go`

**Interfaces:**
- Consumes: (nothing)
- Produces:
  - `type Decision struct { Approved bool; Reason string }`
  - `type Coordinator struct { ... }`
  - `func NewCoordinator() *Coordinator`
  - `func (c *Coordinator) Register() string`
  - `func (c *Coordinator) Decide(id string, approved bool, reason string) bool`
  - `func (c *Coordinator) Await(ctx context.Context, id string, timeout time.Duration) Decision`

- [ ] **Step 1: Write the failing test**

```go
package agent

import (
	"context"
	"testing"
	"time"
)

func TestCoordinatorAllow(t *testing.T) {
	// Decide BEFORE Await: the buffered channel must still deliver the decision.
	c := NewCoordinator()
	id := c.Register()
	if !c.Decide(id, true, "ok") {
		t.Fatal("Decide on a live id must return true")
	}
	d := c.Await(context.Background(), id, time.Second)
	if !d.Approved || d.Reason != "ok" {
		t.Errorf("await = %+v", d)
	}
}

func TestCoordinatorAwaitThenDecide(t *testing.T) {
	// Decide AFTER Await has started blocking (the normal loopback flow).
	c := NewCoordinator()
	id := c.Register()
	go func() {
		time.Sleep(10 * time.Millisecond)
		c.Decide(id, true, "yes")
	}()
	d := c.Await(context.Background(), id, time.Second)
	if !d.Approved || d.Reason != "yes" {
		t.Errorf("await = %+v", d)
	}
}

func TestCoordinatorDeny(t *testing.T) {
	c := NewCoordinator()
	id := c.Register()
	c.Decide(id, false, "nope")
	d := c.Await(context.Background(), id, time.Second)
	if d.Approved || d.Reason != "nope" {
		t.Errorf("await = %+v", d)
	}
}

func TestCoordinatorTimeout(t *testing.T) {
	c := NewCoordinator()
	id := c.Register()
	d := c.Await(context.Background(), id, 20*time.Millisecond)
	if d.Approved || d.Reason != "approval timed out" {
		t.Errorf("timeout await = %+v", d)
	}
	if c.Decide(id, true, "late") {
		t.Error("Decide after timeout must return false (entry discarded)")
	}
}

func TestCoordinatorCancel(t *testing.T) {
	c := NewCoordinator()
	id := c.Register()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := c.Await(ctx, id, time.Second)
	if d.Approved || d.Reason != "cancelled" {
		t.Errorf("cancel await = %+v", d)
	}
}

func TestCoordinatorDoubleDecide(t *testing.T) {
	c := NewCoordinator()
	id := c.Register()
	if !c.Decide(id, true, "first") {
		t.Fatal("first Decide must return true")
	}
	if c.Decide(id, false, "second") {
		t.Error("second Decide must return false (already decided)")
	}
	d := c.Await(context.Background(), id, time.Second)
	if !d.Approved || d.Reason != "first" {
		t.Errorf("await = %+v", d)
	}
}

func TestCoordinatorUnknownID(t *testing.T) {
	c := NewCoordinator()
	if c.Decide("nope", true, "x") {
		t.Error("Decide on unknown id must return false")
	}
	d := c.Await(context.Background(), "nope", time.Second)
	if d.Approved {
		t.Error("Await on unknown id must not approve")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestCoordinator -v`
Expected: FAIL - `undefined: NewCoordinator`.

- [ ] **Step 3: Write minimal implementation**

```go
package agent

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Decision is the user's answer to one gated tool call.
type Decision struct {
	Approved bool
	Reason   string
}

// Coordinator maps each pending approval to a buffered channel the GUI decision
// is delivered on. It is safe for concurrent use and ordering-independent:
// Decide may run before or after Await. At most one decision is ever delivered
// per id; Await is the sole owner of cleanup (it discards the entry on exit),
// and timeout/cancel both deny fail-safe.
type Coordinator struct {
	mu      sync.Mutex
	pending map[string]chan Decision
	seq     atomic.Uint64
}

// NewCoordinator returns an empty Coordinator.
func NewCoordinator() *Coordinator {
	return &Coordinator{pending: make(map[string]chan Decision)}
}

// Register creates a new pending approval and returns its id. The channel is
// buffered (cap 1) so Decide never blocks and the decision survives until Await
// receives it, regardless of Decide/Await ordering.
func (c *Coordinator) Register() string {
	id := "act-" + strconv.FormatUint(c.seq.Add(1), 36)
	c.mu.Lock()
	c.pending[id] = make(chan Decision, 1)
	c.mu.Unlock()
	return id
}

// Decide delivers the user's decision for id. It returns false if id is unknown,
// already discarded (timeout/cancel/awaited), or already decided. It never
// deletes the entry (Await owns cleanup) and never blocks: the send is
// non-blocking on the cap-1 buffer, so a duplicate decision is a no-op false.
func (c *Coordinator) Decide(id string, approved bool, reason string) bool {
	c.mu.Lock()
	ch, ok := c.pending[id]
	c.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- Decision{Approved: approved, Reason: reason}:
		return true
	default:
		return false // already decided (buffer full)
	}
}

// Await blocks until id is decided, the timeout elapses, or ctx is cancelled.
// Timeout and cancellation both resolve to a deny (fail-safe). Await always
// discards the pending entry on exit, so any later Decide is a no-op.
func (c *Coordinator) Await(ctx context.Context, id string, timeout time.Duration) Decision {
	c.mu.Lock()
	ch, ok := c.pending[id]
	c.mu.Unlock()
	if !ok {
		return Decision{Approved: false, Reason: "unknown approval id"}
	}
	defer c.discard(id)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case d := <-ch:
		return d
	case <-timer.C:
		return Decision{Approved: false, Reason: "approval timed out"}
	case <-ctx.Done():
		return Decision{Approved: false, Reason: "cancelled"}
	}
}

// discard removes a pending entry if it is still present.
func (c *Coordinator) discard(id string) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestCoordinator -race -v`
Expected: PASS with no race warnings (all six tests, including Decide-before-Await, Await-then-Decide, and double-Decide).

- [ ] **Step 5: gofmt + vet**

Run: `gofmt -l internal/agent/ && go vet ./internal/agent/`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/gate.go internal/agent/gate_test.go
git commit -m "feat(agent): 승인 Coordinator (순서 무관 전달, Register/Decide/Await, 타임아웃+취소 시 거부)"
```

---

### Task 6: fleet-hook helper binary

Reads `FLEET_HOOK_URL` + tool JSON on stdin, POSTs, prints the PreToolUse decision. Fail-safe deny.

**Files:**
- Create: `cmd/fleet-hook/main.go`
- Test: `cmd/fleet-hook/main_test.go`

**Interfaces:**
- Consumes: (nothing internal; talks to the loopback endpoint from Task 8)
- Produces (test seam within `package main`):
  - `func run(in io.Reader, out io.Writer, hookURL string, client *http.Client)`
  - stdout JSON shape: `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"|"deny","permissionDecisionReason":"..."}}`
  - request seam it POSTs, decoding response as `{"approved":bool,"reason":string}`

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func decode(t *testing.T, b []byte) hookOutput {
	t.Helper()
	var o hookOutput
	if err := json.Unmarshal(b, &o); err != nil {
		t.Fatalf("bad hook output %q: %v", b, err)
	}
	return o
}

func TestRunApprove(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		io.WriteString(w, `{"approved":true,"reason":"ok"}`)
	}))
	defer srv.Close()

	var out bytes.Buffer
	in := bytes.NewBufferString(`{"tool_name":"Edit","tool_input":{"file_path":"x"},"cwd":"/repo"}`)
	run(in, &out, srv.URL, srv.Client())

	o := decode(t, out.Bytes())
	if o.HookSpecificOutput.PermissionDecision != "allow" || o.HookSpecificOutput.PermissionDecisionReason != "ok" {
		t.Errorf("decision = %+v", o.HookSpecificOutput)
	}
	if o.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Errorf("hookEventName = %q", o.HookSpecificOutput.HookEventName)
	}
	if gotBody == "" || gotBody[0] != '{' {
		t.Errorf("server did not receive the tool JSON: %q", gotBody)
	}
}

func TestRunDeny(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"approved":false,"reason":"blocked"}`)
	}))
	defer srv.Close()
	var out bytes.Buffer
	run(bytes.NewBufferString(`{}`), &out, srv.URL, srv.Client())
	o := decode(t, out.Bytes())
	if o.HookSpecificOutput.PermissionDecision != "deny" || o.HookSpecificOutput.PermissionDecisionReason != "blocked" {
		t.Errorf("decision = %+v", o.HookSpecificOutput)
	}
}

func TestRunNoURLDenies(t *testing.T) {
	var out bytes.Buffer
	run(bytes.NewBufferString(`{}`), &out, "", http.DefaultClient)
	if decode(t, out.Bytes()).HookSpecificOutput.PermissionDecision != "deny" {
		t.Error("missing FLEET_HOOK_URL must deny")
	}
}

func TestRunServerErrorDenies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	var out bytes.Buffer
	run(bytes.NewBufferString(`{}`), &out, srv.URL, srv.Client())
	if decode(t, out.Bytes()).HookSpecificOutput.PermissionDecision != "deny" {
		t.Error("5xx must deny")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/fleet-hook/ -v`
Expected: FAIL - `undefined: run` / `undefined: hookOutput` (does not compile).

- [ ] **Step 3: Write minimal implementation**

```go
// Command fleet-hook is the PreToolUse hook helper the fleet app registers with
// the claude CLI (via a run-scoped --settings file). It reads the tool call
// JSON on stdin, forwards it to the running fleet app over the loopback URL in
// FLEET_HOOK_URL, and prints the user's allow/deny decision as the CLI's hook
// output. Any failure fails safe to deny. No dependency on bash/jq/curl.
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"time"
)

type decision struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason"`
}

type hookSpecific struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}

type hookOutput struct {
	HookSpecificOutput hookSpecific `json:"hookSpecificOutput"`
}

func main() {
	// Generous timeout: the app holds the request open while a human approves.
	client := &http.Client{Timeout: 15 * time.Minute}
	run(os.Stdin, os.Stdout, os.Getenv("FLEET_HOOK_URL"), client)
}

// run reads the tool JSON from in, POSTs it to hookURL, and writes the hook
// decision to out. It always writes a valid decision; on any error it denies
// with an explanatory reason (fail-safe).
func run(in io.Reader, out io.Writer, hookURL string, client *http.Client) {
	body, _ := io.ReadAll(io.LimitReader(in, 1<<20))
	if hookURL == "" {
		emit(out, false, "fleet approval endpoint unavailable")
		return
	}
	req, err := http.NewRequest(http.MethodPost, hookURL, bytes.NewReader(body))
	if err != nil {
		emit(out, false, "fleet approval request failed: "+err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		emit(out, false, "fleet approval unreachable: "+err.Error())
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		emit(out, false, "fleet approval denied (status "+resp.Status+")")
		return
	}
	var d decision
	if err := json.Unmarshal(data, &d); err != nil {
		emit(out, false, "fleet approval malformed response")
		return
	}
	reason := d.Reason
	if reason == "" {
		if d.Approved {
			reason = "approved in fleet"
		} else {
			reason = "rejected in fleet"
		}
	}
	emit(out, d.Approved, reason)
}

// emit writes the CLI's PreToolUse hook decision as JSON (exit 0).
func emit(out io.Writer, approved bool, reason string) {
	pd := "deny"
	if approved {
		pd = "allow"
	}
	_ = json.NewEncoder(out).Encode(hookOutput{HookSpecificOutput: hookSpecific{
		HookEventName:            "PreToolUse",
		PermissionDecision:       pd,
		PermissionDecisionReason: reason,
	}})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/fleet-hook/ -v`
Expected: PASS (all four tests).

- [ ] **Step 5: Verify it builds + gofmt/vet**

Run: `go build ./cmd/fleet-hook/ && gofmt -l cmd/fleet-hook/ && go vet ./cmd/fleet-hook/`
Expected: no output (builds clean, no format/vet issues).

- [ ] **Step 6: Commit**

```bash
git add cmd/fleet-hook/main.go cmd/fleet-hook/main_test.go
git commit -m "feat(fleet-hook): PreToolUse 훅 헬퍼 (stdin -> loopback POST -> allow/deny, 실패시 거부)"
```

---

### Task 7: CLI driver (argv, settings file, spawn/stream/cancel)

Builds the argv, writes the run-scoped hook settings, spawns `claude` with `FLEET_HOOK_URL` in the env, streams stdout into events, and cancels via context + WaitDelay. Tested against a compiled fake `claude` stub. (The full driver+stream+**gate** end-to-end test lives in Task 8, where the `ApprovalServer` exists; it reuses `buildStub` defined here.)

**Files:**
- Create: `internal/agent/driver.go`
- Test: `internal/agent/driver_test.go`

**Interfaces:**
- Consumes: `Policy.Flags()` (Task 3), `Parse(line) []Event` + `Event`/`EventKind` (Task 2), `winhide.Apply` (`github.com/hoijun/fleet/internal/winhide`)
- Produces:
  - `type Options struct { RepoDir, Prompt, SystemPrompt string; Policy Policy; SettingsPath, HookURL, ResumeID string; MaxTurns int }`
  - `func BuildArgs(o Options) []string`
  - `func WriteHookSettings(path, hookBin string) error`
  - `type Driver struct { Bin string; WaitDelay time.Duration }`
  - `func (d Driver) Run(ctx context.Context, o Options, onEvent func(Event)) error`
  - test helper `func buildStub(t *testing.T, src string) string` (reused by Task 8)

- [ ] **Step 1: Write the failing test**

```go
package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func joined(args []string) string { return strings.Join(args, " ") }

func TestBuildArgs(t *testing.T) {
	o := Options{
		Prompt:       "what is off?",
		SystemPrompt: "role text",
		Policy:       Policy{Allowed: []string{"Read"}, Disallowed: []string{"Bash(rm:*)"}},
		SettingsPath: "/tmp/s.json",
		ResumeID:     "sess-9",
		MaxTurns:     24,
	}
	got := joined(BuildArgs(o))
	for _, want := range []string{
		"-p what is off?",
		"--output-format stream-json",
		"--include-partial-messages",
		"--verbose",
		"--append-system-prompt role text",
		"--allowedTools Read",
		"--disallowedTools Bash(rm:*)",
		"--settings /tmp/s.json",
		"--max-turns 24",
		"--resume sess-9",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("args missing %q\n%s", want, got)
		}
	}
}

func TestBuildArgsMinimal(t *testing.T) {
	got := joined(BuildArgs(Options{Prompt: "hi"}))
	if strings.Contains(got, "--resume") || strings.Contains(got, "--max-turns") || strings.Contains(got, "--append-system-prompt") {
		t.Errorf("optional flags must be omitted when empty: %s", got)
	}
}

func TestWriteHookSettings(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	if err := WriteHookSettings(p, "C:/fleet/fleet-hook.exe"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("settings not valid JSON: %v", err)
	}
	s := string(data)
	for _, want := range []string{`"PreToolUse"`, `"Edit|Write|Bash"`, "fleet-hook", `"command"`} {
		if !strings.Contains(s, want) {
			t.Errorf("settings missing %q\n%s", want, s)
		}
	}
}

// buildStub compiles a throwaway "claude" replacement whose behavior is the Go
// source in src, returning its path. It exercises the driver's spawn+stream
// pipeline (and, in Task 8, the gate) without the real CLI.
func buildStub(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "stub.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "claude")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", bin, srcPath).CombinedOutput()
	if err != nil {
		t.Fatalf("build stub: %v\n%s", err, out)
	}
	return bin
}

const streamStub = `package main
import ("fmt";"os";"strings")
func main(){
	if p:=os.Getenv("FLEET_STUB_ARGV"); p!=""{ _=os.WriteFile(p,[]byte(strings.Join(os.Args[1:],"\n")),0644) }
	fmt.Println(` + "`" + `{"type":"system","subtype":"init","session_id":"sess-1","model":"claude-x"}` + "`" + `)
	fmt.Println(` + "`" + `{"type":"assistant","message":{"content":[{"type":"text","text":"Looking"}]}}` + "`" + `)
	fmt.Println("")
	fmt.Println(` + "`" + `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"app.go"}}]}}` + "`" + `)
	fmt.Println(` + "`" + `{"type":"result","subtype":"success","result":"done","total_cost_usd":0.012,"session_id":"sess-1"}` + "`" + `)
}
`

func TestRunStreamsEvents(t *testing.T) {
	bin := buildStub(t, streamStub)
	argvFile := filepath.Join(t.TempDir(), "argv.txt")
	t.Setenv("FLEET_STUB_ARGV", argvFile)

	var got []Event
	d := Driver{Bin: bin, WaitDelay: time.Second}
	err := d.Run(context.Background(), Options{Prompt: "q", HookURL: "http://127.0.0.1:1/approve"}, func(ev Event) {
		got = append(got, ev)
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("want 4 events, got %d: %+v", len(got), got)
	}
	if got[0].Kind != KindInit || got[0].SessionID != "sess-1" {
		t.Errorf("init = %+v", got[0])
	}
	if got[1].Kind != KindText || got[1].Text != "Looking" {
		t.Errorf("text = %+v", got[1])
	}
	if got[2].Kind != KindTool || got[2].ToolName != "Read" {
		t.Errorf("tool = %+v", got[2])
	}
	if got[3].Kind != KindResult || got[3].CostUSD != 0.012 {
		t.Errorf("result = %+v", got[3])
	}
	argv, _ := os.ReadFile(argvFile)
	if !strings.Contains(string(argv), "stream-json") {
		t.Errorf("stub did not receive expected argv: %s", argv)
	}
}

const sleepStub = `package main
import ("fmt";"time")
func main(){
	fmt.Println(` + "`" + `{"type":"system","subtype":"init","session_id":"s","model":"m"}` + "`" + `)
	time.Sleep(30*time.Second)
}
`

func TestRunCancel(t *testing.T) {
	bin := buildStub(t, sleepStub)
	ctx, cancel := context.WithCancel(context.Background())
	d := Driver{Bin: bin, WaitDelay: 300 * time.Millisecond}

	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx, Options{Prompt: "q"}, func(ev Event) {
			if ev.Kind == KindInit {
				cancel() // cancel as soon as the process is alive and streaming
			}
		})
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "cancelled") {
			t.Errorf("want cancelled error, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run 'TestBuildArgs|TestWriteHookSettings|TestRun' -v`
Expected: FAIL - `undefined: BuildArgs` / `undefined: Options` / `undefined: Driver` (does not compile).

- [ ] **Step 3: Write minimal implementation**

```go
package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/hoijun/fleet/internal/winhide"
)

// Options configures one agentic run of the claude CLI.
type Options struct {
	RepoDir      string
	Prompt       string
	SystemPrompt string
	Policy       Policy
	SettingsPath string
	HookURL      string
	ResumeID     string
	MaxTurns     int
}

// BuildArgs assembles the claude CLI argv for an agentic streaming run. Pure so
// it can be unit-tested without spawning anything. --verbose is required to
// enable stream-json output under -p on current CLI builds.
func BuildArgs(o Options) []string {
	args := []string{
		"-p", o.Prompt,
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--verbose",
	}
	if strings.TrimSpace(o.SystemPrompt) != "" {
		args = append(args, "--append-system-prompt", o.SystemPrompt)
	}
	args = append(args, o.Policy.Flags()...)
	if o.SettingsPath != "" {
		args = append(args, "--settings", o.SettingsPath)
	}
	if o.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(o.MaxTurns))
	}
	if o.ResumeID != "" {
		args = append(args, "--resume", o.ResumeID)
	}
	return args
}

// WriteHookSettings writes a run-scoped claude settings file at path that
// registers fleet's PreToolUse hook (hookBin) for the mutating tools. The CLI
// is pointed at it with --settings so no project .claude/ is touched. The hook
// timeout is generous (human-scale) so a person has time to approve.
func WriteHookSettings(path, hookBin string) error {
	cfg := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Edit|Write|Bash",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": hookBin,
							"timeout": 900,
						},
					},
				},
			},
		},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Driver spawns the claude CLI and streams its events. Bin defaults to "claude"
// on PATH; tests point it at a stub. WaitDelay bounds Wait after a kill (a Node
// CLI can leave a grandchild holding the stdout pipe), defaulting to 10s.
type Driver struct {
	Bin       string
	WaitDelay time.Duration
}

// Run spawns claude for o and invokes onEvent for every parsed stream event, in
// stream order, until the process exits or ctx is cancelled. FLEET_HOOK_URL is
// injected into the child env so the PreToolUse hook can reach fleet.
func (d Driver) Run(ctx context.Context, o Options, onEvent func(Event)) error {
	bin := d.Bin
	if bin == "" {
		bin = "claude"
	}
	wait := d.WaitDelay
	if wait == 0 {
		wait = 10 * time.Second
	}
	cmd := exec.CommandContext(ctx, bin, BuildArgs(o)...)
	cmd.Dir = o.RepoDir
	winhide.Apply(cmd)
	cmd.WaitDelay = wait
	cmd.Env = append(os.Environ(), "FLEET_HOOK_URL="+o.HookURL)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)
	// tool_use inputs can carry large diffs; allow long lines.
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		for _, ev := range Parse(scanner.Bytes()) {
			onEvent(ev)
		}
	}
	err = cmd.Wait()
	if err != nil {
		if ctx.Err() == context.Canceled {
			return fmt.Errorf("cancelled")
		}
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("claude: %s", msg)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run 'TestBuildArgs|TestWriteHookSettings|TestRun' -v`
Expected: PASS (argv, settings, stream-events, and cancel tests all green).

- [ ] **Step 5: gofmt + vet + full package test**

Run: `gofmt -l internal/agent/ && go vet ./internal/agent/ && go test ./internal/agent/`
Expected: no format/vet output; `ok  github.com/hoijun/fleet/internal/agent`.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/driver.go internal/agent/driver_test.go
git commit -m "feat(agent): claude 드라이버 (argv/settings/spawn/stream/cancel) + 가짜 CLI 스텁 테스트"
```

---

### Task 8: loopback approval server + app.go bindings

The Wails-free `ApprovalServer` (testable via httptest) plus a driver+stream+gate end-to-end test, plus thin `app.go` wiring: bindings, events, driver+gate+server orchestration, runtime version gate, session resume, consent marker.

**Files:**
- Create: `internal/agent/approve.go`
- Test: `internal/agent/approve_test.go`
- Modify: `app.go`
- Test: `app_test.go`

**Interfaces:**
- Consumes: `Coordinator` (Task 5), `Driver`/`Options` (Task 7), `BuildSystemPrompt` (Task 4), `DefaultPolicy` (Task 3), `WriteHookSettings` (Task 7), `ParseVersion`/`MinVersionMet` (Task 1), `store.Record`, `buildStub` (Task 7 test helper), `winhide.Apply`
- Produces:
  - `type ActionRequest struct { ID, ToolName string; ToolInput json.RawMessage; Cwd, SessionID string }` (json tags: `id`,`toolName`,`toolInput`,`cwd`,`sessionId`)
  - `func NewApprovalServer(ctx context.Context, coord *Coordinator, timeout time.Duration, onAction func(ActionRequest)) *ApprovalServer`
  - `func (s *ApprovalServer) Start() error`, `func (s *ApprovalServer) URL() string`, `func (s *ApprovalServer) Stop(ctx context.Context) error`
  - `app.go`: `func (a *App) AgentAvailable() bool`, `func (a *App) AgentConsent() bool`, `func (a *App) GiveAgentConsent() string`, `func (a *App) AgentAsk(projectID, question string) string`, `func (a *App) ApproveAction(id string, approved bool)`, `func (a *App) CancelAgent()`
  - Wails events emitted: `agent:text` (string), `agent:activity` (`{tool,input}`), `agent:action` (`ActionRequest`), `agent:done` (`{result,costUsd,inputTokens,outputTokens}`), `agent:error` (string)

- [ ] **Step 1: Write the failing tests (approval server + driver->gate end-to-end)**

`internal/agent/approve_test.go`:

```go
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func postApprove(t *testing.T, url, body string) map[string]any {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestApprovalServerAllow(t *testing.T) {
	coord := NewCoordinator()
	gotCh := make(chan ActionRequest, 1)
	srv := NewApprovalServer(nil, coord, time.Second, func(a ActionRequest) { gotCh <- a })
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop(nil)

	resCh := make(chan map[string]any, 1)
	go func() {
		resCh <- postApprove(t, srv.URL(), `{"tool_name":"Edit","tool_input":{"file_path":"x"},"cwd":"/r","session_id":"s"}`)
	}()

	req := <-gotCh
	if req.ToolName != "Edit" || req.Cwd != "/r" || req.SessionID != "s" || req.ID == "" {
		t.Fatalf("action req = %+v", req)
	}
	if !coord.Decide(req.ID, true, "yes") {
		t.Fatal("Decide failed")
	}
	res := <-resCh
	if res["approved"] != true || res["reason"] != "yes" {
		t.Errorf("response = %+v", res)
	}
}

func TestApprovalServerTimeout(t *testing.T) {
	coord := NewCoordinator()
	srv := NewApprovalServer(nil, coord, 20*time.Millisecond, func(a ActionRequest) {})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop(nil)
	res := postApprove(t, srv.URL(), `{"tool_name":"Bash"}`)
	if res["approved"] != false {
		t.Errorf("timeout must deny: %+v", res)
	}
}

// gateStub is a fake claude that reads FLEET_HOOK_URL (set by Driver.Run),
// POSTs a mutating tool call to fleet's loopback ApprovalServer exactly as the
// real PreToolUse hook would, and emits a result reflecting the decision. It
// uses only escaped double-quoted strings so it embeds cleanly as a raw string.
const gateStub = `package main
import ("bytes";"encoding/json";"fmt";"io";"net/http";"os")
func main(){
	url:=os.Getenv("FLEET_HOOK_URL")
	resp,err:=http.Post(url,"application/json",bytes.NewReader([]byte("{\"tool_name\":\"Edit\",\"tool_input\":{},\"session_id\":\"s\",\"cwd\":\"/r\"}")))
	if err!=nil{ fmt.Println("post failed"); os.Exit(1) }
	defer resp.Body.Close()
	data,_:=io.ReadAll(resp.Body)
	var d map[string]any
	json.Unmarshal(data,&d)
	res:="denied"
	if a,ok:=d["approved"].(bool); ok && a { res="approved" }
	out,_:=json.Marshal(map[string]any{"type":"result","subtype":"success","result":res})
	os.Stdout.Write(append(out,'\n'))
}
`

// TestDriverGateEndToEnd wires the real Driver + ApprovalServer + Coordinator:
// the stub POSTs through the loopback endpoint, the coordinator decision flows
// back, and the stub's emitted result proves the whole driver->hook->gate->
// driver pipeline works (this would fail under the old delete-before-send
// Coordinator, which lost a Decide that raced ahead of Await).
func TestDriverGateEndToEnd(t *testing.T) {
	coord := NewCoordinator()
	srv := NewApprovalServer(nil, coord, 2*time.Second, func(a ActionRequest) {
		coord.Decide(a.ID, true, "approved in test") // stand in for the GUI approving
	})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop(nil)

	bin := buildStub(t, gateStub)
	var got []Event
	err := Driver{Bin: bin, WaitDelay: time.Second}.Run(
		context.Background(),
		Options{Prompt: "q", HookURL: srv.URL()},
		func(ev Event) { got = append(got, ev) },
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("no events streamed from the gated run")
	}
	last := got[len(got)-1]
	if last.Kind != KindResult || last.Result != "approved" {
		t.Fatalf("gate decision did not flow driver->hook->coordinator->stub: %+v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/agent/ -run 'TestApprovalServer|TestDriverGateEndToEnd' -v`
Expected: FAIL - `undefined: NewApprovalServer` / `undefined: ActionRequest`.

- [ ] **Step 3: Write the approval server**

```go
package agent

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"time"
)

// ActionRequest is a gated tool call awaiting the user's approval, delivered to
// the GUI. ID correlates the later ApproveAction call back to this request.
type ActionRequest struct {
	ID        string          `json:"id"`
	ToolName  string          `json:"toolName"`
	ToolInput json.RawMessage `json:"toolInput"`
	Cwd       string          `json:"cwd"`
	SessionID string          `json:"sessionId"`
}

// hookPost is the JSON the fleet-hook helper POSTs to /approve.
type hookPost struct {
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
	SessionID string          `json:"session_id"`
	Cwd       string          `json:"cwd"`
}

// ApprovalServer is the loopback HTTP endpoint the fleet-hook helper calls. For
// each POST it registers a pending approval, hands the action to the GUI via
// onAction, blocks until the user decides (or timeout/cancel), then answers the
// still-open request with {approved, reason}.
type ApprovalServer struct {
	coord    *Coordinator
	onAction func(ActionRequest)
	timeout  time.Duration
	ctx      context.Context
	srv      *http.Server
	url      string
}

// NewApprovalServer builds the server. ctx (may be nil) cancels any in-flight
// Await so CancelAgent unblocks a waiting hook. onAction emits to the GUI.
func NewApprovalServer(ctx context.Context, coord *Coordinator, timeout time.Duration, onAction func(ActionRequest)) *ApprovalServer {
	return &ApprovalServer{coord: coord, onAction: onAction, timeout: timeout, ctx: ctx}
}

// Start binds an ephemeral loopback port and serves in the background.
func (s *ApprovalServer) Start() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/approve", s.handleApprove)
	s.srv = &http.Server{Handler: mux}
	s.url = "http://" + ln.Addr().String() + "/approve"
	go s.srv.Serve(ln)
	return nil
}

// URL is the endpoint fleet sets as FLEET_HOOK_URL on the claude process.
func (s *ApprovalServer) URL() string { return s.url }

// Stop shuts the server down. A nil ctx uses a short background context.
func (s *ApprovalServer) Stop(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), time.Second)
		defer cancel()
	}
	return s.srv.Shutdown(ctx)
}

func (s *ApprovalServer) handleApprove(w http.ResponseWriter, r *http.Request) {
	var p hookPost
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&p); err != nil {
		writeDecision(w, false, "malformed hook request")
		return
	}
	id := s.coord.Register()
	if s.onAction != nil {
		s.onAction(ActionRequest{ID: id, ToolName: p.ToolName, ToolInput: p.ToolInput, Cwd: p.Cwd, SessionID: p.SessionID})
	}
	ctx := s.ctx
	if ctx == nil {
		ctx = r.Context()
	}
	d := s.coord.Await(ctx, id, s.timeout)
	writeDecision(w, d.Approved, d.Reason)
}

func writeDecision(w http.ResponseWriter, approved bool, reason string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"approved": approved, "reason": reason})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/agent/ -run 'TestApprovalServer|TestDriverGateEndToEnd' -race -v`
Expected: PASS with no race warnings (approval allow/timeout AND the driver+stream+gate end-to-end run).

- [ ] **Step 5: Wire app.go - add imports and fields**

In `app.go`, add `"os"` to the import block (next to `"os/exec"`). Add the two internal imports (keep the block sorted): the agent package next to the other `internal/*` imports, and `winhide` (needed by the runtime version check):

```go
	"github.com/hoijun/fleet/internal/agent"
	"github.com/hoijun/fleet/internal/winhide"
```

Add these fields to the `App` struct (after the `aiGen int` line):

```go
	// agentic deep-dive (drives the claude CLI + PreToolUse approval gate)
	dataDir      string
	agentCoord   *agent.Coordinator
	agentSrv     *agent.ApprovalServer
	agentMu      sync.Mutex
	agentCancel  context.CancelFunc
	agentSession map[string]string
```

In `NewApp`, add these lines inside the returned `&App{...}` literal (e.g. after the `syncView:` line). `dir` is already in scope (`dir := filepath.Dir(cfgPath)`):

```go
		dataDir:      dir,
		agentCoord:   agent.NewCoordinator(),
		agentSession: map[string]string{},
```

- [ ] **Step 6: Wire app.go - add the bindings (with the runtime version gate)**

Append these methods to `app.go`:

```go
// AgentAvailable reports whether the agentic deep-dive can run: the provider
// must be Claude (Claude Code), the claude CLI must be on PATH, and it must meet
// the v2.1 floor (stream-json + PreToolUse JSON decisions). Below the floor the
// UI degrades to the single-shot deep-dive.
func (a *App) AgentAvailable() bool {
	c := a.cfgSnapshot()
	if c.AIProvider != "" && c.AIProvider != "claude" {
		return false
	}
	path, err := exec.LookPath("claude")
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	winhide.Apply(cmd)
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	maj, min, ok := agent.ParseVersion(string(out))
	return ok && agent.MinVersionMet(maj, min)
}

// consentPath is the marker file recording one-time agentic consent.
func (a *App) consentPath() string { return filepath.Join(a.dataDir, "agent_consent") }

// AgentConsent reports whether the one-time agentic consent was given.
func (a *App) AgentConsent() bool {
	_, err := os.Stat(a.consentPath())
	return err == nil
}

// GiveAgentConsent records the one-time consent. Returns "" on success.
func (a *App) GiveAgentConsent() string {
	if err := os.MkdirAll(a.dataDir, 0o755); err != nil {
		return err.Error()
	}
	return errMsg(os.WriteFile(a.consentPath(), []byte("1"), 0o644))
}

// agentHookBinary resolves fleet-hook: a sibling of the running executable if
// present, else the bare name (relying on PATH).
func agentHookBinary() string {
	name := "fleet-hook"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if exe, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(exe), name)
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	return name
}

// AgentAsk starts an agentic deep-dive on projectID's repo for question. It
// spawns the claude CLI, streams events to the front end (agent:text/activity/
// done/error), and gates mutating tool calls through agent:action. Returns ""
// on a successful start, or an "error: ..." string.
func (a *App) AgentAsk(projectID, question string) string {
	if !a.AgentAvailable() {
		return "error: agentic deep-dive requires the Claude (Claude Code) provider"
	}
	repoDir := projectID // a code project's id is its repo path
	rec, _ := a.store.Get(projectID)
	name := rec.Name
	if name == "" {
		name = filepath.Base(projectID)
	}

	tmpDir, err := os.MkdirTemp("", "fleet-agent-")
	if err != nil {
		return "error: " + err.Error()
	}
	settings := filepath.Join(tmpDir, "settings.json")
	if err := agent.WriteHookSettings(settings, agentHookBinary()); err != nil {
		os.RemoveAll(tmpDir)
		return "error: " + err.Error()
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.agentMu.Lock()
	if a.agentCancel != nil {
		a.agentCancel() // supersede any earlier run
	}
	a.agentCancel = cancel
	if a.agentSrv != nil {
		a.agentSrv.Stop(nil)
	}
	srv := agent.NewApprovalServer(ctx, a.agentCoord, 10*time.Minute, func(req agent.ActionRequest) {
		wruntime.EventsEmit(a.ctx, "agent:action", req)
	})
	if err := srv.Start(); err != nil {
		a.agentMu.Unlock()
		cancel()
		os.RemoveAll(tmpDir)
		return "error: " + err.Error()
	}
	a.agentSrv = srv
	resume := a.agentSession[projectID]
	a.agentMu.Unlock()

	opts := agent.Options{
		RepoDir:      repoDir,
		Prompt:       question,
		SystemPrompt: agent.BuildSystemPrompt(name, rec),
		Policy:       agent.DefaultPolicy(),
		SettingsPath: settings,
		HookURL:      srv.URL(),
		ResumeID:     resume,
		MaxTurns:     24,
	}
	go func() {
		defer os.RemoveAll(tmpDir)
		defer cancel()
		err := agent.Driver{}.Run(ctx, opts, func(ev agent.Event) {
			switch ev.Kind {
			case agent.KindInit:
				if ev.SessionID != "" {
					a.agentMu.Lock()
					a.agentSession[projectID] = ev.SessionID
					a.agentMu.Unlock()
				}
			case agent.KindText:
				// Stream only the partial text_delta chunks; the CLI repeats the
				// same text as a complete assistant block when partial messages
				// are on, so emitting both would double the answer.
				if ev.Partial {
					wruntime.EventsEmit(a.ctx, "agent:text", ev.Text)
				}
			case agent.KindTool:
				wruntime.EventsEmit(a.ctx, "agent:activity", map[string]any{
					"tool": ev.ToolName, "input": string(ev.ToolInput),
				})
			case agent.KindResult:
				wruntime.EventsEmit(a.ctx, "agent:done", map[string]any{
					"result": ev.Result, "costUsd": ev.CostUSD,
					"inputTokens": ev.InputTokens, "outputTokens": ev.OutputTokens,
				})
			}
		})
		if err != nil {
			wruntime.EventsEmit(a.ctx, "agent:error", err.Error())
		}
	}()
	return ""
}

// ApproveAction resolves the outstanding gated tool call id with the user's
// decision, unblocking the waiting fleet-hook request.
func (a *App) ApproveAction(id string, approved bool) {
	reason := "approved in fleet"
	if !approved {
		reason = "rejected in fleet"
	}
	a.agentCoord.Decide(id, approved, reason)
}

// CancelAgent kills the in-flight agentic run (context cancel + WaitDelay) and
// unblocks any pending approval as a deny.
func (a *App) CancelAgent() {
	a.agentMu.Lock()
	c := a.agentCancel
	a.agentMu.Unlock()
	if c != nil {
		c()
	}
}
```

- [ ] **Step 7: Write the app_test.go consent test**

```go
package main

import (
	"path/filepath"
	"testing"
)

func TestAgentConsentMarker(t *testing.T) {
	dir := t.TempDir()
	a := &App{dataDir: dir}
	if a.AgentConsent() {
		t.Fatal("consent must be false before it is given")
	}
	if msg := a.GiveAgentConsent(); msg != "" {
		t.Fatalf("GiveAgentConsent error: %s", msg)
	}
	if !a.AgentConsent() {
		t.Error("consent must be true after GiveAgentConsent")
	}
	if a.consentPath() != filepath.Join(dir, "agent_consent") {
		t.Errorf("consentPath = %q", a.consentPath())
	}
}
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/agent/ -run 'TestApprovalServer|TestDriverGateEndToEnd' && go test . -run TestAgentConsentMarker -v && go build ./...`
Expected: PASS for the agent server/gate tests and the consent test; `go build ./...` succeeds (app.go compiles with the new bindings, imports, and fields).

- [ ] **Step 9: gofmt + vet**

Run: `gofmt -l internal/agent/ app.go app_test.go && go vet ./internal/agent/ . ./cmd/fleet-hook/`
Expected: no output.

- [ ] **Step 10: Commit**

```bash
git add internal/agent/approve.go internal/agent/approve_test.go app.go app_test.go
git commit -m "feat(agent): loopback 승인 서버 + app.go 바인딩(AgentAsk/ApproveAction/CancelAgent), 버전 게이트, 이벤트 배선"
```

---

### Task 9: RepoChat.svelte agentic mode

Upgrade the deep-dive UI: when agentic is available and consent is given, run `AgentAsk` and render live activity, streamed answer, an approval card (tool + input, approve/reject), per-run cost, and cancel. Show a one-time consent notice before first use. Non-agentic providers keep the existing single-shot flow untouched. Both the input row and the starter buttons route through one dispatcher so starters honor the agentic path too.

**Files:**
- Modify: `frontend/src/lib/RepoChat.svelte`

**Interfaces:**
- Consumes (Wails bindings): `AgentAsk(projectID, question) => Promise<string>`, `ApproveAction(id, approved) => Promise<void>`, `CancelAgent() => Promise<void>`, `AgentAvailable() => Promise<boolean>`, `AgentConsent() => Promise<boolean>`, `GiveAgentConsent() => Promise<string>`
- Consumes (events): `agent:text`, `agent:activity`, `agent:action`, `agent:done`, `agent:error`
- Produces: (frontend only; no exports)

- [ ] **Step 1: Add imports and agentic state to the `<script>` block**

Replace the first import line:

```ts
  import { AskAI, RepoDiff, Log, RepoSymbols, CancelAI, ReadRepoFile, RepoGrep, RepoFiles } from "../../wailsjs/go/main/App";
```

with:

```ts
  import {
    AskAI, RepoDiff, Log, RepoSymbols, CancelAI, ReadRepoFile, RepoGrep, RepoFiles,
    AgentAsk, ApproveAction, CancelAgent, AgentAvailable, AgentConsent, GiveAgentConsent,
  } from "../../wailsjs/go/main/App";
  import { EventsOn } from "../../wailsjs/runtime/runtime";
  import { onMount, onDestroy } from "svelte";
```

Add this agentic state block immediately after the existing `let genId = 0;` line:

```ts
  // Agentic deep-dive (drives the claude CLI with a live approval gate).
  let agentic = false; // provider is Claude + CLI present and meets the v2.1 floor
  let consent = false; // one-time consent given
  let agentRunning = false;
  let agentStream = ""; // streamed assistant text for the in-flight run
  let activity: { tool: string; input: string }[] = [];
  let pending: { id: string; toolName: string; toolInput: string } | null = null;
  let cost: { costUsd: number; inputTokens: number; outputTokens: number } | null = null;
  let unsubs: Array<() => void> = [];

  function fmtInput(v: any): string {
    if (typeof v === "string") return v;
    try {
      return JSON.stringify(v, null, 2);
    } catch {
      return String(v);
    }
  }

  onMount(async () => {
    try {
      agentic = await AgentAvailable();
      consent = await AgentConsent();
    } catch {
      agentic = false;
    }
    unsubs.push(EventsOn("agent:text", (t: any) => { agentStream += String(t ?? ""); }));
    unsubs.push(EventsOn("agent:activity", (a: any) => {
      activity = [...activity, { tool: a?.tool ?? "", input: fmtInput(a?.input) }];
    }));
    unsubs.push(EventsOn("agent:action", (a: any) => {
      pending = { id: a?.id ?? "", toolName: a?.toolName ?? "", toolInput: fmtInput(a?.toolInput) };
    }));
    unsubs.push(EventsOn("agent:done", (d: any) => {
      cost = { costUsd: d?.costUsd ?? 0, inputTokens: d?.inputTokens ?? 0, outputTokens: d?.outputTokens ?? 0 };
      const answer = agentStream.trim() || String(d?.result ?? "(no answer)");
      turns = [...turns, { role: "assistant", text: answer }];
      saveChat();
      agentStream = "";
      activity = [];
      pending = null;
      agentRunning = false;
    }));
    unsubs.push(EventsOn("agent:error", (e: any) => {
      turns = [...turns, { role: "assistant", text: "error: " + String(e ?? "agent failed") }];
      saveChat();
      agentStream = "";
      pending = null;
      agentRunning = false;
    }));
  });

  onDestroy(() => { unsubs.forEach((u) => u()); });

  async function giveConsent() {
    const msg = await GiveAgentConsent();
    if (!msg) consent = true;
  }

  async function askAgent(text: string) {
    const q = text.trim();
    if (!q || agentRunning) return;
    question = "";
    turns = [...turns, { role: "user", text: q }];
    agentStream = "";
    activity = [];
    pending = null;
    cost = null;
    agentRunning = true;
    const id = project.repoPath || project.path;
    const err = await AgentAsk(id, q);
    if (err) {
      turns = [...turns, { role: "assistant", text: err }];
      agentRunning = false;
    }
  }

  async function decide(approved: boolean) {
    if (!pending) return;
    await ApproveAction(pending.id, approved);
    pending = null;
  }

  function cancelAgent() {
    CancelAgent();
    agentRunning = false;
    pending = null;
  }
```

- [ ] **Step 2: Route the input + starters to the agentic path when active**

Replace the existing `onKey` function:

```ts
  function onKey(e: KeyboardEvent) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      submit();
    }
  }

  // dispatch picks the agentic path when available + consented, else single-shot.
  // Used by the input row (via submit) AND the starter buttons so both honor the
  // agentic mode.
  function dispatch(text: string) {
    if (agentic && consent) askAgent(text);
    else ask(text);
  }

  function submit() {
    dispatch(question);
  }
```

- [ ] **Step 3: Add the agentic UI markup and route the starter buttons**

Insert this block at the top of the `<div class="rchat">`, immediately after the opening tag and before `{#if turns.length === 0}`:

```svelte
  {#if agentic && !consent}
    <div class="rchat-consent">
      <p>
        The agentic deep-dive lets Claude Code read files in this repo and send
        them to Anthropic under your Claude login, and can propose edits or
        commands (each one you approve here first).
      </p>
      <button class="btn btn-primary btn-sm" on:click={giveConsent}>Enable agentic deep-dive</button>
    </div>
  {/if}

  {#if agentic && consent && (agentRunning || activity.length || agentStream || pending)}
    <div class="rchat-agent">
      {#if activity.length}
        <div class="rchat-activity">
          {#each activity as a}
            <div class="rchat-act"><span class="rchat-tool-dot"></span><span class="mono">{a.tool}</span> {a.input}</div>
          {/each}
        </div>
      {/if}
      {#if agentStream}
        <div class="rchat-a rchat-stream">{agentStream}</div>
      {/if}
      {#if pending}
        <div class="rchat-approval">
          <div class="rchat-approval-head">Approve <span class="mono">{pending.toolName}</span>?</div>
          <pre class="rchat-approval-body">{pending.toolInput}</pre>
          <div class="rchat-approval-btns">
            <button class="btn btn-primary btn-sm" on:click={() => decide(true)}>Approve</button>
            <button class="btn btn-sm rchat-reject" on:click={() => decide(false)}>Reject</button>
          </div>
        </div>
      {/if}
      {#if agentRunning}
        <div class="rchat-loading">
          <span class="spinner"></span> working in the repo...
          <button class="rchat-clear" on:click={cancelAgent}>Cancel</button>
        </div>
      {/if}
      {#if cost}
        <div class="rchat-cost">cost ${cost.costUsd.toFixed(4)} - {cost.inputTokens} in / {cost.outputTokens} out tokens</div>
      {/if}
    </div>
  {/if}
```

Route the starter buttons through `dispatch` and disable them during an agentic run. Replace:

```svelte
          <button class="rchat-starter" on:click={() => ask(s)} disabled={loading}>{s}</button>
```

with:

```svelte
          <button class="rchat-starter" on:click={() => dispatch(s)} disabled={loading || agentRunning}>{s}</button>
```

Update the input row's Ask button. Replace:

```svelte
    <button class="btn btn-primary btn-sm" on:click={() => ask(question)} disabled={loading || !question.trim()}>Ask</button>
```

with:

```svelte
    <button class="btn btn-primary btn-sm" on:click={submit} disabled={(loading || agentRunning) || !question.trim()}>Ask</button>
```

and update the input's `disabled` attribute from `disabled={loading}` to `disabled={loading || agentRunning}`.

- [ ] **Step 4: Add styles**

Append these rules inside the `<style>` block (before the closing `</style>`):

```css
  .rchat-consent { border: 1px solid var(--border); border-radius: var(--r-btn); padding: 12px; display: flex; flex-direction: column; gap: 8px; }
  .rchat-consent p { margin: 0; font-size: 12.5px; color: var(--muted); line-height: 1.5; }
  .rchat-agent { display: flex; flex-direction: column; gap: 10px; }
  .rchat-activity { display: flex; flex-direction: column; gap: 4px; }
  .rchat-act { display: flex; align-items: center; gap: 7px; font-size: 11.5px; color: var(--faint); }
  .rchat-stream { white-space: pre-wrap; }
  .rchat-approval { border: 1px solid var(--accent-line); background: var(--accent-soft); border-radius: var(--r-btn); padding: 10px; display: flex; flex-direction: column; gap: 8px; }
  .rchat-approval-head { font-size: 13px; color: var(--text); }
  .rchat-approval-body { margin: 0; max-height: 240px; overflow: auto; font-family: var(--font-mono); font-size: 12px; background: var(--raised); border-radius: 4px; padding: 8px; white-space: pre-wrap; }
  .rchat-approval-btns { display: flex; gap: 8px; }
  .rchat-reject { border: 1px solid var(--err-line); color: var(--err); background: transparent; }
  .rchat-cost { font-size: 11px; color: var(--faint); }
  .mono { font-family: var(--font-mono); }
```

- [ ] **Step 5: Regenerate Wails bindings + build to verify**

Run: `wails generate module && cd frontend && npm run build`
Expected: `wails generate module` writes the new `AgentAsk`/`ApproveAction`/`CancelAgent`/`AgentAvailable`/`AgentConsent`/`GiveAgentConsent` bindings into `frontend/wailsjs/go/main/App.*`; `npm run build` type-checks and bundles with no errors.

- [ ] **Step 6: Full app build**

Run: `wails build`
Expected: build succeeds (Go + frontend compile; no unresolved bindings or Svelte errors).

- [ ] **Step 7: Commit**

```bash
git add frontend/src/lib/RepoChat.svelte frontend/wailsjs/
git commit -m "feat(ui): RepoChat 에이전트 모드 - 실시간 활동/승인 카드/비용/취소/동의 안내"
```

---

## Manual validation (post-implementation, requires the real `claude` CLI logged in)

These cannot be unit-tested (they need the live CLI + login), matching the spec's testing strategy:

- [ ] Build `cmd/fleet-hook` next to the app binary so `agentHookBinary()` resolves the sibling: `go build -o <appdir>/fleet-hook ./cmd/fleet-hook` (add `.exe` on Windows).
- [ ] On a Claude provider, open a code repo, accept consent, ask a question - confirm live `agent:activity` (Read/Grep) appears and a grounded answer streams in.
- [ ] Ask for an edit - confirm an approval card shows the tool + input; Approve applies it, Reject blocks it and the agent adapts.
- [ ] Confirm a `Read(**/.env)` attempt is denied by policy; confirm per-run cost/usage shows on `agent:done`; confirm Cancel kills the run cleanly.
- [ ] With an old CLI (< v2.1) or a non-Claude provider - confirm `AgentAvailable()` returns false and the deep-dive falls back to the single-shot flow with the provider note.

---

## Self-Review

- **Spec coverage:** driving the CLI (Task 7), stream parse (Task 2), PreToolUse gate via loopback + hook (Tasks 5, 6, 8), run-scoped `--settings` (Task 7), context injection (Task 4), tool policy + secrets (Task 3), consent (Task 8/9), cost/usage + resume (Tasks 2, 8), cancellation (Tasks 7, 8), provider gating/fallback + **runtime version-detect** (Task 1 parse wired into `AgentAvailable` in Task 8; UI fallback in Task 9), capability floor (Task 1), tests incl. fake CLI stub + fake decision source + **driver+stream+gate end-to-end** (Tasks 5, 6, 7, 8). All spec sections map to a task.
- **No new dependency:** every new import is Go stdlib or an existing `internal/*` package (`store`, `winhide`); `go.mod` is untouched. `app.go`/`main.go` remain the only Wails importers.
- **Gate concurrency:** `Decide` sends non-blocking on a cap-1 buffered channel and never deletes; `Await` owns cleanup via `defer discard`. Delivery is ordering-independent (Decide-before-Await and Await-then-Decide both covered), double-Decide is a no-op false, timeout/cancel deny fail-safe, and the loopback HTTP handler blocks in `Await` until `ApproveAction`/timeout/cancel. Verified under `-race` including the driver+gate end-to-end run.
- **Placeholder scan:** no TODO/TBD/"add error handling"/"similar to Task N"; every code step carries complete code.
- **Type consistency:** `Event`/`EventKind`/`Parse` (+`Partial`) (Task 2) used verbatim in Tasks 7-8; `Policy.Flags` (Task 3) in Task 7; `Coordinator.Register/Decide/Await` (Task 5) in Task 8; `ActionRequest` json tags (`id`,`toolName`,`toolInput`,`cwd`,`sessionId`) match the Svelte reads in Task 9; `Options` fields (Task 7) match the `AgentAsk` construction (Task 8); `store.Get(id) (Record,bool)`, `store.Record`/`store.Task`, `wruntime.EventsEmit(ctx,name,data)`, and `a.cfgSnapshot().AIProvider` match the repo signatures; hook stdout/response shapes match between Task 6 and Task 8's `writeDecision`.

---

**Plan complete. Two execution options:**

**1. Subagent-Driven (recommended)** - dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** - execute tasks in this session with checkpoints.

Which approach?