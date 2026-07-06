<script lang="ts">
  import { AskAI, RepoDiff, Log, RepoSymbols, CancelAI } from "../../wailsjs/go/main/App";
  import { renderBrief } from "./markdown";
  import { redactSecrets } from "./redact";
  import { daysUntil } from "./pm";

  // The selected code repo. Chat resets when the path changes.
  export let project: any;

  type Turn = { role: "user" | "assistant"; text: string };

  let turns: Turn[] = [];
  let question = "";
  let loading = false;
  let loadedPath = "";
  let genId = 0;

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

  function buildPrompt(ctx: string, latest: string): string {
    const history = turns
      .map((t) => (t.role === "user" ? "Q: " : "A: ") + t.text)
      .join("\n\n");
    return (
      "You are a senior engineer pair-working on ONE repository. Use the concrete code context below - " +
      "reference actual files, commits, and diff lines. Be direct and specific, no filler. " +
      "Answer in " + langName() + " (keep code identifiers as-is).\n\n" +
      "=== Repository context ===\n" + ctx +
      (history ? "\n\n=== Conversation so far ===\n" + history : "") +
      "\n\n=== Question ===\n" + latest
    );
  }

  async function ask(q: string) {
    const text = q.trim();
    if (!text || loading) return;
    const p = project.path; // capture: drop the answer if the repo changes
    const g = ++genId; // or if the user cancels
    question = "";
    turns = [...turns, { role: "user", text }];
    loading = true;
    try {
      const ctx = await buildContext();
      const res = await AskAI(buildPrompt(ctx, text));
      if (p !== project.path || g !== genId) return; // moved on or cancelled
      const answer = typeof res === "string" && res.startsWith("error:") ? res : res || "(no answer)";
      turns = [...turns, { role: "assistant", text: answer }];
      saveChat();
    } catch (e) {
      if (p !== project.path || g !== genId) return;
      turns = [...turns, { role: "assistant", text: "error: " + String(e) }];
      saveChat();
    } finally {
      if (p === project.path && g === genId) loading = false;
    }
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      ask(question);
    }
  }
</script>

<div class="rchat">
  {#if turns.length === 0}
    <div class="rchat-intro">
      <p class="rchat-hint">Ask about this repo - I read its recent commits, uncommitted diff, and exported symbols.</p>
      <div class="rchat-starters">
        {#each STARTERS as s}
          <button class="rchat-starter" on:click={() => ask(s)} disabled={loading}>{s}</button>
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
      disabled={loading}
      aria-label="Ask about this repo"
    />
    <button class="btn btn-primary btn-sm" on:click={() => ask(question)} disabled={loading || !question.trim()}>Ask</button>
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
  .rchat-loading { display: flex; align-items: center; gap: 8px; color: var(--muted); font-size: 13px; }
  .rchat-input { display: flex; gap: 8px; }
  .rchat-input .input { flex: 1; }
</style>
