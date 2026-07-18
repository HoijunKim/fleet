<script lang="ts">
  import { fly } from "svelte/transition";
  import { toasts, dismissToast } from "./toasts";
</script>

<div class="toast-stack" aria-live="polite">
  {#each $toasts as t (t.id)}
    <div
      class="toast {t.kind}"
      role="status"
      in:fly={{ x: 24, duration: 180 }}
      out:fly={{ x: 24, duration: 140 }}
    >
      <span class="toast-dot"></span>
      <span class="toast-msg">{t.message}</span>
      {#if t.action}
        <button class="toast-action" on:click={t.action.run}>{t.action.label}</button>
      {/if}
      <button class="toast-x" on:click={() => dismissToast(t.id)} aria-label="Dismiss">x</button>
    </div>
  {/each}
</div>
