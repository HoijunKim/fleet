<script lang="ts">
  export let filter: string = "";
  export let repos: any[] = [];
  export let onFetchAll: () => void;
  export let onRefresh: () => void;

  $: dirty = repos.filter((r) => r.dirty).length;
  $: behind = repos.filter((r) => r.behind > 0).length;
</script>

<div class="toolbar">
  <strong>fleet</strong>
  <input placeholder="filter by name" bind:value={filter} />
  <span class="counts">repos {repos.length} | dirty {dirty} | behind {behind}</span>
  <button on:click={onRefresh}>Refresh</button>
  <button on:click={onFetchAll}>Fetch All</button>
</div>
