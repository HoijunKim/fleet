<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { ListProjects, LoadRepo, Fetch, Pull, Push, DeleteProject, GetConfig, GetProject, AuthStart, AuthStatus, CancelAuth, SignOut, SyncNow, SyncState, DeleteAccount, StartupHealth, DiscardCorruptStore, RevealDataDir } from "../wailsjs/go/main/App";
  import { EventsOn } from "../wailsjs/runtime/runtime";
  import Toolbar from "./lib/Toolbar.svelte";
  import ProjectTable from "./lib/ProjectTable.svelte";
  import DetailPanel from "./lib/DetailPanel.svelte";
  import CommandPalette from "./lib/CommandPalette.svelte";
  import SearchOverlay from "./lib/SearchOverlay.svelte";
  import SettingsModal from "./lib/SettingsModal.svelte";
  import ContextMenu from "./lib/ContextMenu.svelte";
  import AddProjectModal from "./lib/AddProjectModal.svelte";
  import Overview from "./lib/Overview.svelte";
  import Today from "./lib/Today.svelte";
  import { migrateIntel } from "./lib/intelMigrate";
  import { loadSortPref, saveSortPref } from "./lib/sortPref";
  import Graph from "./lib/Graph.svelte";
  import ProjectsFilterBar from "./lib/ProjectsFilterBar.svelte";
  import UnclonedProjects from "./lib/UnclonedProjects.svelte";
  import Onboarding from "./lib/Onboarding.svelte";
  import Toasts from "./lib/Toasts.svelte";
  import AgentOverlay from "./lib/AgentOverlay.svelte";
  import { setProject as agentSetProject, isFleet } from "./lib/agentSession";
  import { toastSuccess, toastError, toastInfo, toastAction } from "./lib/toasts";
  import StartupBanner from "./lib/StartupBanner.svelte";
  import type { main } from "../wailsjs/go/models";
  import { STATUS_ORDER, deadlineSort, daysUntil, allTags } from "./lib/pm";
  import { toggleTheme } from "./lib/theme";

  // Each row is a ProjectView with (for code projects) live git fields merged in.
  let projects: any[] = [];
  let filter = "";
  let statusFilter: "all" | "dirty" | "behind" | "unpushed" | "overdue" = "all";
  let pmStatusFilter: "all" | "active" | "paused" | "done" = "all";
  let highPriorityOnly = false;
  let tagFilter = "all";
  // Restored from localStorage so the chosen sort survives a restart.
  const _sortPref = loadSortPref();
  let sortKey = _sortPref.key;
  let sortDir: "asc" | "desc" = _sortPref.dir;
  let selectedId = "";
  // Ids checked via the per-row checkboxes, for bulk actions. Stale ids (rows
  // that were removed) are simply ignored by everything that reads this.
  let selectedIds: Set<string> = new Set();
  let scanned = false;
  // Whether any scan root is configured. Starts true so the onboarding card
  // doesn't flash before the config loads; refreshAutoFetch sets the real value.
  let rootsConfigured = true;
  // Top-level view: the fleet-wide Overview (default), the project list, or the
  // interactive dependency Graph.
  let view: "today" | "overview" | "projects" | "graph" = "today";

  // ---- cloud auth + sync ---------------------------------------------------
  let auth = { signedIn: false, login: "", avatarUrl: "" };
  let sync = { state: "signedout", lastSyncedUnix: 0, error: "" };
  let authBusy = false;
  let unsubs: Array<() => void> = [];

  async function signIn() {
    if (authBusy) return;
    authBusy = true;
    try {
      const err = await AuthStart();
      // "cancelled" is a soft outcome (the user backed out) - no error toast.
      if (err && err !== "cancelled") toastError("Sign in: " + err);
    } catch (e) {
      toastError("Sign in: " + errText(e));
    } finally {
      authBusy = false;
    }
  }
  function cancelSignIn() {
    CancelAuth(); // unblocks the in-flight AuthStart, which resolves to "cancelled"
  }
  async function signOut() {
    const err = await SignOut();
    if (err) toastError("Sign out: " + err);
  }
  async function syncNow() {
    const err = await SyncNow();
    if (err) toastError("Sync: " + err);
  }
  async function deleteAccount() {
    const err = await DeleteAccount();
    if (err) toastError("Delete account: " + err);
    else toastSuccess("Account deleted");
  }

  let paletteOpen = false;
  let searchOpen = false;
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
    else if (statusFilter === "unpushed") out = out.filter((p) => p.ahead > 0);
    else if (statusFilter === "overdue") {
      out = out.filter((p) => {
        const d = daysUntil(p.deadline);
        return d !== null && d < 0;
      });
    }
    if (pmStatusFilter !== "all") {
      out = out.filter((p) => (p.status || "active") === pmStatusFilter);
    }
    if (highPriorityOnly) out = out.filter((p) => (p.priority || 0) >= 2);
    if (tagFilter !== "all") out = out.filter((p) => (p.tags || []).includes(tagFilter));
    return out;
  })();

  // If the selected tag filter no longer exists on any project (e.g. its last
  // instance was removed), reset it so the list can't get stuck empty.
  $: if (tagFilter !== "all" && !projects.some((p) => (p.tags || []).includes(tagFilter))) tagFilter = "all";

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
  // Scope the shared agentic session to the current selection. Clearing or
  // changing the selection cancels a live agentic run and rescopes the store
  // (and its overlay) to the newly-selected repo (or none).
  // Skipped while a fleet run is active: `selected` is recomputed (a new
  // object) on every `projects` refresh, which Svelte's safe_not_equal
  // treats as "dirty" even when the selected repo hasn't changed - re-firing
  // agentSetProject(selected) here would rescope away from "__fleet__" and
  // CancelAgent() a live fleet run out from under the user. Leaving fleet
  // mode happens explicitly (e.g. RepoChat's launcher calling setProject on
  // a repo), which flips isFleet back to false and re-enables auto-scope.
  $: if (!$isFleet) agentSetProject(selected);
  $: loadingCount = projects.filter((p) => p.type === "code" && !p.loaded && !p.errMsg).length;
  // Derived from the live list so removed rows never inflate the count.
  $: selectedCount = projects.filter((p) => selectedIds.has(p.id)).length;

  // Fleet-wide counts, computed once here and passed to both the Overview
  // tiles and the slim Projects-view StatsHeader (no duplicate counting).
  $: stats = (() => {
    const total = projects.length;
    // "repos" is a distinct count from "total": only actual git repos
    // (type === "code"), not manual (non-git) projects.
    const repos = projects.filter((p) => p.type === "code").length;
    const active = projects.filter((p) => (p.status || "active") === "active").length;
    const clean = projects.filter(
      (p) => p.isGit && p.loaded && !p.dirty && !p.errMsg && !(p.behind > 0)
    ).length;
    const dirty = projects.filter((p) => p.dirty).length;
    const behind = projects.filter((p) => p.behind > 0).length;
    const unpushed = projects.filter((p) => p.ahead > 0).length;
    const overdue = projects.filter((p) => {
      const n = daysUntil(p.deadline);
      return n !== null && n < 0;
    }).length;
    return { total, repos, active, clean, dirty, behind, unpushed, overdue };
  })();

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

  // Extract a readable message from a rejected IPC call (string | Error | unknown).
  function errText(err: any): string {
    if (!err) return "load failed";
    if (typeof err === "string") return err;
    if (err.message) return String(err.message);
    return String(err);
  }

  // Reset a project's row to a non-loading, errored state after a rejected
  // LoadRepo call. Keeps whatever fields were already merged; only flips the
  // loading/error markers so loadingCount can settle and nothing is left
  // spinning forever on an unhandled rejection.
  function markLoadError(id: string, err: any) {
    const i = projects.findIndex((p) => p.id === id);
    if (i < 0) return;
    projects[i] = { ...projects[i], loaded: true, errMsg: errText(err) };
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

  // Run `worker` over `items` with at most `limit` concurrent in flight (a
  // tiny inline concurrency pool - no new dependency). Each worker is
  // expected to handle its own errors; runPool never rejects.
  async function runPool<T>(items: T[], limit: number, worker: (item: T) => Promise<void>): Promise<void> {
    let idx = 0;
    async function lane(): Promise<void> {
      while (idx < items.length) {
        const item = items[idx++];
        await worker(item);
      }
    }
    const lanes = Array.from({ length: Math.min(limit, items.length) }, () => lane());
    await Promise.all(lanes);
  }

  // Load (or reload) git fields for one project and merge the result. On a
  // rejected LoadRepo call, marks the row errored instead of leaving it
  // stuck in a loading state. Guarded by reqGen so a slow call from an
  // earlier generation cannot clobber newer state.
  async function loadGitField(p: any, gen: number): Promise<void> {
    try {
      const v = await LoadRepo(p.path);
      if (gen === reqGen) mergeGit(p.id, v);
    } catch (err) {
      if (gen === reqGen) markLoadError(p.id, err);
    }
  }

  async function loadAll() {
    const gen = ++reqGen;
    const list = await ListProjects();
    if (gen !== reqGen) return;
    projects = list.map(skeleton);
    scanned = true;
    const code = projects.filter((p) => p.type === "code");
    await runPool(code, 6, (p) => loadGitField(p, gen));
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
    await runPool(toLoad, 6, (p) => loadGitField(p, gen));
  }

  // Targeted PM refresh for one project (keeps its git fields). Used after a
  // task/metadata mutation from the detail panel. Uses GetProject(id) - a
  // single-record lookup - instead of ListProjects(), which would trigger a
  // full filesystem rescan of all roots. Guarded by checking the row still
  // exists locally (rather than reqGen, since this is not part of a
  // list-rebuilding fan-out): if the project was removed from the local list
  // in the meantime (e.g. a concurrent delete + reconcile), the stale patch
  // is simply dropped.
  async function refreshProject(id: string) {
    try {
      const fresh = await GetProject(id);
      const i = projects.findIndex((p) => p.id === id);
      if (i < 0) return;
      projects[i] = {
        ...projects[i],
        id: fresh.id, name: fresh.name, type: fresh.type, repoPath: fresh.repoPath,
        status: fresh.status, priority: fresh.priority, deadline: fresh.deadline,
        notes: fresh.notes, tags: fresh.tags, tasks: fresh.tasks,
      };
      projects = projects;
    } catch (err) {
      toastError("Refresh project: " + errText(err));
    }
  }

  // Reload git fields for a single code project (called by git actions).
  // Bumps reqGen so a slower, already in-flight aggregate load (loadAll /
  // reconcile / fetchAll / auto-fetch) cannot overwrite this fresher result,
  // and vice versa - whichever generation is current when a call resolves
  // wins.
  async function refreshRepo(path: string) {
    const proj = projects.find((p) => p.repoPath === path);
    if (!proj) return;
    const gen = ++reqGen;
    try {
      const v = await LoadRepo(path);
      if (gen === reqGen) mergeGit(proj.id, v);
    } catch (err) {
      if (gen === reqGen) markLoadError(proj.id, err);
    }
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
    const gen = ++reqGen;
    let failed = 0;
    await runPool(git, 6, async (p) => {
      try {
        const err = await Fetch(p.path);
        if (err) failed++;
      } catch {
        failed++;
      }
    });
    if (gen !== reqGen) return;
    await runPool(git, 6, (p) => loadGitField(p, gen));
    if (gen !== reqGen) return;
    if (failed > 0) toastError("Fetched " + git.length + " repos, " + failed + " failed");
    else toastSuccess("Fetched " + git.length + " repos");
  }

  // Shared bulk git runner: fan `fn` (Fetch/Pull/Push) out over `list` with the
  // concurrency cap, refresh those repos, and toast a pass/fail summary. Guarded
  // by reqGen so a slower aggregate load cannot clobber the refreshed rows.
  async function bulkGit(
    list: any[],
    fn: (path: string) => Promise<string>,
    verb: string,
    emptyMsg: string
  ) {
    if (list.length === 0) { toastInfo(emptyMsg); return; }
    const gen = ++reqGen;
    let failed = 0;
    await runPool(list, 6, async (p) => {
      try {
        const err = await fn(p.path);
        if (err) failed++;
      } catch {
        failed++;
      }
    });
    if (gen !== reqGen) return;
    await runPool(list, 6, (p) => loadGitField(p, gen));
    if (gen !== reqGen) return;
    if (failed > 0) toastError(verb + " " + list.length + " repos, " + failed + " failed");
    else toastSuccess(verb + " " + list.length + " repos");
  }

  // Toolbar "Pull all" - every git repo. "Push all" - only repos with commits
  // ahead of their upstream.
  async function pullAll() {
    await bulkGit(codeProjects(), Pull, "Pulled", "No git repositories to pull");
  }
  async function pushAll() {
    const ahead = codeProjects().filter((p) => p.ahead > 0);
    await bulkGit(ahead, Push, "Pushed", "No repositories ahead to push");
  }

  // ---- multi-select bulk actions ------------------------------------------
  function toggleSelect(id: string) {
    if (selectedIds.has(id)) selectedIds.delete(id);
    else selectedIds.add(id);
    selectedIds = selectedIds; // trigger reactivity
  }

  // Header select-all over the currently visible rows: if every visible row is
  // already checked, clear them; otherwise check them all.
  function toggleSelectAll() {
    const allSelected = visible.length > 0 && visible.every((p) => selectedIds.has(p.id));
    for (const p of visible) {
      if (allSelected) selectedIds.delete(p.id);
      else selectedIds.add(p.id);
    }
    selectedIds = selectedIds;
  }

  function clearSelection() {
    selectedIds = new Set();
  }

  // The checked, still-existing code repos (bulk git actions ignore manual and
  // non-git rows). Push additionally narrows to repos that are ahead.
  function selectedCodeProjects(): any[] {
    return projects.filter((p) => selectedIds.has(p.id) && p.type === "code" && p.isGit);
  }

  async function bulkFetch() {
    await bulkGit(selectedCodeProjects(), Fetch, "Fetched", "No git repositories selected");
  }
  async function bulkPull() {
    await bulkGit(selectedCodeProjects(), Pull, "Pulled", "No git repositories selected");
  }
  async function bulkPush() {
    const ahead = selectedCodeProjects().filter((p) => p.ahead > 0);
    await bulkGit(ahead, Push, "Pushed", "No selected repositories ahead to push");
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
    saveSortPref(sortKey, sortDir);
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
  // Reactive so the auth-dependent entries follow sign-in state.
  $: paletteActions = [
    { id: "go-today", label: "Go to Today", hint: "view", run: () => { paletteOpen = false; view = "today"; } },
    { id: "go-overview", label: "Go to Overview", hint: "view", run: () => { paletteOpen = false; view = "overview"; } },
    { id: "go-projects", label: "Go to Projects", hint: "view", run: () => { paletteOpen = false; view = "projects"; } },
    { id: "go-graph", label: "Go to Graph", hint: "view", run: () => { paletteOpen = false; view = "graph"; } },
    { id: "add", label: "Add a project", hint: "new", run: () => { paletteOpen = false; addOpen = true; } },
    { id: "refresh", label: "Refresh all projects", hint: "reload", run: () => { paletteOpen = false; manualRefresh(); } },
    { id: "fetchall", label: "Fetch all repositories", hint: "git fetch", run: () => { paletteOpen = false; fetchAll(); } },
    { id: "pullall", label: "Pull all repositories", hint: "git pull", run: () => { paletteOpen = false; pullAll(); } },
    { id: "pushall", label: "Push all ahead repositories", hint: "git push", run: () => { paletteOpen = false; pushAll(); } },
    { id: "theme", label: "Toggle light / dark theme", hint: "theme", run: () => { paletteOpen = false; toggleTheme(); } },
    { id: "settings", label: "Open settings", hint: "config", run: () => { paletteOpen = false; settingsOpen = true; } },
    ...(auth.signedIn
      ? [
          { id: "syncnow", label: "Sync now", hint: "sync", run: () => { paletteOpen = false; syncNow(); } },
          { id: "signout", label: "Sign out", hint: "account", run: () => { paletteOpen = false; signOut(); } },
        ]
      : [{ id: "signin", label: "Sign in with GitHub", hint: "account", run: () => { paletteOpen = false; signIn(); } }]),
  ];

  function onJump(p: any) {
    selectedId = p.id;
    view = "projects";
    paletteOpen = false;
    requestAnimationFrame(() => {
      const el = document.querySelector(".repo-row.selected");
      if (el) (el as HTMLElement).scrollIntoView({ block: "nearest" });
    });
  }

  // From the Overview: select a project and switch to the Projects view.
  function openFromOverview(id: string) {
    selectedId = id;
    view = "projects";
    requestAnimationFrame(() => {
      const el = document.querySelector(".repo-row.selected");
      if (el) (el as HTMLElement).scrollIntoView({ block: "nearest" });
    });
  }

  // Drill-down from the briefing: open a project straight into its Ask-AI tab.
  let detailTab = "";
  let detailNonce = 0;
  function openDrill(id: string) {
    openFromOverview(id);
    detailTab = "chat";
    detailNonce++;
  }

  // From an Overview stat tile: jump to Projects, applying the matching status
  // filter where one exists (dirty/behind/unpushed/overdue), else show all.
  function onTileFilter(key: string) {
    const filterable = ["dirty", "behind", "unpushed", "overdue"];
    statusFilter = (filterable.includes(key) ? key : "all") as typeof statusFilter;
    view = "projects";
  }

  // ---- settings + auto-fetch ---------------------------------------------
  function setupAutoFetch(minutes: number) {
    if (autoFetchTimer) { clearInterval(autoFetchTimer); autoFetchTimer = undefined; }
    if (minutes > 0) {
      autoFetchTimer = setInterval(async () => {
        const git = codeProjects();
        if (git.length === 0) return;
        const gen = ++reqGen;
        await runPool(git, 6, async (p) => {
          try { await Fetch(p.path); } catch { /* surfaced via next LoadRepo */ }
        });
        if (gen !== reqGen) return;
        await runPool(git, 6, (p) => loadGitField(p, gen));
        if (gen !== reqGen) return;
        toastInfo("Auto-fetched " + git.length + " repos");
      }, minutes * 60 * 1000);
    }
  }

  async function refreshAutoFetch() {
    try {
      const cfg = await GetConfig();
      setupAutoFetch(cfg.AutoFetchMinutes || 0);
      rootsConfigured = (cfg.Roots || []).length > 0;
    } catch {
      // ignore config read failures for the timer
    }
  }

  async function onSettingsSaved() {
    await loadAll();
    await refreshAutoFetch();
  }

  // Files fleet could not read at startup. Normally empty; when it is not, the
  // banner is the only thing standing between the user and an app that looks
  // like it forgot everything.
  let health: main.HealthIssue[] = [];

  async function refreshHealth() {
    try {
      health = await StartupHealth();
    } catch {
      health = []; // the health check itself must never break startup
    }
  }

  async function discardCorruptStore() {
    const err = await DiscardCorruptStore();
    if (err) {
      toastError("Start fresh: " + err);
      return;
    }
    await refreshHealth();
    await loadAll();
  }

  // ---- keyboard shortcuts -------------------------------------------------
  function onKey(e: KeyboardEvent) {
    const el = e.target as HTMLElement | null;
    const typing =
      !!el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || (el as any).isContentEditable);

    if ((e.ctrlKey || e.metaKey) && (e.key === "k" || e.key === "K")) {
      if (settingsOpen || addOpen || menu || diffOpen || searchOpen) return;
      e.preventDefault();
      paletteOpen = !paletteOpen;
      return;
    }

    // Ctrl+Shift+F (Cmd+Shift+F on mac): cross-repo search. Modifier-only
    // combos like this never insert a character into a focused input, so -
    // same as Ctrl+K above - it is checked ahead of the typing guard. It
    // still respects the other overlay guards, including the palette, so
    // only one overlay is ever open at a time.
    if ((e.ctrlKey || e.metaKey) && e.shiftKey && (e.key === "F" || e.key === "f")) {
      if (settingsOpen || addOpen || menu || diffOpen || paletteOpen) return;
      e.preventDefault();
      searchOpen = !searchOpen;
      return;
    }

    if (e.key === "Escape") {
      if (menu) { menu = null; return; }
      if (searchOpen) { searchOpen = false; return; }
      if (paletteOpen) { paletteOpen = false; return; }
      if (addOpen) { addOpen = false; return; }
      if (settingsOpen) { settingsOpen = false; return; }
      return;
    }

    if (typing) return;
    if (paletteOpen || searchOpen || settingsOpen || addOpen || menu) return;
    // List navigation / repo actions (j/k/r/f, "/") only apply to the Projects
    // view - never to the Overview or Graph views. Ctrl+K and Escape above are
    // handled before this gate, so they still work everywhere.
    if (view !== "projects") return;

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
    await migrateIntel();
    await refreshHealth();
    await loadAll();
    await refreshAutoFetch();

    try {
      auth = await AuthStatus();
      sync = await SyncState();
    } catch {
      /* offline-first: ignore */
    }
    unsubs.push(EventsOn("auth:changed", (v: any) => { if (v) auth = v; }));
    unsubs.push(EventsOn("sync:changed", (v: any) => {
      if (v && v.state === "error" && sync.state !== "error") {
        toastError("Sync failed: " + (v.error || "unknown error"));
      }
      if (v) sync = v;
    }));
    unsubs.push(EventsOn("sync:remoteEdit", () => toastInfo("Updated on another device")));
    unsubs.push(EventsOn("sync:conflict", (kind: any) => {
      // A deleted project is gone from the UI entirely, so it must not be
      // reported in the same words as an edit that was merely overwritten.
      // Neither expires on its own: the backup is the only copy left.
      const msg = kind === "deleted"
        ? "A project deleted on another device was removed here. Its local copy - including notes and tasks - was saved for recovery."
        : "A local edit was overwritten by a newer version from another device. The overwritten copy was saved for recovery.";
      toastAction(msg, "error", { label: "Show backup", run: () => RevealDataDir() });
    }));
  });

  onDestroy(() => {
    window.removeEventListener("keydown", onKey);
    if (autoFetchTimer) clearInterval(autoFetchTimer);
    for (const off of unsubs) off();
  });
</script>

<StartupBanner issues={health} onReveal={() => RevealDataDir()} onDiscard={discardCorruptStore} />

<Toolbar
  {loadingCount}
  remoteChanges={stats.behind}
  onRemoteChanges={() => (view = "overview")}
  {view}
  onView={(v) => (view = v)}
  onRefresh={manualRefresh}
  onFetchAll={fetchAll}
  onPullAll={pullAll}
  onPushAll={pushAll}
  onAddProject={addProject}
  onOpenSettings={() => (settingsOpen = true)}
  onOpenPalette={() => (paletteOpen = true)}
  onOpenSearch={() => (searchOpen = true)}
  authSignedIn={auth.signedIn}
  authLogin={auth.login}
  authAvatar={auth.avatarUrl}
  {authBusy}
  syncState={sync}
  onSignIn={signIn}
  onCancelSignIn={cancelSignIn}
  onSignOut={signOut}
  onSyncNow={syncNow}
  onDeleteAccount={deleteAccount}
  onRetrySync={syncNow}
/>

{#if view === "projects"}
  {#if scanned && !rootsConfigured && projects.length === 0}
    <Onboarding onAddRoot={() => (settingsOpen = true)} onAddProject={() => (addOpen = true)} />
  {:else}
  <ProjectsFilterBar
    bind:filter
    bind:filterInput
    repos={projects}
    {statusFilter}
    {pmStatusFilter}
    {highPriorityOnly}
    {tagFilter}
    onStatus={(s) => (statusFilter = s)}
    onPmStatus={(s) => (pmStatusFilter = s)}
    onHighPriorityToggle={() => (highPriorityOnly = !highPriorityOnly)}
    onTagFilter={(t) => (tagFilter = t)}
  />

  <UnclonedProjects onCloned={loadAll} />

  {#if selectedCount > 0}
    <div class="bulk-bar">
      <span class="bulk-count">{selectedCount} selected</span>
      <div class="bulk-actions">
        <button class="btn btn-secondary btn-sm" on:click={bulkFetch}>Fetch</button>
        <button class="btn btn-secondary btn-sm" on:click={bulkPull}>Pull</button>
        <button class="btn btn-secondary btn-sm" on:click={bulkPush}>Push</button>
      </div>
      <div class="toolbar-spacer"></div>
      <button class="btn btn-secondary btn-sm" on:click={clearSelection}>Clear</button>
    </div>
  {/if}

  <div class="main">
    <ProjectTable
      projects={visible}
      total={projects.length}
      {selectedId}
      {selectedIds}
      {scanned}
      {sortKey}
      {sortDir}
      onSelect={(p) => (selectedId = p.id)}
      {onSort}
      {onContext}
      onToggleSelect={toggleSelect}
      onToggleSelectAll={toggleSelectAll}
    />
    <DetailPanel
      project={selected}
      onRepoChanged={refreshRepo}
      onProjectChanged={refreshProject}
      onDeleteProject={deleteProject}
      allTags={allTags(projects)}
      requestTab={detailTab}
      requestNonce={detailNonce}
      bind:diffOpen
    />
  </div>
  {/if}
{:else if view === "graph"}
  <Graph onOpen={openFromOverview} {projects} />
{:else if view === "overview"}
  <Overview
    {projects}
    {stats}
    {runPool}
    onOpen={openFromOverview}
    onFilter={onTileFilter}
  />
{:else}
  <Today {projects} onOpen={openFromOverview} onDrill={openDrill} />
{/if}

<Toasts />

<AgentOverlay projectName={selected?.name ?? ""} />

{#if paletteOpen}
  <CommandPalette
    repos={projects}
    actions={paletteActions}
    onClose={() => (paletteOpen = false)}
    {onJump}
  />
{/if}

{#if searchOpen}
  <SearchOverlay onClose={() => (searchOpen = false)} />
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
