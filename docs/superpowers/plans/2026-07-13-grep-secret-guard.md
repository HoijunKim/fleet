# Grep/Glob Secret Guard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Route the agent's `Grep`/`Glob` tools through the fail-closed approval hook so an explicit secret-path target denies; add an `allow` verdict so safe searches auto-run with no prompt.

**Architecture:** Add an `allow` verdict to the classifier and `Grep`/`Glob` cases (secret-path target -> deny, else allow); make the approval server honor `allow` by auto-approving without a GUI prompt; remove `Grep`/`Glob` from the CLI allow-list and add them to the `PreToolUse` hook matcher so they reach the classifier.

**Tech Stack:** Go 1.22 stdlib, `internal/agent`. No frontend, no new Wails binding, no `wailsjs` regen.

## Global Constraints

From `docs/superpowers/specs/2026-07-13-grep-secret-guard-design.md`:
- **No new runtime dependencies;** Go stdlib + existing `internal/agent`. No frontend/Wails change.
- **Fail-closed preserved and extended:** any `Grep`/`Glob` parse failure or ambiguity is `deny`; the nil-`classify` shim stays `gate` (never auto-allow without a real classifier).
- **`Grep.pattern` is content, never a path:** `Grep(pattern="secret")` / `Grep(pattern="password")` MUST allow. Only `Grep.path`/`Grep.glob` and `Glob.pattern`/`Glob.path` are checked against `secretPathRe`.
- **Behavior-preserving for Edit/Write/Bash:** existing classifier cases and push/secret/commit logic untouched.
- **Read stays allow-listed** (its secret globs already gate it).
- **Green gates:** `go build ./...`, `go vet ./...`, `go test ./...`; `wails build` compiles.

## Commit authorship (all tasks)
`git -c user.name=hoijun -c user.email=hoijun.kim00@gmail.com commit -m "..."` - NO Co-Authored-By/Claude trailer.

---

## Task 1: Classifier - `allow` verdict + Grep/Glob cases

**Files:**
- Modify: `internal/agent/classify.go`
- Modify: `internal/agent/classify_test.go`

**Interfaces:**
- Produces: `Verdict{Decision:"allow"}` (new third decision alongside `"gate"`/`"deny"`); `allow(reason string) Verdict`. `Classify` now returns `allow` for a safe read-scope `Grep`/`Glob`, `deny` for a secret-path target or parse failure.
- Consumes: existing `secretPathRe` (unchanged).

- [ ] **Step 1: Write the failing test.** Add to `internal/agent/classify_test.go`:

```go
func TestClassifyGrepGlob(t *testing.T) {
	// Secret-path targets deny; content patterns and normal scopes allow.
	cases := []struct {
		name, tool, input, want string
	}{
		{"grep secret path", "Grep", `{"pattern":"x","path":"repo/.env"}`, "deny"},
		{"grep secret glob", "Grep", `{"pattern":"x","glob":"**/*.key"}`, "deny"},
		{"grep secret path id_rsa", "Grep", `{"pattern":"x","path":".ssh/id_rsa"}`, "deny"},
		{"grep content pattern secret", "Grep", `{"pattern":"secret"}`, "allow"},
		{"grep content pattern password", "Grep", `{"pattern":"password"}`, "allow"},
		{"grep normal path", "Grep", `{"pattern":"TODO","path":"internal/agent"}`, "allow"},
		{"grep normal glob", "Grep", `{"pattern":"func","glob":"**/*.go"}`, "allow"},
		{"grep unparseable", "Grep", `{"pattern":123}`, "deny"},
		{"glob secret pattern", "Glob", `{"pattern":"**/id_rsa"}`, "deny"},
		{"glob secret pattern key", "Glob", `{"pattern":"**/*.pem"}`, "deny"},
		{"glob secret path", "Glob", `{"pattern":"**/*.go","path":".ssh"}`, "deny"},
		{"glob normal", "Glob", `{"pattern":"**/*.go"}`, "allow"},
		{"glob unparseable", "Glob", `{"pattern":123}`, "deny"},
	}
	for _, c := range cases {
		got := v(c.tool, c.input, "feat/x")
		if got.Decision != c.want {
			t.Fatalf("%s: got %q want %q (%+v)", c.name, got.Decision, c.want, got)
		}
	}
}
```

- [ ] **Step 2: Run it, verify it fails.** Run: `go test ./internal/agent/ -run TestClassifyGrepGlob`. Expected: FAIL (Grep/Glob hit `default: deny` today, so the `allow` rows fail).

- [ ] **Step 3: Implement.** In `internal/agent/classify.go`:

  (a) Update the `Verdict.Decision` doc comment and add the `allow` helper (next to `deny`):

```go
// Verdict is the classifier's decision for one gated tool call.
type Verdict struct {
	Decision string // "allow" | "gate" | "deny"
	Reason   string
	Category Category
	Severity Severity
	Summary  string
}
```

```go
func deny(reason string) Verdict  { return Verdict{Decision: "deny", Reason: reason} }
func allow(reason string) Verdict { return Verdict{Decision: "allow", Reason: reason} }
```

  (b) Update the `Classify` doc comment - it returns `allow` only for a safe read-scope Grep/Glob; everything else is still gate/deny, fail-closed:

```go
// Classify decides how a gated tool call is handled. It returns "allow" ONLY
// for a safe read-scope Grep/Glob (no secret-shaped path/glob target); callers
// treat "allow" as "run without asking", "gate" as "ask the user", and "deny"
// as "block". Any parse failure or ambiguity is deny (fail-closed).
```

  (c) Add `Grep` and `Glob` cases in the `Classify` switch, BEFORE `default:`:

```go
	case "Grep":
		// pattern is a CONTENT regex, not a path - never checked. Only path/glob
		// scope which files are searched, so only they can target a secret file.
		var p struct {
			Path string `json:"path"`
			Glob string `json:"glob"`
		}
		if json.Unmarshal(toolInput, &p) != nil {
			return deny("unreadable grep")
		}
		if secretPathRe.MatchString(p.Path) || secretPathRe.MatchString(p.Glob) {
			return deny("grep of a secret path is blocked")
		}
		return allow("search")
	case "Glob":
		// pattern IS a path glob here; both it and an optional path scope can
		// target a secret file.
		var p struct {
			Pattern string `json:"pattern"`
			Path    string `json:"path"`
		}
		if json.Unmarshal(toolInput, &p) != nil {
			return deny("unreadable glob")
		}
		if secretPathRe.MatchString(p.Pattern) || secretPathRe.MatchString(p.Path) {
			return deny("glob of a secret path is blocked")
		}
		return allow("search")
```

Note on the unparseable rows: `{"pattern":123}` fails to unmarshal into a `string` field, so both cases return `deny`. (`Grep`'s struct omits `pattern` entirely, but a numeric `pattern` still makes `json.Unmarshal` of the whole object error on the type mismatch for a declared field only - since `Grep`'s struct does not declare `pattern`, add `Pattern json.RawMessage` is NOT needed; instead the `Grep` unparseable case is covered by a malformed `path`/`glob`. To keep the `{"pattern":123}` Grep row meaningful, decode into a struct that WILL error on it: include `Pattern string` in the Grep struct too and simply do not use it.)

  Revised `Grep` struct to make the unparseable row deny deterministically:

```go
	case "Grep":
		var p struct {
			Pattern string `json:"pattern"` // decoded so a malformed pattern denies; not used for classification
			Path    string `json:"path"`
			Glob    string `json:"glob"`
		}
		if json.Unmarshal(toolInput, &p) != nil {
			return deny("unreadable grep")
		}
		if secretPathRe.MatchString(p.Path) || secretPathRe.MatchString(p.Glob) {
			return deny("grep of a secret path is blocked")
		}
		return allow("search")
```

- [ ] **Step 4: Run tests, verify pass.** Run: `go test ./internal/agent/ -run TestClassifyGrepGlob`. Expected: PASS. Then `go test ./internal/agent/` (whole package - the existing Edit/Write/Bash/push/secret tests must stay green; nothing routed through them changed).

- [ ] **Step 5: Verify + commit.** `go build ./... && go vet ./... && go test ./...` green. Then:

```bash
git add internal/agent/classify.go internal/agent/classify_test.go
git -c user.name=hoijun -c user.email=hoijun.kim00@gmail.com commit -m "feat(hardening): classifier allow verdict + Grep/Glob secret-path deny"
```

---

## Task 2: Wire the verdict + route Grep/Glob to the hook

**Files:**
- Modify: `internal/agent/approve.go`
- Modify: `internal/agent/approve_test.go`
- Modify: `internal/agent/policy.go`
- Modify: `internal/agent/policy_test.go`
- Modify: `internal/agent/driver.go`
- Modify: `internal/agent/driver_test.go`

**Interfaces:**
- Consumes: `Verdict{Decision:"allow"}` (Task 1).
- Produces: `handleApprove` auto-allows an `allow` verdict (no register/notify); `Grep`/`Glob` removed from `Policy.Allowed`; hook matcher `"Edit|Write|Bash|Grep|Glob"`.

- [ ] **Step 1: Write the failing test (auto-allow).** Add to `internal/agent/approve_test.go`:

```go
func TestApprovalServerAutoAllows(t *testing.T) {
	coord := NewCoordinator()
	fired := false
	classify := func(tool string, _ json.RawMessage, _ string) Verdict {
		if tool == "Grep" {
			return Verdict{Decision: "allow", Reason: "search"}
		}
		return Verdict{Decision: "gate", Category: CatEdit, Summary: "Edit x"}
	}
	s := NewApprovalServer(context.Background(), coord, time.Second, func(ActionRequest) { fired = true }, classify)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop(nil)

	body := `{"tool_name":"Grep","tool_input":{"pattern":"TODO"},"cwd":"/r"}`
	resp, err := http.Post(s.URL(), "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if out["approved"] != true {
		t.Fatalf("auto-allow should answer approved=true, got %v", out)
	}
	if fired {
		t.Fatal("onAction must NOT fire for an auto-allowed action")
	}
}
```

- [ ] **Step 2: Run it, verify it fails.** Run: `go test ./internal/agent/ -run TestApprovalServerAutoAllows`. Expected: FAIL (today an `allow` verdict falls through to register+await, so `onAction` fires and the request blocks until timeout -> `approved=false`).

- [ ] **Step 3: Implement the `allow` short-circuit.** In `internal/agent/approve.go` `handleApprove`, after the existing `deny` block and before `id := s.coord.Register()`:

```go
	v := s.classify(p.ToolName, p.ToolInput, p.Cwd)
	if v.Decision == "deny" {
		// Auto-deny: never register a pending decision or notify the GUI, so
		// the user is never asked to approve something already blocked.
		writeDecision(w, false, v.Reason)
		return
	}
	if v.Decision == "allow" {
		// Auto-allow: a safe read-scope search runs with no prompt - do not
		// register a pending decision or notify the GUI.
		writeDecision(w, true, v.Reason)
		return
	}
	id := s.coord.Register()
	// ... unchanged gate path
```

- [ ] **Step 4: Run tests, verify pass.** Run: `go test ./internal/agent/ -run 'TestApprovalServerAutoAllows|TestApprovalServerAutoDenies|TestApprovalServerAllow'`. Expected: PASS (auto-allow answers true without firing onAction; auto-deny and the human-gate path still behave).

- [ ] **Step 5: Remove Grep/Glob from the allow-list.** In `internal/agent/policy.go` `DefaultPolicy().Allowed`, change:

```go
			"Read", "Grep", "Glob",
```
to:
```go
			"Read",
```

(Leaving `"Read"` and the read-only git Bash entries. Grep/Glob now fall through to the hook.)

- [ ] **Step 6: Update the policy test.** In `internal/agent/policy_test.go`, the assertion at ~line 19 requires Grep/Glob in `Allowed`; invert it to require Read allow-listed and Grep/Glob NOT:

```go
	if !has(p.Allowed, "Read") {
		t.Errorf("Read must be allowed: %+v", p.Allowed)
	}
	if has(p.Allowed, "Grep") || has(p.Allowed, "Glob") {
		t.Errorf("Grep/Glob must NOT be allow-listed (they route through the approval hook): %+v", p.Allowed)
	}
```

- [ ] **Step 7: Add Grep/Glob to the hook matcher.** In `internal/agent/driver.go` `WriteHookSettings`, change:

```go
					"matcher": "Edit|Write|Bash",
```
to:
```go
					"matcher": "Edit|Write|Bash|Grep|Glob",
```

- [ ] **Step 8: Update the driver test.** In `internal/agent/driver_test.go` `TestWriteHookSettings`, update the expected matcher substring `"Edit|Write|Bash"` to `"Edit|Write|Bash|Grep|Glob"` (the `for _, want := range []string{...}` list).

- [ ] **Step 9: Verify + commit.** `go build ./... && go vet ./... && go test ./...` green. Then `wails build` (from repo root) compiles (no frontend change, but the binary must build). Then:

```bash
git add internal/agent/approve.go internal/agent/approve_test.go internal/agent/policy.go internal/agent/policy_test.go internal/agent/driver.go internal/agent/driver_test.go
git -c user.name=hoijun -c user.email=hoijun.kim00@gmail.com commit -m "feat(hardening): route Grep/Glob through the approval hook, auto-allow safe searches"
```

---

## Self-Review Notes (author)

- **Spec coverage:** W1 (allow verdict + Grep/Glob cases) -> Task 1; W2 (approve auto-allow + policy + matcher) -> Task 2. Every File-Structure entry appears in a task, including the two existing tests that MUST be updated (policy_test ~L19, driver_test matcher list) or the suite fails.
- **Type consistency:** `allow(reason)` / `Verdict{Decision:"allow"}` produced in Task 1 is consumed by `handleApprove` in Task 2. `secretPathRe` is reused, not redefined.
- **Fail-closed:** unparseable Grep/Glob -> deny; nil classify shim -> gate (untouched); only an explicit `allow` verdict auto-approves. `Grep.pattern` is deliberately never matched against `secretPathRe` (content, not path) - covered by the `pattern="secret"`/`pattern="password"` allow rows.
- **Residual documented:** a path-less content `Grep` still allows (can surface secret values) - stated in the spec threat model; not hook-preventable, out of scope.
