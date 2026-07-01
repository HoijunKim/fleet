<script lang="ts">
  export let repos: any[] = [];

  // Curated colors for common languages; anything else falls back to a
  // deterministic palette so the bar stays stable across renders.
  const LANG_COLORS: Record<string, string> = {
    Go: "#00add8",
    TypeScript: "#3178c6",
    JavaScript: "#f1e05a",
    Svelte: "#ff3e00",
    Python: "#3572a5",
    Rust: "#dea584",
    Java: "#b07219",
    "C++": "#f34b7d",
    C: "#a8b9cc",
    "C#": "#178600",
    Ruby: "#701516",
    PHP: "#4f5d95",
    Shell: "#89e051",
    HTML: "#e34c26",
    CSS: "#563d7c",
    Vue: "#41b883",
    Kotlin: "#a97bff",
    Swift: "#f05138",
    Dart: "#00b4ab",
    Lua: "#000080",
    Elixir: "#6e4a7e",
    Zig: "#ec915c",
  };

  const PALETTE = [
    "#6ea8fe", "#3fb950", "#d29922", "#f85149", "#a371f7",
    "#39c5cf", "#e685b5", "#89e051", "#f0883e", "#8b949e",
  ];

  function colorFor(lang: string, i: number): string {
    return LANG_COLORS[lang] || PALETTE[i % PALETTE.length];
  }

  $: total = repos.length;
  $: dirty = repos.filter((r) => r.dirty).length;
  $: behind = repos.filter((r) => r.behind > 0).length;
  $: clean = repos.filter(
    (r) => r.isGit && r.loaded && !r.dirty && !r.errMsg && !(r.behind > 0)
  ).length;

  $: langCounts = (() => {
    const m = new Map<string, number>();
    for (const r of repos) {
      const l = r.language;
      if (!l) continue;
      m.set(l, (m.get(l) || 0) + 1);
    }
    return [...m.entries()].sort((a, b) => b[1] - a[1]);
  })();
  $: langTotal = langCounts.reduce((s, [, c]) => s + c, 0);
</script>

<section class="stats">
  <div class="stat-tiles">
    <div class="stat-tile">
      <span class="stat-dot all"></span>
      <span class="stat-num">{total}</span>
      <span class="stat-label">total</span>
    </div>
    <div class="stat-tile">
      <span class="stat-dot ok"></span>
      <span class="stat-num">{clean}</span>
      <span class="stat-label">clean</span>
    </div>
    <div class="stat-tile">
      <span class="stat-dot dirty"></span>
      <span class="stat-num">{dirty}</span>
      <span class="stat-label">dirty</span>
    </div>
    <div class="stat-tile">
      <span class="stat-dot err"></span>
      <span class="stat-num">{behind}</span>
      <span class="stat-label">behind</span>
    </div>
  </div>

  <div class="lang-dist">
    {#if langTotal > 0}
      <div class="lang-bar">
        {#each langCounts as [lang, count], i (lang)}
          <span
            class="lang-seg"
            style="width:{(count / langTotal) * 100}%; background:{colorFor(lang, i)}"
            title="{lang}: {count}"
          ></span>
        {/each}
      </div>
      <div class="lang-legend">
        {#each langCounts as [lang, count], i (lang)}
          <span class="lang-item">
            <span class="lang-swatch" style="background:{colorFor(lang, i)}"></span>
            {lang}
            <span class="lang-count">{count}</span>
          </span>
        {/each}
      </div>
    {:else}
      <div class="lang-empty">No language data yet</div>
    {/if}
  </div>
</section>
