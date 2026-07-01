<script lang="ts">
  export let repos: any[] = [];
  export let selectedPath: string = "";
  export let scanned: boolean = false;
  export let onSelect: (r: any) => void;

  // Returns null when the row should render a loading skeleton instead.
  function status(r: any): { cls: string; text: string } | null {
    if (!r.loaded && !r.errMsg) return null;
    if (r.errMsg) return { cls: "err", text: "error" };
    if (!r.isGit) return { cls: "nogit", text: "not a repo" };
    if (r.dirty) return { cls: "dirty", text: `${r.modified} changed` };
    return { cls: "ok", text: "clean" };
  }
</script>

<div class="table-wrap">
  {#if scanned && repos.length === 0}
    <div class="empty-state">
      <div>No repositories found.</div>
      <div class="hint">Edit your roots in the config.</div>
    </div>
  {:else}
    <table class="repo-table">
      <thead>
        <tr>
          <th>Name</th>
          <th>Branch</th>
          <th>Status</th>
          <th>Up / Dn</th>
          <th>Last</th>
          <th>Lang</th>
          <th class="right">TODO</th>
        </tr>
      </thead>
      <tbody>
        {#each repos as r (r.path)}
          <tr
            class="repo-row"
            class:selected={r.path === selectedPath}
            class:nogit={!r.isGit && r.loaded}
            on:click={() => onSelect(r)}
          >
            <td class="cell-name">{r.name}</td>
            <td class="cell-branch">{r.branch || ""}</td>
            <td>
              {#if status(r)}
                {@const s = status(r)}
                <span class="status {s.cls}">
                  <span class="dot {s.cls}"></span>
                  {s.text}
                </span>
              {:else}
                <span class="skeleton"></span>
              {/if}
            </td>
            <td>
              {#if r.hasUpstream}
                <span class="updn">
                  {#if r.ahead > 0}<span class="updn-pill up">up {r.ahead}</span>{/if}
                  {#if r.behind > 0}<span class="updn-pill dn">dn {r.behind}</span>{/if}
                </span>
              {/if}
            </td>
            <td class="cell-last">{r.lastWhen || ""}</td>
            <td class="cell-lang">{r.language || ""}</td>
            <td class="right">
              {#if r.todo > 0}<span class="todo-chip">{r.todo}</span>{/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</div>
