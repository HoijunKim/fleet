<script lang="ts">
  export let filter: string = "";
  export let repos: any[] = [];
  export let loadingCount: number = 0;
  export let view: "overview" | "projects" = "overview";
  export let onView: (v: "overview" | "projects") => void;
  export let statusFilter: "all" | "dirty" | "behind" = "all";
  export let pmStatusFilter: "all" | "active" | "paused" | "done" = "all";
  export let highPriorityOnly: boolean = false;
  export let filterInput: HTMLInputElement | undefined = undefined;
  export let onStatus: (s: "all" | "dirty" | "behind") => void;
  export let onPmStatus: (s: "all" | "active" | "paused" | "done") => void;
  export let onHighPriorityToggle: () => void;
  export let onFetchAll: () => void;
  export let onRefresh: () => void;
  export let onOpenSettings: () => void;
  export let onOpenPalette: () => void;
  export let onAddProject: () => void;

  $: dirty = repos.filter((r) => r.dirty).length;
  $: behind = repos.filter((r) => r.behind > 0).length;
</script>

<header class="toolbar">
  <div class="brand">
    <span class="brand-dot"></span>
    <span class="brand-name">fleet</span>
  </div>

  <div class="view-tabs">
    <button class="view-tab" class:active={view === "overview"} on:click={() => onView("overview")}>
      Overview
    </button>
    <button class="view-tab" class:active={view === "projects"} on:click={() => onView("projects")}>
      Projects
    </button>
  </div>

  <div class="toolbar-search">
    <input
      class="input"
      type="text"
      placeholder="Filter projects..."
      bind:value={filter}
      bind:this={filterInput}
      aria-label="Filter projects by name"
    />
  </div>

  {#if view === "projects"}
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

    <div class="pm-filters">
      <select
        class="input toolbar-select"
        aria-label="Filter by project status"
        bind:value={pmStatusFilter}
        on:change={() => onPmStatus(pmStatusFilter)}
      >
        <option value="all">All statuses</option>
        <option value="active">Active</option>
        <option value="paused">Paused</option>
        <option value="done">Done</option>
      </select>

      <button
        class="fchip"
        class:active={highPriorityOnly}
        on:click={onHighPriorityToggle}
        aria-pressed={highPriorityOnly}
      >
        High priority
      </button>
    </div>
  {/if}

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

    <button class="btn btn-secondary" on:click={onAddProject}>+ Project</button>
    <button class="btn btn-secondary" on:click={onRefresh}>Refresh</button>
    <button class="btn btn-primary" on:click={onFetchAll}>Fetch All</button>
  </div>
</header>
