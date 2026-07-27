<script lang="ts">
  import { Branches, Checkout, CreateBranch, DeleteBranch, DeleteBranchForce } from "../../wailsjs/go/main/App";
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
  let newName = "";
  let confirmDel = ""; // branch name armed for delete confirm
  let forceDel = ""; // branch name whose safe delete was refused as unmerged

  // Popover position (fixed, so the detail panel's overflow never clips it).
  let mx = 0;
  let my = 0;
  let mw = 0;
  let triggerEl: HTMLButtonElement;

  // Lazily (re)load branches whenever the selected repo changes.
  $: if (path && path !== loadedPath) {
    loadedPath = path;
    open = false;
    confirmDel = "";
    newName = "";
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

  function closeMenu() {
    open = false;
    confirmDel = ""; // an armed delete never survives closing the popover
  }
  function toggle() {
    if (open) {
      closeMenu();
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

  async function createBranch() {
    const b = newName.trim();
    if (!b || switching) return;
    switching = true;
    try {
      const err = await CreateBranch(path, b);
      if (err) {
        toastError("New branch: " + err);
      } else {
        newName = "";
        open = false;
        toastSuccess("Created and switched to " + b);
        await load();
        onChanged(path);
      }
    } finally {
      switching = false;
    }
  }

  async function deleteBranch(b: string) {
    confirmDel = "";
    if (switching) return;
    switching = true;
    try {
      const err = await DeleteBranch(path, b);
      if (err) {
        // git refuses an unmerged branch with "not fully merged"; offer a force
        // delete instead of a dead-end toast.
        if (/not fully merged/i.test(err)) { forceDel = b; }
        else toastError("Delete " + b + ": " + err);
      } else { toastSuccess("Deleted " + b); await load(); }
    } finally {
      switching = false;
    }
  }

  async function forceDelete(b: string) {
    forceDel = "";
    if (switching) return;
    if (!confirm(`Delete unmerged branch "${b}"? Its unmerged commits will be lost.`)) return;
    switching = true;
    try {
      const err = await DeleteBranchForce(path, b);
      if (err) toastError("Force delete " + b + ": " + err);
      else { toastSuccess("Deleted " + b); await load(); }
    } finally {
      switching = false;
    }
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === "Escape" && open) {
      e.preventDefault();
      closeMenu();
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
    on:click={closeMenu}
    on:contextmenu|preventDefault={closeMenu}
  ></div>
  <div class="branch-pop" style="left:{mx}px; top:{my}px; min-width:{mw}px" role="listbox">
    {#each all as b (b)}
      <div class="branch-row" class:current={b === current} role="presentation">
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
        {#if b !== current}
          {#if forceDel === b}
            <button class="branch-del branch-del-yes" title="Branch is unmerged - force delete, losing its commits" on:click|stopPropagation={() => forceDelete(b)}>force?</button>
          {:else if confirmDel === b}
            <button class="branch-del branch-del-yes" title="Confirm delete" on:click|stopPropagation={() => deleteBranch(b)}>del?</button>
          {:else}
            <button class="branch-del" title="Delete branch" aria-label={"Delete " + b} on:click|stopPropagation={() => (confirmDel = b)}>x</button>
          {/if}
        {/if}
      </div>
    {/each}
    <div class="branch-new">
      <input
        class="input branch-new-input"
        type="text"
        placeholder="+ New branch"
        bind:value={newName}
        on:keydown={(e) => { if (e.key === "Enter") { e.preventDefault(); createBranch(); } }}
        aria-label="New branch name"
      />
      <button class="btn btn-secondary btn-sm" on:click={createBranch} disabled={!newName.trim() || switching}>Create</button>
    </div>
  </div>
{/if}
