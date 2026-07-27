<script lang="ts">
  import {
    CommitAll, CommitStaged, CommitAmend, Push, RepoDiff, AskAI,
    StatusFiles, StageFile, UnstageFile, LastCommitMessage,
    GitOperation, Conflicts, ResolveConflict, ContinueOperation, AbortOperation,
  } from "../../wailsjs/go/main/App";
  import type { main } from "../../wailsjs/go/models";
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
  // An in-progress merge/rebase and its conflicted files. mode is "" when the
  // repo is not mid-operation, which hides the whole conflict block.
  let mode = "";
  let conflicts: main.GitConflictView[] = [];

  let loadSeq = 0;
  async function loadStatus() {
    const p = path;
    const seq = ++loadSeq;
    try {
      const [fs, op] = await Promise.all([StatusFiles(p), GitOperation(p)]);
      // Drop a stale result: the selection changed, or a newer load was issued.
      if (p !== path || seq !== loadSeq) return;
      files = fs || [];
      mode = op || "";
      conflicts = mode ? (await Conflicts(p)) || [] : [];
      if (p !== path || seq !== loadSeq) return;
    } catch {
      if (p === path && seq === loadSeq) {
        files = [];
        mode = "";
        conflicts = [];
      }
    }
  }

  async function resolve(file: string, side: string) {
    if (busy) return;
    busy = true;
    try {
      const err = await ResolveConflict(path, file, side);
      if (err) toastError("Resolve " + file + ": " + err);
      await loadStatus();
    } finally {
      busy = false;
    }
  }

  async function finishOp() {
    if (busy) return;
    busy = true;
    try {
      const err = await ContinueOperation(path);
      if (err) {
        toastError("Continue " + opLabel.toLowerCase() + ": " + err);
        await loadStatus();
        return;
      }
      toastSuccess(opLabel + " completed " + name);
      msg = "";
      onChanged(path);
    } finally {
      busy = false;
    }
  }

  async function abortOp() {
    if (busy) return;
    busy = true;
    try {
      const err = await AbortOperation(path);
      if (err) {
        toastError("Abort " + opLabel.toLowerCase() + ": " + err);
        return;
      }
      toastSuccess(opLabel + " aborted " + name);
      onChanged(path);
    } finally {
      busy = false;
    }
  }

  $: staged = files.filter((f) => f.staged);
  $: unstaged = files.filter((f) => f.unstaged);
  // A conflict must be gone before the operation can be finished, and the label
  // per file comes from the binding so the ours/theirs swap is never re-derived.
  $: conflictByPath = new Map(conflicts.map((c) => [c.path, c]));
  $: hasConflict = conflicts.length > 0;
  // Title-case the in-progress operation for the banner and toasts, so merge,
  // rebase and cherry-pick all read naturally.
  const OP_LABELS: Record<string, string> = { merge: "Merge", rebase: "Rebase", "cherry-pick": "Cherry-pick" };
  $: opLabel = OP_LABELS[mode] || "Operation";
  $: hasStaged = staged.length > 0;
  $: count = dirtyFiles ? dirtyFiles.length : 0;
  $: clean = count === 0;
  // While a conflict remains the correct verb is Continue, not Commit: git
  // refuses a commit with unmerged paths anyway, so the buttons stay disabled.
  $: canCommitStaged = !busy && !hasConflict && hasStaged && msg.trim().length > 0;
  $: canCommitAll = !busy && !hasConflict && !clean && msg.trim().length > 0;
  $: canAmend = !busy && !hasConflict && msg.trim().length > 0;

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

  {#if mode}
    <div class="op-banner" class:resolved={!hasConflict}>
      <div class="op-banner-head">
        <span class="op-title">
          {opLabel} in progress
          {#if hasConflict}<span class="op-sub">- {conflicts.length} file{conflicts.length === 1 ? "" : "s"} conflict</span>{/if}
        </span>
        <div class="op-actions">
          <button class="btn btn-primary btn-sm" on:click={finishOp} disabled={busy || hasConflict}
            title={hasConflict ? "Resolve every conflict first" : "Finish the operation"}>
            {#if busy}<span class="spinner"></span>{/if} Continue
          </button>
          <button class="btn btn-secondary btn-sm" on:click={abortOp} disabled={busy}>Abort</button>
        </div>
      </div>
    </div>
  {/if}

  {#if !clean}
    <div class="stage-groups">
      {#if unstaged.length}
        <div class="stage-group">
          <span class="stage-label">Unstaged</span>
          {#each unstaged as f (f.path)}
            <div class="stage-file" class:conflict-row={f.conflict}>
              {#if f.conflict}
                <span class="stage-conflict" title="Unmerged - choose a side or edit it and mark resolved">!</span>
                <span class="stage-name mono conflict">{f.path}</span>
                {#if conflictByPath.get(f.path)}
                  {@const c = conflictByPath.get(f.path)}
                  <div class="conflict-choices">
                    <button class="conflict-btn" on:click={() => resolve(f.path, "mine")} disabled={busy} title="Keep this branch's version">{c?.mineLabel}</button>
                    <button class="conflict-btn" on:click={() => resolve(f.path, "incoming")} disabled={busy} title="Take the incoming version">{c?.incomingLabel}</button>
                    <button class="conflict-btn resolved" on:click={() => resolve(f.path, "worktree")} disabled={busy} title="Stage the file as you edited it by hand">Resolved</button>
                  </div>
                {:else}
                  <span class="stage-conflict-tag">conflict</span>
                {/if}
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

  .op-banner {
    border: 1px solid var(--dirty);
    background: var(--accent-soft);
    border-radius: var(--r-btn);
    padding: 8px 10px;
    margin-bottom: 8px;
  }
  /* Every conflict cleared: the banner shifts from "attention" to "ready". */
  .op-banner.resolved { border-color: var(--ok); }
  .op-banner-head { display: flex; align-items: center; gap: 8px; }
  .op-title { font-size: 12px; font-weight: 600; color: var(--text); }
  .op-sub { font-weight: 400; color: var(--dirty); }
  .op-actions { margin-left: auto; display: flex; gap: 6px; }

  .conflict-choices { margin-left: auto; display: flex; gap: 4px; }
  .conflict-btn {
    font: inherit;
    font-size: 11px;
    color: var(--text);
    background: var(--surface-2, var(--accent-soft));
    border: 1px solid var(--border);
    border-radius: var(--r-pill);
    padding: 1px 8px;
    cursor: pointer;
    transition: border-color var(--t), background var(--t);
  }
  .conflict-btn:hover { border-color: var(--border-hover); }
  .conflict-btn.resolved:hover { border-color: var(--ok); }
  .conflict-btn:disabled { opacity: 0.6; cursor: default; }
  .conflict-row { flex-wrap: wrap; row-gap: 4px; }
</style>
