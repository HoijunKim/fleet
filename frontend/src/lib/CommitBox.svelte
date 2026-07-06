<script lang="ts">
  import { CommitAll, Push, RepoDiff, AskAI } from "../../wailsjs/go/main/App";
  import { toastSuccess, toastError } from "./toasts";

  export let path: string;
  export let name = "";
  export let dirtyFiles: string[] = [];
  export let onChanged: (path: string) => void;

  let msg = "";
  let busy = false;
  let drafting = false;

  // Draft a commit message from the actual working diff (CommitAll commits all
  // uncommitted changes, so draft from the working diff, not just staged).
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
        "Output ONLY the message - no preamble, no backticks, no quotes.\n\nChanges:\n" + diff;
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

  $: count = dirtyFiles ? dirtyFiles.length : 0;
  $: clean = count === 0;
  $: canCommit = !busy && !clean && msg.trim().length > 0;

  async function commit(push: boolean) {
    if (!canCommit) return;
    busy = true;
    try {
      const err = await CommitAll(path, msg.trim());
      if (err) {
        toastError("Commit " + name + ": " + err);
        return;
      }
      if (push) {
        const perr = await Push(path);
        if (perr) {
          toastError("Push " + name + ": " + perr);
          onChanged(path);
          return;
        }
        toastSuccess("Committed and pushed " + name);
      } else {
        toastSuccess("Committed " + name);
      }
      msg = "";
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

  <textarea
    class="input commit-msg"
    rows="2"
    placeholder={clean ? "Working tree clean" : "Commit message"}
    bind:value={msg}
    disabled={clean || busy}
    aria-label="Commit message"
  ></textarea>

  <div class="commit-actions">
    <button class="btn btn-primary btn-sm" on:click={() => commit(false)} disabled={!canCommit}>
      {#if busy}<span class="spinner"></span>{/if} Commit all
    </button>
    <button class="btn btn-secondary btn-sm" on:click={() => commit(true)} disabled={!canCommit}>
      Commit all &amp; push
    </button>
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
