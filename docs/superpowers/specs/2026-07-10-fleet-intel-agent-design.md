# fleet Intel Agent (drive the `claude` CLI agentically) - Design

> Slice 1 of "core product depth": upgrade fleet's per-repo AI deep-dive from a
> single-shot text call into a real **agentic session**, by driving the local
> `claude` CLI (Claude Code) in headless mode with its built-in tools, and
> gating every mutating action through fleet's own approval UI. Later slices
> (a fleet MCP server for cross-repo/PM tools, multi-repo briefing correlation,
> more gated actions, non-Claude agentic, managed AI) are out of scope.

## Goal and context

fleet's differentiator is its AI intel. Today the Claude path is single-shot:
`internal/ai.ClaudeRunner.Ask` shells out to `claude --print` with the prompt on
stdin and returns the text. The "code-aware drill" is faked by the frontend
(the AI's text is read, `ReadRepoFile`/`RepoGrep`/`RepoFiles` bindings are
called, results fed back manually). There is no real agent loop.

**Key architectural fact (verified):** fleet does NOT use an Anthropic API key -
it invokes the locally-installed `claude` CLI, which uses the user's existing
Claude Code login/subscription. The CLI is already a full agent harness with
built-in tools (Read/Grep/Glob/Edit/Write/Bash), the agent loop, permissions,
sessions, and hooks. So the right way to make fleet's intel agentic is to
**drive that CLI in headless agentic mode**, not to build our own loop against
the API SDK (which would require an API key fleet doesn't collect and reinvent
the harness). fleet's value-add is the fleet-specific context and the UI/gating
around the CLI.

## Direction decisions (settled during brainstorming)

- Focus: deepen the core product; first feature = intel deepening via a real
  agent.
- Architecture: **Approach A'** - drive the `claude` CLI agentically (headless,
  stream-json), gate actions with a `PreToolUse` hook that routes approvals to
  fleet's GUI. Rejected: **A** (official Anthropic Go SDK + user API key + hand-
  built loop/tools/gate) - requires an API key fleet doesn't collect (setup
  regression) and reinvents the CLI's harness; **C** (Managed Agents) - its
  hosted sandbox can't see local repos.
- Slice 1 = drive the CLI on one repo + inject that project's PM context +
  PreToolUse approval gate + live activity/cost UI. A fleet MCP server exposing
  cross-repo/PM tools is deferred to slice 2.

## CLI facts this design is built on (verified via claude-code-guide, v2.1+)

- Headless agentic run: `claude -p "<prompt>" --output-format stream-json
  --include-partial-messages` in the repo's working directory. Tools are enabled
  by default in `-p`. Multi-turn continuation via `--resume <session_id>` (and
  `--input-format stream-json`), `--max-turns` caps turns.
- stream-json is newline-delimited JSON events: a `system`/`init` event
  (carries `session_id`, `model`), content-block events (`content_block_delta`
  with `text_delta` for assistant text, and `tool_use` blocks with tool name +
  input), and a final `result` event carrying `result` text, `total_cost_usd`,
  and `usage`.
- Permission gating: a **`PreToolUse` hook** fires before each tool call. The
  hook is a command that receives the tool call as JSON on stdin
  (`tool_name`, `tool_input`, `session_id`, `cwd`) and returns a decision as JSON
  on stdout (exit 0):
  `{"hookSpecificOutput": {"hookEventName": "PreToolUse",
  "permissionDecision": "allow"|"deny", "permissionDecisionReason": "..."}}`.
  In headless `-p` mode there is no interactive prompt, so the hook must return a
  concrete `allow`/`deny` - which is exactly what fleet wants: the hook asks
  fleet's GUI and returns the user's answer. The hook must return within its
  configured `timeout`.
- Tool allow/deny: `--allowedTools` / `--disallowedTools` with argument patterns
  (`Read`, `Edit`, `Bash(git *)`, `Read(**/.env)`, `mcp__fleet__*`).
- Context injection: `--append-system-prompt "<text>"` for dynamic per-run
  instructions; `--mcp-config <file|json>` to attach a custom MCP server
  (slice 2). MCP tools are named `mcp__<server>__<tool>`.
- Auth/cost: the user's existing Claude Code login (no API key); same quota as
  interactive; `total_cost_usd` + `usage` are in the output for cost display.
- Cancellation: kill the process (no graceful interrupt flag) - reuse fleet's
  existing `winhide` + `WaitDelay` handling from `ClaudeRunner`.

## Architecture

```
fleet/
  internal/ai/        # existing single-shot Runner (briefing, OpenAI, Gemini) - unchanged
  internal/agent/     # NEW: the CLI driver
    driver.go         #   build the `claude` argv, spawn, stream stdout, kill/cancel
    stream.go         #   parse stream-json events -> typed activity/tool/result structs
    gate.go           #   approval coordinator: map a hook request to a pending approval, await GUI decision
    prompt.go         #   build the append-system-prompt (fleet role + this project's PM context)
    policy.go         #   allow/deny tool policy (gate mutators; keep away from secret files)
  cmd/fleet-hook/     # NEW: tiny PreToolUse hook helper (a fleet-shipped executable / fleet.exe subcommand)
                      #   reads tool JSON on stdin -> asks the running fleet app -> prints permissionDecision
  app.go              # NEW bindings: AgentAsk, ApproveAction, CancelAgent + Wails events
  frontend/src/lib/   # RepoChat.svelte upgraded: live activity view + approval cards + cost/cancel
```

- `internal/agent` is Wails-free; `app.go` adapts between it and Wails
  events/bindings. It depends only on stdlib + `internal/store`/`internal/config`
  (to build the PM context) and `os/exec` (to run the CLI). **No new third-party
  dependency** (unlike Approach A's SDK).

## Driving the CLI

- On a deep-dive ask, `internal/agent` spawns `claude -p "<user question>"
  --output-format stream-json --include-partial-messages
  --append-system-prompt "<fleet context>"
  --allowedTools "<read set>" --disallowedTools "<secret + destructive set>"
  --settings "<run-scoped settings file with the PreToolUse hook>"
  --max-turns <cap>` with the repo path as the process working directory. (The
  exact settings-injection flag - `--settings <file>` vs writing a temporary
  `.claude/settings.json` - is confirmed at plan time; see Open questions.)
- stdout is streamed line-by-line and parsed (`stream.go`). Each event maps to a
  Wails event fleet emits to the deep-dive UI:
  - assistant `text_delta` -> `agent:text` (streamed answer)
  - `tool_use` -> `agent:activity` (which tool, which file/args)
  - `result` -> `agent:done` (final text + `total_cost_usd` + `usage`)
  - `system`/`init` -> capture `session_id` for `--resume` on the next turn
- Cancellation: `CancelAgent` kills the process (context cancel + the existing
  `WaitDelay`/`winhide` handling), ending the run cleanly.

## Permission gating (fleet's approval UI)

- fleet writes a run-scoped settings file registering a `PreToolUse` hook with
  matcher `Edit|Write|Bash` (the mutating tools). The hook command is fleet's
  own helper (`cmd/fleet-hook`, shipped as / invoked as a fleet executable so
  there is **no dependency on bash/jq/curl** - important on Windows).
- Flow: the CLI is about to Edit/Write/Bash -> `PreToolUse` fires -> the fleet-
  hook helper reads the tool JSON on stdin and hands it to the running fleet app
  over a local IPC channel (a loopback endpoint fleet already knows how to run,
  or a named pipe) -> `app.go` emits `agent:action` with the tool, target path,
  and a rendered diff/command -> the deep-dive UI shows an **approval card**
  (Approve / Reject) -> the user decides -> `ApproveAction(id, approved)` returns
  the decision to the helper -> the helper prints
  `permissionDecision: "allow"` or `"deny"` (with a reason) -> the CLI proceeds
  or blocks and the agent adapts.
- The hook `timeout` is set generously (long enough for a human to approve); if
  it elapses the decision defaults to deny. Only one approval is outstanding at
  a time.
- Read-only tools (Read/Grep/Glob/git via Bash-read) are allow-listed and run
  without a prompt. Bash is gated (or narrowly allow-listed to read-only git
  commands) because it can mutate.

## Context injection (slice 1)

- `--append-system-prompt` carries: fleet's role framing ("you are fleet's
  code-aware assistant for this project"), and **this project's PM context** from
  the store - open tasks, deadlines, notes, recent status - so the agent's
  answers and proposals are grounded in what the user is actually tracking.
- Cross-repo context and fleet-specific tools (list other projects, query the
  portfolio) via a **fleet MCP server** are deferred to slice 2.

## Tool policy & secrets (honest change from the API approach)

Because the CLI's own tools read files and send contents to Claude, fleet does
NOT mediate or mask each read (unlike an in-process tool loop would). Therefore:

- **Keep the agent away from secrets by policy, not masking:** `--disallowedTools`
  denies reading obvious secret files (`Read(**/.env)`, `Read(**/*secret*)`,
  `Read(**/id_rsa)`, etc.) and denies destructive Bash (`Bash(rm *)`,
  `Bash(git push *)` unless approved).
- **Consent:** a one-time notice that the agentic deep-dive lets Claude Code read
  local files in this repo and send them to Anthropic under the user's Claude
  login, shown before first use.
- Everything mutating still passes the human approval gate above.

This is a cleaner trust boundary (fleet doesn't hold the data) but is an honest
change from the first draft's "fleet masks every read" - fleet can't, in the CLI
model, so it relies on allow/deny policy + the gate + consent.

## Provider handling

- Agentic deep-dive is **Claude-only** (it IS the `claude` CLI). If the user's
  configured provider is OpenAI/Gemini, the deep-dive keeps the existing single-
  shot `internal/ai` flow, and the UI notes that agentic deep-dive requires the
  Claude (Claude Code) provider.

## Cost & lifecycle

- `total_cost_usd` + `usage` from the `result` event are surfaced per run in the
  UI (cost is on the user's Claude subscription/quota, not a fleet bill).
- `--max-turns` caps the loop; sessions continue via `--resume <session_id>` for
  a multi-turn chat; cancel kills the process.

## Testing strategy

- `stream.go`: parse recorded stream-json fixtures (init/text_delta/tool_use/
  result) into typed events - table tests; malformed/partial lines don't crash.
- `gate.go`: the approval coordinator maps a hook request to a pending approval
  and returns the GUI decision; test allow, deny, timeout-defaults-to-deny, and
  cancel-while-pending, with a fake decision source (no real CLI).
- `policy.go`: allow/deny sets are correct (secret reads denied, mutators gated)
  - pure unit tests.
- `cmd/fleet-hook`: given tool JSON on stdin and a stubbed app response, it emits
  the correct `permissionDecision` JSON - unit test.
- `driver.go`: argv construction is correct (flags, cwd, settings path) - unit
  test; the actual spawn is exercised with a fake `claude` script (a shell/Go
  stub on PATH that emits canned stream-json) so the driver+stream+gate pipeline
  is tested end-to-end without the real CLI.
- Frontend: activity view + approval card render from event payloads (component
  test with mock events); `wails build` passes.
- The live `claude` CLI is validated manually (needs the CLI installed + logged
  in), like the OAuth leg of the backend spine.

## Open questions (confirm at plan time)

- Exact settings-injection for the run-scoped `PreToolUse` hook: a `--settings
  <file>` flag vs a temporary project `.claude/settings.json` (non-invasive
  preferred). Confirm the flag name/behavior against the installed CLI version.
- Max hook `timeout` (how long fleet may hold the CLI waiting for a human
  approval) and the CLI's behavior when it elapses.
- The local IPC between the fleet-hook helper and the running app (loopback
  endpoint vs named pipe) - pick the simplest robust option on Windows.
- Minimum supported `claude` CLI version (stream-json + PreToolUse JSON decision
  are v2.1+); detect and degrade to single-shot if older.

## Out of scope (later slices)

- Fleet MCP server exposing cross-repo/PM tools to the agent (richer context +
  fleet-native tools).
- Multi-repo briefing correlation.
- More gated actions surfaced specially (create_task, draft_pr, etc. - the CLI
  can already Edit/Bash; these are fleet-native conveniences).
- Agentic path for OpenAI/Gemini.
- Managed / metered AI (app-provided key) - not needed while the CLI uses the
  user's own Claude login; relevant only for a future cloud-side AI surface.

## Success criteria

- On a Claude (Claude Code) provider, the deep-dive runs a real agentic session:
  the agent explores the repo with the CLI's own tools and answers grounded in
  what it read, with live activity shown in fleet's UI.
- Any Edit/Write/Bash the agent attempts is paused and shown in a fleet approval
  card with an exact diff/command; approve applies it, reject blocks it and the
  agent adapts.
- Secret files are denied by policy; a one-time consent is shown; cancel stops
  the run cleanly; per-run cost/usage is displayed.
- Non-Claude providers keep the single-shot deep-dive with a clear note.
- `internal/agent` + `cmd/fleet-hook` tests (fake CLI / fake decision source)
  green; gofmt/vet clean; `wails build` passes.
