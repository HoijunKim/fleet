# Fleet Cross-Repo Agent - Design Spec (Intel Slice 3)

**Date:** 2026-07-13
**Status:** Approved for planning
**Topic:** A fleet-wide agentic deep-dive: ask one question that reasons across ALL your repos at once (not just one selected project). The agent runs at the projects-root directory so it can read/grep across every repo, with fleet-wide project context, the same human-approval gate, and the same tree-wide secret policy.

## Goal

Today the agentic deep-dive (`AgentOverlay`) runs on a single selected repo (`cmd.Dir` = that repo). This slice adds a fleet mode: the agent runs at the projects root so a question like "which repos have failing tests and what's the common cause?" or "find every place I read a JWT across my projects" can span all repos. It reuses the whole agent stack (driver, approval gate, classifier, overlay) - only the working directory, the system prompt, and the launcher change.

## Context

- `AgentAsk(projectID, question)` (app.go:1205) sets `repoDir = projectID` (the repo path), builds a per-project `agent.BuildSystemPrompt(name, rec)`, spins up the loopback `ApprovalServer` with the classify closure, and runs the `claude` driver in a goroutine (single-run mutex `agentMu`/`agentCancel`/`agentSrv`, cleanup on exit). `cmd.Dir = o.RepoDir` (driver.go:121).
- `cfg.Roots []string` are the scan roots (typically ONE, e.g. `C:/Users/hoijun/Projects`, with repos directly under it). `scan.Discover` enumerates repos under the roots.
- The classifier resolves the current branch from each tool call's `cwd` (so `git -C <repo>` works); the secret-deny globs (`Read(**/.env)` etc.) are relative to the run's `cwd`, so at the root they match `<any-repo>/.env` - the secret policy already applies tree-wide.
- The frontend agentic run lives in `agentSession.ts` (project-scoped via `agentStale`) + `AgentOverlay.svelte`; `RepoChat.svelte` launches the per-repo overlay. `Today.svelte` is the fleet-wide hub (the brief).

## Global Constraints

- **No new runtime dependencies.** Go stdlib + existing `internal/agent`; `frontend/package.json` unchanged.
- **The approval gate and classifier are unchanged and still fail-closed** - fleet mode reuses them verbatim; every mutating action across any repo is still user-approved, default-branch push still denied, secret reads still denied (now tree-wide by the same globs).
- **Reuse, don't fork, the run pipeline.** Extract the shared setup/run logic from `AgentAsk` so `AgentAsk` (per-repo) and `AgentAskFleet` (fleet) share it - do not copy-paste the goroutine/mutex/cleanup.
- **Single-run semantics preserved:** a fleet run and a per-repo run use the same single-run guard (`agentMu`/`agentCancel`/`agentSrv`); starting one supersedes the other.
- **Consent is explicit about scope:** fleet mode reads across ALL projects; the consent/overlay copy must say so plainly (the per-action gate + secret policy are the real protections, but the user must understand the read scope).
- **Green gates:** `go build`/`go vet`/`go test ./...`, `npx svelte-check` 0 errors, `npx vitest run`, `wails build`.

## Workstream 1 - Fleet run (backend)

- **New `agent.BuildFleetSystemPrompt(projects []FleetProject) string`** (in `internal/agent/prompt.go`) where `FleetProject{Name, Status, Deadline string; OpenTasks int}`. It frames the agent as fleet-wide ("you are working across all of the user's projects under this directory; read/grep any of them; propose changes that are reviewed and approved per action") + a compact per-project PM line (name, status, deadline, open task count) so the agent knows the fleet. Keeps the ASCII-punctuation and "each edit/command is approved" framing from `BuildSystemPrompt`.
- **Extract the shared run logic** from `AgentAsk` into `func (a *App) runAgent(repoDir, systemPrompt, sessionKey, question string) string` - the consent/available checks, tmp settings, ctx/cancel + single-run mutex, `ApprovalServer` (with the classify closure), `opts`, and the run goroutine (events + cleanup). `AgentAsk` becomes: resolve `repoDir`/`name`/`rec` -> `runAgent(repoDir, BuildSystemPrompt(name, rec), projectID, question)`. This must be behavior-preserving for the per-repo path (existing `app` agentic tests stay green).
- **New binding `AgentAskFleet(question string) string`:** requires `len(cfg.Roots) >= 1` (else `error: no project root configured`); uses `repoDir = cfg.Roots[0]`; builds the fleet prompt from the store's records (`a.store` -> `[]FleetProject`, projects with meaningful PM data or all code projects); `sessionKey = "__fleet__"`. Delegates to `runAgent`. Multi-root note: v1 runs at `Roots[0]` (reads limited to that tree); the fleet PM context still lists all projects. Documented limitation.
- Consent/available checks live in `runAgent` (shared), so fleet mode enforces the same consent gate.

## Workstream 2 - Fleet launcher + overlay fleet mode (frontend)

- **`agentSession.ts`:** support a fleet identity. Add `openFleetOverlay()` that calls `setProject({ path: "__fleet__", name: "All projects", isFleet: true })` and opens the overlay. The store's `ask(q)` dispatches to `AgentAskFleet(q)` when the current project `isFleet`, else the existing `AgentAsk(id, q)`. Project-scoping (`agentStale`) uses the `"__fleet__"` path like any other, so a fleet run is correctly scoped and cancelled on switch.
- **`Today.svelte`:** add a fleet launcher - an "Ask across all projects" affordance (mirroring the per-repo Ask-AI launcher's consent-gated button) that calls `openFleetOverlay()`. Only shown when the agent is available + consented (reuse `available`/`consent` from the store); if not consented, the overlay's consent card handles it.
- **`AgentOverlay.svelte`:** in fleet mode (`projectName === "All projects"` or an `isFleet` flag on the store), the header reads "All projects - agentic deep-dive" and the consent card copy says it reads across ALL your projects and sends them to Anthropic under your Claude login (each edit/command approved here first). Everything else (activity feed, approval card, thread, cost/cancel) is identical.
- Reuse the existing consent flag (the agentic feature is one grant); the fleet consent COPY makes the wider read scope explicit.
- Regenerate Wails bindings for `AgentAskFleet`.

## Data Flow

Today fleet launcher -> `openFleetOverlay()` -> store fleet identity + overlay open -> user asks -> `AgentAskFleet(q)` -> `runAgent(Roots[0], BuildFleetSystemPrompt(store records), "__fleet__", q)` -> `claude` at the root cwd, reading/grepping across repos, each mutating tool call through the same `ApprovalServer` + classifier -> `agent:*` events -> overlay. Per-repo `AgentAsk` unchanged (same `runAgent`).

## Error Handling / Edge Cases

- No roots configured -> `AgentAskFleet` returns an error; the launcher can surface it (or is hidden when no roots).
- Multi-root: runs at `Roots[0]`; reads limited to that tree (documented); fleet PM context still lists all.
- Single-run: a fleet run supersedes a per-repo run and vice-versa (shared mutex); project-switch / overlay close behave as today via the `"__fleet__"` scoping.
- Secret/gate: unchanged; secret globs match tree-wide, mutations approved per action, default-branch push denied.

## Testing Strategy

- Backend: `BuildFleetSystemPrompt` (role framing + per-project lines + empty/one/many projects) - a `prompt_test.go` table. `runAgent` extraction: the existing `AgentAsk` behavior tests must stay green (verifies the refactor is behavior-preserving); an `AgentAskFleet` test that a missing-root returns the error and (where testable without a live claude) that it targets `Roots[0]` with the fleet prompt. Follow the existing `internal/agent` prompt-test + `app_test.go` agentic-test patterns.
- Frontend: `agentSession` fleet dispatch - a test that `ask()` routes to `AgentAskFleet` when the project `isFleet`, else `AgentAsk` (mock the bindings as the existing `agentSession.test.ts` does). Overlay/launcher render assertions where the SSR pattern fits.
- Existing suites green; `wails build` succeeds. Manual: launch, open the fleet ask on Today, ask a cross-repo read question, confirm it reads multiple repos and answers; confirm an edit in any repo still shows the approval card.

## Out of Scope (YAGNI)

- Multi-root simultaneous reads (v1 = Roots[0]).
- A separate fleet-consent flag (reuse the agentic consent with fleet-aware copy).
- Cross-repo-specific tools (the agent uses the same Read/Grep/Glob/Bash across the tree).
- Persisting fleet chat separately per-root (reuse the `fleet.chat:__fleet__` key via the existing store persistence).

## File Structure

- **Modify:** `internal/agent/prompt.go` (`BuildFleetSystemPrompt` + `FleetProject`), `internal/agent/prompt_test.go`, `app.go` (extract `runAgent`; add `AgentAskFleet` + the store->FleetProject gather), `app_test.go`, `frontend/src/lib/agentSession.ts` (fleet identity + dispatch), `frontend/src/lib/Today.svelte` (fleet launcher), `frontend/src/lib/AgentOverlay.svelte` (fleet header/consent copy), regenerate `frontend/wailsjs/**`.
- **Create:** `frontend/src/lib/agentSession` fleet-dispatch test rows (in the existing `agentSession.test.ts`).
