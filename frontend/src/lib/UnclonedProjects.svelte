<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { SyncedUncloned, CloneUncloned } from "../../wailsjs/go/main/App";
  import { EventsOn } from "../../wailsjs/runtime/runtime";
  import { toastSuccess, toastError } from "./toasts";

  // Called after a successful clone so the parent can rescan projects.
  export let onCloned: () => void = () => {};

  let items: any[] = [];
  let cloningId = "";
  let off: (() => void) | undefined;

  async function refresh() {
    try {
      items = await SyncedUncloned();
    } catch {
      items = [];
    }
  }
  // The Projects view re-mounts on navigation, so onMount re-pulls the list each
  // time the user returns to it; a live listener also catches detached records
  // that arrive via a background sync while the view is already open.
  onMount(() => {
    refresh();
    off = EventsOn("sync:changed", refresh);
  });
  onDestroy(() => off?.());

  async function clone(it: any) {
    if (cloningId) return;
    cloningId = it.id;
    try {
      const err = await CloneUncloned(it.id, "");
      if (err) {
        toastError("Clone: " + err);
      } else {
        toastSuccess("Cloned " + (it.name || "project"));
        await refresh();
        onCloned();
      }
    } finally {
      cloningId = "";
    }
  }
</script>

{#if items.length > 0}
  <div class="uncloned">
    <div class="uncloned-head">
      <span class="section-label">Synced from other devices</span>
      <span class="uncloned-sub">{items.length} not cloned on this machine</span>
    </div>
    <ul class="uncloned-list">
      {#each items as it (it.id)}
        <li class="uncloned-row">
          <span class="uncloned-name">{it.name || "(untitled)"}</span>
          {#if it.taskCount > 0}
            <span class="uncloned-meta">{it.taskCount} task{it.taskCount === 1 ? "" : "s"}</span>
          {/if}
          {#if it.remote}
            <span class="uncloned-remote mono">{it.remote}</span>
          {/if}
          <div class="uncloned-spacer"></div>
          {#if it.canClone}
            <button
              class="btn btn-secondary btn-sm"
              on:click={() => clone(it)}
              disabled={cloningId === it.id}
            >{cloningId === it.id ? "Cloning..." : "Clone"}</button>
          {:else}
            <span class="uncloned-nocl" title="No git remote was recorded for this project">no remote</span>
          {/if}
        </li>
      {/each}
    </ul>
  </div>
{/if}
