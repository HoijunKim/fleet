<script lang="ts">
  export let filter: string = "";
  export let repos: any[] = [];
  export let loadingCount: number = 0;
  export let statusFilter: "all" | "dirty" | "behind" = "all";
  export let filterInput: HTMLInputElement | undefined = undefined;
  export let onStatus: (s: "all" | "dirty" | "behind") => void;
  export let onFetchAll: () => void;
  export let onRefresh: () => void;
  export let onOpenSettings: () => void;
  export let onOpenPalette: () => void;

  $: dirty = repos.filter((r) => r.dirty).length;
  $: behind = repos.filter((r) => r.behind > 0).length;
</script>

<header class="toolbar">
  <div class="brand">
    <span class="brand-dot"></span>
    <span class="brand-name">fleet</span>
  </div>

  <div class="toolbar-search">
    <input
      class="input"
      type="text"
      placeholder="Filter repositories..."
      bind:value={filter}
      bind:this={filterInput}
      aria-label="Filter repositories by name"
    />
  </div>

  <div class="filter-chips">
    <button class="fchip" class:active={statusFilter === "all"} on:click={() => onStatus("all")}>
      All
    </button>
    <button class="fchip" class:active={statusFilter === "dirty"} on:click={() => onStatus("dirty")}>
      Dirty <span class="fchip-n">{dirty}</span>
    </button>
    <button class="fchip" class:active={statusFilter === "behind"} on:click={() => onStatus("behind")}>
      Behind <span class="fchip-n">{behind}</span>
    </button>
  </div>

  <div class="toolbar-spacer"></div>

  <div class="toolbar-right">
    {#if loadingCount > 0}
      <span class="loading-note">
        <span class="spinner"></span>
        loading {loadingCount}
      </span>
    {/if}

    <button class="palette-btn" on:click={onOpenPalette} title="Command palette">
      <span class="palette-label">Search</span>
      <span class="palette-kbd">Ctrl K</span>
    </button>

    <button class="icon-btn" on:click={onOpenSettings} title="Settings" aria-label="Settings">
      <span class="gear"></span>
    </button>

    <button class="btn btn-secondary" on:click={onRefresh}>Refresh</button>
    <button class="btn btn-primary" on:click={onFetchAll}>Fetch All</button>
  </div>
</header>
