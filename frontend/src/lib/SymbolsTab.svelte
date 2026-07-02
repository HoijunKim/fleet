<script lang="ts">
  import { RepoSymbols } from "../../wailsjs/go/main/App";

  // Stale-drop key: the repo path. Reload whenever the selected project
  // changes, mirroring the lazy-load-on-select guard used by GitHubBadge.
  export let path: string = "";

  let loadedPath = "";
  let loading = false;
  let view: {
    goMainPkgs?: string[];
    goExported?: string[];
    npmScripts?: string[];
    npmBin?: string[];
    truncated?: boolean;
  } | null = null;

  // Reset state whenever the selected repo (path) changes, then kick off a
  // fresh load for the new path (if any).
  $: if (path !== loadedPath) {
    loadedPath = path;
    view = null;
    loading = false;
    if (path) load();
  }

  async function load() {
    const p = path;
    loading = true;
    try {
      const v = await RepoSymbols(path);
      if (p !== path) return; // selection changed during await -> drop stale result
      view = v;
    } catch {
      if (p !== path) return;
      view = null;
    } finally {
      if (p === path) loading = false;
    }
  }

  $: goMainPkgs = (view && view.goMainPkgs) || [];
  $: goExported = (view && view.goExported) || [];
  $: npmScripts = (view && view.npmScripts) || [];
  $: npmBin = (view && view.npmBin) || [];
  $: truncated = !!(view && view.truncated);
  $: allEmpty =
    !!view &&
    goMainPkgs.length === 0 &&
    goExported.length === 0 &&
    npmScripts.length === 0 &&
    npmBin.length === 0;
</script>

<div class="symbols-tab">
  {#if loading && !view}
    <div class="dl-row">
      <span class="dl-value">Loading...</span>
    </div>
  {:else if view}
    {#if truncated}
      <div class="symbols-note">showing first 400 files</div>
    {/if}
    {#if allEmpty}
      <div class="dl-row">
        <span class="dl-value">No symbols found</span>
      </div>
    {:else}
      {#if goMainPkgs.length}
        <div class="symbols-group">
          <span class="dl-label">Go main packages</span>
          <ul class="symbols-list">
            {#each goMainPkgs as s}
              <li class="mono">{s}</li>
            {/each}
          </ul>
        </div>
      {/if}
      {#if goExported.length}
        <div class="symbols-group">
          <span class="dl-label">Exported</span>
          <ul class="symbols-list">
            {#each goExported as s}
              <li class="mono">{s}</li>
            {/each}
          </ul>
        </div>
      {/if}
      {#if npmScripts.length}
        <div class="symbols-group">
          <span class="dl-label">npm scripts</span>
          <ul class="symbols-list">
            {#each npmScripts as s}
              <li class="mono">{s}</li>
            {/each}
          </ul>
        </div>
      {/if}
      {#if npmBin.length}
        <div class="symbols-group">
          <span class="dl-label">npm bin</span>
          <ul class="symbols-list">
            {#each npmBin as s}
              <li class="mono">{s}</li>
            {/each}
          </ul>
        </div>
      {/if}
    {/if}
  {/if}
</div>

<style>
  .symbols-tab {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .symbols-note {
    font-size: 11px;
    color: var(--muted);
  }
  .symbols-group {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .symbols-list {
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin: 0;
    padding: 0;
    list-style: none;
    max-height: 200px;
    overflow: auto;
  }
  .symbols-list li {
    font-size: 12px;
    word-break: break-all;
  }
</style>
