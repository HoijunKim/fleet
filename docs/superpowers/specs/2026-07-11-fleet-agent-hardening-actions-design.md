# Fleet Agent Hardening + Gated Actions - Design Spec (Intel/Hardening Slice 1)

**Date:** 2026-07-11
**Status:** Approved for planning
**Topic:** Deepen and harden the agentic deep-dive: let the agent propose commit/push/PR actions (each user-approved, default-branch push always blocked), and turn the approval moment into a first-class, trustworthy review UI - a readable diff, a plain-language summary of what will happen, and a severity badge - so a developer can approve confidently and fast.

## Goal

Make the agentic deep-dive both **more capable** (it can carry a change all the way to a pull request, not just propose an edit) and **more trustworthy** (every mutating action is classified server-side, dangerous ones are auto-blocked, and the rest are shown to the user as a clear, readable review rather than raw JSON). The bar is craft: the approval card should feel like a great code-review moment, not a permission dialog.

## Context

- The agentic run is driven by the local `claude` CLI (`internal/agent/driver.go`), gated by fleet's own `PreToolUse` hook. The hook (`internal/agent/hook.go` `RunHook`) POSTs each tool call to fleet's loopback `ApprovalServer` (`internal/agent/approve.go`), which registers a pending decision, hands the action to the GUI via `onAction(ActionRequest)`, blocks on the `Coordinator` until the user decides, and answers `{approved, reason}`.
- **Current policy** (`internal/agent/policy.go` `DefaultPolicy`): `Allowed` = `Read, Grep, Glob` + 5 read-only git Bash patterns; `Disallowed` = secret Reads + `Bash(rm:*)`, `Bash(git push:*)`, `Bash(sudo:*)`, `Bash(curl:*)`. Because `Edit`/`Write`/general `Bash` are in neither list and the hook matcher is `Edit|Write|Bash`, they already fall through to the approval hook - so **`git commit`, `git add`, `git checkout -b`, and `gh pr create` are already gated (approvable) today; only `git push` is hard-denied.**
- **Current approval UI** (`frontend/src/lib/AgentOverlay.svelte`): the approval card shows `Approve <toolName>?` + the raw `toolInput` JSON in a `<pre>` + Approve/Reject. It is functional but not a review - an `Edit`'s old/new strings are shown as escaped JSON.
- The `ActionRequest` (`approve.go`) carries `ID, ToolName, ToolInput, Cwd, SessionID`. The `agent:action` event forwards `id, toolName, toolInput` to the overlay.
- The agentic run already streams `agent:activity` events (tool calls) into the overlay's activity feed.
- Working-tree line-ending churn (`frontend/wailsjs/go/main/App.{d.ts,js}`, `go.mod` show as modified with an empty content diff) is a pure LF↔CRLF artifact from `wails build`; no real change.

## Global Constraints

Copy verbatim into the plan's Global Constraints; every task inherits them.

- **No new runtime dependencies.** Go stays stdlib-only for the agent package (matches the existing `internal/agent` constraint). Frontend adds no packages; the diff renderer is hand-written; motion uses the existing `motion.ts` helpers.
- **The gate must not be weakened.** Fail-safe stays fail-safe: any classify/parse error, timeout, cancel, or ambiguity resolves to **deny**, never allow. Moving `git push` out of `--disallowedTools` is only safe because the server-side classifier now blocks default-branch pushes and fails closed - the plan must prove this with tests.
- **Default-branch push is always blocked**, in every form (`git push origin main`, `git push origin HEAD:main`, a bare `git push` while the current branch is `main`/`master`, `--force`, refspec variants). If the classifier cannot determine a push's target is a non-default branch, it **denies** (fail-closed).
- **`prefers-reduced-motion` honored** for any motion added to the approval card / activity feed (route through `motion.ts`).
- **Craft bar:** the approval card renders a human review, not raw JSON - a readable diff for `Edit`/`Write`, a plain-language one-line summary for every action, and a severity badge. Copy is developer-facing and specific.
- **No regression to existing agent behavior:** consent gate, project-scoping, single-run, the existing read-only allow-list, and the fail-safe approval all keep working; existing `internal/agent` and `app` tests stay green.
- **Green gates each task:** `go build ./...` + `go vet ./...` clean, `go test ./...` green, `npx svelte-check` 0 errors, `npx vitest run` green, and (for tasks touching the frontend) `wails build` succeeds.

## Workstream 1 - Action classifier (server-side, pure, tested)

**New `internal/agent/classify.go`** - a pure, unit-tested function that is the single policy brain for gated actions:

```
type Category string // "edit" | "shell" | "remote"
type Severity string // "low" | "medium" | "high"

type ClassifyContext struct {
    CurrentBranch    string   // the repo's current branch (fleet already knows this)
    ProtectedBranches []string // default = ["main", "master"]
}

type Verdict struct {
    Decision string   // "gate" | "deny"
    Reason   string   // shown when denied
    Category Category
    Severity Severity
    Summary  string   // plain-language: "Push branch feat/x to origin", "Edit README.md", "Commit: fix typo"
}

func Classify(toolName string, toolInput json.RawMessage, ctx ClassifyContext) Verdict
```

Rules:
- `Edit` / `Write` → `gate`, category `edit`, severity `low` (Write that creates/overwrites → `medium`), summary `Edit <file>` / `Create <file>` / `Overwrite <file>`.
- `Bash` with `git push …` → parse the push target using `ctx.CurrentBranch`; if it resolves to a protected branch, or the target cannot be determined → `deny` (reason: "push to the default branch is blocked"); otherwise `gate`, category `remote`, severity `high`, summary `Push branch <b> to <remote>`.
- `Bash` with `git commit …` → `gate`, category `shell`, severity `medium`, summary `Commit: <message>` (parse `-m`).
- `Bash` with `gh pr create …` (or `git request-pull`) → `gate`, category `remote`, severity `medium`, summary `Open a pull request`.
- `Bash` that reads a secret file (`cat|less|more|head|tail|xxd|base64|strings <secret-path>` where the path matches the secret globs) → `deny` (best-effort shell secret-read guard).
- Any other `Bash` → `gate`, category `shell`, severity `medium`, summary `Run: <cmd, truncated to ~80 chars>`.
- Unknown tool / unparseable input → `gate` with a generic summary if it is a known gated tool, else `deny`. Never silently allow.

The push-target parser is the security-critical unit; the plan gives it its own exhaustive test table (all the bypass forms above resolve to deny).

## Workstream 2 - Approval server uses the classifier; ActionRequest carries the review

**Modify `internal/agent/approve.go`:**
- `handleApprove` calls `Classify(p.ToolName, p.ToolInput, ctx)`. If `Decision == "deny"` → `writeDecision(w, false, verdict.Reason)` immediately, **without** `Register`/`onAction`/`Await` (the user is never asked to approve something that is auto-blocked; it just gets denied and the agent is told why).
- If `Decision == "gate"` → register + `onAction` as today, but the `ActionRequest` now carries `Category`, `Severity`, `Summary` (new fields).
- The `ClassifyContext` (current branch, protected branches) is supplied by the caller that constructs the `ApprovalServer` (`app.go`), from the project's known git branch; `ProtectedBranches` defaults to `["main","master"]`.

**Modify `internal/agent/policy.go`:** remove `Bash(git push:*)` from `Disallowed` so pushes reach the hook/classifier. Keep `Bash(rm:*)`, `Bash(sudo:*)`, `Bash(curl:*)` denied (hard CLI block - they never need to reach the classifier). Expand the secret Read-deny globs (add `**/*token*`, `**/*.p12`, `**/*.pfx`, `**/.netrc`, `**/*.keystore`, `**/*.ovpn`).

**Modify `app.go`:** the `agent:action` event payload gains `category`, `severity`, `summary` (passed through from `ActionRequest`). No other binding/event changes.

## Workstream 3 - Approval card as a real review (frontend craft)

**Modify `frontend/src/lib/AgentOverlay.svelte`** - replace the raw-JSON approval card with a categorized review:

- **Header:** a severity-colored category badge (`edit` = accent/blue, `shell` = amber, `remote` = purple/red) + the `summary` line (e.g. "Push branch feat/x to origin", "Edit README.md").
- **Body by category:**
  - `edit` (`Edit`): a readable **diff** - split `old_string`/`new_string` into lines, render removed lines (`-`, red tint) then added lines (`+`, green tint) in a monospace block with `overflow-x: auto`. Show the target `file_path` as a header.
  - `edit` (`Write`): the `file_path` + a content **preview** (first ~20 lines, monospace, scroll).
  - `shell` / `remote` (`Bash`): the command in a monospace block, plus (for `remote`) the resolved target ("→ origin/feat/x") so the user sees exactly what leaves the machine.
  - Unknown shape → fall back to the raw JSON `<pre>` (never crash).
- **Actions:** Approve / Reject as today (`decide(true|false)`), with the icons from `Icon.svelte` (`check`/`x`). The card animates in via `fadeScaleIn()` (already used) - keep it, tuned so the review draws the eye.
- A tiny diff renderer lives in a testable helper (`frontend/src/lib/agentAction.ts` - `parseAction(category, toolName, toolInput)` → a normalized shape the card renders), unit-tested with vitest.

## Workstream 4 - Close the loop: action outcome in the activity feed

**Modify `frontend/src/lib/agentSession.ts` + `AgentOverlay.svelte`:** when the user decides, append a compact line to the activity feed so the transcript reads as a story:
- Approve → `✓ approved: <summary>` (accent).
- Reject → `⨯ rejected: <summary>` (muted).
(Use the `Icon` set, not literal glyphs, honoring the branch's ASCII-punctuation direction.) This requires the overlay to know the current pending action's `summary` at decide-time - it already holds `$pending`; extend `pending` to carry `category`/`severity`/`summary` from the `agent:action` event. No backend round-trip; purely reflecting the decision the user just made.

## Workstream 5 - Cleanup (fold the accepted debt)

- **`.gitattributes`** at repo root normalizing line endings so `wails`-touched generated files (`frontend/wailsjs/**`, `go.mod`, `*.ts`, `*.svelte`, `*.go`) stop showing as phantom-modified (LF for text; mark generated wailsjs as `-text` or `eol=lf`). Verify `git status` is clean afterward on a fresh checkout.
- **Deferred GUI Minors** (safe, from the gui-polish final review): unify the ellipsis usage (`...` vs `…`) toward ASCII `...` to match the new ASCII-punctuation direction; add the lost `/* Command-palette key hint */` comment above `.ic-jump`; add a `__reset()` to `agentSession.ts` for test isolation; remove the redundant leftover `on:keydown` on the overlay backdrop div (Escape is handled at `onOverlayKey`).

## Data Flow

Unchanged transport; richer payload. Tool call → hook → `ApprovalServer.handleApprove` → `Classify` → (deny: immediate `{approved:false}`) or (gate: `onAction(ActionRequest{…category,severity,summary})` → `agent:action` event → overlay card → `decide` → `ApproveAction(id, approved)` → `Coordinator` → hook `{approved,reason}`). No new IPC, no new events (existing `agent:action` gains fields).

## Error Handling / Edge Cases

- Classifier parse failure / ambiguous push target → **deny** (fail-closed), with a clear reason surfaced to the agent and (as a rejected line) to the activity feed.
- Default-branch push in any form → denied, never shown as approvable.
- Malformed `toolInput` in the card → raw-JSON fallback, no crash.
- Reduced motion → card/feed motion collapses to instant.

## Testing Strategy

- `classify_test.go`: an exhaustive table - every `git push` bypass form (`origin main`, `HEAD:main`, bare push on `main`, `--force origin main`, refspec `+main`) → deny; feature-branch pushes → gate `remote`; `Edit`/`Write` → gate `edit` with correct summary; `git commit -m x` → gate `shell` summary `Commit: x`; `gh pr create` → gate `remote`; secret-read Bash → deny; unknown → deny. Fail-closed on empty/garbage input.
- `approve_test.go` (extend): a POST that classifies as deny returns `{approved:false}` **without** registering a pending action (no `onAction` fired); a gate POST fires `onAction` with the populated `Category/Severity/Summary`.
- `policy_test.go` (extend): `git push` no longer in `Disallowed`; new secret globs present; `rm/sudo/curl` still denied.
- `agentAction.test.ts`: `parseAction` produces the right diff lines for an `Edit`, content preview for a `Write`, command+target for a push, and falls back safely on garbage.
- Existing `internal/agent`, `app`, and frontend suites stay green; `wails build` succeeds.

## Out of Scope (YAGNI / later slices)

- True secret containment (OS sandbox / result filtering) - Grep/Glob content remains best-effort within the consented boundary; documented honestly in the consent copy + a code comment. (Later hardening.)
- Cross-repo actions, GitHub/CI signals, deeper brief, cross-repo intel - later slices.
- No auto-approval, no "approve all", no policy UI - every mutating action stays individually approved.

## File Structure

- **Create:** `internal/agent/classify.go`, `internal/agent/classify_test.go`, `frontend/src/lib/agentAction.ts`, `frontend/src/lib/agentAction.test.ts`, `.gitattributes`.
- **Modify:** `internal/agent/approve.go` (classifier wiring + `ActionRequest` fields), `internal/agent/policy.go` (push out of disallow, secret globs), `internal/agent/approve_test.go`, `internal/agent/policy_test.go`, `app.go` (ClassifyContext from project branch; `agent:action` payload fields), `frontend/src/lib/AgentOverlay.svelte` (review card + outcome line), `frontend/src/lib/agentSession.ts` (pending carries category/severity/summary; `__reset()`), `frontend/src/app.css` (badge/diff styles; `.ic-jump` comment), plus the small ellipsis unification where the AI-facing/UI copy uses `…`.
