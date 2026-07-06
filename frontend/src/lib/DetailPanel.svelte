<script lang="ts">
  import { Fetch, Pull, OpenEditor, OpenTerminal } from "../../wailsjs/go/main/App";
  import { toastSuccess, toastError } from "./toasts";
  import { ddayLabel } from "./pm";
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
  // Bindable flag so App.svelte knows whether the diff modal is open (for
  // suppressing overlapping overlays / the Ctrl+K palette).
  export let diffOpen = false;

  // Active detail tab for code projects. Remembered across selections; defaults
  // to Overview. Manual projects ignore this and show only their Tasks content.
  let activeTab: "overview" | "git" | "tasks" | "symbols" | "chat" = "overview";

  // Diff viewer state - the file whose diff is open (null = closed).
  let diffFile: string | null = null;
  let lastId = "";

  $: diffOpen = diffFile !== null;
  $: isCode = !!project && project.type === "code";

  // Reset transient panel state when the selection changes.
  $: if (project && project.id !== lastId) {
    lastId = project.id;
    diffFile = null;
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
    diffFile = f;
  }
  function closeDiff() {
    diffFile = null;
  }

  async function doFetch() {
    const e = await Fetch(project.path);
    if (e) toastError("Fetch " + project.name + ": " + e);
    else toastSuccess("Fetched " + project.name);
    onRepoChanged(project.path);
  }
  async function doPull() {
    const e = await Pull(project.path);
    if (e) toastError("Pull " + project.name + ": " + e);
    else toastSuccess("Pulled " + project.name);
    onRepoChanged(project.path);
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
  <aside class="detail">
    <div class="detail-card">
      <div class="detail-head">
        <span class="dot {dotClass}"></span>
        <h3 class="detail-title">{project.name}</h3>
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
                  <BranchMenu path={project.path} name={project.name} onChanged={onRepoChanged} />
                </div>
                {#if project.remote}
                  <div class="dl-row">
                    <span class="dl-label">Remote</span>
                    <span class="dl-value mono">{project.remote}</span>
                  </div>
                {/if}
                <GitHubBadge remote={project.remote || ""} path={project.path} />
                {#if project.dirtyFiles && project.dirtyFiles.length}
                  <div class="dl-row">
                    <span class="dl-label">Changed ({project.dirtyFiles.length})</span>
                    <div class="dirty-files">
                      {#each project.dirtyFiles as f}
                        <button class="df" on:click={() => openDiff(f)} title="View diff">{f}</button>
                      {/each}
                    </div>
                  </div>
                {/if}
              </div>

              <div class="detail-sep"></div>
              <CommitBox path={project.path} name={project.name} dirtyFiles={project.dirtyFiles} onChanged={onRepoChanged} />

              <div class="detail-sep"></div>
              <div class="detail-actions">
                <button class="btn btn-primary btn-sm" on:click={doFetch}>Fetch</button>
                <button class="btn btn-primary btn-sm" on:click={doPull}>Pull</button>
                <button class="btn btn-secondary btn-sm" on:click={doEdit}>Editor</button>
                <button class="btn btn-secondary btn-sm" on:click={doTerm}>Terminal</button>
              </div>

              <div class="detail-sep"></div>
              <StashPanel path={project.path} name={project.name} onChanged={onRepoChanged} />

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
            />
          {/if}
        </div>
      {:else}
        <div class="detail-body">
          <PMSection
            {project}
            onChanged={onProjectChanged}
            onDelete={onDeleteProject}
          />
        </div>
      {/if}
    </div>
  </aside>

  {#if diffFile !== null}
    <DiffModal path={project.path} file={diffFile} onClose={closeDiff} />
  {/if}
{:else}
  <aside class="detail-empty">
    <span>Select a project</span>
  </aside>
{/if}
