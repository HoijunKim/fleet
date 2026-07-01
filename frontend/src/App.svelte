<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { ScanRepos, LoadRepo, Fetch, GetConfig } from "../wailsjs/go/main/App";
  import Toolbar from "./lib/Toolbar.svelte";
  import StatsHeader from "./lib/StatsHeader.svelte";
  import RepoTable from "./lib/RepoTable.svelte";
  import DetailPanel from "./lib/DetailPanel.svelte";
  import CommandPalette from "./lib/CommandPalette.svelte";
  import SettingsModal from "./lib/SettingsModal.svelte";
  import ContextMenu from "./lib/ContextMenu.svelte";
  import Toasts from "./lib/Toasts.svelte";
  import { toastSuccess, toastError, toastInfo } from "./lib/toasts";

  let repos: any[] = [];
  let filter = "";
  let statusFilter: "all" | "dirty" | "behind" = "all";
  let sortKey = "";
  let sortDir: "asc" | "desc" = "asc";
  let selectedPath = "";
  let scanned = false;

  let paletteOpen = false;
  let settingsOpen = false;
  let menu: { x: number; y: number; repo: any } | null = null;

  let filterInput: HTMLInputElement | undefined;
  let autoFetchTimer: ReturnType<typeof setInterval> | undefined;

  // ---- derived list: name filter + status filter + sort ------------------
  function statusRank(r: any): number {
    if (r.errMsg) return 0;
    if (r.dirty) return 1;
    if (r.behind > 0) return 2;
    if (r.ahead > 0) return 3;
    if (!r.isGit) return 5;
    return 4; // clean
  }

  function sortVal(r: any, key: string): any {
    switch (key) {
      case "name": return (r.name || "").toLowerCase();
      case "branch": return (r.branch || "").toLowerCase();
      case "status": return statusRank(r);
      case "last": return (r.lastWhen || "").toLowerCase();
      case "todo": return r.todo || 0;
      default: return (r.name || "").toLowerCase();
    }
  }

  $: filtered = (() => {
    let out = repos;
    if (filter) {
      const q = filter.toLowerCase();
      out = out.filter((r) => r.name.toLowerCase().includes(q));
    }
    if (statusFilter === "dirty") out = out.filter((r) => r.dirty);
    else if (statusFilter === "behind") out = out.filter((r) => r.behind > 0);
    return out;
  })();

  $: visible = (() => {
    if (!sortKey) return filtered;
    const dir = sortDir === "asc" ? 1 : -1;
    return [...filtered].sort((a, b) => {
      const va = sortVal(a, sortKey);
      const vb = sortVal(b, sortKey);
      if (va < vb) return -1 * dir;
      if (va > vb) return 1 * dir;
      return 0;
    });
  })();

  $: selected = repos.find((r) => r.path === selectedPath) || null;
  $: loadingCount = repos.filter((r) => !r.loaded && !r.errMsg).length;

  // ---- data flow (live-load contract) ------------------------------------
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

  async function manualRefresh() {
    await loadAll();
    toastInfo("Repositories refreshed");
  }

  async function fetchAll() {
    const gitRepos = repos.filter((r) => r.isGit);
    if (gitRepos.length === 0) { toastInfo("No git repositories to fetch"); return; }
    const results = await Promise.all(gitRepos.map((r) => Fetch(r.path)));
    const failed = results.filter((e) => !!e).length;
    await Promise.all(gitRepos.map((r) => LoadRepo(r.path).then(upsert)));
    if (failed > 0) toastError("Fetched " + gitRepos.length + " repos, " + failed + " failed");
    else toastSuccess("Fetched " + gitRepos.length + " repos");
  }

  async function refreshSelected() {
    const r = selected;
    if (!r) return;
    await refreshOne(r.path);
    toastSuccess("Refreshed " + r.name);
  }

  async function fetchSelected() {
    const r = selected;
    if (!r || !r.isGit) return;
    const err = await Fetch(r.path);
    if (err) toastError("Fetch " + r.name + ": " + err);
    else { toastSuccess("Fetched " + r.name); await refreshOne(r.path); }
  }

  // ---- sort ---------------------------------------------------------------
  function onSort(key: string) {
    if (sortKey !== key) { sortKey = key; sortDir = "asc"; }
    else if (sortDir === "asc") { sortDir = "desc"; }
    else { sortKey = ""; sortDir = "asc"; }
  }

  // ---- selection movement -------------------------------------------------
  function moveSelection(delta: number) {
    if (!visible.length) return;
    let idx = visible.findIndex((r) => r.path === selectedPath);
    if (idx === -1) idx = delta > 0 ? 0 : visible.length - 1;
    else idx = Math.min(Math.max(idx + delta, 0), visible.length - 1);
    selectedPath = visible[idx].path;
    requestAnimationFrame(() => {
      const el = document.querySelector(".repo-row.selected");
      if (el) (el as HTMLElement).scrollIntoView({ block: "nearest" });
    });
  }

  // ---- context menu -------------------------------------------------------
  function onContext(r: any, e: MouseEvent) {
    e.preventDefault();
    selectedPath = r.path;
    menu = { x: e.clientX, y: e.clientY, repo: r };
  }

  // ---- command palette actions -------------------------------------------
  $: paletteActions = [
    { id: "refresh", label: "Refresh all repositories", hint: "reload", run: () => { paletteOpen = false; manualRefresh(); } },
    { id: "fetchall", label: "Fetch all repositories", hint: "git fetch", run: () => { paletteOpen = false; fetchAll(); } },
    { id: "settings", label: "Open settings", hint: "config", run: () => { paletteOpen = false; settingsOpen = true; } },
  ];

  function onJump(r: any) {
    selectedPath = r.path;
    paletteOpen = false;
    requestAnimationFrame(() => {
      const el = document.querySelector(".repo-row.selected");
      if (el) (el as HTMLElement).scrollIntoView({ block: "nearest" });
    });
  }

  // ---- settings + auto-fetch ---------------------------------------------
  function setupAutoFetch(minutes: number) {
    if (autoFetchTimer) { clearInterval(autoFetchTimer); autoFetchTimer = undefined; }
    if (minutes > 0) {
      autoFetchTimer = setInterval(async () => {
        const gitRepos = repos.filter((r) => r.isGit);
        if (gitRepos.length === 0) return;
        await Promise.all(gitRepos.map((r) => Fetch(r.path)));
        await Promise.all(gitRepos.map((r) => LoadRepo(r.path).then(upsert)));
        toastInfo("Auto-fetched " + gitRepos.length + " repos");
      }, minutes * 60 * 1000);
    }
  }

  async function refreshAutoFetch() {
    try {
      const cfg = await GetConfig();
      setupAutoFetch(cfg.AutoFetchMinutes || 0);
    } catch {
      // ignore config read failures for the timer
    }
  }

  async function onSettingsSaved() {
    await loadAll();
    await refreshAutoFetch();
  }

  // ---- keyboard shortcuts -------------------------------------------------
  function onKey(e: KeyboardEvent) {
    const el = e.target as HTMLElement | null;
    const typing =
      !!el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || (el as any).isContentEditable);

    if ((e.ctrlKey || e.metaKey) && (e.key === "k" || e.key === "K")) {
      if (settingsOpen || menu) return;
      e.preventDefault();
      paletteOpen = !paletteOpen;
      return;
    }

    if (e.key === "Escape") {
      if (menu) { menu = null; return; }
      if (paletteOpen) { paletteOpen = false; return; }
      if (settingsOpen) { settingsOpen = false; return; }
      return;
    }

    if (typing) return;
    if (paletteOpen || settingsOpen || menu) return;

    switch (e.key) {
      case "/":
        e.preventDefault();
        filterInput && filterInput.focus();
        break;
      case "j":
      case "ArrowDown":
        e.preventDefault();
        moveSelection(1);
        break;
      case "k":
      case "ArrowUp":
        e.preventDefault();
        moveSelection(-1);
        break;
      case "r":
        e.preventDefault();
        refreshSelected();
        break;
      case "f":
        e.preventDefault();
        fetchSelected();
        break;
    }
  }

  onMount(async () => {
    window.addEventListener("keydown", onKey);
    await loadAll();
    await refreshAutoFetch();
  });

  onDestroy(() => {
    window.removeEventListener("keydown", onKey);
    if (autoFetchTimer) clearInterval(autoFetchTimer);
  });
</script>

<Toolbar
  bind:filter
  bind:filterInput
  {repos}
  {loadingCount}
  {statusFilter}
  onStatus={(s) => (statusFilter = s)}
  onRefresh={manualRefresh}
  onFetchAll={fetchAll}
  onOpenSettings={() => (settingsOpen = true)}
  onOpenPalette={() => (paletteOpen = true)}
/>

<StatsHeader {repos} />

<div class="main">
  <RepoTable
    repos={visible}
    {selectedPath}
    {scanned}
    {sortKey}
    {sortDir}
    onSelect={(r) => (selectedPath = r.path)}
    {onSort}
    {onContext}
  />
  <DetailPanel repo={selected} onChanged={refreshOne} />
</div>

<Toasts />

{#if paletteOpen}
  <CommandPalette
    {repos}
    actions={paletteActions}
    onClose={() => (paletteOpen = false)}
    {onJump}
  />
{/if}

{#if settingsOpen}
  <SettingsModal onClose={() => (settingsOpen = false)} onSaved={onSettingsSaved} />
{/if}

{#if menu}
  <ContextMenu x={menu.x} y={menu.y} repo={menu.repo} onClose={() => (menu = null)} />
{/if}
