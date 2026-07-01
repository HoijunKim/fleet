<script lang="ts">
  import { Fetch, Pull, OpenEditor, OpenTerminal, RunCommand } from "../../wailsjs/go/main/App";
  export let repo: any = null;
  export let onChanged: (path: string) => void;

  let cmd = "";
  let output = "";
  let running = false;

  $: dotClass = repo
    ? repo.errMsg
      ? "err"
      : !repo.isGit
        ? "nogit"
        : repo.dirty
          ? "dirty"
          : "ok"
    : "nogit";

  async function doFetch() { const e = await Fetch(repo.path); output = e || "Fetched."; onChanged(repo.path); }
  async function doPull()  { const e = await Pull(repo.path);  output = e || "Pulled.";  onChanged(repo.path); }
  async function doEdit()  { const e = await OpenEditor(repo.path);   if (e) output = e; }
  async function doTerm()  { const e = await OpenTerminal(repo.path); if (e) output = e; }
  async function doRun() {
    if (!cmd) return;
    running = true;
    try { output = await RunCommand(repo.path, cmd); }
    finally { running = false; }
  }
</script>

{#if repo}
  <aside class="detail">
    <div class="detail-card">
      <div class="detail-head">
        <span class="dot {dotClass}"></span>
        <h3 class="detail-title">{repo.name}</h3>
      </div>

      <div class="detail-body">
        <div class="dl">
          <div class="dl-row">
            <span class="dl-label">Path</span>
            <span class="dl-value mono">{repo.path}</span>
          </div>

          {#if repo.lastHash}
            <div class="dl-row">
              <span class="dl-label">Head</span>
              <span class="dl-value head-line">
                <span class="head-hash">{repo.lastHash.slice(0, 7)}</span>
                <span class="head-msg">{repo.lastMsg}</span>
                <br />
                <span class="head-meta">{repo.lastAuthor} - {repo.lastWhen}</span>
              </span>
            </div>
          {/if}

          {#if repo.remote}
            <div class="dl-row">
              <span class="dl-label">Remote</span>
              <span class="dl-value mono">{repo.remote}</span>
            </div>
          {/if}

          {#if repo.errMsg}
            <div class="dl-row">
              <span class="dl-label">Error</span>
              <span class="dl-value err">{repo.errMsg}</span>
            </div>
          {/if}

          {#if repo.dirtyFiles && repo.dirtyFiles.length}
            <div class="dl-row">
              <span class="dl-label">Changed ({repo.dirtyFiles.length})</span>
              <div class="dirty-files">
                {#each repo.dirtyFiles as f}
                  <span class="df">{f}</span>
                {/each}
              </div>
            </div>
          {/if}
        </div>

        <div class="detail-sep"></div>

        <div class="detail-actions">
          {#if repo.isGit}
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
      </div>
    </div>
  </aside>
{:else}
  <aside class="detail-empty">
    <span>Select a repository</span>
  </aside>
{/if}
