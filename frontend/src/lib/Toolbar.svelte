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

  // Bulk git actions collapse into one dropdown so the bar stays a single row.
  let actionsOpen = false;
  function runAction(fn: () => void) {
    actionsOpen = false;
    fn();
  }
  function onWindowClick(e: MouseEvent) {
    if (!actionsOpen) return;
    const t = e.target as HTMLElement;
    if (!t.closest(".tb-actions")) actionsOpen = false;
  }
</script>

<svelte:window on:click={onWindowClick} />

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

    <!-- Bulk git actions collapse into one dropdown on the repo-centric views -->
    {#if view === "projects" || view === "overview"}
      <div class="tb-actions">
        <button class="btn btn-primary tb-actions-btn" on:click|stopPropagation={() => (actionsOpen = !actionsOpen)} aria-expanded={actionsOpen}>
          Actions <span class="tb-caret">v</span>
        </button>
        {#if actionsOpen}
          <div class="tb-menu">
            <button class="tb-menu-item" on:click={() => runAction(onFetchAll)}>Fetch all</button>
            <button class="tb-menu-item" on:click={() => runAction(onPullAll)}>Pull all</button>
            <button class="tb-menu-item" on:click={() => runAction(onPushAll)}>Push all</button>
            <button class="tb-menu-item" on:click={() => runAction(onRefresh)}>Refresh</button>
            <div class="tb-menu-div"></div>
            <button class="tb-menu-item" on:click={() => runAction(onAddProject)}>+ New project</button>
          </div>
        {/if}
      </div>
      <span class="toolbar-div"></span>
    {/if}

    <!-- Global utility: jump, search, settings - always at the far right -->
    <button class="icon-btn" on:click={onOpenPalette} title="Command palette (Ctrl K)" aria-label="Command palette">
      <span class="ic-jump">K</span>
    </button>
    <button class="icon-btn" on:click={onOpenSearch} title="Search across repos (Ctrl Shift F)" aria-label="Search">
      <span class="ic-search"></span>
    </button>
    <button class="icon-btn" on:click={onOpenSettings} title="Settings" aria-label="Settings">
      <span class="gear"></span>
    </button>
  </div>
</header>
