<script lang="ts">
  import { AskAI, AIAvailable, NotionTasks, NotionAvailable, OpenURL } from "../../wailsjs/go/main/App";
  import { daysUntil } from "./pm";

  // Full project list (code + manual) from App.svelte.
  export let projects: any[] = [];
  // Open a project in the Projects view.
  export let onOpen: (id: string) => void;

  // ---- Forgotten work: derived client-side from the loaded projects, the way
  // the attention queue is. "Forgotten" means genuinely neglected, so fresh
  // work is not flagged - WIP/unpushed must have sat a couple of days first.
  function daysSince(when: string): number | null {
    const d = daysUntil(when);
    return d === null ? null : -d;
  }

  type Forgot = { p: any; kind: string; text: string; days: number };

  $: forgotten = (() => {
    const out: Forgot[] = [];
    for (const p of projects || []) {
      if (p.type !== "code" || !p.isGit) continue;
      const since = daysSince(p.lastWhen);
      const s = since === null ? 0 : since;
      if (p.dirty && s >= 2) {
        out.push({ p, kind: "wip", text: (p.modified || 0) + " uncommitted, idle " + s + "d", days: s });
      }
      if (p.ahead > 0 && s >= 2) {
        out.push({ p, kind: "unpushed", text: p.ahead + " unpushed, idle " + s + "d", days: s });
      }
      if (!p.dirty && p.ahead === 0 && since !== null && since > 14) {
        out.push({ p, kind: "idle", text: "untouched " + since + "d", days: since });
      }
      if ((p.todo || 0) >= 8) {
        out.push({ p, kind: "todo", text: p.todo + " TODOs in code", days: s });
      }
    }
    out.sort((a, b) => b.days - a.days);
    return out.slice(0, 8);
  })();

  const KIND_LABEL: Record<string, string> = {
    wip: "WIP", unpushed: "unpushed", idle: "idle", todo: "TODOs",
  };

  // ---- AI briefing --------------------------------------------------------
  let aiAvailable = false;
  let loading = false;
  let brief = "";
  let briefError = "";

  AIAvailable().then((ok) => (aiAvailable = !!ok)).catch(() => (aiAvailable = false));

  // ---- Notion tasks (read-only, if configured) ----------------------------
  let notionOn = false;
  let notionTasks: any[] = [];
  let notionError = "";

  NotionAvailable()
    .then((ok) => {
      notionOn = !!ok;
      if (notionOn) return NotionTasks();
      return { tasks: [], error: "" };
    })
    .then((res: any) => {
      const r = res || { tasks: [], error: "" };
      notionError = r.error || "";
      notionTasks = (r.tasks || []).filter((t: any) => !t.done);
    })
    .catch((e) => {
      notionError = (e && (e as any).message) || String(e);
      notionTasks = [];
    });

  $: notionSorted = [...(notionTasks || [])].sort((a, b) => {
    if (!a.due) return b.due ? 1 : 0; // empty due sorts last
    if (!b.due) return -1;
    return a.due < b.due ? -1 : a.due > b.due ? 1 : 0;
  });

  function openNotion(url: string) {
    if (url) OpenURL(url);
  }

  // Compact one-line state per code project for the prompt.
  function projectLine(p: any): string {
    const bits: string[] = [];
    if (p.dirty) bits.push((p.modified || "?") + " uncommitted");
    if (p.behind > 0) bits.push(p.behind + " behind");
    if (p.ahead > 0) bits.push(p.ahead + " unpushed");
    if (p.deadline) {
      const d = daysUntil(p.deadline);
      if (d !== null) bits.push(d < 0 ? "deadline " + -d + "d overdue" : "deadline in " + d + "d");
    }
    if (p.taskCount > 0) bits.push("tasks " + p.doneCount + "/" + p.taskCount);
    const since = daysSince(p.lastWhen);
    if (since !== null) bits.push("last commit " + since + "d ago");
    if ((p.todo || 0) >= 8) bits.push(p.todo + " code TODOs");
    return "- " + (p.name || p.id) + ": " + (bits.length ? bits.join(", ") : "clean, on track");
  }

  function buildPrompt(): string {
    // Only include repos whose git state has actually loaded - an un-loaded or
    // errored repo has no git fields and would otherwise be briefed as "clean,
    // on track", making the AI reason over confidently wrong state.
    const code = (projects || []).filter(
      (p) => p.type === "code" && p.loaded && !p.errMsg
    );
    const pending = (projects || []).filter(
      (p) => p.type === "code" && (!p.loaded || p.errMsg)
    ).length;
    const manual = (projects || []).filter((p) => p.type === "manual");
    const lines = code.map(projectLine);
    if (pending > 0) {
      lines.push("- (" + pending + " repo(s) still loading - not included)");
    }
    for (const p of manual) {
      const bits: string[] = [];
      if (p.deadline) {
        const d = daysUntil(p.deadline);
        if (d !== null) bits.push(d < 0 ? "deadline " + -d + "d overdue" : "deadline in " + d + "d");
      }
      if (p.taskCount > 0) bits.push("tasks " + p.doneCount + "/" + p.taskCount);
      lines.push("- " + (p.name || p.id) + " (notes-only): " + (bits.length ? bits.join(", ") : "no signals"));
    }
    let notionBlock = "";
    if (notionTasks && notionTasks.length) {
      const nl = notionTasks.slice(0, 30).map((t: any) => {
        const bits: string[] = [];
        if (t.status) bits.push(t.status);
        if (t.due) {
          const d = daysUntil(t.due);
          if (d !== null) bits.push(d < 0 ? "due " + -d + "d overdue" : "due in " + d + "d");
        }
        return "- " + t.title + (bits.length ? " (" + bits.join(", ") + ")" : "");
      });
      notionBlock = "\n\nNotion tasks (my planning board):\n" + nl.join("\n");
    }
    return (
      "You are my focused engineering chief-of-staff. Below is the live state of my dev projects" +
      (notionBlock ? " plus my Notion planning board" : "") +
      ". Tell me what to work on FIRST today and why, as 3-5 short concrete bullets that name the projects. " +
      "Then one line flagging anything I appear to have forgotten (stale WIP, long-unpushed, abandoned work). " +
      "Be direct, no preamble, under 160 words.\n\nProjects:\n" +
      lines.join("\n") +
      notionBlock
    );
  }

  async function generate() {
    if (loading) return;
    loading = true;
    brief = "";
    briefError = "";
    try {
      const res = await AskAI(buildPrompt());
      if (typeof res === "string" && res.startsWith("error:")) {
        briefError = res.slice(6).trim() || "AI request failed";
      } else {
        brief = res || "";
      }
    } catch (e) {
      briefError = (e && (e as any).message) || String(e);
    } finally {
      loading = false;
    }
  }
</script>

<div class="today">
  <div class="today-inner">
    <div class="today-head">
      <h1 class="today-title">Today</h1>
      <p class="today-sub">What you'd lose track of, and what to do first.</p>
    </div>

    <!-- AI briefing leads: the reason to open fleet -->
    <section class="ov-card brief-card">
      <div class="ov-card-head">
        <h3 class="ov-card-title">Briefing</h3>
        {#if aiAvailable}
          <button class="btn btn-primary btn-sm brief-btn" on:click={generate} disabled={loading}>
            {loading ? "Thinking..." : brief || briefError ? "Regenerate" : "What first today?"}
          </button>
        {/if}
      </div>

      {#if !aiAvailable}
        <div class="ov-empty">
          Install the <span class="mono">claude</span> CLI to get an AI briefing across your projects.
        </div>
      {:else if loading}
        <div class="brief-loading">
          <span class="spinner"></span> Reading your projects...
        </div>
      {:else if briefError}
        <div class="brief-error">Could not generate a briefing: {briefError}</div>
      {:else if brief}
        <div class="brief-body">{brief}</div>
      {:else}
        <div class="ov-empty">
          One click reads every project's git + tasks and tells you where to start.
        </div>
      {/if}
    </section>

    <!-- Forgotten work: deterministic, always available -->
    <section class="ov-card">
      <div class="ov-card-head">
        <h3 class="ov-card-title">Easy to forget</h3>
        <span class="ov-count">{forgotten.length}</span>
      </div>
      {#if forgotten.length === 0}
        <div class="ov-empty">Nothing slipping through the cracks. Clean.</div>
      {:else}
        <ul class="ov-list">
          {#each forgotten as f (f.p.id + ":" + f.kind)}
            <li>
              <button class="ov-row" on:click={() => onOpen(f.p.id)}>
                <span class="ov-name">{f.p.name}</span>
                <span class="forgot-detail">{f.text}</span>
                <span class="ov-tags">
                  <span class="ov-pill forgot-{f.kind}">{KIND_LABEL[f.kind] || f.kind}</span>
                </span>
              </button>
            </li>
          {/each}
        </ul>
      {/if}
    </section>

    <!-- Notion tasks, if connected -->
    {#if notionOn}
      <section class="ov-card">
        <div class="ov-card-head">
          <h3 class="ov-card-title">From Notion</h3>
          <span class="ov-count">{notionSorted.length}</span>
        </div>
        {#if notionError}
          <div class="brief-error">Could not reach Notion: {notionError}</div>
        {:else if notionSorted.length === 0}
          <div class="ov-empty">No open tasks in your Notion board.</div>
        {:else}
          <ul class="ov-list">
            {#each notionSorted as t (t.url + ":" + t.title)}
              <li>
                <button class="ov-row" on:click={() => openNotion(t.url)} title="Open in Notion">
                  <span class="ov-name">{t.title}</span>
                  {#if t.due}
                    {@const d = daysUntil(t.due)}
                    <span class="forgot-detail" class:due-late={d !== null && d < 0}>
                      {d === null ? t.due : d < 0 ? -d + "d overdue" : d === 0 ? "today" : "in " + d + "d"}
                    </span>
                  {/if}
                  {#if t.status}
                    <span class="ov-tags"><span class="ov-pill stale">{t.status}</span></span>
                  {/if}
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    {/if}
  </div>
</div>

<style>
  .today {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 28px 24px 40px;
    background:
      radial-gradient(1100px 460px at 78% -80px, rgba(110, 168, 254, 0.06), transparent 62%),
      var(--bg);
  }
  .today-inner {
    max-width: 860px;
    margin: 0 auto;
    display: flex;
    flex-direction: column;
    gap: 20px;
  }
  .today-head { margin-bottom: 2px; }
  .today-title {
    margin: 0;
    font-size: 24px;
    font-weight: 700;
    letter-spacing: -0.02em;
    color: var(--text);
  }
  .today-sub { margin: 4px 0 0; color: var(--muted); font-size: 14px; }

  .brief-card { border-color: #33415d; }
  .brief-btn { margin-left: auto; }
  .brief-body {
    white-space: pre-wrap;
    line-height: 1.6;
    font-size: 14px;
    color: var(--text);
    user-select: text;
  }
  .brief-loading { display: flex; align-items: center; gap: 10px; color: var(--muted); font-size: 13px; }
  .brief-error { color: var(--err); font-size: 13px; }

  .forgot-detail {
    color: var(--muted);
    font-size: 12px;
    font-variant-numeric: tabular-nums;
    margin-right: 8px;
    white-space: nowrap;
  }
  .forgot-detail.due-late { color: var(--err); }
  .ov-pill.forgot-wip      { color: var(--dirty); border-color: var(--dirty-line); background: var(--dirty-soft); }
  .ov-pill.forgot-unpushed { color: var(--ahead); border-color: var(--ok-line);    background: var(--ok-soft); }
  .ov-pill.forgot-idle     { color: var(--muted); border-color: var(--border);     background: var(--raised); }
  .ov-pill.forgot-todo     { color: var(--accent); border-color: var(--accent-line); background: var(--accent-soft); }
</style>
