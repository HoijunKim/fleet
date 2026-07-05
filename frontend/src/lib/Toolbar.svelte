<script lang="ts">
  export let loadingCount: number = 0;
  // Count of code projects with unfetched upstream commits (behind > 0).
  // Computed once in App.svelte as stats.behind - not recomputed here so the
  // toolbar never triggers its own GitHubInfo/git calls.
  export let remoteChanges: number = 0;
  export let onRemoteChanges: (() => void) | undefined = undefined;
  export let view: "today" | "overview" | "projects" | "graph" = "today";
  export let onView: (v: "today" | "overview" | "projects" | "graph") => void;
  export let onFetchAll: () => void;
  export let onPullAll: () => void;
  export let onPushAll: () => void;
  export let onRefresh: () => void;
  export let onOpenSettings: () => void;
  export let onOpenPalette: () => void;
  export let onOpenSearch: () => void;
  export let onAddProject: () => void;
</script>

<header class="toolbar">
  <div class="brand">
    <span class="brand-dot"></span>
    <span class="brand-name">fleet</span>
  </div>

  <div class="view-tabs">
    <button class="view-tab" class:active={view === "today"} on:click={() => onView("today")}>
      Today
    </button>
    <button class="view-tab" class:active={view === "overview"} on:click={() => onView("overview")}>
      Overview
    </button>
    <button class="view-tab" class:active={view === "projects"} on:click={() => onView("projects")}>
      Projects
    </button>
    <button class="view-tab" class:active={view === "graph"} on:click={() => onView("graph")}>
      Graph
    </button>
  </div>


  <div class="toolbar-spacer"></div>

  <div class="toolbar-right">
    {#if remoteChanges > 0}
      <button
        class="remote-changes-btn"
        on:click={() => onRemoteChanges && onRemoteChanges()}
        title="{remoteChanges} repo(s) have unfetched upstream commits"
      >
        <span class="remote-changes-dot"></span>
        remote changes: {remoteChanges}
      </button>
    {/if}

    {#if loadingCount > 0}
      <span class="loading-note">
        <span class="spinner"></span>
        loading {loadingCount}
      </span>
    {/if}

    <!-- Git bulk actions live only on the repo-centric views -->
    {#if view === "projects" || view === "overview"}
      <button class="btn btn-secondary" on:click={onAddProject}>+ Project</button>
      <button class="btn btn-secondary" on:click={onRefresh}>Refresh</button>
      <button class="btn btn-secondary" on:click={onPullAll}>Pull all</button>
      <button class="btn btn-secondary" on:click={onPushAll}>Push all</button>
      <button class="btn btn-primary" on:click={onFetchAll}>Fetch All</button>
      <span class="toolbar-div"></span>
    {/if}

    <!-- Global utility: jump, search, settings - always at the far right -->
    <button class="palette-btn" on:click={onOpenPalette} title="Command palette (Ctrl K)">
      <span class="palette-label">Jump</span>
      <span class="palette-kbd">Ctrl K</span>
    </button>
    <button class="palette-btn" on:click={onOpenSearch} title="Search across repos (Ctrl Shift F)">
      <span class="palette-label">Search</span>
      <span class="palette-kbd">Ctrl Shift F</span>
    </button>
    <button class="icon-btn" on:click={onOpenSettings} title="Settings" aria-label="Settings">
      <span class="gear"></span>
    </button>
  </div>
</header>
