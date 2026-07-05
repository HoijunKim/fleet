<script lang="ts">
  import Select from "./Select.svelte";
  // Project-list filters, lifted out of the global toolbar into a dedicated row
  // above the table so the top bar stays about navigation, not filtering.
  export let filter: string = "";
  export let filterInput: HTMLInputElement | undefined = undefined;
  export let repos: any[] = [];
  export let statusFilter: "all" | "dirty" | "behind" | "unpushed" | "overdue" = "all";
  export let pmStatusFilter: "all" | "active" | "paused" | "done" = "all";
  export let highPriorityOnly: boolean = false;
  export let tagFilter: string = "all";
  export let onStatus: (s: "all" | "dirty" | "behind" | "unpushed" | "overdue") => void;
  export let onPmStatus: (s: "all" | "active" | "paused" | "done") => void;
  export let onHighPriorityToggle: () => void;
  export let onTagFilter: (t: string) => void;

  function pmChange(v: string) {
    onPmStatus(v as "all" | "active" | "paused" | "done");
  }

  $: dirty = repos.filter((r) => r.dirty).length;
  $: behind = repos.filter((r) => r.behind > 0).length;
  $: allTags = Array.from(new Set(repos.flatMap((r) => r.tags || []))).sort();
</script>

<div class="pfbar">
  <span class="pfbar-count"><b>{repos.length}</b> projects</span>

  <div class="pfbar-search">
    <input
      class="input"
      type="text"
      placeholder="Filter projects..."
      bind:value={filter}
      bind:this={filterInput}
      aria-label="Filter projects by name"
    />
  </div>

  <div class="filter-chips">
    <button class="fchip" class:active={statusFilter === "all"} on:click={() => onStatus("all")}>All</button>
    <button class="fchip" class:active={statusFilter === "dirty"} on:click={() => onStatus("dirty")}>
      Dirty <span class="fchip-n">{dirty}</span>
    </button>
    <button class="fchip" class:active={statusFilter === "behind"} on:click={() => onStatus("behind")}>
      Behind <span class="fchip-n">{behind}</span>
    </button>
  </div>

  <div class="pfbar-spacer"></div>

  <Select
    value={pmStatusFilter}
    options={[
      { value: "all", label: "All statuses" },
      { value: "active", label: "Active" },
      { value: "paused", label: "Paused" },
      { value: "done", label: "Done" },
    ]}
    ariaLabel="Filter by project status"
    onChange={pmChange}
  />

  <button class="fchip" class:active={highPriorityOnly} on:click={onHighPriorityToggle} aria-pressed={highPriorityOnly}>
    High priority
  </button>

  {#if allTags.length > 0}
    <Select
      value={tagFilter}
      options={[{ value: "all", label: "All tags" }, ...allTags.map((t) => ({ value: t, label: t }))]}
      ariaLabel="Filter by tag"
      onChange={(v) => onTagFilter(v)}
    />
  {/if}
</div>

<style>
  .pfbar {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    padding: 10px 16px;
    border-bottom: 1px solid var(--hairline);
    background: var(--surface);
  }
  .pfbar-count { font-size: 12.5px; color: var(--muted); white-space: nowrap; margin-right: 2px; }
  .pfbar-count b { color: var(--text); font-weight: 700; font-variant-numeric: tabular-nums; }
  .pfbar-search { flex: 0 1 240px; min-width: 150px; }
  .pfbar-search .input { width: 100%; }
  .pfbar-spacer { flex: 1; }
</style>
