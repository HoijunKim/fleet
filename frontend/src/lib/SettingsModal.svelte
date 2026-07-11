<script lang="ts">
  import { GetConfig, SaveConfig, AICheck, AskAI, NotionDatabases, DetectEditors } from "../../wailsjs/go/main/App";
  import type { config } from "../../wailsjs/go/models";
  import { toastSuccess, toastError } from "./toasts";
  import { editorSelection } from "./editorSelection";
  import Logo from "./Logo.svelte";

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
      syncEditorChoice();
    } catch (e) {
      toastError("Load failed: " + String(e));
    } finally {
      loading = false;
    }
  }

  // ---- Editor picker: known editors detected on PATH, plus a Custom fallback ----
  let editors: { name: string; command: string; installed: boolean }[] = [];
  let editorChoice = "custom";

  // Non-blocking: a failed detection just leaves the picker with only "Custom...".
  async function loadEditors() {
    try {
      editors = await DetectEditors();
    } catch {
      editors = [];
    }
    syncEditorChoice();
  }

  // Re-derive the dropdown selection from the saved config. Only called right
  // after cfg/editors finish loading - never while the user is actively
  // picking or typing - so the picker stays non-destructive. Computed
  // directly (not via a reactive `$:` value) so it can't read a stale
  // pre-invalidation snapshot when called synchronously after `cfg` loads.
  function syncEditorChoice() {
    if (!cfg) return;
    editorChoice = editorSelection(cfg.Editor, editors).selected;
  }

  function onEditorChoice() {
    if (!cfg || editorChoice === "custom") return;
    cfg.Editor = editorChoice;
  }

  function refreshAiOk() {
    if (!cfg) return;
    AICheck(cfg.AIProvider, cfg.OpenAIKey || "", cfg.GeminiKey || "")
      .then((ok) => (aiOk = !!ok))
      .catch(() => (aiOk = false));
  }

  // ---- AI test: run a tiny prompt to prove the provider actually answers ----
  let aiTesting = false;
  let aiTestMsg = "";
  async function testAI() {
    if (!cfg || aiTesting) return;
    aiTesting = true;
    aiTestMsg = "";
    try {
      // Save first so AskAI uses the provider/key in front of you.
      const serr = await SaveConfig(cfg);
      if (serr) {
        aiTestMsg = "error: " + serr;
        return;
      }
      const res = await AskAI("Reply with exactly the word: ok");
      aiTestMsg = typeof res === "string" && res.startsWith("error:") ? res : "Works - " + res;
    } catch (e) {
      aiTestMsg = "error: " + String(e);
    } finally {
      aiTesting = false;
      refreshAiOk();
    }
  }

  // ---- Notion database picker ----------------------------------------------
  let notionDBs: { id: string; title: string }[] = [];
  let notionLoading = false;
  let notionDbErr = "";
  async function loadNotionDBs() {
    if (!cfg || notionLoading) return;
    notionLoading = true;
    notionDbErr = "";
    try {
      const res: any = await NotionDatabases(cfg.NotionToken || "");
      if (res && res.error) {
        notionDbErr = res.error;
        notionDBs = [];
      } else {
        notionDBs = (res && res.dbs) || [];
        if (notionDBs.length === 0) notionDbErr = "No databases shared with this integration yet.";
      }
    } catch (e) {
      notionDbErr = String(e);
      notionDBs = [];
    } finally {
      notionLoading = false;
    }
  }

  load();
  loadEditors();

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
      <Logo size={16} />
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
            <select id="set-editor" class="input" bind:value={editorChoice} on:change={onEditorChoice}>
              {#each editors as e (e.command)}
                <option value={e.command}>{e.name}{e.installed ? " (installed)" : ""}</option>
              {/each}
              <option value="custom">Custom...</option>
            </select>
            {#if editorChoice === "custom"}
              <input class="input" type="text" placeholder="code" bind:value={cfg.Editor} />
            {/if}
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
        {#if cfg.AIProvider === "openai" || cfg.AIProvider === "gemini"}
          <div class="ai-hint ai-hint-warn">
            Code-aware features send repo diffs to {cfg.AIProvider} (obvious secrets are masked first).
            The Claude CLI keeps everything on your machine.
          </div>
        {/if}

        <div class="ai-test-row">
          <button class="btn btn-secondary btn-sm" on:click={testAI} disabled={aiTesting}>
            {aiTesting ? "Testing..." : "Test"}
          </button>
          {#if aiTestMsg}
            <span class="ai-test-msg" class:err={aiTestMsg.startsWith("error:")}>{aiTestMsg}</span>
          {/if}
        </div>
      {:else}
        <div class="field">
          <span class="field-label">GitHub</span>
          <div class="ai-hint">CI / PR / issue badges use the <span class="mono">gh</span> CLI's existing auth. Nothing to configure here.</div>
        </div>

        <div class="field">
          <label class="field-label" for="set-notion">Notion integration token</label>
          <input id="set-notion" class="input mono" type="password" placeholder="secret_..." bind:value={cfg.NotionToken} />
          <div class="ai-hint">
            Create one at <span class="mono">notion.so/my-integrations</span>, then share your tasks
            database with it (database menu -> Connections).
          </div>
        </div>

        <div class="field">
          <span class="field-label">Tasks database</span>
          <div class="notion-pick">
            <button class="btn btn-secondary btn-sm" on:click={loadNotionDBs} disabled={notionLoading || !cfg.NotionToken}>
              {notionLoading ? "Loading..." : "Load databases"}
            </button>
            {#if notionDBs.length > 0}
              <select class="input notion-select" bind:value={cfg.NotionTasksDB}>
                <option value="">- pick a database -</option>
                {#each notionDBs as db (db.id)}
                  <option value={db.id}>{db.title}</option>
                {/each}
              </select>
            {/if}
          </div>
          {#if notionDbErr}
            <div class="ai-test-msg err">{notionDbErr}</div>
          {/if}
          <input
            class="input mono notion-db-manual"
            type="text"
            placeholder="...or paste a database id"
            bind:value={cfg.NotionTasksDB}
          />
          <div class="ai-hint">Pulls open tasks and deadlines into Today. Read-only.</div>
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
  .ai-test-row { display: flex; align-items: center; gap: 10px; margin-top: 14px; }
  .ai-test-msg { font-size: 12px; color: var(--ok); }
  .ai-test-msg.err { color: var(--err); }
  .notion-pick { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .notion-select { flex: 1; min-width: 160px; }
  .notion-db-manual { margin-top: 8px; }
</style>
