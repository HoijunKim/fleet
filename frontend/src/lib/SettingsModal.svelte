<script lang="ts">
  import { GetConfig, SaveConfig, AIAvailable } from "../../wailsjs/go/main/App";
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
  let tab: "general" | "ai" | "integrations" = "general";
  let aiOk = false;

  // Load the real Config object so we bind to whatever field names it declares.
  async function load() {
    loading = true;
    saveErr = "";
    try {
      cfg = await GetConfig();
      if (!cfg.Roots) cfg.Roots = [];
      if (!cfg.AIProvider) cfg.AIProvider = "claude";
      refreshAiOk();
    } catch (e) {
      toastError("Load failed: " + String(e));
    } finally {
      loading = false;
    }
  }

  function refreshAiOk() {
    AIAvailable().then((ok) => (aiOk = !!ok)).catch(() => (aiOk = false));
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
  <div class="modal set-modal" on:click|stopPropagation>
    <div class="modal-head">
      <span class="brand-dot"></span>
      <h3 class="modal-title">Settings</h3>
      <button class="btn btn-secondary btn-sm modal-close" on:click={onClose} aria-label="Close">x</button>
    </div>

    {#if !loading && cfg}
      <div class="detail-tabs set-tabs">
        <button class="detail-tab" class:active={tab === "general"} on:click={() => (tab = "general")}>General</button>
        <button class="detail-tab" class:active={tab === "ai"} on:click={() => (tab = "ai")}>AI</button>
        <button class="detail-tab" class:active={tab === "integrations"} on:click={() => (tab = "integrations")}>Integrations</button>
      </div>
    {/if}

    <div class="modal-body">
      {#if loading || !cfg}
        <div class="modal-loading"><span class="spinner"></span> loading config</div>
      {:else if tab === "general"}
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
      {:else if tab === "ai"}
        <div class="field">
          <span class="field-label">Provider for the Today briefing</span>
          <div class="ai-providers">
            <label class="ai-radio" class:on={cfg.AIProvider === "claude"}>
              <input type="radio" bind:group={cfg.AIProvider} value="claude" on:change={refreshAiOk} />
              <span class="ai-radio-name">Claude CLI</span>
              <span class="ai-radio-note">no key - uses your local claude</span>
            </label>
            <label class="ai-radio" class:on={cfg.AIProvider === "openai"}>
              <input type="radio" bind:group={cfg.AIProvider} value="openai" on:change={refreshAiOk} />
              <span class="ai-radio-name">OpenAI</span>
              <span class="ai-radio-note">API key required</span>
            </label>
            <label class="ai-radio" class:on={cfg.AIProvider === "gemini"}>
              <input type="radio" bind:group={cfg.AIProvider} value="gemini" on:change={refreshAiOk} />
              <span class="ai-radio-name">Gemini</span>
              <span class="ai-radio-note">API key required</span>
            </label>
          </div>
          <div class="ai-status" class:ok={aiOk}>
            {aiOk ? "Ready" : "Not ready - install claude, or set the API key below"}
          </div>
        </div>

        <div class="field">
          <label class="field-label" for="set-model">Model (optional)</label>
          <input
            id="set-model"
            class="input mono"
            type="text"
            placeholder={cfg.AIProvider === "openai" ? "gpt-4o" : cfg.AIProvider === "gemini" ? "gemini-2.0-flash" : "(the CLI default)"}
            bind:value={cfg.AIModel}
          />
        </div>

        {#if cfg.AIProvider === "openai"}
          <div class="field">
            <label class="field-label" for="set-openai">OpenAI API key</label>
            <input id="set-openai" class="input mono" type="password" placeholder="sk-..." bind:value={cfg.OpenAIKey} on:input={refreshAiOk} />
          </div>
        {:else if cfg.AIProvider === "gemini"}
          <div class="field">
            <label class="field-label" for="set-gemini">Gemini API key</label>
            <input id="set-gemini" class="input mono" type="password" placeholder="AIza..." bind:value={cfg.GeminiKey} on:input={refreshAiOk} />
          </div>
        {:else}
          <div class="ai-hint">
            The Claude CLI needs no key - fleet shells to your local <span class="mono">claude</span> install.
          </div>
        {/if}
        <div class="ai-hint ai-hint-warn">Keys are stored in plain text in your local config file.</div>
      {:else}
        <div class="field">
          <span class="field-label">GitHub</span>
          <div class="ai-hint">CI / PR / issue badges use the <span class="mono">gh</span> CLI's existing auth. Nothing to configure here.</div>
        </div>
        <div class="field">
          <label class="field-label" for="set-notion">Notion integration token</label>
          <input id="set-notion" class="input mono" type="password" placeholder="secret_..." bind:value={cfg.NotionToken} />
        </div>
        <div class="field">
          <label class="field-label" for="set-notion-db">Notion tasks database id</label>
          <input id="set-notion-db" class="input mono" type="text" placeholder="database id" bind:value={cfg.NotionTasksDB} />
          <div class="ai-hint">Pulls tasks and deadlines into Today. Read-only.</div>
        </div>
      {/if}

      {#if saveErr}
        <div class="save-err">{saveErr}</div>
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

<style>
  .set-modal { max-width: 540px; }
  .set-tabs { padding: 0 20px; margin-top: -4px; }
  .ai-providers { display: flex; flex-direction: column; gap: 8px; margin-top: 4px; }
  .ai-radio {
    display: grid;
    grid-template-columns: auto auto 1fr;
    align-items: center;
    gap: 10px;
    padding: 10px 12px;
    border: 1px solid var(--border);
    border-radius: var(--r-btn);
    cursor: pointer;
    transition: border-color var(--t), background var(--t);
  }
  .ai-radio:hover { border-color: var(--border-hover); }
  .ai-radio.on { border-color: var(--accent-line); background: var(--accent-soft); }
  .ai-radio-name { font-weight: 600; font-size: 13px; color: var(--text); }
  .ai-radio-note { font-size: 11.5px; color: var(--muted); text-align: right; }
  .ai-status { margin-top: 10px; font-size: 12px; color: var(--dirty); }
  .ai-status.ok { color: var(--ok); }
  .ai-hint { font-size: 12px; color: var(--muted); margin-top: 6px; line-height: 1.5; }
  .ai-hint-warn { color: var(--dirty); margin-top: 12px; }
</style>
