<script lang="ts">
  import { onMount, onDestroy } from "svelte";

  // State: offline | syncing | synced | error | signedout
  export let state: string = "signedout";
  export let lastSyncedUnix: number = 0;
  export let error: string = "";
  export let onRetry: () => void = () => {};
  export let onSignIn: () => void = () => {};

  let now = Date.now();
  let timer: ReturnType<typeof setInterval> | undefined;
  onMount(() => {
    timer = setInterval(() => (now = Date.now()), 1000);
  });
  onDestroy(() => timer && clearInterval(timer));

  function ago(unix: number): string {
    if (!unix) return "just now";
    const s = Math.max(0, Math.floor(now / 1000 - unix));
    if (s < 60) return s + "s ago";
    const m = Math.floor(s / 60);
    if (m < 60) return m + "m ago";
    const h = Math.floor(m / 60);
    return h + "h ago";
  }
</script>

<div class="pill pill-{state}" title={error || state}>
  {#if state === "syncing"}
    <span class="spinner"></span><span class="pill-text">Syncing...</span>
  {:else if state === "synced"}
    <span class="dot dot-ok"></span><span class="pill-text">Synced {ago(lastSyncedUnix)}</span>
  {:else if state === "offline"}
    <span class="dot dot-warn"></span><span class="pill-text">Offline</span>
  {:else if state === "error"}
    <span class="dot dot-err"></span><span class="pill-text">Sync error</span>
    <button class="pill-action" on:click={onRetry}>Retry</button>
  {:else}
    <span class="dot dot-idle"></span>
    <button class="pill-action" on:click={onSignIn}>Sign in to sync</button>
  {/if}
</div>

<style>
  .pill {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    height: 28px;
    min-width: 140px; /* stable width: no layout shift across states */
    padding: 0 10px;
    border: 1px solid var(--border);
    border-radius: var(--r-pill);
    background: var(--surface);
    font-size: 12px;
    color: var(--muted);
    white-space: nowrap;
  }
  .pill-text { font-variant-numeric: tabular-nums; }
  .dot { width: 7px; height: 7px; border-radius: 50%; flex: none; }
  .dot-ok { background: var(--ok); }
  .dot-warn { background: var(--muted); }
  .dot-err { background: var(--err); }
  .dot-idle { background: var(--faint); }
  .pill-error { border-color: var(--err-line); color: var(--err); }
  .pill-action {
    font: inherit;
    font-size: 11.5px;
    color: var(--accent);
    background: transparent;
    border: none;
    padding: 0;
    cursor: pointer;
  }
  .pill-action:hover { text-decoration: underline; }
</style>
