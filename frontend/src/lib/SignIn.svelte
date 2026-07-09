<script lang="ts">
  export let onSignIn: () => void;
  export let busy: boolean = false;
</script>

<button
  class="signin"
  on:click={onSignIn}
  disabled={busy}
  title="Works without signing in; sign in to sync across devices"
>
  {busy ? "Opening browser..." : "Sign in with GitHub"}
</button>

<style>
  .signin {
    display: inline-flex;
    align-items: center;
    height: 28px;
    padding: 0 12px;
    border: 1px solid var(--accent-line);
    border-radius: var(--r-pill);
    background: var(--accent-soft);
    color: var(--accent);
    font: inherit;
    font-size: 12px;
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
    transition: background var(--t), border-color var(--t);
  }
  .signin:hover:not(:disabled) { background: rgba(110, 168, 254, 0.22); }
  .signin:disabled { opacity: 0.6; cursor: default; }
  .signin:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
</style>
