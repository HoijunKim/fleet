<script lang="ts">
  import { StashList, Stash, StashPop, StashApply, StashDrop } from "../../wailsjs/go/main/App";
  import { toastSuccess, toastError } from "./toasts";

  export let path: string;
  export let name = "";
  export let onChanged: (path: string) => void;

  let open = false;
  let entries: string[] = [];
  let busy = false;
  let loadedPath = "";
  let confirmDrop = -1; // index armed for a Drop confirm, or -1

  // Reset when the selected repo changes; reload only if currently expanded.
  $: if (path !== loadedPath) {
    loadedPath = path;
    entries = [];
    confirmDrop = -1;
    if (open) load();
  }

  async function toggle() {
    open = !open;
    confirmDrop = -1; // any armed Drop is void once the panel is toggled
    if (open) await load();
  }

  async function load() {
    const p = path;
    confirmDrop = -1; // indices may shift; never carry an armed Drop across a reload
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
    confirmDrop = -1;
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
    confirmDrop = -1;
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

  async function doApply(i: number) {
    if (busy) return;
    busy = true;
    try {
      const err = await StashApply(path, i);
      if (err) toastError("Stash apply: " + err);
      else { toastSuccess("Applied stash@{" + i + "}"); onChanged(path); }
      await load();
    } finally {
      busy = false;
    }
  }

  async function doDrop(i: number) {
    if (busy) return;
    busy = true;
    confirmDrop = -1;
    try {
      const err = await StashDrop(path, i);
      if (err) toastError("Stash drop: " + err);
      else toastSuccess("Dropped stash@{" + i + "}");
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
            <div class="stash-row">
              <span class="stash-entry" title={s}>{s}</span>
              {#if confirmDrop === i}
                <button class="stash-mini" on:click={() => (confirmDrop = -1)}>Cancel</button>
                <button class="stash-mini stash-danger" on:click={() => doDrop(i)} disabled={busy}>Drop</button>
              {:else}
                <button class="stash-mini" on:click={() => doApply(i)} disabled={busy}>Apply</button>
                <button class="stash-mini stash-danger" on:click={() => (confirmDrop = i)} disabled={busy}>Drop</button>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
</div>
