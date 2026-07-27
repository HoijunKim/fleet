<script lang="ts">
  import { Reflog, RestoreReflog } from "../../wailsjs/go/main/App";
  import { toastSuccess, toastError } from "./toasts";

  export let path: string;
  export let name = "";
  export let onChanged: (path: string) => void;

  let open = false;
  let loading = false;
  let restoring = false;
  let entries: { hash: string; ref: string; subject: string; when: string }[] = [];

  async function load() {
    loading = true;
    try {
      entries = (await Reflog(path, 30)) || [];
    } catch {
      entries = [];
    } finally {
      loading = false;
    }
  }

  function toggle() {
    open = !open;
    if (open) load();
  }

  async function restore(ref: string) {
    if (restoring) return;
    if (!confirm(`Move the current branch to ${ref}? Commits after it stay recoverable in the reflog.`)) return;
    restoring = true;
    try {
      const err = await RestoreReflog(path, ref);
      if (err) { toastError("Restore: " + err); return; } // e.g. dirty-tree refusal; keep the picker open
      toastSuccess("Moved " + name + " to " + ref);
      open = false;
      onChanged(path);
    } finally {
      restoring = false;
    }
  }
</script>

<div class="reflog">
  <button class="btn btn-secondary btn-sm" on:click={toggle} disabled={restoring} title="Move the branch back to a previous point (reflog)">
    History
  </button>
  {#if open}
    <div class="reflog-pop">
      {#if loading}
        <div class="reflog-empty"><span class="spinner"></span> loading</div>
      {:else if entries.length === 0}
        <div class="reflog-empty">No reflog entries</div>
      {:else}
        <ul class="reflog-list">
          {#each entries as e (e.ref)}
            <li>
              <button class="reflog-entry" on:click={() => restore(e.ref)} disabled={restoring} title={e.hash}>
                <span class="reflog-ref mono">{e.ref}</span>
                <span class="reflog-subject">{e.subject}</span>
                <span class="reflog-when">{e.when}</span>
              </button>
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  {/if}
</div>

<style>
  .reflog { position: relative; display: inline-block; }
  .reflog-pop {
    position: absolute;
    z-index: 20;
    top: calc(100% + 4px);
    left: 0;
    width: 360px;
    max-width: 82vw;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--r-btn);
    box-shadow: var(--shadow-pop, 0 8px 24px rgba(0, 0, 0, 0.25));
    padding: 8px;
  }
  .reflog-list { list-style: none; margin: 0; padding: 0; max-height: 240px; overflow-y: auto; }
  .reflog-list li { display: flex; }
  .reflog-entry {
    display: grid;
    grid-template-columns: auto 1fr auto;
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
  .reflog-entry:hover { background: var(--accent-soft); }
  .reflog-entry:disabled { opacity: 0.6; cursor: default; }
  .reflog-ref { color: var(--faint); flex: none; }
  .reflog-subject { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .reflog-when { color: var(--faint); font-variant-numeric: tabular-nums; flex: none; }
  .reflog-empty { font-size: 12px; color: var(--muted); padding: 6px; }
</style>
