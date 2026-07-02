<script lang="ts">
  import { GetConfig, SaveConfig } from "../../wailsjs/go/main/App";
  import type { config } from "../../wailsjs/go/models";
  import { toastSuccess, toastError } from "./toasts";

  export let onClose: () => void;
  // Called after a successful save so the parent can rescan / reset auto-fetch.
  export let onSaved: () => void;

  let cfg: config.Config | null = null;
  let newRoot = "";
  let saveErr = "";
  let saving = false;
  let loading = true;

  // Load the real Config object so we bind to whatever field names it declares.
  async function load() {
    loading = true;
    saveErr = "";
    try {
      cfg = await GetConfig();
      if (!cfg.Roots) cfg.Roots = [];
    } catch (e) {
      toastError("Load failed: " + String(e));
    } finally {
      loading = false;
    }
  }

  load();

  function addRoot() {
    const v = newRoot.trim();
    if (!v || !cfg) return;
    cfg.Roots = [...cfg.Roots, v];
    newRoot = "";
  }

  function removeRoot(i: number) {
    if (!cfg) return;
    cfg.Roots = cfg.Roots.filter((_, idx) => idx !== i);
  }

  function onRootKey(e: KeyboardEvent) {
    if (e.key === "Enter") {
      e.preventDefault();
      addRoot();
    }
  }

  async function save() {
    if (!cfg) return;
    saving = true;
    saveErr = "";
    try {
      cfg.ScanDepth = Number(cfg.ScanDepth) || 0;
      cfg.AutoFetchMinutes = Number(cfg.AutoFetchMinutes) || 0;
      const err = await SaveConfig(cfg);
      if (err) {
        saveErr = err;
        toastError("Save failed: " + err);
        return;
      }
      toastSuccess("Settings saved");
      onSaved();
      onClose();
    } catch (e) {
      saveErr = String(e);
      toastError("Save failed: " + String(e));
    } finally {
      saving = false;
    }
  }
</script>

<!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
<div class="overlay" on:click={onClose}>
  <div class="modal" on:click|stopPropagation>
    <div class="modal-head">
      <span class="brand-dot"></span>
      <h3 class="modal-title">Settings</h3>
      <button class="btn btn-secondary btn-sm modal-close" on:click={onClose} aria-label="Close">x</button>
    </div>

    <div class="modal-body">
      {#if loading || !cfg}
        <div class="modal-loading"><span class="spinner"></span> loading config</div>
      {:else}
        <div class="field">
          <label class="field-label" for="set-roots">Roots</label>
          <div class="root-list">
            {#each cfg.Roots as root, i (i)}
              <div class="root-row">
                <span class="root-path mono">{root}</span>
                <button class="root-x" on:click={() => removeRoot(i)} aria-label="Remove root">x</button>
              </div>
            {/each}
            {#if cfg.Roots.length === 0}
              <div class="root-empty">No roots configured</div>
            {/if}
          </div>
          <div class="root-add">
            <input
              id="set-roots"
              class="input mono"
              type="text"
              placeholder="C:\path\to\projects"
              bind:value={newRoot}
              on:keydown={onRootKey}
            />
            <button class="btn btn-secondary btn-sm" on:click={addRoot} disabled={!newRoot.trim()}>Add</button>
          </div>
        </div>

        <div class="field-grid">
          <div class="field">
            <label class="field-label" for="set-editor">Editor</label>
            <input id="set-editor" class="input" type="text" placeholder="code" bind:value={cfg.Editor} />
          </div>
          <div class="field">
            <label class="field-label" for="set-terminal">Terminal</label>
            <input id="set-terminal" class="input" type="text" placeholder="wt" bind:value={cfg.Terminal} />
          </div>
          <div class="field">
            <label class="field-label" for="set-depth">Scan depth</label>
            <input id="set-depth" class="input" type="number" min="0" bind:value={cfg.ScanDepth} />
          </div>
          <div class="field">
            <label class="field-label" for="set-autofetch">Auto-fetch minutes</label>
            <input id="set-autofetch" class="input" type="number" min="0" bind:value={cfg.AutoFetchMinutes} />
          </div>
        </div>

        <div class="field">
          <label class="toggle">
            <input type="checkbox" bind:checked={cfg.ShowNonGit} />
            <span class="toggle-track"><span class="toggle-thumb"></span></span>
            <span class="toggle-text">Show non-git folders</span>
          </label>
        </div>

        {#if saveErr}
          <div class="save-err">{saveErr}</div>
        {/if}
      {/if}
    </div>

    <div class="modal-foot">
      <button class="btn btn-secondary" on:click={onClose}>Cancel</button>
      <button class="btn btn-primary" on:click={save} disabled={saving || loading || !cfg}>
        {#if saving}<span class="spinner"></span> saving{:else}Save{/if}
      </button>
    </div>
  </div>
</div>
