<script lang="ts">
  import { Fetch, Pull, Push, MergeUpstream, RebaseUpstream, OpenEditor, OpenTerminal, RenameProject, GitOperation } from "../../wailsjs/go/main/App";
  import { toastSuccess, toastError } from "./toasts";
  import { scale } from "svelte/transition";
  import { ddayLabel } from "./pm";
  import { fadeScaleIn } from "./motion";
  import BranchMenu from "./BranchMenu.svelte";
  import CommitBox from "./CommitBox.svelte";
  import DiffModal from "./DiffModal.svelte";
  import HistoryList from "./HistoryList.svelte";
  import StashPanel from "./StashPanel.svelte";
  import PMSection from "./PMSection.svelte";
  import GitHubBadge from "./GitHubBadge.svelte";
  import SymbolsTab from "./SymbolsTab.svelte";
  import RepoChat from "./RepoChat.svelte";

  export let project: any = null;
  // Reload git fields for a code project (called with its repo path).
  export let onRepoChanged: (path: string) => void;
  // Reload project-management fields (called with the project id).
  export let onProjectChanged: (id: string) => void;
  // Delete a manual project (called with the project id).
  export let onDeleteProject: (id: string) => void;
  // All tags in use across projects, for TagChips autocomplete (threaded to PMSection).
  export let allTags: string[] = [];
  // Bindable flag so App.svelte knows whether the diff modal is open (for
  // suppressing overlapping overlays / the Ctrl+K palette).
  export let diffOpen = false;
  // Drill-down: App bumps requestNonce with requestTab set to jump to a tab
  // (e.g. "chat" from the Today briefing). Applied once per bump.
  export let requestTab = "";
  export let requestNonce = 0;

  // Active detail tab for code projects. Remembered across selections; defaults
  // to Overview. Manual projects ignore this and show only their Tasks content.
  let activeTab: "overview" | "git" | "tasks" | "symbols" | "chat" = "overview";
  let lastNonce = 0;
  $: if (requestNonce !== lastNonce) {
    lastNonce = requestNonce;
    if (requestTab) activeTab = requestTab as typeof activeTab;
  }

  // Diff viewer state - which diff is open (null = closed). `all` shows the
  // whole working-tree diff; otherwise `file` is the single file to diff.
  let diffView: { file: string; all: boolean } | null = null;
  let lastId = "";

  // Inline project rename (manual projects only). renaming = the header title
  // is swapped for an input; renameVal holds the working copy; savingName gates
  // re-entrancy while the rename RPC is in flight.
  let renaming = false;
  let renameVal = "";
  let savingName = false;

  $: diffOpen = diffView !== null;
  $: isCode = !!project && project.type === "code";
  // Upstream diverged: local has commits to push AND remote has commits to
  // integrate. The Pull button stays --ff-only; this drives the Merge/Rebase UI.
  $: diverged = !!project && project.hasUpstream && project.ahead > 0 && project.behind > 0;

  // "merge" / "rebase" when the repo is mid-operation, "" otherwise. Refreshed
  // on selection and after any action that could start or finish one, since an
  // operation begun in a terminal is invisible in ahead/behind.
  let gitOp = "";
  async function refreshGitOp() {
    if (!project || !isCode) {
      gitOp = "";
      return;
    }
    const path = project.path;
    try {
      const op = await GitOperation(path);
      if (project && project.path === path) gitOp = op; // ignore a stale reply
    } catch {
      gitOp = "";
    }
  }

  // Every repo mutation funnels through here so nothing can refresh the project
  // while leaving gitOp stale. Committing is the case that matters: it is what
  // ENDS a merge, and the project object it refreshes keeps the same id, so the
  // reactive block below does not re-run and the banner would otherwise insist
  // the merge is still in progress for the rest of the session.
  function repoChanged(path: string) {
    onRepoChanged(path);
    refreshGitOp();
  }

  // Reset transient panel state when the selection changes.
  $: if (project && project.id !== lastId) {
    lastId = project.id;
    diffView = null;
    renaming = false;
    savingName = false;
    gitOp = "";
    refreshGitOp();
  }

  // Read-only Overview values.
  $: tasks = project && project.tasks ? project.tasks : [];
  $: doneCount = tasks.filter((t: any) => t.done).length;
  $: dday = project ? ddayLabel(project.deadline) : null;
  $: changedCount = project
    ? project.modified || (project.dirtyFiles ? project.dirtyFiles.length : 0)
    : 0;

  $: dotClass = project
    ? isCode
      ? project.errMsg
        ? "err"
        : !project.isGit
          ? "nogit"
          : project.dirty
            ? "dirty"
            : "ok"
      : project.status || "active"
    : "nogit";

  function openDiff(f: string) {
    diffView = { file: f, all: false };
  }
  function openDiffAll() {
    diffView = { file: "", all: true };
  }
  function closeDiff() {
    diffView = null;
  }

  // Rename is offered for manual projects only; a code project's name comes from
  // its scanned folder and RenameProject is a no-op on it.
  function startRename() {
    if (project.type !== "manual") return;
    renameVal = project.name || "";
    renaming = true;
  }
  function cancelRename() {
    renaming = false;
  }
  async function saveRename() {
    // Guard the blur-fires-after-unmount race: Enter and Escape both drop
    // `renaming` before the input unmounts, and that unmount triggers on:blur.
    // Bail if we are no longer editing (blur cannot re-save or clobber an
    // Escape) or if a save is already in flight.
    if (!renaming || savingName) return;
    const name = renameVal.trim();
    if (!name) { toastError("Name cannot be empty"); return; }
    if (name === project.name) { renaming = false; return; }
    savingName = true;
    const e = await RenameProject(project.id, name);
    savingName = false;
    if (e) { toastError("Rename: " + e); return; } // keep the editor open to retry
    renaming = false;
    onProjectChanged(project.id);
  }
  function onRenameKey(e: KeyboardEvent) {
    if (e.key === "Enter") { e.preventDefault(); saveRename(); }
    else if (e.key === "Escape") { e.preventDefault(); cancelRename(); }
  }

  async function doFetch() {
    const e = await Fetch(project.path);
    if (e) toastError("Fetch " + project.name + ": " + e);
    else toastSuccess("Fetched " + project.name);
    repoChanged(project.path);
  }
  async function doPull() {
    const e = await Pull(project.path);
    if (e) toastError("Pull " + project.name + ": " + e);
    else toastSuccess("Pulled " + project.name);
    repoChanged(project.path);
  }
  async function doPush() {
    const e = await Push(project.path);
    if (e) toastError("Push " + project.name + ": " + e);
    else toastSuccess("Pushed " + project.name);
    repoChanged(project.path);
  }
  async function doMerge() {
    const e = await MergeUpstream(project.path);
    if (e) toastError("Merge " + project.name + ": " + e);
    else toastSuccess("Merged upstream into " + project.name);
    repoChanged(project.path);
  }
  async function doRebase() {
    const e = await RebaseUpstream(project.path);
    if (e) toastError("Rebase " + project.name + ": " + e);
    else toastSuccess("Rebased " + project.name + " onto upstream");
    repoChanged(project.path);
  }
  async function doEdit() {
    const e = await OpenEditor(project.path);
    if (e) toastError("Editor: " + e);
    else toastSuccess("Opened editor");
  }
  async function doTerm() {
    const e = await OpenTerminal(project.path);
    if (e) toastError("Terminal: " + e);
    else toastSuccess("Opened terminal");
  }
</script>

{#if project}
  <aside class="detail" transition:scale={fadeScaleIn()}>
    <div class="detail-card">
      <div class="detail-head">
        <span class="dot {dotClass}"></span>
        {#if renaming}
          <!-- svelte-ignore a11y_autofocus -->
          <input
            class="input detail-rename"
            type="text"
            bind:value={renameVal}
            on:keydown={onRenameKey}
            on:blur={saveRename}
            aria-label="Project name"
            autofocus
          />
        {:else if project.type === "manual"}
          <h3 class="detail-title">
            <button class="detail-title-btn" on:click={startRename} title="Click to rename">{project.name}</button>
          </h3>
        {:else}
          <h3 class="detail-title">{project.name}</h3>
        {/if}
        <span class="type-badge {project.type}">{project.type}</span>
      </div>

      {#if isCode}
        <div class="detail-tabs" role="tablist">
          <button
            class="detail-tab"
            class:active={activeTab === "overview"}
            role="tab"
            aria-selected={activeTab === "overview"}
            on:click={() => (activeTab = "overview")}
          >Overview</button>
          <button
            class="detail-tab"
            class:active={activeTab === "git"}
            role="tab"
            aria-selected={activeTab === "git"}
            on:click={() => (activeTab = "git")}
          >Git</button>
          <button
            class="detail-tab"
            class:active={activeTab === "tasks"}
            role="tab"
            aria-selected={activeTab === "tasks"}
            on:click={() => (activeTab = "tasks")}
          >Tasks</button>
          <button
            class="detail-tab"
            class:active={activeTab === "symbols"}
            role="tab"
            aria-selected={activeTab === "symbols"}
            on:click={() => (activeTab = "symbols")}
          >Symbols</button>
          <button
            class="detail-tab"
            class:active={activeTab === "chat"}
            role="tab"
            aria-selected={activeTab === "chat"}
            on:click={() => (activeTab = "chat")}
          >Ask AI</button>
        </div>

        <div class="detail-body">
          {#if activeTab === "overview"}
            <div class="dl">
              {#if project.isGit}
                <div class="dl-row">
                  <span class="dl-label">Branch</span>
                  <span class="dl-value mono">{project.branch || "-"}</span>
                </div>
                <div class="dl-row">
                  <span class="dl-label">Status</span>
                  <span class="status-pills">
                    {#if project.errMsg}
                      <span class="ov-pill over">error</span>
                    {:else if project.dirty || project.ahead > 0 || project.behind > 0}
                      {#if project.dirty}
                        <span class="ov-pill dirty">{changedCount} changed</span>
                      {/if}
                      {#if project.ahead > 0}
                        <span class="ov-pill up">ahead {project.ahead}</span>
                      {/if}
                      {#if project.behind > 0}
                        <span class="ov-pill dn">behind {project.behind}</span>
                      {/if}
                    {:else}
                      <span class="ov-pill">clean</span>
                    {/if}
                  </span>
                </div>
              {:else}
                <div class="dl-row">
                  <span class="dl-label">Git</span>
                  <span class="dl-value">{project.errMsg ? project.errMsg : project.loaded ? "not a repository" : "loading"}</span>
                </div>
              {/if}

              {#if project.lastHash}
                <div class="dl-row">
                  <span class="dl-label">Last commit</span>
                  <span class="dl-value head-line">
                    <span class="head-hash">{project.lastHash.slice(0, 7)}</span>
                    <span class="head-msg">{project.lastMsg}</span>
                    <br />
                    <span class="head-meta">{project.lastWhen}</span>
                  </span>
                </div>
              {/if}

              <div class="dl-row">
                <span class="dl-label">Deadline</span>
                <span class="dl-value">
                  {#if dday}
                    <span class="dday {dday.cls}">{dday.text}</span>
                  {:else}
                    <span class="pm-dash">-</span>
                  {/if}
                </span>
              </div>

              <div class="dl-row">
                <span class="dl-label">Tasks</span>
                <span class="dl-value">
                  {#if tasks.length > 0}
                    <span class="task-chip" class:complete={doneCount === tasks.length}>{doneCount}/{tasks.length}</span>
                  {:else}
                    <span class="pm-dash">-</span>
                  {/if}
                </span>
              </div>
            </div>
          {:else if activeTab === "git"}
            {#if project.isGit}
              <div class="dl">
                <div class="dl-row">
                  <span class="dl-label">Path</span>
                  <span class="dl-value mono">{project.path}</span>
                </div>
                <div class="dl-row">
                  <span class="dl-label">Branch</span>
                  <BranchMenu path={project.path} name={project.name} onChanged={repoChanged} />
                </div>
                {#if project.remote}
                  <div class="dl-row">
                    <span class="dl-label">Remote</span>
                    <span class="dl-value mono">{project.remote}</span>
                  </div>
                {/if}
                <GitHubBadge remote={project.remote || ""} path={project.path} />
                {#if project.hasUpstream}
                  <div class="dl-row">
                    <span class="dl-label">Upstream</span>
                    <span class="dl-value sync-state">
                      {#if project.ahead > 0}<span class="sync-pill ahead" title="commits to push">&uarr;{project.ahead}</span>{/if}
                      {#if project.behind > 0}<span class="sync-pill behind" title="commits to pull">&darr;{project.behind}</span>{/if}
                      {#if project.ahead === 0 && project.behind === 0}<span class="pm-dash">up to date</span>{/if}
                    </span>
                  </div>
                {/if}
                {#if project.dirtyFiles && project.dirtyFiles.length}
                  <div class="dl-row">
                    <span class="dl-label">Changed ({project.dirtyFiles.length})</span>
                    <div class="dirty-files">
                      {#each project.dirtyFiles as f}
                        <button class="df" on:click={() => openDiff(f)} title="View diff">{f}</button>
                      {/each}
                      <button class="df df-all" on:click={openDiffAll} title="View every change as one diff">View all changes</button>
                    </div>
                  </div>
                {/if}
              </div>

              <div class="detail-sep"></div>
              <CommitBox path={project.path} name={project.name} dirtyFiles={project.dirtyFiles} onChanged={repoChanged} />

              <div class="detail-sep"></div>
              <div class="detail-actions">
                <button class="btn btn-primary btn-sm" on:click={doFetch}>Fetch</button>
                <button class="btn btn-primary btn-sm" on:click={doPull}>Pull</button>
                <button
                  class="btn btn-primary btn-sm"
                  on:click={doPush}
                  disabled={!project.hasUpstream}
                  title={project.hasUpstream ? "Push commits to upstream" : "No upstream set for this branch"}
                >Push{project.ahead > 0 ? " " + project.ahead : ""}</button>
                <button class="btn btn-secondary btn-sm" on:click={doEdit}>Editor</button>
                <button class="btn btn-secondary btn-sm" on:click={doTerm}>Terminal</button>
              </div>

              {#if gitOp}
                <!-- The diverged banner stays lit mid-merge (ahead/behind do not
                     change while one is in progress), so offering Merge/Rebase
                     here would put the user one click from an operation fleet
                     must refuse - and whose only unwind would destroy their
                     half-finished work. Say what is happening instead. -->
                <div class="diverged-banner">
                  <div class="diverged-msg">
                    <strong>{gitOp === "rebase" ? "Rebase" : "Merge"} in progress</strong>
                    - started outside fleet.
                    <span class="diverged-hint">
                      Finish or abort it in a terminal; fleet will not touch it.
                    </span>
                  </div>
                  <div class="diverged-actions">
                    <button class="btn btn-secondary btn-sm" on:click={doTerm}>Terminal</button>
                  </div>
                </div>
              {:else if diverged}
                <div class="diverged-banner">
                  <div class="diverged-msg">
                    <strong>Diverged</strong> - {project.ahead} local ahead, {project.behind} on upstream.
                    <span class="diverged-hint">Merge keeps both histories; Rebase replays your commits on top. A conflict stops for you to resolve it in the commit panel.</span>
                  </div>
                  <div class="diverged-actions">
                    <button class="btn btn-secondary btn-sm" on:click={doMerge}>Merge</button>
                    <button class="btn btn-secondary btn-sm" on:click={doRebase}>Rebase</button>
                  </div>
                </div>
              {/if}

              <div class="detail-sep"></div>
              <StashPanel path={project.path} name={project.name} onChanged={repoChanged} />

              <div class="detail-sep"></div>
              <HistoryList path={project.path} />
            {:else}
              <div class="dl">
                <div class="dl-row">
                  <span class="dl-label">Path</span>
                  <span class="dl-value mono">{project.path}</span>
                </div>
                {#if project.errMsg}
                  <div class="dl-row">
                    <span class="dl-label">Error</span>
                    <span class="dl-value err">{project.errMsg}</span>
                  </div>
                {/if}
              </div>

              <div class="detail-sep"></div>
              <div class="detail-actions">
                <button class="btn btn-secondary btn-sm" on:click={doEdit}>Editor</button>
                <button class="btn btn-secondary btn-sm" on:click={doTerm}>Terminal</button>
              </div>
            {/if}
          {:else if activeTab === "symbols"}
            {#if project.isGit}
              <SymbolsTab path={project.path} />
            {:else}
              <div class="dl-row">
                <span class="dl-label">Symbols</span>
                <span class="dl-value">not a repository</span>
              </div>
            {/if}
          {:else if activeTab === "chat"}
            {#if project.isGit}
              <RepoChat {project} />
            {:else}
              <div class="dl-row">
                <span class="dl-label">Ask AI</span>
                <span class="dl-value">not a repository</span>
              </div>
            {/if}
          {:else}
            <PMSection
              {project}
              onChanged={onProjectChanged}
              onDelete={onDeleteProject}
              {allTags}
            />
          {/if}
        </div>
      {:else}
        <div class="detail-body">
          <PMSection
            {project}
            onChanged={onProjectChanged}
            onDelete={onDeleteProject}
              {allTags}
          />
        </div>
      {/if}
    </div>
  </aside>

  {#if diffView !== null}
    <DiffModal path={project.path} file={diffView.file} all={diffView.all} onClose={closeDiff} />
  {/if}
{:else}
  <aside class="detail-empty">
    <span>Select a project</span>
  </aside>
{/if}
