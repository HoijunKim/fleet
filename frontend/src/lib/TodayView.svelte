<script lang="ts">
  import { onDestroy } from "svelte";
  import { CommitActivity } from "../../wailsjs/go/main/App";
  import { daysUntil, ddayLabel } from "./pm";
  import Heatmap from "./Heatmap.svelte";

  // Full project list (code + manual) from App.svelte.
  export let projects: any[] = [];
  // Select a project and switch back to the Projects view.
  export let onOpen: (id: string) => void;

  // How far ahead a deadline / task due date counts as "soon".
  const DEADLINE_HORIZON = 14;
  const TASK_HORIZON = 7;

  // ---- upcoming deadlines -------------------------------------------------
  $: deadlines = projects
    .map((p) => ({ p, n: daysUntil(p.deadline) }))
    .filter((x) => x.n !== null && (x.n as number) <= DEADLINE_HORIZON)
    .sort((a, b) => (a.n as number) - (b.n as number));

  // ---- open high-priority / near-due tasks, grouped by project ------------
  $: taskGroups = projects
    .map((p) => {
      const tasks = (p.tasks || []).filter((t: any) => {
        if (t.done) return false;
        if ((p.priority || 0) >= 2) return true;
        const n = t.due ? daysUntil(t.due) : null;
        return n !== null && n <= TASK_HORIZON;
      });
      return { p, tasks };
    })
    .filter((g) => g.tasks.length > 0)
    .sort((a, b) => (b.p.priority || 0) - (a.p.priority || 0));

  // ---- git attention (dirty / behind / unpushed) --------------------------
  $: gitAttention = projects.filter(
    (p) => p.type === "code" && (p.dirty || p.behind > 0 || p.ahead > 0)
  );

  // ---- aggregate commit activity across all code projects ----------------
  let aggDays: Array<{ date: string; count: number }> = [];
  let aggGen = 0;
  let lastSig = "\0"; // sentinel so the first real signature always loads

  // Recompute only when the set of code repo paths changes (git-field merges
  // reuse the same paths, so this stays stable once the list has loaded).
  $: codePaths = projects
    .filter((p) => p.type === "code" && p.repoPath)
    .map((p) => p.repoPath);
  $: {
    const sig = codePaths.join("\n");
    if (sig !== lastSig) {
      lastSig = sig;
      loadAggregate(codePaths.slice());
    }
  }

  async function loadAggregate(paths: string[]) {
    const gen = ++aggGen;
    if (paths.length === 0) {
      aggDays = [];
      return;
    }
    const results = await Promise.all(
      paths.map((path) => CommitActivity(path, 16).catch(() => []))
    );
    if (gen !== aggGen) return; // a newer request superseded this one
    const map = new Map<string, number>();
    for (const arr of results) {
      for (const d of arr || []) {
        if (d && d.date) map.set(d.date, (map.get(d.date) || 0) + (d.count || 0));
      }
    }
    aggDays = [...map.entries()]
      .map(([date, count]) => ({ date, count }))
      .sort((a, b) => (a.date < b.date ? -1 : a.date > b.date ? 1 : 0));
  }

  onDestroy(() => {
    aggGen++; // invalidate any in-flight aggregate load
  });

  function badge(p: any): { text: string; cls: string } | null {
    return ddayLabel(p.deadline);
  }
</script>

<div class="today">
  <div class="today-grid">
    <!-- Upcoming deadlines -->
    <section class="today-card">
      <div class="today-card-head">
        <h3 class="today-card-title">Upcoming deadlines</h3>
        <span class="today-count">{deadlines.length}</span>
      </div>
      {#if deadlines.length === 0}
        <div class="today-empty">Nothing due in the next {DEADLINE_HORIZON} days</div>
      {:else}
        <ul class="today-list">
          {#each deadlines as d (d.p.id)}
            {@const b = badge(d.p)}
            <li>
              <button class="today-row" on:click={() => onOpen(d.p.id)}>
                <span class="dot {d.p.status || 'active'}"></span>
                <span class="today-name">{d.p.name}</span>
                <span class="type-badge {d.p.type}">{d.p.type}</span>
                {#if b}<span class="dday {b.cls}">{b.text}</span>{/if}
              </button>
            </li>
          {/each}
        </ul>
      {/if}
    </section>

    <!-- Git attention -->
    <section class="today-card">
      <div class="today-card-head">
        <h3 class="today-card-title">Git attention</h3>
        <span class="today-count">{gitAttention.length}</span>
      </div>
      {#if gitAttention.length === 0}
        <div class="today-empty">All repositories clean and in sync</div>
      {:else}
        <ul class="today-list">
          {#each gitAttention as p (p.id)}
            <li>
              <button class="today-row" on:click={() => onOpen(p.id)}>
                <span class="dot {p.dirty ? 'dirty' : 'ok'}"></span>
                <span class="today-name">{p.name}</span>
                <span class="today-tags">
                  {#if p.dirty}<span class="mini-pill dirty">dirty</span>{/if}
                  {#if p.behind > 0}<span class="mini-pill dn">down {p.behind}</span>{/if}
                  {#if p.ahead > 0}<span class="mini-pill up">up {p.ahead}</span>{/if}
                </span>
              </button>
            </li>
          {/each}
        </ul>
      {/if}
    </section>

    <!-- Open high-priority tasks -->
    <section class="today-card today-span">
      <div class="today-card-head">
        <h3 class="today-card-title">Open focus tasks</h3>
        <span class="today-count">{taskGroups.reduce((s, g) => s + g.tasks.length, 0)}</span>
      </div>
      {#if taskGroups.length === 0}
        <div class="today-empty">No high-priority or near-due tasks</div>
      {:else}
        <div class="today-groups">
          {#each taskGroups as g (g.p.id)}
            <div class="today-group">
              <button class="today-group-head" on:click={() => onOpen(g.p.id)}>
                <span class="dot {g.p.status || 'active'}"></span>
                <span class="today-name">{g.p.name}</span>
                <span class="today-prio prio-{g.p.priority || 0}">
                  <span class="prio-dot" class:on={(g.p.priority || 0) >= 1}></span>
                  <span class="prio-dot" class:on={(g.p.priority || 0) >= 2}></span>
                  <span class="prio-dot" class:on={(g.p.priority || 0) >= 3}></span>
                </span>
              </button>
              <ul class="today-tasklist">
                {#each g.tasks as t (t.id)}
                  {@const td = t.due ? ddayLabel(t.due) : null}
                  <li class="today-task">
                    <span class="today-task-title">{t.title}</span>
                    {#if td}<span class="pm-task-due {td.cls}">{td.text}</span>{/if}
                  </li>
                {/each}
              </ul>
            </div>
          {/each}
        </div>
      {/if}
    </section>

    <!-- Aggregate commit activity -->
    <section class="today-card today-span">
      <div class="today-card-head">
        <h3 class="today-card-title">Commit activity</h3>
        <span class="today-sub">last 16 weeks, all repositories</span>
      </div>
      <Heatmap days={aggDays} />
    </section>
  </div>
</div>
