<script lang="ts">
  import { onMount } from "svelte";
  import { OpenInBrowser, RevealInExplorer } from "../../wailsjs/go/main/App";
  import { toastSuccess, toastError } from "./toasts";

  export let x = 0;
  export let y = 0;
  export let repo: any;
  export let onClose: () => void;

  let menuEl: HTMLElement;
  let px = x;
  let py = y;

  onMount(() => {
    // Keep the menu inside the viewport.
    const r = menuEl.getBoundingClientRect();
    if (x + r.width > window.innerWidth) px = Math.max(8, window.innerWidth - r.width - 8);
    if (y + r.height > window.innerHeight) py = Math.max(8, window.innerHeight - r.height - 8);
  });

  $: hasRemote = !!(repo && repo.remote);
  $: hasPath = !!(repo && repo.path);

  async function openBrowser() {
    onClose();
    if (!hasRemote) return;
    const err = await OpenInBrowser(repo.remote);
    if (err) toastError("Open in browser: " + err);
    else toastSuccess("Opened in browser");
  }

  async function reveal() {
    onClose();
    if (!hasPath) return;
    const err = await RevealInExplorer(repo.path);
    if (err) toastError("Reveal: " + err);
    else toastSuccess("Revealed in explorer");
  }

  async function copyPath() {
    onClose();
    if (!hasPath) return;
    try {
      await navigator.clipboard.writeText(repo.path);
      toastSuccess("Path copied");
    } catch {
      toastError("Copy failed");
    }
  }

  async function copyRemote() {
    onClose();
    if (!hasRemote) return;
    try {
      await navigator.clipboard.writeText(repo.remote);
      toastSuccess("Remote copied");
    } catch {
      toastError("Copy failed");
    }
  }
</script>

<!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
<div class="ctx-backdrop" on:click={onClose} on:contextmenu|preventDefault={onClose}></div>
<div class="ctx-menu" bind:this={menuEl} style="left:{px}px; top:{py}px">
  <div class="ctx-head">{repo.name}</div>
  <button class="ctx-item" on:click={openBrowser} disabled={!hasRemote}>Open in Browser</button>
  <button class="ctx-item" on:click={reveal} disabled={!hasPath}>Reveal in Explorer</button>
  <div class="ctx-sep"></div>
  <button class="ctx-item" on:click={copyPath} disabled={!hasPath}>Copy Path</button>
  <button class="ctx-item" on:click={copyRemote} disabled={!hasRemote}>Copy Remote</button>
</div>
