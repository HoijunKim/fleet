<script lang="ts">
  // Fleet-wide agenda: project deadlines + incomplete due/doing tasks,
  // bucketed by how soon they are due. Pure presentation - all data comes
  // from the Agenda() binding via Overview.svelte.
  import { daysUntil } from "./pm";

  export let items: any[] = [];
  // Select a project and switch to the Projects view (opens its detail).
  export let onOpen: (id: string) => void;

  type Bucket = { key: string; label: string; items: any[] };

  // Bucket items client-side by daysUntil(item.due). Items with no due date
  // (the doing-no-due tasks) form their own "In progress" group and always
  // render last, regardless of the other buckets' order.
  $: buckets = bucketize(items || []);

  function bucketize(list: any[]): Bucket[] {
    const overdue: any[] = [];
    const today: any[] = [];
    const week: any[] = [];
    const later: any[] = [];
    const inProgress: any[] = [];

    for (const item of list || []) {
      if (!item) continue;
      const due = item.due || "";
      if (due === "") {
        inProgress.push(item);
        continue;
      }
      const d = daysUntil(due);
      if (d === null) {
        inProgress.push(item);
      } else if (d < 0) {
        overdue.push(item);
      } else if (d === 0) {
        today.push(item);
      } else if (d <= 7) {
        week.push(item);
      } else {
        later.push(item);
      }
    }

    const out: Bucket[] = [
      { key: "overdue", label: "Overdue", items: overdue },
      { key: "today", label: "Today", items: today },
      { key: "week", label: "This week", items: week },
      { key: "later", label: "Later", items: later },
      { key: "progress", label: "In progress", items: inProgress },
    ];
    return out.filter((b) => b.items.length > 0);
  }
</script>

<section class="ov-card ov-agenda">
  <div class="ov-card-head">
    <h3 class="ov-card-title">Agenda</h3>
    <span class="ov-count">{(items || []).length}</span>
  </div>
  {#if buckets.length === 0}
    <div class="ov-empty">Nothing scheduled</div>
  {:else}
    <div class="ov-agenda-groups">
      {#each buckets as b (b.key)}
        <div class="ov-agenda-group">
          <div class="ov-agenda-group-head">{b.label}</div>
          <ul class="ov-list">
            {#each b.items as item (item.projectId + ":" + item.kind + ":" + item.title + ":" + (item.due || ""))}
              <li>
                <button class="ov-row" on:click={() => onOpen(item.projectId)}>
                  <span class="ov-name">
                    {item.projectName}
                    <span class="ov-agenda-title">{item.title}</span>
                  </span>
                  <span class="ov-tags">
                    {#if item.kind === "deadline"}
                      <span class="ov-pill deadline">deadline</span>
                    {/if}
                  </span>
                </button>
              </li>
            {/each}
          </ul>
        </div>
      {/each}
    </div>
  {/if}
</section>

<style>
  .ov-agenda-groups {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .ov-agenda-group-head {
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--muted);
    margin-bottom: 4px;
  }
  .ov-agenda-title {
    color: var(--faint);
    font-weight: 400;
    margin-left: 6px;
  }
  .ov-pill.deadline {
    color: var(--accent);
    border-color: var(--accent);
    background: var(--raised);
  }
</style>
