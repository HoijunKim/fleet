<script lang="ts">
  import {
    AskAI, RepoDiff, Log, RepoSymbols, CancelAI, ReadRepoFile, RepoGrep, RepoFiles,
  } from "../../wailsjs/go/main/App";
  import { onMount } from "svelte";
  import { renderBrief } from "./markdown";
  import { redactSecrets } from "./redact";
  import { daysUntil } from "./pm";
  import {
    available, consent, running, openOverlay, setProject as agentSetProject, initAgentSession,
  } from "./agentSession";

  // The selected code repo. Chat resets when the path changes.
  export let project: any;

  type Turn = { role: "user" | "assistant" | "tool"; text: string };

  let turns: Turn[] = [];
  let question = "";
  let loading = false;
  let loadedPath = "";
  let genId = 0;

  // The agentic deep-dive now lives in the shared agentSession store and runs
  // in <AgentOverlay/> (mounted once in App). This component only LAUNCHES it;
  // the single-shot fallback below is used when agentic isn't available.
  onMount(async () => {
    await initAgentSession();
  });

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
    // Rescope the shared agentic store to this repo. If a live agentic run is
    // still going for the OLD repo, setProject cancels the real process (not
    // just the UI) so it can't leak into the newly-selected repo.
    agentSetProject(project);
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
      "diff lines; be direct and specific, no filler. Answer in " + langName() + " (keep code identifiers as-is). " +
      "Write in plain text with ASCII punctuation only: use a hyphen (-), not em or en dashes; straight quotes; " +
      "no other special Unicode symbols.\n\n" +
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
  // agentic mode. The agentic path launches the shared overlay instead of
  // running inline; the single-shot fallback still runs inline here.
  function dispatch(text: string) {
    if ($available && $consent) { agentSetProject(project); openOverlay(project); }
    else ask(text);
  }

  function submit() {
    dispatch(question);
  }
</script>

<div class="rchat">
  {#if $available}
    <div class="rchat-launch">
      {#if !$consent}
        <p class="rchat-hint">Agentic deep-dive - Claude Code reads this repo (you approve every edit/command). Opens in a focused view.</p>
      {/if}
      <button class="btn btn-primary btn-sm" on:click={() => { agentSetProject(project); openOverlay(project); }}>
        {$running ? "Resume agentic deep-dive..." : "Open agentic deep-dive"}
        {#if $running}<span class="rchat-run-dot"></span>{/if}
      </button>
    </div>
  {/if}

  {#if turns.length === 0}
    <div class="rchat-intro">
      <p class="rchat-hint">Ask about this repo - I read its recent commits, uncommitted diff, and exported symbols.</p>
      {#if !$available}
        <p class="rchat-note">Agentic deep-dive (live activity, approvals, cost) needs the Claude (Claude Code) provider with the claude CLI installed - using single-shot mode for now.</p>
      {/if}
      <div class="rchat-starters">
        {#each STARTERS as s}
          <button class="rchat-starter" on:click={() => dispatch(s)} disabled={loading || $running}>{s}</button>
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
      disabled={loading || $running}
      aria-label="Ask about this repo"
    />
    <button class="btn btn-primary btn-sm" on:click={submit} disabled={(loading || $running) || !question.trim()}>Ask</button>
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
  .rchat-launch { border: 1px solid var(--border); border-radius: var(--r-btn); padding: 12px; display: flex; flex-direction: column; gap: 8px; }
  .rchat-launch .rchat-hint { margin: 0; }
  .rchat-launch .btn { align-self: flex-start; }
  .rchat-run-dot {
    display: inline-block; width: 6px; height: 6px; border-radius: 50%;
    background: var(--accent); margin-left: 6px; vertical-align: middle;
  }
  .mono { font-family: var(--font-mono); }
</style>
