<script lang="ts">
  import { onDestroy } from "svelte";
  import { CommitActivity } from "../../wailsjs/go/main/App";
  import { daysUntil } from "./pm";
  import Heatmap from "./Heatmap.svelte";

  // Full project list (code + manual) assembled in App.svelte.
  export let projects: any[] = [];
  // Fleet-wide counts, computed once in App.svelte and passed down.
  export let stats: {
    total: number;
    active: number;
    dirty: number;
    behind: number;
    unpushed: number;
    overdue: number;
  } = { total: 0, active: 0, dirty: 0, behind: 0, unpushed: 0, overdue: 0 };
  // Select a project and switch to the Projects view (opens its detail).
  export let onOpen: (id: string) => void;
  // Bounded concurrency pool from App.svelte. Aggregate heatmap fan-out goes
  // through this (cap 6) instead of an unbounded Promise.all.
  export let runPool: <T>(
    items: T[],
    limit: number,
    worker: (item: T) => Promise<void>
  ) => Promise<void>;

  // A commit older than this many days marks a repo "stale".
  const STALE_DAYS = 14;

  // Stat tiles: label + value + accent class (accent only lights up when the
  // count is non-zero, so a healthy fleet reads calm).
  $: tiles = [
    { key: "total", label: "repos", value: stats.total, tone: "accent", hot: false },
    { key: "active", label: "active", value: stats.active, tone: "ok", hot: false },
    { key: "dirty", label: "dirty", value: stats.dirty, tone: "dirty", hot: stats.dirty > 0 },
    { key: "behind", label: "behind", value: stats.behind, tone: "err", hot: stats.behind > 0 },
    { key: "unpushed", label: "unpushed", value: stats.unpushed, tone: "ahead", hot: stats.unpushed > 0 },
    { key: "overdue", label: "overdue", value: stats.overdue, tone: "err", hot: stats.overdue > 0 },
  ];

  // ---- needs-attention queue ----------------------------------------------
  // One ranked list. A code project earns a spot if it is dirty, behind,
  // unpushed (ahead), has an overdue deadline, or is stale. Each reason adds a
  // tag and a score weight; overdue/behind/unpushed outrank stale.
  type Reason = { text: string; cls: string };

  function evalAttention(p: any): { reasons: Reason[]; score: number } {
    const reasons: Reason[] = [];
    let score = 0;

    const dl = daysUntil(p.deadline);
    if (dl !== null && dl < 0) {
      const over = -dl;
      reasons.push({ text: "overdue " + over + "d", cls: "over" });
      score += 1000 + over;
    }
    if (p.behind > 0) {
      reasons.push({ text: "behind " + p.behind, cls: "dn" });
      score += 500 + p.behind;
    }
    if (p.ahead > 0) {
      reasons.push({ text: "unpushed " + p.ahead, cls: "up" });
      score += 400 + p.ahead;
    }
    if (p.dirty) {
      const n = p.modified || (p.dirtyFiles ? p.dirtyFiles.length : 0);
      reasons.push({ text: n > 0 ? n + " changed" : "changed", cls: "dirty" });
      score += 200 + n;
    }
    const lw = daysUntil(p.lastWhen);
    if (lw !== null && -lw > STALE_DAYS) {
      reasons.push({ text: "stale", cls: "stale" });
      score += 100;
    }
    return { reasons, score };
  }

  $: attention = projects
    .filter((p) => p.type === "code")
    .map((p) => ({ p, ...evalAttention(p) }))
    .filter((x) => x.reasons.length > 0)
    .sort((a, b) => b.score - a.score || a.p.name.localeCompare(b.p.name));

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
  });
</script>

<div class="overview">
  <div class="ov-inner">
    <!-- Stat tiles -->
    <section class="ov-stats">
      {#each tiles as t (t.key)}
        <div class="ov-tile tone-{t.tone}" class:hot={t.hot}>
          <span class="ov-tile-num">{t.value}</span>
          <span class="ov-tile-label">{t.label}</span>
        </div>
      {/each}
    </section>

    <div class="ov-grid">
      <!-- Needs-attention queue -->
      <section class="ov-card ov-attention">
        <div class="ov-card-head">
          <h3 class="ov-card-title">Needs attention</h3>
          <span class="ov-count">{attention.length}</span>
        </div>
        {#if attention.length === 0}
          <div class="ov-empty">Everything is clean, in sync, and on time</div>
        {:else}
          <ul class="ov-list">
            {#each attention as a (a.p.id)}
              <li>
                <button class="ov-row" on:click={() => onOpen(a.p.id)}>
                  <span class="ov-name">{a.p.name}</span>
                  <span class="ov-tags">
                    {#each a.reasons as r}
                      <span class="ov-pill {r.cls}">{r.text}</span>
                    {/each}
                  </span>
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </section>

      <!-- Aggregate commit activity -->
      <section class="ov-card ov-activity">
        <div class="ov-card-head">
          <h3 class="ov-card-title">Commit activity</h3>
          <span class="ov-sub">last 16 weeks, all repos</span>
        </div>
        <Heatmap days={aggDays} />
      </section>
    </div>
  </div>
</div>
