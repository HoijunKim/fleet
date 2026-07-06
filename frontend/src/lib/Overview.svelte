<script lang="ts">
  import { onDestroy } from "svelte";
  import { Agenda, CommitActivity } from "../../wailsjs/go/main/App";
  import Heatmap from "./Heatmap.svelte";
  import AgendaCard from "./AgendaCard.svelte";

  // Full project list (code + manual) assembled in App.svelte.
  export let projects: any[] = [];
  // Fleet-wide counts, computed once in App.svelte and passed down.
  export let stats: {
    total: number;
    repos: number;
    active: number;
    dirty: number;
    behind: number;
    unpushed: number;
    overdue: number;
  } = { total: 0, repos: 0, active: 0, dirty: 0, behind: 0, unpushed: 0, overdue: 0 };
  // Select a project and switch to the Projects view (opens its detail).
  export let onOpen: (id: string) => void;
  // Click a stat tile to jump into the Projects view, filtered where the
  // metric maps to a filter (dirty / behind). Key is the tile key.
  export let onFilter: (key: string) => void = () => {};
  // Bounded concurrency pool from App.svelte. Aggregate heatmap fan-out goes
  // through this (cap 6) instead of an unbounded Promise.all.
  export let runPool: <T>(
    items: T[],
    limit: number,
    worker: (item: T) => Promise<void>
  ) => Promise<void>;

  // Stat tiles: label + value + accent class (accent only lights up when the
  // count is non-zero, so a healthy fleet reads calm).
  $: tiles = [
    { key: "repos", label: "repos", value: stats.repos, tone: "accent", hot: false },
    { key: "active", label: "active", value: stats.active, tone: "ok", hot: false },
    { key: "dirty", label: "dirty", value: stats.dirty, tone: "dirty", hot: stats.dirty > 0 },
    { key: "behind", label: "behind", value: stats.behind, tone: "err", hot: stats.behind > 0 },
    { key: "unpushed", label: "unpushed", value: stats.unpushed, tone: "ahead", hot: stats.unpushed > 0 },
    { key: "overdue", label: "overdue", value: stats.overdue, tone: "err", hot: stats.overdue > 0 },
  ];

  // ---- aggregate commit activity across all code projects -----------------
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
    const map = new Map<string, number>();
    // Bounded fan-out: at most 6 CommitActivity calls in flight at once.
    await runPool(paths, 6, async (path) => {
      let arr: Array<{ date: string; count: number }>;
      try {
        arr = await CommitActivity(path, 16);
      } catch {
        arr = [];
      }
      if (gen !== aggGen) return; // a newer request superseded this one
      for (const d of arr || []) {
        if (d && d.date) map.set(d.date, (map.get(d.date) || 0) + (d.count || 0));
      }
    });
    if (gen !== aggGen) return;
    aggDays = [...map.entries()]
      .map(([date, count]) => ({ date, count }))
      .sort((a, b) => (a.date < b.date ? -1 : a.date > b.date ? 1 : 0));
  }

  onDestroy(() => {
    aggGen++; // invalidate any in-flight aggregate load
    agendaGen++; // invalidate any in-flight agenda load
  });

  // ---- fleet-wide agenda ---------------------------------------------------
  // Single cheap store-only call (no fan-out needed). Re-fetched only when a
  // PM-relevant field actually changes - the frequent git-field merges and
  // auto-fetch ticks reassign `projects` too (bare `projects = projects`), so
  // guard on a signature over just deadline/status/tasks to avoid firing an
  // Agenda() call per repo on every refresh.
  let agendaItems: any[] = [];
  let agendaGen = 0;
  let lastAgendaSig = "\0"; // sentinel so the first real signature always loads

  $: agendaSig = projects
    .map((p) => p.id + "|" + (p.deadline || "") + "|" + (p.status || "") + "|" + JSON.stringify(p.tasks || []))
    .join("\n");
  $: {
    if (agendaSig !== lastAgendaSig) {
      lastAgendaSig = agendaSig;
      loadAgenda();
    }
  }

  async function loadAgenda() {
    const gen = ++agendaGen;
    let items: any[] = [];
    try {
      items = (await Agenda()) || [];
    } catch {
      items = [];
    }
    if (gen !== agendaGen) return; // a newer request superseded this one
    agendaItems = items;
  }
</script>

<div class="overview">
  <div class="ov-inner">
    <!-- Stat tiles -->
    <section class="ov-stats">
      {#each tiles as t (t.key)}
        <button
          class="ov-tile tone-{t.tone}"
          class:hot={t.hot}
          on:click={() => onFilter(t.key)}
          title={["dirty", "behind", "unpushed", "overdue"].includes(t.key)
            ? "Filter Projects to " + t.label
            : "Show all projects"}
        >
          <span class="ov-tile-num">{t.value}</span>
          <span class="ov-tile-label">{t.label}</span>
        </button>
      {/each}
    </section>

    <div class="ov-grid">
      <!-- Fleet-wide agenda: deadlines + due/doing tasks, soonest first.
           The old "Needs attention" queue moved to Today ("Easy to forget") so
           there is one attention list, not two near-identical ones. -->
      <AgendaCard items={agendaItems} {onOpen} />

      <!-- Aggregate commit activity -->
      <section class="ov-card ov-activity ov-wide">
        <div class="ov-card-head">
          <h3 class="ov-card-title">Commit activity</h3>
          <span class="ov-sub">last 16 weeks, all repos</span>
        </div>
        <Heatmap days={aggDays} />
      </section>
    </div>
  </div>
</div>
