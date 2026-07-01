<script lang="ts">
  export let filter: string = "";
  export let repos: any[] = [];
  export let loadingCount: number = 0;
  export let onFetchAll: () => void;
  export let onRefresh: () => void;

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
      aria-label="Filter repositories by name"
    />
  </div>

  <div class="toolbar-spacer"></div>

  <div class="toolbar-right">
    {#if loadingCount > 0}
      <span class="loading-note">
        <span class="spinner"></span>
        loading {loadingCount}
      </span>
    {/if}

    <div class="chips">
      <span class="chip">
        <span class="chip-num">{repos.length}</span> repos
      </span>
      {#if dirty > 0}
        <span class="chip dirty">
          <span class="chip-num">{dirty}</span> dirty
        </span>
      {/if}
      {#if behind > 0}
        <span class="chip behind">
          <span class="chip-num">{behind}</span> behind
        </span>
      {/if}
    </div>

    <button class="btn btn-secondary" on:click={onRefresh}>Refresh</button>
    <button class="btn btn-primary" on:click={onFetchAll}>Fetch All</button>
  </div>
</header>
