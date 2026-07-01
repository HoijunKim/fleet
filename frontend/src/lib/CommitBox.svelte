<script lang="ts">
  import { CommitAll, Push } from "../../wailsjs/go/main/App";
  import { toastSuccess, toastError } from "./toasts";

  export let path: string;
  export let name = "";
  export let dirtyFiles: string[] = [];
  export let onChanged: (path: string) => void;

  let msg = "";
  let busy = false;

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
