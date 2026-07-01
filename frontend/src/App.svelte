<script lang="ts">
  import { onMount } from "svelte";
  import { ScanRepos, LoadRepo } from "../wailsjs/go/main/App";
  import Toolbar from "./lib/Toolbar.svelte";
  import RepoTable from "./lib/RepoTable.svelte";
  import DetailPanel from "./lib/DetailPanel.svelte";

  let repos: any[] = [];
  let filter = "";
  let selectedPath = "";
  let scanned = false;

  $: visible = filter
    ? repos.filter((r) => r.name.toLowerCase().includes(filter.toLowerCase()))
    : repos;
  $: selected = repos.find((r) => r.path === selectedPath) || null;
  $: loadingCount = repos.filter((r) => !r.loaded && !r.errMsg).length;

  function upsert(v: any) {
    const i = repos.findIndex((r) => r.path === v.path);
    if (i >= 0) { repos[i] = v; repos = repos; } else { repos = [...repos, v]; }
  }

  async function loadAll() {
    const skeletons = await ScanRepos();
    repos = skeletons;
    scanned = true;
    await Promise.all(skeletons.map((s: any) => LoadRepo(s.path).then(upsert)));
  }

  async function refreshOne(path: string) {
    const v = await LoadRepo(path);
    upsert(v);
  }

  onMount(loadAll);
</script>

<Toolbar
  bind:filter
  {repos}
  {loadingCount}
  onRefresh={loadAll}
  onFetchAll={async () => {
    const { Fetch } = await import("../wailsjs/go/main/App");
    await Promise.all(repos.filter((r) => r.isGit).map((r) => Fetch(r.path)));
    await loadAll();
  }}
/>

<div class="main">
  <RepoTable
    repos={visible}
    {selectedPath}
    {scanned}
    onSelect={(r) => (selectedPath = r.path)}
  />
  <DetailPanel repo={selected} onChanged={refreshOne} />
</div>
