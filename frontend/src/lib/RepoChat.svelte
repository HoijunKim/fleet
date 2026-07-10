<script lang="ts">
  import {
    AskAI, RepoDiff, Log, RepoSymbols, CancelAI, ReadRepoFile, RepoGrep, RepoFiles,
    AgentAsk, ApproveAction, CancelAgent, AgentAvailable, AgentConsent, GiveAgentConsent,
  } from "../../wailsjs/go/main/App";
  import { EventsOn } from "../../wailsjs/runtime/runtime";
  import { onMount, onDestroy } from "svelte";
  import { renderBrief } from "./markdown";
  import { redactSecrets } from "./redact";
  import { daysUntil } from "./pm";
  import { toastError } from "./toasts";

  // The selected code repo. Chat resets when the path changes.
  export let project: any;

  type Turn = { role: "user" | "assistant" | "tool"; text: string };

  let turns: Turn[] = [];
  let question = "";
  let loading = false;
  let loadedPath = "";
  let genId = 0;

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
      const msg = String(e ?? "agent failed");
      turns = [...turns, { role: "assistant", text: "error: " + msg }];
      saveChat();
      toastError("Agentic deep-dive: " + msg);
      agentStream = "";
      pending = null;
      agentRunning = false;
    }));
  });

  onDestroy(() => { unsubs.forEach((u) => u()); });

  async function giveConsent() {
    const msg = await GiveAgentConsent();
    if (!msg) consent = true;
    else toastError(msg);
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
      toastError(err);
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

  function cancelAsk() {
    genId++;
    loading = false;
    CancelAI();
  }

  const STARTERS = [
    "What's off in this repo right now?",
    "What should I do next here?",
    "Summarize my uncommitted changes.",
  ];

  function langName(): string {
    const code =
      (typeof localStorage !== "undefined" && localStorage.getItem("fleet.briefLang")) || "ko";
    return { ko: "Korean", en: "English", ja: "Japanese", zh: "Chinese" }[code] || "Korean";
  }

  // Load this repo's saved conversation when the selection changes (per-repo
  // memory across sessions).
  $: if (project && project.path !== loadedPath) {
    loadedPath = project.path;
    question = "";
    loading = false; // any in-flight answer for the old repo is dropped
    turns = loadChat(project.path);
  }

  function chatKey(p: string): string {
    return "fleet.chat:" + p;
  }
  function loadChat(p: string): Turn[] {
    if (typeof localStorage === "undefined") return [];
    try {
      const raw = localStorage.getItem(chatKey(p));
      const parsed = raw ? JSON.parse(raw) : [];
      return Array.isArray(parsed) ? parsed : [];
    } catch {
      return [];
    }
  }
  function saveChat() {
    if (typeof localStorage === "undefined" || !loadedPath) return;
    try {
      localStorage.setItem(chatKey(loadedPath), JSON.stringify(turns.slice(-20)));
    } catch {
      /* quota or serialization - non-fatal */
    }
  }
  function clearChat() {
    turns = [];
    if (typeof localStorage !== "undefined" && loadedPath) localStorage.removeItem(chatKey(loadedPath));
  }

  // Gather real code context for THIS repo. Rebuilt on every question so a
  // follow-up reasons over the CURRENT diff, not a stale snapshot.
  async function buildContext(): Promise<string> {
    const p = project.path;
    const [diff, commits, syms] = await Promise.all([
      RepoDiff(p).catch(() => ""),
      Log(p, 10).catch(() => []),
      RepoSymbols(p).catch(() => null),
    ]);
    const lines: string[] = [];
    lines.push("Repo: " + (project.name || p));
    if (project.branch) lines.push("Branch: " + project.branch);
    const state: string[] = [];
    if (project.dirty) state.push((project.modified || "?") + " uncommitted");
    if (project.behind > 0) state.push(project.behind + " behind");
    if (project.ahead > 0) state.push(project.ahead + " unpushed");
    if (project.deadline) {
      const d = daysUntil(project.deadline);
      if (d !== null) state.push(d < 0 ? "deadline " + -d + "d overdue" : "deadline in " + d + "d");
    }
    if (project.taskCount > 0) state.push("tasks " + project.doneCount + "/" + project.taskCount);
    if (state.length) lines.push("State: " + state.join(", "));

    if (Array.isArray(commits) && commits.length) {
      lines.push("\nRecent commits:");
      for (const c of commits) lines.push("- " + (c.hash || "").slice(0, 7) + " " + c.message + " (" + c.author + ", " + c.when + ")");
    }
    if (syms) {
      const ex = (syms.goExported || []).slice(0, 40);
      if (ex.length) lines.push("\nExported Go symbols: " + ex.join(", "));
      const scr = syms.npmScripts || [];
      if (scr.length) lines.push("npm scripts: " + scr.join(", "));
    }
    if (diff) lines.push("\nUncommitted diff:\n```\n" + redactSecrets(diff) + "\n```");

    return lines.join("\n");
  }

  function buildPrompt(ctx: string, latest: string, toolLog: string): string {
    const history = turns
      .filter((t) => t.role === "user" || t.role === "assistant")
      .map((t) => (t.role === "user" ? "Q: " : "A: ") + t.text)
      .join("\n\n");
    return (
      "You are a senior engineer pair-working on ONE repository. Reference actual files, commits and " +
      "diff lines; be direct and specific, no filler. Answer in " + langName() + " (keep code identifiers as-is).\n\n" +
      "You can INSPECT the repo. To use a tool, reply with EXACTLY one line and nothing else:\n" +
      "TOOL read <repo-relative-path>   (read a file)\n" +
      "TOOL grep <pattern>              (search the code)\n" +
      "TOOL list <dir>                  (list tracked files; empty dir = repo root)\n" +
      "I will reply with the result and you continue. Use tools when the diff/commits are not enough " +
      "(e.g. to see a function's body). When you have enough, give your FINAL answer with no TOOL line.\n\n" +
      "=== Repository context ===\n" + ctx +
      (history ? "\n\n=== Conversation so far ===\n" + history : "") +
      (toolLog ? "\n\n=== Tool results ===\n" + toolLog : "") +
      "\n\n=== Question ===\n" + latest
    );
  }

  type Tool = { kind: string; arg: string };
  function parseTool(res: string): Tool | null {
    const m = res.match(/^\s*TOOL\s+(read|grep|list)\b[ \t]*(.*)$/im);
    if (!m) return null;
    return { kind: m[1].toLowerCase(), arg: (m[2] || "").trim() };
  }
  async function runTool(t: Tool): Promise<string> {
    let out = "";
    if (t.kind === "read") out = await ReadRepoFile(project.path, t.arg);
    else if (t.kind === "grep") out = await RepoGrep(project.path, t.arg);
    else if (t.kind === "list") out = await RepoFiles(project.path, t.arg);
    return redactSecrets(out || "");
  }

  const MAX_TOOL_STEPS = 6;

  async function ask(q: string) {
    const text = q.trim();
    if (!text || loading) return;
    const p = project.path; // capture: drop the answer if the repo changes
    const g = ++genId; // or if the user cancels
    question = "";
    turns = [...turns, { role: "user", text }];
    loading = true;
    const stale = () => p !== project.path || g !== genId;
    try {
      const ctx = await buildContext();
      if (stale()) return;
      // Agentic loop: the model may inspect the repo (read/grep/list) before
      // answering, so "code-aware" holds for any language, not just the diff.
      let toolLog = "";
      let answer = "";
      for (let step = 0; step < MAX_TOOL_STEPS; step++) {
        const res = await AskAI(buildPrompt(ctx, text, toolLog));
        if (stale()) return;
        if (typeof res === "string" && res.startsWith("error:")) {
          answer = res;
          break;
        }
        const tool = parseTool(res || "");
        if (!tool) {
          answer = res || "(no answer)";
          break;
        }
        turns = [...turns, { role: "tool", text: tool.kind + " " + tool.arg }];
        const out = await runTool(tool);
        if (stale()) return;
        toolLog += "TOOL " + tool.kind + " " + tool.arg + " ->\n" + out + "\n\n";
        if (step === MAX_TOOL_STEPS - 1) answer = "(stopped after inspecting the repo; ask a narrower question)";
      }
      turns = [...turns, { role: "assistant", text: answer }];
      saveChat();
    } catch (e) {
      if (stale()) return;
      turns = [...turns, { role: "assistant", text: "error: " + String(e) }];
      saveChat();
    } finally {
      if (!stale()) loading = false;
    }
  }

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
</script>

<div class="rchat">
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

  {#if turns.length === 0}
    <div class="rchat-intro">
      <p class="rchat-hint">Ask about this repo - I read its recent commits, uncommitted diff, and exported symbols.</p>
      {#if !agentic}
        <p class="rchat-note">Agentic deep-dive (live activity, approvals, cost) needs the Claude (Claude Code) provider with the claude CLI installed - using single-shot mode for now.</p>
      {/if}
      <div class="rchat-starters">
        {#each STARTERS as s}
          <button class="rchat-starter" on:click={() => dispatch(s)} disabled={loading || agentRunning}>{s}</button>
        {/each}
      </div>
    </div>
  {:else}
    <div class="rchat-bar">
      <span class="rchat-saved">Saved for this repo</span>
      <button class="rchat-clear" on:click={clearChat} disabled={loading}>Clear</button>
    </div>
    <div class="rchat-thread">
      {#each turns as t}
        {#if t.role === "user"}
          <div class="rchat-q">{t.text}</div>
        {:else if t.role === "tool"}
          <div class="rchat-tool"><span class="rchat-tool-dot"></span>inspected <span class="mono">{t.text}</span></div>
        {:else}
          <div class="rchat-a" class:err={t.text.startsWith("error:")}>{@html renderBrief(t.text)}</div>
        {/if}
      {/each}
      {#if loading}
        <div class="rchat-loading">
          <span class="spinner"></span> reading the repo...
          <button class="rchat-clear" on:click={cancelAsk}>Cancel</button>
        </div>
      {/if}
    </div>
  {/if}

  <div class="rchat-input">
    <input
      class="input"
      type="text"
      placeholder="Ask about this repo..."
      bind:value={question}
      on:keydown={onKey}
      disabled={loading || agentRunning}
      aria-label="Ask about this repo"
    />
    <button class="btn btn-primary btn-sm" on:click={submit} disabled={(loading || agentRunning) || !question.trim()}>Ask</button>
  </div>
</div>

<style>
  .rchat { display: flex; flex-direction: column; gap: 12px; min-height: 0; }
  .rchat-bar { display: flex; align-items: center; justify-content: space-between; }
  .rchat-saved { font-size: 11px; color: var(--faint); }
  .rchat-clear {
    font: inherit;
    font-size: 11.5px;
    color: var(--muted);
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--r-pill);
    padding: 2px 10px;
    cursor: pointer;
    transition: color var(--t), border-color var(--t);
  }
  .rchat-clear:hover { color: var(--err); border-color: var(--err-line); }
  .rchat-hint { font-size: 13px; color: var(--muted); margin: 0 0 12px; line-height: 1.5; }
  .rchat-starters { display: flex; flex-direction: column; gap: 8px; }
  .rchat-starter {
    text-align: left;
    font: inherit;
    font-size: 13px;
    color: var(--text);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--r-btn);
    padding: 9px 12px;
    cursor: pointer;
    transition: border-color var(--t), background var(--t);
  }
  .rchat-starter:hover { border-color: var(--accent-line); background: var(--accent-soft); }
  .rchat-thread { display: flex; flex-direction: column; gap: 12px; }
  .rchat-q {
    align-self: flex-end;
    max-width: 85%;
    background: var(--accent-soft);
    border: 1px solid var(--accent-line);
    border-radius: 12px 12px 4px 12px;
    padding: 8px 12px;
    font-size: 13px;
    color: var(--text);
    user-select: text;
    white-space: pre-wrap;
  }
  .rchat-a {
    align-self: flex-start;
    max-width: 92%;
    font-size: 13.5px;
    line-height: 1.6;
    color: var(--text);
    user-select: text;
  }
  .rchat-a.err { color: var(--err); }
  .rchat-a :global(p) { margin: 0 0 8px; }
  .rchat-a :global(p:last-child) { margin-bottom: 0; }
  .rchat-a :global(.md-list) { margin: 4px 0 10px; padding-left: 18px; }
  .rchat-a :global(strong) { color: var(--text); font-weight: 700; }
  .rchat-a :global(code) {
    font-family: var(--font-mono);
    font-size: 12.5px;
    background: var(--raised);
    padding: 1px 5px;
    border-radius: 4px;
  }
  .rchat-tool { display: flex; align-items: center; gap: 7px; font-size: 11.5px; color: var(--faint); }
  .rchat-tool-dot { width: 5px; height: 5px; border-radius: 50%; background: var(--accent); flex: none; }
  .rchat-loading { display: flex; align-items: center; gap: 8px; color: var(--muted); font-size: 13px; }
  .rchat-input { display: flex; gap: 8px; }
  .rchat-input .input { flex: 1; }
  .rchat-note { font-size: 11.5px; color: var(--faint); margin: -6px 0 12px; line-height: 1.5; }
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
</style>
