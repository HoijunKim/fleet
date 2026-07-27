<script lang="ts">
  import { Branches, LogRef, CherryPick } from "../../wailsjs/go/main/App";
  import { toastSuccess, toastError } from "./toasts";

  export let path: string;
  export let name = "";
  export let onChanged: (path: string) => void;

  let open = false;
  let loading = false;
  let picking = false;
  let branches: string[] = [];
  let current = "";
  let source = ""; // chosen source branch
  let commits: { hash: string; message: string; author: string; when: string }[] = [];

  async function loadBranches() {
    loading = true;
    try {
      const info = await Branches(path);
      current = info.current || "";
      // Cherry-pick applies a commit from ANOTHER branch, so hide the current one.
      branches = (info.all || []).filter((b) => b !== current);
      source = branches[0] || "";
      if (source) await loadCommits();
    } catch {
      branches = [];
    } finally {
      loading = false;
    }
  }

  async function loadCommits() {
    if (!source) { commits = []; return; }
    const s = source;
    try {
      const res = await LogRef(path, s, 25);
      if (s !== source) return; // dropped a stale result
      commits = res || [];
    } catch {
      commits = [];
    }
  }

  function toggle() {
    open = !open;
    if (open) loadBranches();
  }

  async function onSourceChange() {
    await loadCommits();
  }

  async function pick(hash: string) {
    if (picking) return;
    picking = true;
    try {
      const err = await CherryPick(path, hash);
      // A conflict leaves the pick in progress; CommitBox's conflict panel takes
      // over on the rescan, so a conflict message here is informational only.
      if (err) toastError("Cherry-pick " + name + ": " + err);
      else toastSuccess("Cherry-picked onto " + name);
      open = false;
      onChanged(path);
    } finally {
      picking = false;
    }
  }
</script>

<div class="cherry">
  <button class="btn btn-secondary btn-sm" on:click={toggle} disabled={picking} title="Apply a commit from another branch">
    Cherry-pick
  </button>
  {#if open}
    <div class="cherry-pop">
      {#if loading}
        <div class="cherry-empty"><span class="spinner"></span> loading</div>
      {:else if branches.length === 0}
        <div class="cherry-empty">No other branches to pick from</div>
      {:else}
        <label class="cherry-src">
          <span>From</span>
          <select class="input" bind:value={source} on:change={onSourceChange}>
            {#each branches as b (b)}<option value={b}>{b}</option>{/each}
          </select>
        </label>
        <ul class="cherry-commits">
          {#each commits as c (c.hash)}
            <li>
              <button class="cherry-commit" on:click={() => pick(c.hash)} disabled={picking} title={c.hash}>
                <span class="cherry-hash mono">{c.hash.slice(0, 7)}</span>
                <span class="cherry-msg">{c.message}</span>
              </button>
            </li>
          {/each}
          {#if commits.length === 0}
            <li class="cherry-empty">No commits</li>
          {/if}
        </ul>
      {/if}
    </div>
  {/if}
</div>

<style>
  .cherry { position: relative; display: inline-block; }
  .cherry-pop {
    position: absolute;
    z-index: 20;
    top: calc(100% + 4px);
    left: 0;
    width: 320px;
    max-width: 80vw;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--r-btn);
    box-shadow: var(--shadow-pop, 0 8px 24px rgba(0, 0, 0, 0.25));
    padding: 8px;
  }
  .cherry-src { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--muted); margin-bottom: 6px; }
  .cherry-src select { flex: 1; }
  .cherry-commits { list-style: none; margin: 0; padding: 0; max-height: 220px; overflow-y: auto; }
  .cherry-commits li { display: flex; }
  .cherry-commit {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    border-radius: var(--r-btn);
    padding: 4px 6px;
    cursor: pointer;
    color: var(--text);
    font: inherit;
    font-size: 12px;
  }
  .cherry-commit:hover { background: var(--accent-soft); }
  .cherry-commit:disabled { opacity: 0.6; cursor: default; }
  .cherry-hash { color: var(--faint); flex: none; }
  .cherry-msg { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .cherry-empty { font-size: 12px; color: var(--muted); padding: 6px; }
</style>
