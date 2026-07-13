# Grep/Glob Secret Guard - Design Spec (Hardening Slice)

**Date:** 2026-07-13
**Status:** Approved for planning
**Topic:** Bring the agent's `Grep`/`Glob` tools under the approval hook so a call that explicitly targets a secret-shaped path (`Grep` over `.env`, `Glob` for `**/*.key`) is denied by the same fail-closed classifier that already gates Edit/Write/Bash - closing the gap where read-only search tools were allow-listed and thus escaped the gate entirely. Adds an `allow` verdict so safe searches auto-run with no user prompt.

## Goal

Today `Read`/`Grep`/`Glob` are allow-listed (`policy.go`), so `Grep`/`Glob` run WITHOUT hitting fleet's `PreToolUse` approval hook. `Read` of a secret is still blocked by the CLI's `Read(**/.env)` deny globs, but there is no equivalent path-deny for `Grep`/`Glob`: an agent can `Grep` with `path=<repo>/.env` or `Glob(**/*.key)` and surface secret contents/filenames with no gate. Now that fleet mode reads across ALL repos, that blast radius is fleet-wide. This slice routes `Grep`/`Glob` through the classifier: an explicit secret-path target denies; anything else auto-allows (no prompt, preserving today's frictionless search UX).

## Threat Model / Honest Scope (partial hardening)

This is a **partial** containment, by design, and the spec states the limit plainly:

- **What it closes:** `Grep`/`Glob` calls whose `path`/`glob` (Grep) or `pattern`/`path` (Glob) EXPLICITLY name a secret-shaped location. These now deny, matching the existing Bash secret-read deny and Read secret globs.
- **What it CANNOT close (documented residual):** a broad content `Grep` with no path (e.g. `Grep(pattern="password")`) still runs and can return secret VALUES from any file, because the hook decides from the call's arguments BEFORE the search runs - it never sees the results. The same residual covers a broad GLOB that only *incidentally* matches a family-secret through a non-distinctive tail - e.g. `Grep(glob="**/*.json")` reaching `credentials.json`, or `**/*.conf` reaching `db_secret.conf`: the guard denies globs that *structurally target* a secret (a secret extension via glob intersection, a distinctive secret name, or a `.ssh`/`.aws` dir), but a legitimate broad `*.json`/`*.conf` search is indistinguishable from an evasion without reading the filesystem, and denying it would break ordinary dev search while the attacker simply drops the glob and content-greps anyway. True containment of content-leak needs an OS sandbox or a redacted filesystem view; that is a separate, larger effort explicitly out of scope here. The agentic feature already sends approved file contents to Anthropic under the user's Claude login, so the proportionate posture is: deny obvious/structural secret targets + explicit consent + per-action approval (all of which this preserves).

- **Convergence:** the classifier's `scopeTargetsSecret`/`globsIntersect` was hardened across four adversarial review rounds (literal-substring bypass, Windows backslash, fixed-stem canonicals, brace-bomb fail-open, no-dot extension) and the final round verified `globsIntersect` correct against a brute-force `path.Match` oracle (millions of witness cases, zero fail-open). Every remaining "allowed-yet-matches" case is the broad-glob/content residual above, not a structural bypass.

## Context

- `internal/agent/classify.go` - `Classify(toolName, toolInput, ctx) Verdict` is the fail-closed brain. Today it handles `Edit`/`Write`/`Bash` and `default: deny("tool not permitted")`. `Verdict.Decision` is `"gate"|"deny"` and the doc comment says it NEVER returns allow. `secretPathRe` already matches a secret-shaped path substring (`.env`, `id_rsa`, `.pem`, `.key`, `.p12`, `.pfx`, `.netrc`, `credentials`, `secret`, `.ssh/`).
- `internal/agent/approve.go` - `handleApprove` calls `classify`; on `"deny"` it writes `approved=false` WITHOUT registering/notifying the GUI; otherwise it registers a pending decision, notifies the GUI (`onAction`), and blocks on `coord.Await` (a real human gate).
- `internal/agent/policy.go` - `DefaultPolicy().Allowed` includes `"Read", "Grep", "Glob"` (allow-listed = no hook). `Disallowed` has the `Read(**/.env)`-style secret globs + `rm`/`sudo`/`curl`.
- `internal/agent/driver.go` - `WriteHookSettings` registers the `PreToolUse` hook with `"matcher": "Edit|Write|Bash"`; only matched tools invoke the hook.
- The `Grep` tool input is `{pattern, path?, glob?, ...}` where `pattern` is a CONTENT regex (not a path) and `path`/`glob` scope WHICH files are searched. The `Glob` tool input is `{pattern, path?}` where `pattern` IS a path glob.

## Global Constraints

- **No new runtime dependencies.** Go stdlib + existing `internal/agent`. No frontend change, no new Wails binding, no `wailsjs` regen (the auto-allow is silent; nothing new is exposed to the GUI).
- **Fail-closed is preserved and extended.** Any parse failure or ambiguity for `Grep`/`Glob` is `deny`. The nil-`classify` fail-safe shim in `NewApprovalServer` stays `gate` (never auto-allow without a real classifier).
- **`Grep.pattern` is never treated as a path.** A legitimate content search like `Grep(pattern="secret")` or `Grep(pattern="password")` must AUTO-ALLOW (it is a content regex, not a secret file target); only `Grep.path`/`Grep.glob` are checked against `secretPathRe`. Denying content patterns would break normal code search and is explicitly wrong.
- **Behavior-preserving for Edit/Write/Bash.** The classifier's existing cases and the push/secret/commit logic are untouched; only new `Grep`/`Glob` cases and an `allow` verdict are added.
- **Read stays allow-listed.** Its secret-deny globs already work; do not move Read under the hook (that would add a hook round-trip to every file read for no gain).
- **Green gates:** `go build ./...`, `go vet ./...`, `go test ./...`; `wails build` still succeeds (no frontend touched, but the binary must compile).

## Workstream 1 - Classifier: `allow` verdict + Grep/Glob

- **Add an `allow` verdict.** `Verdict.Decision` gains `"allow"`; update the doc comment (it no longer "NEVER returns allow" - it returns allow ONLY for a safe read-scope `Grep`/`Glob`). Add a helper `func allow(reason string) Verdict { return Verdict{Decision: "allow", Reason: reason} }`.
- **`case "Grep"`:** unmarshal `{pattern, path, glob string}`. Parse failure -> `deny("unreadable grep")`. If `secretPathRe.MatchString(path)` OR `secretPathRe.MatchString(glob)` -> `deny("grep of a secret path is blocked")`. Else -> `allow("search")`. `pattern` is NOT checked.
- **`case "Glob"`:** unmarshal `{pattern, path string}`. Parse failure -> `deny("unreadable glob")`. If `secretPathRe.MatchString(pattern)` OR `secretPathRe.MatchString(path)` -> `deny("glob of a secret path is blocked")`. Else -> `allow("search")`.
- The `default: deny("tool not permitted")` case is unchanged - any OTHER tool routed to the hook still denies.

## Workstream 2 - Wire the verdict + route the tools

- **`approve.go` `handleApprove`:** after the `deny` short-circuit, add an `allow` short-circuit: `if v.Decision == "allow" { writeDecision(w, true, v.Reason); return }` - auto-allow WITHOUT `coord.Register()` or `onAction` (no GUI prompt, no pending decision). Only a `"gate"` verdict reaches the register/notify/await path. Order: deny first, then allow, then gate (all three are mutually exclusive; deny and allow are the non-interactive short-circuits).
- **`policy.go`:** remove `"Grep", "Glob"` from `Allowed` (leaving `"Read"` plus the read-only git Bash entries). Now that they are not allow-listed, an unmatched `Grep`/`Glob` falls through to the `PreToolUse` hook (same mechanism as Edit/Write). Do NOT add `Grep`/`Glob` secret globs to `Disallowed` (the CLI's per-tool path-deny semantics for search tools are not relied upon; the classifier is the mechanism).
- **`driver.go` `WriteHookSettings`:** change `"matcher": "Edit|Write|Bash"` to `"matcher": "Edit|Write|Bash|Grep|Glob"` so `Grep`/`Glob` invoke the hook.

## Data Flow

Agent issues `Grep`/`Glob` -> not allow-listed -> `PreToolUse` hook (matcher now includes Grep/Glob) -> fleet loopback `/approve` -> `classify` -> `Classify` Grep/Glob case: secret-path target -> `deny` (hook returns `approved=false`, blocked, no prompt); safe scope -> `allow` (hook returns `approved=true`, no prompt, runs immediately). Edit/Write/Bash unchanged (still `gate` -> human approval card). `Read` unchanged (allow-listed + secret globs).

## Error Handling / Edge Cases

- `Grep`/`Glob` with unparseable input -> `deny` (fail-closed).
- `Grep` with only a `pattern` (no path/glob) -> `allow` (documented residual: broad content search can still surface secret values; not hook-preventable).
- `Grep(path=".env")`, `Grep(glob="**/*.key")`, `Glob(pattern="**/id_rsa")`, `Glob(path=".ssh")` -> `deny`.
- `Grep(pattern="secret")` (content search for the word) -> `allow` (pattern is content, not a path).
- nil `classify` (fail-safe shim) -> `gate` for everything, including Grep/Glob (never auto-allows without a real classifier).

## Testing Strategy

- **`classify_test.go`:** a `TestClassifyGrepGlob` table - `Grep` with a secret `path` denies; `Grep` with a secret `glob` denies; `Grep` with only a content `pattern` (incl. `pattern="secret"`) allows; `Grep` with a normal `path` allows; `Glob` with a secret `pattern` denies; `Glob` with a normal `pattern` allows; unparseable `Grep`/`Glob` denies. Assert `Decision` is exactly `"allow"`/`"deny"` as expected. Mirror the existing `v(tool, input, cur)` helper.
- **`approve_test.go`:** a `TestApprovalServerAutoAllows` - a `classify` returning `allow` for `Grep` answers `approved=true` and `onAction` does NOT fire and NO pending decision is registered (mirror `TestApprovalServerAutoDeniesWithoutAsking`, asserting the allow side).
- **`policy_test.go`:** assert `Grep`/`Glob` are NOT in `Allowed` (and `Read` still is) if the existing policy test enumerates the allow-list; otherwise add a focused assertion.
- **`driver_test.go`:** if a test asserts the hook matcher string, update it to the new value; otherwise add a small assertion that `WriteHookSettings` output contains `Grep|Glob`.
- Existing suites stay green (the push/secret/commit classifier tests and the gate E2E are untouched by the new cases).

## Out of Scope (YAGNI)

- Broad content-grep containment (needs OS sandbox / redacted view) - documented residual.
- Moving `Read` under the hook (its secret globs already gate it; a hook round-trip per read is pure cost).
- A GUI surface for auto-allowed searches (they are silent by design; the activity feed already shows the agent's tool use via the stream).
- Per-tool `Disallowed` secret globs for Grep/Glob (classifier is the mechanism; unproven CLI semantics not relied upon).

## File Structure

- **Modify:** `internal/agent/classify.go` (`allow` helper + `Verdict.Decision` doc + `Grep`/`Glob` cases), `internal/agent/classify_test.go` (`TestClassifyGrepGlob`), `internal/agent/approve.go` (`allow` short-circuit in `handleApprove`), `internal/agent/approve_test.go` (`TestApprovalServerAutoAllows`), `internal/agent/policy.go` (drop `Grep`/`Glob` from `Allowed`), `internal/agent/policy_test.go` (allow-list assertion), `internal/agent/driver.go` (matcher adds `Grep|Glob`), `internal/agent/driver_test.go` (matcher assertion if present).
- **Create:** none.
