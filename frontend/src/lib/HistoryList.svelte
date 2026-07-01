<script lang="ts">
  import { Log } from "../../wailsjs/go/main/App";

  export let path: string;

  let open = false;
  let loading = false;
  let commits: any[] = [];
  let loadedPath = "";

  // Reset when the selected repo changes; reload only if already expanded.
  $: if (path !== loadedPath) {
    loadedPath = path;
    commits = [];
    if (open) load();
  }

  async function toggle() {
    open = !open;
    if (open && commits.length === 0) await load();
  }

  async function load() {
    const p = path;
    loading = true;
    try {
      const res = await Log(p, 15);
      if (p !== path) return; // selection changed during await -> drop stale result
      commits = res;
    } catch {
      if (p !== path) return;
      commits = [];
    } finally {
      if (p === path) loading = false;
    }
  }
</script>

<div class="collapse">
  <button class="collapse-head" on:click={toggle} aria-expanded={open}>
    <span class="collapse-caret" class:open></span>
    <span class="section-label">Recent commits</span>
  </button>

  {#if open}
    {#if loading}
      <div class="modal-loading"><span class="spinner"></span> loading history</div>
    {:else if commits.length === 0}
      <div class="hist-empty">No commits</div>
    {:else}
      <div class="hist-list">
        {#each commits as c (c.hash)}
          <div class="hist-row">
            <span class="hist-hash">{c.hash.slice(0, 7)}</span>
            <div class="hist-main">
              <span class="hist-msg">{c.message}</span>
              <span class="hist-meta">{c.author}{c.when ? " - " + c.when : ""}</span>
            </div>
          </div>
        {/each}
      </div>
    {/if}
  {/if}
</div>
