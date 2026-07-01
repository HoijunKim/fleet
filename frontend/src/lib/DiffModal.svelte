<script lang="ts">
  import { onMount } from "svelte";
  import { DiffFile } from "../../wailsjs/go/main/App";

  export let path: string;
  export let file: string;
  export let onClose: () => void;

  let diff = "";
  let loading = true;

  onMount(async () => {
    try {
      diff = await DiffFile(path, file);
    } catch (e) {
      diff = "[error: " + String(e) + "]";
    } finally {
      loading = false;
    }
  });

  $: lines = diff ? diff.replace(/\n$/, "").split("\n") : [];

  // Per-line class for +/- coloring. Header lines (+++/---/@@/diff/index)
  // are treated as meta so real additions/removals stay unambiguous.
  function lineClass(l: string): string {
    if (l.startsWith("@@")) return "at";
    if (l.startsWith("+++") || l.startsWith("---")) return "meta";
    if (
      l.startsWith("diff ") ||
      l.startsWith("index ") ||
      l.startsWith("new file") ||
      l.startsWith("deleted file") ||
      l.startsWith("old mode") ||
      l.startsWith("new mode") ||
      l.startsWith("similarity ") ||
      l.startsWith("rename ") ||
      l.startsWith("\\ ")
    )
      return "meta";
    if (l.startsWith("+")) return "add";
    if (l.startsWith("-")) return "del";
    return "";
  }

  function keydown(e: KeyboardEvent) {
    if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    }
  }
</script>

<svelte:window on:keydown={keydown} />

<!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
<div class="overlay" on:click={onClose}>
  <div class="modal diff-modal" on:click|stopPropagation>
    <div class="modal-head">
      <span class="brand-dot"></span>
      <h3 class="modal-title diff-title">{file}</h3>
      <button class="btn btn-secondary btn-sm modal-close" on:click={onClose} aria-label="Close">x</button>
    </div>

    <div class="diff-body">
      {#if loading}
        <div class="modal-loading"><span class="spinner"></span> loading diff</div>
      {:else if lines.length === 0}
        <div class="diff-empty">No textual diff (file may be untracked, binary, or unchanged).</div>
      {:else}
        <pre class="diff-code">{#each lines as l}<span class="diff-line {lineClass(l)}">{l || " "}</span>{/each}</pre>
      {/if}
    </div>

    <div class="modal-foot">
      <button class="btn btn-secondary" on:click={onClose}>Close</button>
    </div>
  </div>
</div>
