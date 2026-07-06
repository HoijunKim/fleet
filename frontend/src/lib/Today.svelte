<script lang="ts">
  import { AskAI, AIAvailable, NotionTasks, NotionAvailable, OpenURL, Log, NotionComplete, CancelAI } from "../../wailsjs/go/main/App";
  import { fly } from "svelte/transition";
  import { flyUp } from "./motion";
  import { renderBrief } from "./markdown";
  import { toastSuccess, toastError } from "./toasts";
  import Select from "./Select.svelte";
  import { daysUntil } from "./pm";

  // Full project list (code + manual) from App.svelte.
  export let projects: any[] = [];
  // Open a project in the Projects view.
  export let onOpen: (id: string) => void;
  // Open a project straight into its Ask-AI deep-dive tab.
  export let onDrill: (id: string) => void = () => {};

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
  let genId = 0; // soft-cancel: a bump makes an in-flight result get dropped

  function cancelBrief() {
    genId++;
    loading = false;
    CancelAI(); // actually kill the in-flight request, not just hide the spinner
  }

  // Output language for the briefing, remembered across sessions.
  const LANGS: { code: string; name: string }[] = [
    { code: "ko", name: "Korean" },
    { code: "en", name: "English" },
    { code: "ja", name: "Japanese" },
    { code: "zh", name: "Chinese" },
  ];
  let briefLang =
    (typeof localStorage !== "undefined" && localStorage.getItem("fleet.briefLang")) || "ko";
  function onLangChange() {
    if (typeof localStorage !== "undefined") localStorage.setItem("fleet.briefLang", briefLang);
  }
  function langName(code: string): string {
    return (LANGS.find((l) => l.code === code) || LANGS[0]).name;
  }

  AIAvailable().then((ok) => (aiAvailable = !!ok)).catch(() => (aiAvailable = false));

  // Persist the last briefing so reopening Today shows it, not a blank card.
  let briefAt = "";
  function loadBrief() {
    if (typeof localStorage === "undefined") return;
    try {
      const raw = localStorage.getItem("fleet.brief");
      if (raw) {
        const o = JSON.parse(raw);
        brief = o.text || "";
        briefAt = o.at || "";
      }
    } catch {
      /* ignore */
    }
  }
  function saveBrief() {
    if (typeof localStorage === "undefined") return;
    briefAt = new Date().toLocaleString();
    try {
      localStorage.setItem("fleet.brief", JSON.stringify({ text: brief, at: briefAt }));
      // record that today's brief exists so the auto-brief doesn't re-run
      localStorage.setItem("fleet.briefAutoDate", new Date().toDateString());
    } catch {
      /* ignore */
    }
  }
  loadBrief();

  // PUSH: on the first open of the day, generate the briefing automatically so
  // fleet surfaces what you forgot before you ask. Fires once per day, once
  // enough repo state has loaded to brief on.
  let autoTried = false;
  $: maybeAutoBrief(aiAvailable, projects);
  function maybeAutoBrief(_ok: boolean, _p: any[]) {
    if (autoTried || loading || !aiAvailable) return;
    const code = (projects || []).filter((p) => p.type === "code" && p.isGit);
    if (code.length > 0 && !code.some((p) => p.loaded)) return; // wait for load
    if (typeof localStorage !== "undefined" &&
        localStorage.getItem("fleet.briefAutoDate") === new Date().toDateString()) {
      autoTried = true;
      return;
    }
    autoTried = true;
    generate();
  }

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

  // Two-way: check off a checkbox-based Notion task, then drop it from the list.
  let completing = "";
  async function completeNotion(t: any) {
    if (!t.checkboxProp || completing) return;
    completing = t.id;
    try {
      const err = await NotionComplete(t.id, t.checkboxProp);
      if (err) {
        toastError("Notion: " + err.replace(/^error:\s*/, ""));
        return;
      }
      notionTasks = notionTasks.filter((x) => x.id !== t.id);
      toastSuccess("Marked done in Notion");
    } catch (e) {
      toastError("Notion: " + String(e));
    } finally {
      completing = "";
    }
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
      "Be direct, no preamble, under 160 words. " +
      "Write the entire response in " + langName(briefLang) + ", but keep project names and code identifiers as-is." +
      "\n\nProjects:\n" +
      lines.join("\n") +
      notionBlock
    );
  }

  async function generate() {
    if (loading) return;
    const g = ++genId;
    loading = true;
    brief = "";
    briefError = "";
    try {
      const res = await AskAI(buildPrompt());
      if (g !== genId) return; // cancelled
      if (typeof res === "string" && res.startsWith("error:")) {
        briefError = res.slice(6).trim() || "AI request failed";
      } else {
        brief = res || "";
        saveBrief();
      }
    } catch (e) {
      if (g !== genId) return;
      briefError = (e && (e as any).message) || String(e);
    } finally {
      if (g === genId) loading = false;
    }
  }

  // "This week": summarize recent commits across all code repos.
  async function generateWeek() {
    if (loading) return;
    const g = ++genId;
    loading = true;
    brief = "";
    briefError = "";
    try {
      const code = (projects || []).filter((p) => p.type === "code" && p.isGit).slice(0, 24);
      const blocks: string[] = [];
      await Promise.all(
        code.map(async (p) => {
          let commits: any[] = [];
          try {
            commits = (await Log(p.path, 30)) || [];
          } catch {
            commits = [];
          }
          const recent = commits.filter((c) => {
            const d = daysUntil(c.when);
            return d !== null && -d >= 0 && -d < 7; // strict last-7-days window
          });
          if (recent.length) {
            blocks.push(
              (p.name || p.path) + ":\n" + recent.map((c) => "  - " + c.message).join("\n")
            );
          }
        })
      );
      if (g !== genId) return; // cancelled during the log fan-out
      if (blocks.length === 0) {
        brief = "No commits in the last 7 days.";
        return;
      }
      const prompt =
        "Summarize what I worked on in the last 7 days, grouped by project, as short bullets. " +
        "Base it ONLY on the commit messages below; do not invent. Note momentum or a stalled project if obvious. " +
        "Concise, under 180 words, in " + langName(briefLang) + " (keep project names and code identifiers as-is)." +
        "\n\nCommits by project:\n" + blocks.join("\n\n");
      const res = await AskAI(prompt);
      if (g !== genId) return; // cancelled
      if (typeof res === "string" && res.startsWith("error:")) {
        briefError = res.slice(6).trim() || "AI request failed";
      } else {
        brief = res || "";
        saveBrief();
      }
    } catch (e) {
      if (g !== genId) return;
      briefError = (e && (e as any).message) || String(e);
    } finally {
      if (g === genId) loading = false;
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
          <div class="brief-controls">
            <div class="brief-lang">
              <Select
                bind:value={briefLang}
                options={LANGS.map((l) => ({ value: l.code, label: l.name }))}
                ariaLabel="Briefing language"
                onChange={onLangChange}
              />
            </div>
            <button class="btn btn-secondary btn-sm" on:click={generateWeek} disabled={loading} title="Summarize the last 7 days">
              This week
            </button>
            <button class="btn btn-primary btn-sm" on:click={generate} disabled={loading}>
              {loading ? "Thinking..." : brief || briefError ? "Regenerate" : "What first today?"}
            </button>
          </div>
        {/if}
      </div>

      {#if !aiAvailable}
        <div class="ov-empty">
          Install the <span class="mono">claude</span> CLI to get an AI briefing across your projects.
        </div>
      {:else if loading}
        <div class="brief-loading">
          <span class="spinner"></span> Reading your projects...
          <button class="brief-cancel" on:click={cancelBrief}>Cancel</button>
        </div>
      {:else if briefError}
        <div class="brief-error">Could not generate a briefing: {briefError}</div>
      {:else if brief}
        <div class="brief-body">{@html renderBrief(brief)}</div>
        {#if briefAt}<div class="brief-at">generated {briefAt}</div>{/if}
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
          {#each forgotten as f, i (f.p.id + ":" + f.kind)}
            <li in:fly={flyUp(i)} class="forgot-li">
              <button class="ov-row" on:click={() => onOpen(f.p.id)}>
                <span class="ov-name">{f.p.name}</span>
                <span class="forgot-detail">{f.text}</span>
                <span class="ov-tags">
                  <span class="ov-pill forgot-{f.kind}">{KIND_LABEL[f.kind] || f.kind}</span>
                </span>
              </button>
              <button class="forgot-ask" on:click|stopPropagation={() => onDrill(f.p.id)} title="Ask AI about this repo">Ask AI</button>
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
            {#each notionSorted as t, i (t.id || t.url + ":" + t.title)}
              <li in:fly={flyUp(i)} class="notion-li">
                {#if t.checkboxProp}
                  <button
                    class="notion-done"
                    on:click|stopPropagation={() => completeNotion(t)}
                    disabled={completing === t.id}
                    title="Mark done in Notion"
                    aria-label="Mark done in Notion"
                  ></button>
                {/if}
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
  .brief-controls { margin-left: auto; display: flex; align-items: center; gap: 8px; }
  .brief-lang { min-width: 128px; }
  .brief-lang :global(.sel) { width: 100%; }
  .brief-body {
    line-height: 1.6;
    font-size: 14px;
    color: var(--text);
    user-select: text;
  }
  .brief-body :global(p) { margin: 0 0 8px; }
  .brief-body :global(p:last-child) { margin-bottom: 0; }
  .brief-body :global(.md-h) {
    font-weight: 700;
    font-size: 12px;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--muted);
    margin: 12px 0 6px;
  }
  .brief-body :global(.md-list) { margin: 4px 0 10px; padding-left: 18px; }
  .brief-body :global(.md-list li) { margin: 3px 0; }
  .brief-body :global(strong) { color: var(--text); font-weight: 700; }
  .brief-body :global(em) { color: var(--muted); font-style: italic; }
  .brief-body :global(code) {
    font-family: var(--font-mono);
    font-size: 12.5px;
    background: var(--raised);
    padding: 1px 5px;
    border-radius: 4px;
  }
  .brief-at { margin-top: 10px; font-size: 11px; color: var(--faint); }
  .brief-loading { display: flex; align-items: center; gap: 10px; color: var(--muted); font-size: 13px; }
  .brief-cancel {
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
  .brief-cancel:hover { color: var(--err); border-color: var(--err-line); }
  .brief-error { color: var(--err); font-size: 13px; }

  .forgot-detail {
    color: var(--muted);
    font-size: 12px;
    font-variant-numeric: tabular-nums;
    margin-right: 8px;
    white-space: nowrap;
  }
  .forgot-detail.due-late { color: var(--err); }
  .forgot-li { display: flex; align-items: center; gap: 6px; }
  .forgot-li .ov-row { flex: 1; min-width: 0; }
  .forgot-ask {
    flex: none;
    font: inherit;
    font-size: 11px;
    color: var(--accent);
    background: var(--accent-soft);
    border: 1px solid var(--accent-line);
    border-radius: var(--r-pill);
    padding: 3px 10px;
    cursor: pointer;
    opacity: 0;
    transition: opacity var(--t), background var(--t);
  }
  .forgot-li:hover .forgot-ask, .forgot-ask:focus-visible { opacity: 1; }
  .forgot-ask:hover { background: rgba(110, 168, 254, 0.22); }
  .notion-li { display: flex; align-items: center; gap: 6px; }
  .notion-li .ov-row { flex: 1; min-width: 0; }
  .notion-done {
    flex: none;
    width: 17px;
    height: 17px;
    border-radius: 50%;
    border: 1.5px solid var(--faint);
    background: transparent;
    cursor: pointer;
    transition: border-color var(--t), background var(--t);
  }
  .notion-done:hover { border-color: var(--ok); background: var(--ok-soft); }
  .notion-done:disabled { opacity: 0.5; cursor: default; }
  .ov-pill.forgot-wip      { color: var(--dirty); border-color: var(--dirty-line); background: var(--dirty-soft); }
  .ov-pill.forgot-unpushed { color: var(--ahead); border-color: var(--ok-line);    background: var(--ok-soft); }
  .ov-pill.forgot-idle     { color: var(--muted); border-color: var(--border);     background: var(--raised); }
  .ov-pill.forgot-todo     { color: var(--accent); border-color: var(--accent-line); background: var(--accent-soft); }
</style>
