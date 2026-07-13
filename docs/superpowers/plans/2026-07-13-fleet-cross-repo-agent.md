# Fleet Cross-Repo Agent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** A fleet-wide agentic deep-dive - ask one question that reasons across all repos, run at the projects root, reusing the whole agent stack (gate, classifier, overlay).

**Architecture:** Extract the shared run pipeline from `AgentAsk` into `runAgent(...)`; add `AgentAskFleet(question)` that runs at `Roots[0]` with a fleet-wide system prompt; a `Today.svelte` fleet launcher opens the existing overlay in a fleet identity.

**Tech Stack:** Go 1.22 (stdlib), Wails v2, Svelte-TS, vitest, Go testing.

## Global Constraints

From the spec `docs/superpowers/specs/2026-07-13-fleet-cross-repo-agent-design.md`:
- **No new runtime dependencies**; Go stdlib + existing `internal/agent`; `frontend/package.json` unchanged.
- **The approval gate + classifier are UNCHANGED and still fail-closed** - fleet reuses them verbatim (every mutation approved, default-branch push denied, secret reads denied tree-wide).
- **Reuse, don't fork, the run pipeline** - extract `runAgent` shared by `AgentAsk` and `AgentAskFleet` (no copy-pasted goroutine/mutex/cleanup); the per-repo `AgentAsk` stays behavior-preserving (existing agentic tests green).
- **Single-run semantics preserved** (shared `agentMu`/`agentCancel`/`agentSrv`).
- **Consent copy explicit** about the fleet read scope.
- **Green gates:** `go build`/`go vet`/`go test ./...`, `npx svelte-check` 0 errors, `npx vitest run`, `wails build`.

## Commit authorship (all tasks)
`git -c user.name=hoijun -c user.email=hoijun.kim00@gmail.com commit -m "..."` - NO Co-Authored-By/Claude trailer.

---

## Task 1: Fleet run pipeline (backend)

**Files:**
- Modify: `internal/agent/prompt.go` (add `FleetProject` + `BuildFleetSystemPrompt`), `internal/agent/prompt_test.go`
- Modify: `app.go` (extract `runAgent`; add `AgentAskFleet` + store->FleetProject gather), `app_test.go`

**Interfaces:**
- Produces: `agent.FleetProject{Name, Status, Deadline string; OpenTasks int}`, `agent.BuildFleetSystemPrompt(projects []FleetProject) string`; `(a *App) runAgent(repoDir, systemPrompt, sessionKey, question string) string` (shared); `(a *App) AgentAskFleet(question string) string`.

- [ ] **Step 1: BuildFleetSystemPrompt (TDD).** Add to `internal/agent/prompt_test.go`:

```go
func TestBuildFleetSystemPrompt(t *testing.T) {
	got := BuildFleetSystemPrompt([]FleetProject{
		{Name: "fleet", Status: "active", Deadline: "2026-08-01", OpenTasks: 3},
		{Name: "arsi", Status: "paused"},
	})
	if !strings.Contains(got, "fleet") || !strings.Contains(got, "arsi") {
		t.Fatalf("projects not listed: %s", got)
	}
	// fleet-wide framing + the approval note must be present
	if !strings.Contains(got, "approve") && !strings.Contains(got, "approved") {
		t.Fatalf("missing approval framing: %s", got)
	}
	// empty list must not panic and still frame the role
	if BuildFleetSystemPrompt(nil) == "" {
		t.Fatal("empty fleet prompt should still frame the role")
	}
}
```

Run: `go test ./internal/agent/ -run TestBuildFleetSystemPrompt`. Expected: FAIL (undefined).

- [ ] **Step 2: Implement BuildFleetSystemPrompt.** In `internal/agent/prompt.go` (mirror `BuildSystemPrompt`'s style + the ASCII-punctuation + approval framing):

```go
// FleetProject is one project's PM summary for the fleet-wide system prompt.
type FleetProject struct {
	Name      string
	Status    string
	Deadline  string
	OpenTasks int
}

// BuildFleetSystemPrompt frames the agent as working across ALL of the user's
// projects under the run directory, and lists each project's PM state so the
// agent knows the fleet. Read tools span every repo under the directory; any
// edit, file write, or shell command is reviewed and approved by the user
// before it takes effect.
func BuildFleetSystemPrompt(projects []FleetProject) string {
	var b strings.Builder
	b.WriteString("You are fleet's code-aware assistant working across ALL of the user's projects ")
	b.WriteString("under this directory. You can read and grep any repo here to answer questions that ")
	b.WriteString("span projects. Propose concrete, file-grounded changes. Any edit, file write, or shell ")
	b.WriteString("command you run is reviewed and approved by the user before it takes effect. ")
	b.WriteString("Write in plain text with ASCII punctuation only: use a hyphen (-), not em or en dashes; ")
	b.WriteString("straight quotes; and no other special Unicode symbols.\n\n")
	b.WriteString("=== Projects (from fleet) ===\n")
	for _, p := range projects {
		fmt.Fprintf(&b, "- %s", p.Name)
		var bits []string
		if s := strings.TrimSpace(p.Status); s != "" {
			bits = append(bits, s)
		}
		if d := strings.TrimSpace(p.Deadline); d != "" {
			bits = append(bits, "deadline "+d)
		}
		if p.OpenTasks > 0 {
			bits = append(bits, fmt.Sprintf("%d open tasks", p.OpenTasks))
		}
		if len(bits) > 0 {
			fmt.Fprintf(&b, " (%s)", strings.Join(bits, ", "))
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}
```

Run: `go test ./internal/agent/ -run TestBuildFleetSystemPrompt`. Expected: PASS.

- [ ] **Step 3: Extract `runAgent` from `AgentAsk` (behavior-preserving).** Read `app.go` `AgentAsk` (~1205-1300) fully. Move the shared body - from the consent/available checks through the tmp-settings, ctx/cancel + single-run mutex, `NewApprovalServer` (with the classify closure), `srv.Start`, `opts` build, and the run goroutine (events + cleanup) - into:

```go
func (a *App) runAgent(repoDir, systemPrompt, sessionKey, question string) string { ... }
```

where the only differences from the current `AgentAsk` body are: `RepoDir` = the `repoDir` param, `SystemPrompt` = the `systemPrompt` param, and the session resume/store key = `sessionKey` (currently `projectID`). `AgentAsk` becomes:

```go
func (a *App) AgentAsk(projectID, question string) string {
	repoDir := projectID
	rec, _ := a.store.Get(projectID)
	name := rec.Name
	if name == "" {
		name = filepath.Base(projectID)
	}
	return a.runAgent(repoDir, agent.BuildSystemPrompt(name, rec), projectID, question)
}
```

Keep the consent/available checks inside `runAgent` (so both callers enforce them). Do NOT change any behavior for the per-repo path. Run the full `go test ./...` (esp. `app_test.go`'s agentic tests + `TestAgentAskRefusesWithoutConsent`) - they must stay green, proving the refactor is behavior-preserving.

- [ ] **Step 4: Add `AgentAskFleet` (TDD).** In `app_test.go`, add a test that `AgentAskFleet` returns an error when no roots are configured (the only branch testable without a live claude), mirroring how existing agentic tests set up the `App` + config:

```go
func TestAgentAskFleetNoRoots(t *testing.T) {
	// App with an empty Roots config
	got := app.AgentAskFleet("hi")
	if !strings.Contains(got, "root") {
		t.Fatalf("expected a no-root error, got %q", got)
	}
}
```

(Adapt the App/config setup to what `app_test.go` already uses; if consent/available gate it earlier, set those up as the existing agentic tests do so the root check is reached, or assert on whichever guard fires first for an unconfigured App - keep the test meaningful.)

Run: `go test ./ -run TestAgentAskFleetNoRoots`. Expected: FAIL (undefined).

- [ ] **Step 5: Implement `AgentAskFleet`.** In `app.go`:

```go
// AgentAskFleet starts a fleet-wide agentic deep-dive: the agent runs at the
// first configured project root and can read/grep across every repo under it,
// with each mutating action approved by the user. Returns "" on a clean start.
func (a *App) AgentAskFleet(question string) string {
	cfg := a.cfgSnapshot()
	if len(cfg.Roots) == 0 {
		return "error: no project root configured"
	}
	root := cfg.Roots[0]

	var fps []agent.FleetProject
	for _, r := range a.store.All() { // use the store's list method (check its name)
		open := 0
		for _, t := range r.Tasks {
			if t.Status != "done" {
				open++
			}
		}
		name := r.Name
		if name == "" {
			name = filepath.Base(r.Path) // adapt to the Record's path field
		}
		fps = append(fps, agent.FleetProject{Name: name, Status: r.Status, Deadline: r.Deadline, OpenTasks: open})
	}
	return a.runAgent(root, agent.BuildFleetSystemPrompt(fps), "__fleet__", question)
}
```

Adapt `a.store.All()` / the `Record` fields (`Name`/`Status`/`Deadline`/`Tasks`/path) to the real `internal/store` API - read `internal/store/store.go` for the exact list method and field names before writing. Run: `go test ./ -run TestAgentAskFleetNoRoots`. Expected: PASS.

- [ ] **Step 6: Verify + commit.** `go build ./... && go vet ./... && go test ./...` (green, incl. the untouched per-repo agentic tests). Then:

```bash
git add internal/agent/prompt.go internal/agent/prompt_test.go app.go app_test.go
git commit -m "feat(intel): AgentAskFleet - fleet-wide agentic deep-dive at the projects root (shared runAgent)"
```

---

## Task 2: Fleet launcher + overlay fleet mode (frontend)

**Files:**
- Modify: `frontend/src/lib/agentSession.ts` (fleet identity + dispatch), `frontend/src/lib/agentSession.test.ts`
- Modify: `frontend/src/lib/Today.svelte` (fleet launcher), `frontend/src/lib/AgentOverlay.svelte` (fleet header/consent copy)
- Modify (generated): `frontend/wailsjs/**` (regenerate for `AgentAskFleet`)

**Interfaces:**
- Consumes: `AgentAskFleet` (Task 1). Reuses `available`/`consent`/`overlayOpen` from the store.

- [ ] **Step 1: agentSession fleet dispatch (TDD).** Read the CURRENT `frontend/src/lib/agentSession.ts` (`setProject`, `ask`, `openOverlay`, the project shape). Add to `agentSession.test.ts` a test that `ask()` calls `AgentAskFleet` (not `AgentAsk`) when the current project is a fleet identity, and `AgentAsk` otherwise - extend the existing `vi.mock('../../wailsjs/go/main/App', ...)` to include `AgentAskFleet` and assert which was called.

```ts
it("routes ask to AgentAskFleet in fleet mode, AgentAsk otherwise", async () => {
  await S.initAgentSession();
  S.setProject({ path: "/repo/a", repoPath: "/repo/a" });
  await S.ask("q1");
  expect(calls).toContainEqual(["ask", "/repo/a", "q1"]); // AgentAsk
  S.setProject({ path: "__fleet__", name: "All projects", isFleet: true });
  await S.ask("q2");
  expect(calls).toContainEqual(["askFleet", "q2"]); // AgentAskFleet
});
```

(Match the mock's call-recording shape used by the existing tests.)

Run: `cd frontend && npx vitest run src/lib/agentSession.test.ts`. Expected: FAIL.

- [ ] **Step 2: Implement fleet dispatch + `openFleetOverlay`.** In `agentSession.ts`:
  - Import `AgentAskFleet` alongside `AgentAsk`.
  - The `Proj` type gains an optional `isFleet?: boolean`.
  - In `ask(q)`, when `project?.isFleet`, call `AgentAskFleet(text)`; else the existing `AgentAsk(project.repoPath || project.path, text)`. Everything else (running/turns/scoping) unchanged - the `"__fleet__"` path scopes like any other.
  - Add `export function openFleetOverlay(): void { openOverlay({ path: "__fleet__", name: "All projects", isFleet: true }); }`.

Run the test: PASS.

- [ ] **Step 3: Fleet launcher in Today.svelte.** Read the per-repo launcher pattern (`RepoChat.svelte`'s "Open agentic deep-dive" button gated on `$available`). In `Today.svelte`, import `{ available, consent, openFleetOverlay }` from `./agentSession` and `initAgentSession` (call in onMount if not already). Add a launcher affordance (a button, styled like the existing launchers) - shown when `$available` - labeled "Ask across all projects" that calls `openFleetOverlay()`. Place it sensibly in the Today layout (e.g. near the briefing header). If `!$consent`, the overlay's consent card handles it on open.

- [ ] **Step 4: Overlay fleet copy.** In `AgentOverlay.svelte`, when the current identity is fleet (the store's project `isFleet`, exposed via a store value or the `projectName === "All projects"`), the header title reads "All projects - agentic deep-dive" and the consent card copy says: it lets Claude Code read files across ALL your projects and send them to Anthropic under your Claude login, and can propose edits or commands (each one you approve here first). Keep ASCII punctuation. Non-fleet copy unchanged.

- [ ] **Step 5: Regenerate Wails bindings.** `wails generate module` (repo root) so `AgentAskFleet` appears in `frontend/wailsjs/go/main/App.{d.ts,js}`; confirm additive.

- [ ] **Step 6: Verify.** `cd frontend && npx vitest run && npx svelte-check` (green, 0 errors), then `cd .. && wails build`. Manual-smoke note in the report: the Today fleet launcher opens the overlay with the "All projects" header + fleet consent copy; asking a cross-repo question runs at the root; an edit still shows the approval card. (The live cross-repo run is manual-validation by design.)

- [ ] **Step 7: Commit.**

```bash
git add frontend/src/lib/agentSession.ts frontend/src/lib/agentSession.test.ts frontend/src/lib/Today.svelte frontend/src/lib/AgentOverlay.svelte frontend/wailsjs
git commit -m "feat(intel): fleet launcher - ask across all projects (agent runs at the root)"
```

---

## Self-Review Notes (author)

- **Spec coverage:** W1 -> Task 1; W2 -> Task 2. Every File-Structure entry appears in a task.
- **Type consistency:** `FleetProject{Name,Status,Deadline,OpenTasks}` (Task 1) feeds `BuildFleetSystemPrompt`; `AgentAskFleet(question)` (Task 1) is consumed by `agentSession.ask` fleet branch (Task 2). The `"__fleet__"` scoping key is used in both the store dispatch and the session resume.
- **Behavior-preserving refactor:** `runAgent` extraction is verified by the existing per-repo agentic tests staying green (Task 1 Step 3); the gate/classifier/consent are untouched (reused verbatim).
- **Reviewer heads-up:** Task 1 Step 5 adapts to the real `internal/store` list method + `Record` fields; the implementer must read `store.go` and use the actual names, not the placeholder `store.All()`/field guesses.
