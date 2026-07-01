<script lang="ts">
  import { Branches, Checkout } from "../../wailsjs/go/main/App";
  import { toastSuccess, toastError } from "./toasts";

  export let path: string;
  export let name = "";
  export let onChanged: (path: string) => void;

  let current = "";
  let all: string[] = [];
  let loading = false;
  let switching = false;
  let open = false;
  let loadedPath = "";

  // Popover position (fixed, so the detail panel's overflow never clips it).
  let mx = 0;
  let my = 0;
  let mw = 0;
  let triggerEl: HTMLButtonElement;

  // Lazily (re)load branches whenever the selected repo changes.
  $: if (path && path !== loadedPath) {
    loadedPath = path;
    open = false;
    load();
  }

  async function load() {
    const p = path;
    loading = true;
    try {
      const info = await Branches(p);
      if (p !== path) return; // selection changed during await -> drop stale result
      current = info.current || "";
      all = (info.all || []).slice();
    } catch {
      if (p !== path) return;
      current = "";
      all = [];
    } finally {
      if (p === path) loading = false;
    }
  }

  function toggle() {
    if (open) {
      open = false;
      return;
    }
    const r = triggerEl.getBoundingClientRect();
    mx = r.left;
    my = r.bottom + 4;
    mw = r.width;
    open = true;
  }

  async function choose(b: string) {
    open = false;
    if (b === current || switching) return;
    switching = true;
    try {
      const err = await Checkout(path, b);
      if (err) {
        toastError("Checkout " + b + ": " + err);
      } else {
        current = b;
        toastSuccess("Switched " + name + " to " + b);
        onChanged(path);
      }
    } finally {
      switching = false;
    }
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === "Escape" && open) {
      e.preventDefault();
      open = false;
    }
  }
</script>

<svelte:window on:keydown={onKey} />

<div class="branch-menu">
  <button
    class="branch-trigger"
    bind:this={triggerEl}
    on:click={toggle}
    disabled={loading || switching || all.length === 0}
    title="Switch branch"
    aria-haspopup="listbox"
    aria-expanded={open}
  >
    {#if switching}
      <span class="spinner"></span>
    {/if}
    <span class="branch-cur">{loading ? "loading" : current || "no branch"}</span>
    <span class="branch-caret"></span>
  </button>
</div>

{#if open}
  <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
  <div
    class="menu-backdrop"
    on:click={() => (open = false)}
    on:contextmenu|preventDefault={() => (open = false)}
  ></div>
  <div class="branch-pop" style="left:{mx}px; top:{my}px; min-width:{mw}px" role="listbox">
    {#each all as b (b)}
      <button
        class="branch-opt"
        class:current={b === current}
        role="option"
        aria-selected={b === current}
        on:click={() => choose(b)}
      >
        <span class="branch-dot"></span>
        <span class="branch-opt-name">{b}</span>
      </button>
    {/each}
  </div>
{/if}
