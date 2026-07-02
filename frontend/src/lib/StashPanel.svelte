<script lang="ts">
  import { StashList, Stash, StashPop } from "../../wailsjs/go/main/App";
  import { toastSuccess, toastError } from "./toasts";

  export let path: string;
  export let name = "";
  export let onChanged: (path: string) => void;

  let open = false;
  let entries: string[] = [];
  let busy = false;
  let loadedPath = "";

  // Reset when the selected repo changes; reload only if currently expanded.
  $: if (path !== loadedPath) {
    loadedPath = path;
    entries = [];
    if (open) load();
  }

  async function toggle() {
    open = !open;
    if (open) await load();
  }

  async function load() {
    const p = path;
    try {
      const res = await StashList(p);
      if (p !== path) return; // selection changed during await -> drop stale result
      entries = res || [];
    } catch {
      if (p !== path) return;
      entries = [];
    }
  }

  async function doStash() {
    if (busy) return;
    busy = true;
    try {
      const err = await Stash(path);
      if (err) toastError("Stash " + name + ": " + err);
      else {
        toastSuccess("Stashed " + name);
        onChanged(path);
      }
      await load();
    } finally {
      busy = false;
    }
  }

  async function doPop() {
    if (busy) return;
    busy = true;
    try {
      const err = await StashPop(path);
      if (err) toastError("Stash pop " + name + ": " + err);
      else {
        toastSuccess("Popped stash " + name);
        onChanged(path);
      }
      await load();
    } finally {
      busy = false;
    }
  }
</script>

<div class="collapse">
  <button class="collapse-head" on:click={toggle} aria-expanded={open}>
    <span class="collapse-caret" class:open></span>
    <span class="section-label">Stash</span>
  </button>

  {#if open}
    <div class="stash">
      <div class="stash-actions">
        <button class="btn btn-secondary btn-sm" on:click={doStash} disabled={busy}>Stash</button>
        <button class="btn btn-secondary btn-sm" on:click={doPop} disabled={busy || entries.length === 0}>
          Pop
        </button>
      </div>

      {#if entries.length === 0}
        <div class="stash-empty">No stash entries</div>
      {:else}
        <div class="stash-list">
          {#each entries as s, i (i)}
            <span class="stash-entry">{s}</span>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
</div>
