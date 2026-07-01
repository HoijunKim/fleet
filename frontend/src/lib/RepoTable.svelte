<script lang="ts">
  export let repos: any[] = [];
  export let selectedPath: string = "";
  export let onSelect: (r: any) => void;

  function pill(r: any) {
    if (r.errMsg) return { cls: "err", text: "!" };
    if (!r.isGit) return { cls: "nogit", text: "-" };
    if (!r.loaded) return { cls: "nogit", text: "..." };
    if (r.dirty) return { cls: "dirty", text: "*" + r.modified };
    return { cls: "clean", text: "ok" };
  }
</script>

<div class="table">
  <table>
    <thead>
      <tr><th>Name</th><th>Branch</th><th>Status</th><th>Up/Dn</th><th>Last</th><th>Lang</th><th>TODO</th></tr>
    </thead>
    <tbody>
      {#each repos as r (r.path)}
        <tr class:sel={r.path === selectedPath} on:click={() => onSelect(r)}>
          <td>{r.name}</td>
          <td>{r.branch}</td>
          <td><span class="pill {pill(r).cls}">{pill(r).text}</span></td>
          <td>
            {#if r.hasUpstream}
              {#if r.ahead > 0}<span class="ahead">up{r.ahead}</span>{/if}
              {#if r.behind > 0}<span class="behind">dn{r.behind}</span>{/if}
            {/if}
          </td>
          <td>{r.lastWhen}</td>
          <td>{r.language}</td>
          <td>{r.todo}</td>
        </tr>
      {/each}
    </tbody>
  </table>
</div>
