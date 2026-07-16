<script lang="ts">
  export let onSignIn: () => void;
  export let onCancel: () => void = () => {};
  export let busy: boolean = false;
</script>

<span class="signin-wait">
  <!-- Persistent live region (always in the a11y tree) so the busy state is
       announced when its text changes - a region mounted together with its text
       is not reliably announced. -->
  <span class="sr-live" role="status" aria-live="polite">{busy ? "Waiting for browser sign-in" : ""}</span>
  {#if busy}
    <span class="signin signin-busy" aria-hidden="true">Waiting for browser...</span>
    <button class="signin-cancel" on:click={onCancel} title="Cancel this sign-in">Cancel</button>
  {:else}
    <button
      class="signin"
      on:click={onSignIn}
      title="Works without signing in; sign in to sync across devices"
    >Sign in with GitHub</button>
  {/if}
</span>

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
  .signin-wait { display: inline-flex; align-items: center; gap: 6px; }
  /* Screen-reader-only, but always present in the tree (not display:none) so the
     polite live region can announce its text change. */
  .sr-live {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }
  .signin-busy { opacity: 0.75; cursor: default; }
  .signin-cancel {
    height: 28px;
    padding: 0 10px;
    border: 1px solid var(--border);
    border-radius: var(--r-pill);
    background: transparent;
    color: var(--muted);
    font: inherit;
    font-size: 12px;
    cursor: pointer;
    transition: border-color var(--t), color var(--t);
  }
  .signin-cancel:hover { border-color: var(--err); color: var(--err); }
  .signin:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
</style>
