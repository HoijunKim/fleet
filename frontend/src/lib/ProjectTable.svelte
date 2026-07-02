<script lang="ts">
  import { ddayLabel } from "./pm";

  export let projects: any[] = [];
  export let selectedId: string = "";
  export let scanned: boolean = false;
  export let sortKey: string = "";
  export let sortDir: "asc" | "desc" = "asc";
  export let onSelect: (p: any) => void;
  export let onSort: (key: string) => void;
  export let onContext: (p: any, e: MouseEvent) => void;

  // Git status pill for code projects. kind is "none" for manual projects (no
  // pill) and "skeleton" while the repo is still loading.
  function gitStatus(p: any): { kind: string; cls: string; text: string } {
    if (p.type !== "code") return { kind: "none", cls: "", text: "" };
    if (!p.loaded && !p.errMsg) return { kind: "skeleton", cls: "", text: "" };
    if (p.errMsg) return { kind: "pill", cls: "err", text: "error" };
    if (!p.isGit) return { kind: "pill", cls: "nogit", text: "not a repo" };
    if (p.dirty) return { kind: "pill", cls: "dirty", text: `${p.modified} changed` };
    if (p.behind > 0) return { kind: "pill", cls: "err", text: `behind ${p.behind}` };
    return { kind: "pill", cls: "ok", text: "clean" };
  }

  function taskProgress(p: any): { done: number; total: number } {
    const tasks = p.tasks || [];
    return { done: tasks.filter((t: any) => t.done).length, total: tasks.length };
  }

  function statusLabel(p: any): string {
    return p.status || "active";
  }

  function arrow(key: string): string {
    if (sortKey !== key) return "";
    return sortDir === "asc" ? "up" : "dn";
  }
</script>

<div class="table-wrap">
  {#if scanned && projects.length === 0}
    <div class="empty-state">
      <div>No projects yet.</div>
      <div class="hint">Add roots in Settings or use "+ Project".</div>
    </div>
  {:else}
    <table class="repo-table">
      <thead>
        <tr>
          <th class="sortable" class:sorted={sortKey === "name"} on:click={() => onSort("name")}>
            Name{#if arrow("name")}<span class="sort-arrow {arrow('name')}"></span>{/if}
          </th>
          <th class="sortable" class:sorted={sortKey === "type"} on:click={() => onSort("type")}>
            Type{#if arrow("type")}<span class="sort-arrow {arrow('type')}"></span>{/if}
          </th>
          <th class="sortable" class:sorted={sortKey === "git"} on:click={() => onSort("git")}>
            Git{#if arrow("git")}<span class="sort-arrow {arrow('git')}"></span>{/if}
          </th>
          <th class="sortable" class:sorted={sortKey === "tasks"} on:click={() => onSort("tasks")}>
            Tasks{#if arrow("tasks")}<span class="sort-arrow {arrow('tasks')}"></span>{/if}
          </th>
          <th class="sortable" class:sorted={sortKey === "deadline"} on:click={() => onSort("deadline")}>
            Deadline{#if arrow("deadline")}<span class="sort-arrow {arrow('deadline')}"></span>{/if}
          </th>
          <th class="sortable" class:sorted={sortKey === "status"} on:click={() => onSort("status")}>
            Status{#if arrow("status")}<span class="sort-arrow {arrow('status')}"></span>{/if}
          </th>
          <th class="right sortable" class:sorted={sortKey === "priority"} on:click={() => onSort("priority")}>
            Priority{#if arrow("priority")}<span class="sort-arrow {arrow('priority')}"></span>{/if}
          </th>
        </tr>
      </thead>
      <tbody>
        {#each projects as p (p.id)}
          <tr
            class="repo-row"
            class:selected={p.id === selectedId}
            on:click={() => onSelect(p)}
            on:contextmenu={(e) => onContext(p, e)}
          >
            <td class="cell-name">{p.name}</td>
            <td>
              <span class="type-badge {p.type}">{p.type}</span>
            </td>
            <td>
              {#if gitStatus(p).kind === "skeleton"}
                <span class="skeleton"></span>
              {:else if gitStatus(p).kind === "none"}
                <span class="pm-dash">-</span>
              {:else}
                <span class="status {gitStatus(p).cls}">
                  <span class="dot {gitStatus(p).cls}"></span>
                  {gitStatus(p).text}
                </span>
              {/if}
            </td>
            <td>
              {#if (p.tasks || []).length > 0}
                {@const tp = taskProgress(p)}
                <span class="task-chip" class:complete={tp.done === tp.total}>{tp.done}/{tp.total}</span>
              {:else}
                <span class="pm-dash">-</span>
              {/if}
            </td>
            <td>
              {#if ddayLabel(p.deadline)}
                {@const d = ddayLabel(p.deadline)}
                <span class="dday {d.cls}">{d.text}</span>
              {:else}
                <span class="pm-dash">-</span>
              {/if}
            </td>
            <td>
              <span class="status-chip {statusLabel(p)}">{statusLabel(p)}</span>
            </td>
            <td class="right">
              <span class="prio-dots prio-{p.priority || 0}" title="priority {p.priority || 0}">
                {#each [1, 2, 3] as lvl}
                  <span class="prio-dot" class:on={(p.priority || 0) >= lvl}></span>
                {/each}
              </span>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</div>
