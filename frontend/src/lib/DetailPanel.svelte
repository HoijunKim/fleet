<script lang="ts">
  import { Fetch, Pull, OpenEditor, OpenTerminal, RunCommand } from "../../wailsjs/go/main/App";
  export let repo: any = null;
  export let onChanged: (path: string) => void;

  let cmd = "";
  let output = "";

  async function doFetch() { const e = await Fetch(repo.path); output = e || "fetched"; onChanged(repo.path); }
  async function doPull() { const e = await Pull(repo.path); output = e || "pulled"; onChanged(repo.path); }
  async function doEdit() { const e = await OpenEditor(repo.path); if (e) output = e; }
  async function doTerm() { const e = await OpenTerminal(repo.path); if (e) output = e; }
  async function doRun() { if (cmd) output = await RunCommand(repo.path, cmd); }
</script>

{#if repo}
  <div class="detail">
    <h3>{repo.name}</h3>
    <div class="row"><span class="k">path</span> {repo.path}</div>
    {#if repo.lastHash}
      <div class="row"><span class="k">head</span> {repo.lastHash.slice(0, 7)} "{repo.lastMsg}" - {repo.lastAuthor}, {repo.lastWhen}</div>
    {/if}
    {#if repo.remote}<div class="row"><span class="k">remote</span> {repo.remote}</div>{/if}
    {#if repo.errMsg}<div class="row"><span class="k">error</span> {repo.errMsg}</div>{/if}
    {#if repo.dirtyFiles && repo.dirtyFiles.length}
      <div class="row"><span class="k">dirty</span> {repo.dirtyFiles.join("  ")}</div>
    {/if}

    <div class="actions">
      {#if repo.isGit}
        <button on:click={doFetch}>Fetch</button>
        <button on:click={doPull}>Pull</button>
      {/if}
      <button on:click={doEdit}>Editor</button>
      <button on:click={doTerm}>Terminal</button>
    </div>

    <div class="actions">
      <input placeholder="run command" bind:value={cmd} style="flex:1" on:keydown={(e) => e.key === 'Enter' && doRun()} />
      <button on:click={doRun}>Run</button>
    </div>

    {#if output}<pre>{output}</pre>{/if}
  </div>
{/if}
