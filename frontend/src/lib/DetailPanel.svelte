<script lang="ts">
  import { Fetch, Pull, OpenEditor, OpenTerminal, RunCommand } from "../../wailsjs/go/main/App";
  import { toastSuccess, toastError } from "./toasts";
  import BranchMenu from "./BranchMenu.svelte";
  import CommitBox from "./CommitBox.svelte";
  import DiffModal from "./DiffModal.svelte";
  import HistoryList from "./HistoryList.svelte";
  import StashPanel from "./StashPanel.svelte";
  import PMSection from "./PMSection.svelte";

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

  let cmd = "";
  let output = "";
  let running = false;

  // Diff viewer state - the file whose diff is open (null = closed).
  let diffFile: string | null = null;
  let lastId = "";

  $: diffOpen = diffFile !== null;
  $: isCode = !!project && project.type === "code";

  // Reset transient panel state when the selection changes.
  $: if (project && project.id !== lastId) {
    lastId = project.id;
    diffFile = null;
    output = "";
    cmd = "";
  }

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
    output = e || "Fetched.";
    if (e) toastError("Fetch " + project.name + ": " + e);
    else toastSuccess("Fetched " + project.name);
    onRepoChanged(project.path);
  }
  async function doPull() {
    const e = await Pull(project.path);
    output = e || "Pulled.";
    if (e) toastError("Pull " + project.name + ": " + e);
    else toastSuccess("Pulled " + project.name);
    onRepoChanged(project.path);
  }
  async function doEdit() {
    const e = await OpenEditor(project.path);
    if (e) { output = e; toastError("Editor: " + e); }
    else toastSuccess("Opened editor");
  }
  async function doTerm() {
    const e = await OpenTerminal(project.path);
    if (e) { output = e; toastError("Terminal: " + e); }
    else toastSuccess("Opened terminal");
  }
  async function doRun() {
    if (!cmd) return;
    running = true;
    try { output = await RunCommand(project.path, cmd); }
    finally { running = false; }
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

      <div class="detail-body">
        <PMSection
          {project}
          onChanged={onProjectChanged}
          onDelete={onDeleteProject}
        />

        {#if isCode}
          <div class="detail-sep"></div>

          <div class="dl">
            <div class="dl-row">
              <span class="dl-label">Path</span>
              <span class="dl-value mono">{project.path}</span>
            </div>

            {#if project.isGit}
              <div class="dl-row">
                <span class="dl-label">Branch</span>
                <BranchMenu path={project.path} name={project.name} onChanged={onRepoChanged} />
              </div>
            {/if}

            {#if project.lastHash}
              <div class="dl-row">
                <span class="dl-label">Head</span>
                <span class="dl-value head-line">
                  <span class="head-hash">{project.lastHash.slice(0, 7)}</span>
                  <span class="head-msg">{project.lastMsg}</span>
                  <br />
                  <span class="head-meta">{project.lastAuthor} - {project.lastWhen}</span>
                </span>
              </div>
            {/if}

            {#if project.remote}
              <div class="dl-row">
                <span class="dl-label">Remote</span>
                <span class="dl-value mono">{project.remote}</span>
              </div>
            {/if}

            {#if project.errMsg}
              <div class="dl-row">
                <span class="dl-label">Error</span>
                <span class="dl-value err">{project.errMsg}</span>
              </div>
            {/if}

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

          {#if project.isGit}
            <div class="detail-sep"></div>
            <CommitBox path={project.path} name={project.name} dirtyFiles={project.dirtyFiles} onChanged={onRepoChanged} />
          {/if}

          <div class="detail-sep"></div>

          <div class="detail-actions">
            {#if project.isGit}
              <button class="btn btn-primary btn-sm" on:click={doFetch}>Fetch</button>
              <button class="btn btn-primary btn-sm" on:click={doPull}>Pull</button>
            {/if}
            <button class="btn btn-secondary btn-sm" on:click={doEdit}>Editor</button>
            <button class="btn btn-secondary btn-sm" on:click={doTerm}>Terminal</button>
          </div>

          <div class="detail-sep"></div>

          <div class="section-label">Run command</div>
          <div class="runner">
            <input
              class="input mono"
              type="text"
              placeholder="git status"
              bind:value={cmd}
              on:keydown={(e) => e.key === "Enter" && doRun()}
              aria-label="Command to run"
            />
            <button class="btn btn-secondary btn-sm" on:click={doRun} disabled={running || !cmd}>
              {running ? "..." : "Run"}
            </button>
          </div>

          {#if output}<pre class="output">{output}</pre>{/if}

          {#if project.isGit}
            <div class="detail-sep"></div>
            <StashPanel path={project.path} name={project.name} onChanged={onRepoChanged} />

            <div class="detail-sep"></div>
            <HistoryList path={project.path} />
          {/if}
        {/if}
      </div>
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
