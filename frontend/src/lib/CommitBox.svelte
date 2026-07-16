<script lang="ts">
  import {
    CommitAll, CommitStaged, CommitAmend, Push, RepoDiff, AskAI,
    StatusFiles, StageFile, UnstageFile, LastCommitMessage,
  } from "../../wailsjs/go/main/App";
  import { toastSuccess, toastError } from "./toasts";
  import { redactSecrets } from "./redact";

  export let path: string;
  export let name = "";
  export let dirtyFiles: string[] = [];
  export let onChanged: (path: string) => void;

  let msg = "";
  let busy = false;
  let drafting = false;
  let amend = false;
  let files: { path: string; staged: boolean; unstaged: boolean; conflict: boolean }[] = [];

  // Reload per-file staging state whenever the repo or its dirty set changes
  // (dirtyFiles is refreshed by the parent after every mutation).
  let statusKey = "";
  $: {
    const key = path + "|" + (dirtyFiles || []).join(",");
    if (key !== statusKey) {
      statusKey = key;
      loadStatus();
    }
  }
  let loadSeq = 0;
  async function loadStatus() {
    const p = path;
    const seq = ++loadSeq;
    try {
      const fs = await StatusFiles(p);
      // Drop a stale result: the selection changed, or a newer load was issued.
      if (p !== path || seq !== loadSeq) return;
      files = fs || [];
    } catch {
      if (p === path && seq === loadSeq) files = [];
    }
  }

  $: staged = files.filter((f) => f.staged);
  $: unstaged = files.filter((f) => f.unstaged);
  $: hasStaged = staged.length > 0;
  $: count = dirtyFiles ? dirtyFiles.length : 0;
  $: clean = count === 0;
  $: canCommitStaged = !busy && hasStaged && msg.trim().length > 0;
  $: canCommitAll = !busy && !clean && msg.trim().length > 0;
  $: canAmend = !busy && msg.trim().length > 0;

  async function draft() {
    if (drafting || clean) return;
    drafting = true;
    try {
      const diff = await RepoDiff(path);
      if (!diff || !diff.trim()) {
        toastError("Nothing to summarize yet");
        return;
      }
      const prompt =
        "Write a concise git commit message for these changes. First line: an imperative " +
        "summary under 60 characters. Optionally a blank line then 1-3 short '- ' bullets. " +
        "Output ONLY the message - no preamble, no backticks, no quotes.\n\nChanges:\n" + redactSecrets(diff);
      const res = await AskAI(prompt);
      if (typeof res === "string" && res.startsWith("error:")) {
        toastError("Draft: " + res.slice(6).trim());
        return;
      }
      msg = (res || "").trim();
    } catch (e) {
      toastError("Draft failed: " + String(e));
    } finally {
      drafting = false;
    }
  }

  async function toggleAmend() {
    amend = !amend;
    // Prefill the message from the last commit when arming amend on an empty box.
    if (amend && !msg.trim()) {
      const last = await LastCommitMessage(path);
      // Re-check after the await: the user may have typed, or toggled amend off,
      // during the round-trip - don't clobber that.
      if (last && amend && !msg.trim()) msg = last;
    }
  }

  async function stage(file: string) {
    if (busy) return;
    busy = true;
    try {
      const err = await StageFile(path, file);
      if (err) toastError("Stage: " + err);
      // Staging doesn't change the parent's dirty set (a staged file is still
      // "changed"), so reload the staged/unstaged split directly here.
      await loadStatus();
    } finally {
      busy = false;
    }
  }
  async function unstage(file: string) {
    if (busy) return;
    busy = true;
    try {
      const err = await UnstageFile(path, file);
      if (err) toastError("Unstage: " + err);
      await loadStatus();
    } finally {
      busy = false;
    }
  }

  // mode: "staged" | "all" | "allpush" | "amend"
  async function doCommit(mode: string) {
    const m = msg.trim();
    if (!m || busy) return;
    busy = true;
    try {
      let err: string;
      if (mode === "amend") err = await CommitAmend(path, m);
      else if (mode === "staged") err = await CommitStaged(path, m);
      else err = await CommitAll(path, m);
      if (err) {
        toastError("Commit " + name + ": " + err);
        return;
      }
      if (mode === "allpush") {
        const perr = await Push(path);
        if (perr) {
          toastError("Push " + name + ": " + perr);
          onChanged(path);
          return;
        }
        toastSuccess("Committed and pushed " + name);
      } else {
        toastSuccess((mode === "amend" ? "Amended " : "Committed ") + name);
      }
      msg = "";
      amend = false;
      onChanged(path);
    } finally {
      busy = false;
    }
  }
</script>

<div class="commit-box">
  <div class="commit-head">
    <span class="section-label">Commit</span>
    {#if clean}
      <span class="commit-count clean">clean</span>
    {:else}
      <span class="commit-count"><span class="commit-num">{count}</span> changed</span>
      <button class="commit-draft" on:click={draft} disabled={drafting} title="Draft a message from the diff with AI">
        {#if drafting}<span class="spinner"></span> drafting{:else}Draft with AI{/if}
      </button>
    {/if}
  </div>

  {#if !clean}
    <div class="stage-groups">
      {#if unstaged.length}
        <div class="stage-group">
          <span class="stage-label">Unstaged</span>
          {#each unstaged as f (f.path)}
            <div class="stage-file">
              {#if f.conflict}
                <span class="stage-conflict" title="Merge conflict - resolve it in your editor or terminal before staging">!</span>
                <span class="stage-name mono conflict">{f.path}</span>
                <span class="stage-conflict-tag">conflict</span>
              {:else}
                <button class="stage-toggle add" title="Stage" aria-label={"Stage " + f.path} on:click={() => stage(f.path)} disabled={busy}>+</button>
                <span class="stage-name mono">{f.path}</span>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
      {#if staged.length}
        <div class="stage-group">
          <span class="stage-label staged">Staged</span>
          {#each staged as f (f.path)}
            <div class="stage-file">
              <button class="stage-toggle del" title="Unstage" aria-label={"Unstage " + f.path} on:click={() => unstage(f.path)} disabled={busy}>-</button>
              <span class="stage-name mono">{f.path}</span>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}

  <textarea
    class="input commit-msg"
    rows="2"
    placeholder={clean && !amend ? "Working tree clean" : "Commit message"}
    bind:value={msg}
    disabled={busy || (clean && !amend)}
    aria-label="Commit message"
  ></textarea>

  <label class="amend-row">
    <input type="checkbox" checked={amend} on:change={toggleAmend} disabled={busy} />
    <span>Amend last commit</span>
    {#if amend}<span class="amend-warn">rewrites the last commit</span>{/if}
  </label>

  <div class="commit-actions">
    {#if amend}
      <button class="btn btn-primary btn-sm" on:click={() => doCommit("amend")} disabled={!canAmend}>
        {#if busy}<span class="spinner"></span>{/if} Amend last commit
      </button>
    {:else}
      <button class="btn btn-primary btn-sm" on:click={() => doCommit("staged")} disabled={!canCommitStaged} title="Commit only staged files">
        {#if busy}<span class="spinner"></span>{/if} Commit staged
      </button>
      <button class="btn btn-secondary btn-sm" on:click={() => doCommit("all")} disabled={!canCommitAll}>Commit all</button>
      <button class="btn btn-secondary btn-sm" on:click={() => doCommit("allpush")} disabled={!canCommitAll}>&amp; push</button>
    {/if}
  </div>
</div>

<style>
  .commit-draft {
    margin-left: auto;
    font: inherit;
    font-size: 11.5px;
    color: var(--accent);
    background: var(--accent-soft);
    border: 1px solid var(--accent-line);
    border-radius: var(--r-pill);
    padding: 2px 10px;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 5px;
    transition: background var(--t);
  }
  .commit-draft:hover { background: rgba(110, 168, 254, 0.22); }
  .commit-draft:disabled { opacity: 0.6; cursor: default; }
</style>
