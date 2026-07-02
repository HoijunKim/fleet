<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { ListProjects, LoadRepo, Fetch, DeleteProject, GetConfig } from "../wailsjs/go/main/App";
  import Toolbar from "./lib/Toolbar.svelte";
  import StatsHeader from "./lib/StatsHeader.svelte";
  import ProjectTable from "./lib/ProjectTable.svelte";
  import DetailPanel from "./lib/DetailPanel.svelte";
  import CommandPalette from "./lib/CommandPalette.svelte";
  import SettingsModal from "./lib/SettingsModal.svelte";
  import ContextMenu from "./lib/ContextMenu.svelte";
  import AddProjectModal from "./lib/AddProjectModal.svelte";
  import Toasts from "./lib/Toasts.svelte";
  import { toastSuccess, toastError, toastInfo } from "./lib/toasts";
  import { STATUS_ORDER, deadlineSort } from "./lib/pm";

  // Each row is a ProjectView with (for code projects) live git fields merged in.
  let projects: any[] = [];
  let filter = "";
  let statusFilter: "all" | "dirty" | "behind" = "all";
  let pmStatusFilter: "all" | "active" | "paused" | "done" = "all";
  let highPriorityOnly = false;
  let sortKey = "";
  let sortDir: "asc" | "desc" = "asc";
  let selectedId = "";
  let scanned = false;

  let paletteOpen = false;
  let settingsOpen = false;
  let addOpen = false;
  let menu: { x: number; y: number; project: any } | null = null;
  let diffOpen = false;

  let filterInput: HTMLInputElement | undefined;
  let autoFetchTimer: ReturnType<typeof setInterval> | undefined;

  // Monotonic request generation so a slow list load cannot clobber a newer one.
  let reqGen = 0;

  // ---- sort / filter ------------------------------------------------------
  function gitRank(p: any): number {
    if (p.type !== "code") return 6;
    if (p.errMsg) return 0;
    if (p.dirty) return 1;
    if (p.behind > 0) return 2;
    if (p.ahead > 0) return 3;
    if (!p.isGit) return 5;
    return 4; // clean
  }

  function taskRemaining(p: any): number {
    const tasks = p.tasks || [];
    return tasks.filter((t: any) => !t.done).length;
  }

  function sortVal(p: any, key: string): any {
    switch (key) {
      case "name": return (p.name || "").toLowerCase();
      case "type": return p.type || "";
      case "git": return gitRank(p);
      case "tasks": return taskRemaining(p);
      case "deadline": return deadlineSort(p.deadline);
      case "status": return STATUS_ORDER[p.status || "active"] ?? 0;
      case "priority": return p.priority || 0;
      default: return (p.name || "").toLowerCase();
    }
  }

  $: filtered = (() => {
    let out = projects;
    if (filter) {
      const q = filter.toLowerCase();
      out = out.filter((p) => (p.name || "").toLowerCase().includes(q));
    }
    if (statusFilter === "dirty") out = out.filter((p) => p.dirty);
    else if (statusFilter === "behind") out = out.filter((p) => p.behind > 0);
    if (pmStatusFilter !== "all") {
      out = out.filter((p) => (p.status || "active") === pmStatusFilter);
    }
    if (highPriorityOnly) out = out.filter((p) => (p.priority || 0) >= 2);
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

  $: selected = projects.find((p) => p.id === selectedId) || null;
  $: loadingCount = projects.filter((p) => p.type === "code" && !p.loaded && !p.errMsg).length;

  // ---- data flow (live-load contract) ------------------------------------
  const GIT_FIELDS = [
    "isGit", "branch", "dirty", "modified", "ahead", "behind", "hasUpstream",
    "remote", "dirtyFiles", "lastHash", "lastMsg", "lastAuthor", "lastWhen",
    "language", "todo", "errMsg", "loaded",
  ];

  // Merge git fields from a LoadRepo result onto the project with this id.
  function mergeGit(id: string, v: any) {
    const i = projects.findIndex((p) => p.id === id);
    if (i < 0) return;
    const merged = { ...projects[i], path: v.path };
    for (const f of GIT_FIELDS) merged[f] = v[f];
    projects[i] = merged;
    projects = projects;
  }

  // Build a fresh row from a ProjectView (before git fields are loaded).
  function skeleton(pv: any): any {
    return {
      ...pv,
      path: pv.repoPath,
      isGit: false,
      dirtyFiles: [],
      loaded: pv.type === "manual", // manual rows are complete immediately
    };
  }

  async function loadAll() {
    const gen = ++reqGen;
    const list = await ListProjects();
    if (gen !== reqGen) return;
    projects = list.map(skeleton);
    scanned = true;
    const code = projects.filter((p) => p.type === "code");
    await Promise.all(
      code.map((p) =>
        LoadRepo(p.repoPath).then((v) => {
          if (gen === reqGen) mergeGit(p.id, v);
        })
      )
    );
  }

  // Reconciling refresh: keep already-merged git fields, pull fresh PM fields,
  // add newly-created projects, drop deleted ones. Used after add/delete.
  async function reconcile() {
    const gen = ++reqGen;
    const list = await ListProjects();
    if (gen !== reqGen) return;
    const prev = new Map(projects.map((p) => [p.id, p]));
    const next = list.map((fresh: any) => {
      const old = prev.get(fresh.id);
      if (old) {
        return {
          ...old,
          name: fresh.name, type: fresh.type, repoPath: fresh.repoPath,
          status: fresh.status, priority: fresh.priority, deadline: fresh.deadline,
          notes: fresh.notes, tags: fresh.tags, tasks: fresh.tasks,
        };
      }
      return skeleton(fresh);
    });
    projects = next;
    if (selectedId && !projects.some((p) => p.id === selectedId)) selectedId = "";
    const toLoad = next.filter((p) => p.type === "code" && !p.loaded);
    await Promise.all(
      toLoad.map((p) =>
        LoadRepo(p.repoPath).then((v) => {
          if (gen === reqGen) mergeGit(p.id, v);
        })
      )
    );
  }

  // Targeted PM refresh for one project (keeps its git fields). Used after a
  // task/metadata mutation from the detail panel.
  async function refreshProject(id: string) {
    const list = await ListProjects();
    const fresh = list.find((p: any) => p.id === id);
    const i = projects.findIndex((p) => p.id === id);
    if (!fresh) {
      if (i >= 0) { projects = projects.filter((p) => p.id !== id); }
      if (selectedId === id) selectedId = "";
      return;
    }
    if (i < 0) return;
    projects[i] = {
      ...projects[i],
      name: fresh.name, type: fresh.type, repoPath: fresh.repoPath,
      status: fresh.status, priority: fresh.priority, deadline: fresh.deadline,
      notes: fresh.notes, tags: fresh.tags, tasks: fresh.tasks,
    };
    projects = projects;
  }

  // Reload git fields for a single code project (called by git actions).
  async function refreshRepo(path: string) {
    const proj = projects.find((p) => p.repoPath === path);
    if (!proj) return;
    const v = await LoadRepo(path);
    mergeGit(proj.id, v);
  }

  function codeProjects(): any[] {
    return projects.filter((p) => p.type === "code" && p.isGit);
  }

  async function manualRefresh() {
    await loadAll();
    toastInfo("Projects refreshed");
  }

  async function fetchAll() {
    const git = codeProjects();
    if (git.length === 0) { toastInfo("No git repositories to fetch"); return; }
    const results = await Promise.all(git.map((p) => Fetch(p.path)));
    const failed = results.filter((e) => !!e).length;
    await Promise.all(git.map((p) => LoadRepo(p.path).then((v) => mergeGit(p.id, v))));
    if (failed > 0) toastError("Fetched " + git.length + " repos, " + failed + " failed");
    else toastSuccess("Fetched " + git.length + " repos");
  }

  async function refreshSelected() {
    const p = selected;
    if (!p) return;
    if (p.type === "code") await refreshRepo(p.repoPath);
    else await refreshProject(p.id);
    toastSuccess("Refreshed " + p.name);
  }

  async function fetchSelected() {
    const p = selected;
    if (!p || p.type !== "code" || !p.isGit) return;
    const err = await Fetch(p.path);
    if (err) toastError("Fetch " + p.name + ": " + err);
    else { toastSuccess("Fetched " + p.name); await refreshRepo(p.repoPath); }
  }

  async function addProject() {
    addOpen = true;
  }

  async function onProjectAdded(id: string) {
    await reconcile();
    selectedId = id;
    requestAnimationFrame(() => {
      const el = document.querySelector(".repo-row.selected");
      if (el) (el as HTMLElement).scrollIntoView({ block: "nearest" });
    });
  }

  async function deleteProject(id: string) {
    const p = projects.find((x) => x.id === id);
    const err = await DeleteProject(id);
    if (err) { toastError("Delete: " + err); return; }
    toastSuccess("Deleted " + (p ? p.name : "project"));
    await reconcile();
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
    let idx = visible.findIndex((p) => p.id === selectedId);
    if (idx === -1) idx = delta > 0 ? 0 : visible.length - 1;
    else idx = Math.min(Math.max(idx + delta, 0), visible.length - 1);
    selectedId = visible[idx].id;
    requestAnimationFrame(() => {
      const el = document.querySelector(".repo-row.selected");
      if (el) (el as HTMLElement).scrollIntoView({ block: "nearest" });
    });
  }

  // ---- context menu -------------------------------------------------------
  function onContext(p: any, e: MouseEvent) {
    e.preventDefault();
    selectedId = p.id;
    menu = { x: e.clientX, y: e.clientY, project: p };
  }

  // ---- command palette actions -------------------------------------------
  $: paletteActions = [
    { id: "add", label: "Add a project", hint: "new", run: () => { paletteOpen = false; addOpen = true; } },
    { id: "refresh", label: "Refresh all projects", hint: "reload", run: () => { paletteOpen = false; manualRefresh(); } },
    { id: "fetchall", label: "Fetch all repositories", hint: "git fetch", run: () => { paletteOpen = false; fetchAll(); } },
    { id: "settings", label: "Open settings", hint: "config", run: () => { paletteOpen = false; settingsOpen = true; } },
  ];

  function onJump(p: any) {
    selectedId = p.id;
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
        const git = codeProjects();
        if (git.length === 0) return;
        await Promise.all(git.map((p) => Fetch(p.path)));
        await Promise.all(git.map((p) => LoadRepo(p.path).then((v) => mergeGit(p.id, v))));
        toastInfo("Auto-fetched " + git.length + " repos");
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
      if (settingsOpen || addOpen || menu || diffOpen) return;
      e.preventDefault();
      paletteOpen = !paletteOpen;
      return;
    }

    if (e.key === "Escape") {
      if (menu) { menu = null; return; }
      if (paletteOpen) { paletteOpen = false; return; }
      if (addOpen) { addOpen = false; return; }
      if (settingsOpen) { settingsOpen = false; return; }
      return;
    }

    if (typing) return;
    if (paletteOpen || settingsOpen || addOpen || menu) return;

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
  repos={projects}
  {loadingCount}
  {statusFilter}
  {pmStatusFilter}
  {highPriorityOnly}
  onStatus={(s) => (statusFilter = s)}
  onPmStatus={(s) => (pmStatusFilter = s)}
  onHighPriorityToggle={() => (highPriorityOnly = !highPriorityOnly)}
  onRefresh={manualRefresh}
  onFetchAll={fetchAll}
  onAddProject={addProject}
  onOpenSettings={() => (settingsOpen = true)}
  onOpenPalette={() => (paletteOpen = true)}
/>

<StatsHeader repos={projects} />

<div class="main">
  <ProjectTable
    projects={visible}
    {selectedId}
    {scanned}
    {sortKey}
    {sortDir}
    onSelect={(p) => (selectedId = p.id)}
    {onSort}
    {onContext}
  />
  <DetailPanel
    project={selected}
    onRepoChanged={refreshRepo}
    onProjectChanged={refreshProject}
    onDeleteProject={deleteProject}
    bind:diffOpen
  />
</div>

<Toasts />

{#if paletteOpen}
  <CommandPalette
    repos={projects}
    actions={paletteActions}
    onClose={() => (paletteOpen = false)}
    {onJump}
  />
{/if}

{#if settingsOpen}
  <SettingsModal onClose={() => (settingsOpen = false)} onSaved={onSettingsSaved} />
{/if}

{#if addOpen}
  <AddProjectModal onClose={() => (addOpen = false)} onAdded={onProjectAdded} />
{/if}

{#if menu}
  <ContextMenu x={menu.x} y={menu.y} repo={menu.project} onClose={() => (menu = null)} />
{/if}
