<script lang="ts">
  import type { main } from "../../wailsjs/go/models";

  // Files fleet could not load at startup. Empty in the normal case, and then
  // this component renders nothing.
  export let issues: main.HealthIssue[] = [];
  export let onReveal: () => void;
  export let onDiscard: () => void;

  // "Start fresh" throws away data fleet failed to read, so it arms first: one
  // click to reveal what it means, a second to do it. The quarantined copy
  // survives either way, but the user should not discover that after the fact.
  let armed = false;

  const LABEL: Record<string, string> = {
    projects: "Your projects, tasks and notes",
    config: "Your settings",
    edges: "Your manual graph links",
  };

  function label(scope: string): string {
    return LABEL[scope] ?? scope;
  }

  $: frozen = issues.some((i) => i.frozen);
</script>

{#if issues.length}
  <div class="health" role="alert">
    <div class="health-body">
      <strong class="health-title">
        {issues.length === 1 ? "A file could not be read" : "Some files could not be read"}
      </strong>
      <ul class="health-list">
        {#each issues as issue (issue.scope)}
          <li>
            <span class="health-scope">{label(issue.scope)}</span>
            <span class="health-detail">{issue.error}</span>
          </li>
        {/each}
      </ul>
      <p class="health-note">
        {#if frozen}
          The original file was kept, and fleet has stopped saving so it cannot
          overwrite anything. Sync is paused for the same reason.
        {:else}
          The original file was kept next to fleet's data so nothing is lost.
        {/if}
      </p>
    </div>
    <div class="health-actions">
      <button class="health-btn" on:click={onReveal}>Reveal folder</button>
      {#if frozen}
        {#if armed}
          <button class="health-btn health-btn-danger" on:click={onDiscard}>
            Start fresh - discard it
          </button>
          <button class="health-btn" on:click={() => (armed = false)}>Cancel</button>
        {:else}
          <button class="health-btn" on:click={() => (armed = true)}>Start fresh</button>
        {/if}
      {/if}
    </div>
  </div>
{/if}

<style>
  .health {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
    padding: 10px 14px;
    border-bottom: 1px solid var(--err-line);
    background: var(--err-bg, transparent);
    color: var(--text);
    font-size: 12.5px;
  }
  .health-title {
    display: block;
    color: var(--err);
    font-size: 13px;
  }
  .health-list {
    margin: 4px 0 0;
    padding-left: 16px;
  }
  .health-list li {
    margin-top: 2px;
  }
  .health-scope {
    color: var(--text);
  }
  .health-detail {
    color: var(--muted);
  }
  .health-note {
    margin: 6px 0 0;
    color: var(--muted);
  }
  .health-actions {
    display: flex;
    gap: 8px;
    flex: none;
    padding-top: 2px;
  }
  .health-btn {
    font: inherit;
    font-size: 12px;
    padding: 4px 10px;
    border: 1px solid var(--border);
    border-radius: var(--r-sm, 6px);
    background: var(--surface);
    color: var(--text);
    cursor: pointer;
    white-space: nowrap;
  }
  .health-btn:hover {
    border-color: var(--accent);
  }
  .health-btn-danger {
    border-color: var(--err-line);
    color: var(--err);
  }
</style>
