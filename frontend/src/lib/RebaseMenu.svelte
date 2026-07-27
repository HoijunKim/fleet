<script lang="ts">
  import { RebaseCommits, InteractiveRebase } from "../../wailsjs/go/main/App";
  import { toastSuccess, toastError } from "./toasts";

  export let path: string;
  export let name = "";
  export let onChanged: (path: string) => void;

  type Row = { hash: string; message: string; op: "pick" | "fixup" | "drop" };

  let open = false;
  let loading = false;
  let running = false;
  let base = "";
  let rows: Row[] = [];

  async function load() {
    loading = true;
    try {
      const v = await RebaseCommits(path, 10);
      base = v.base || "";
      rows = (v.commits || []).map((c) => ({ hash: c.hash, message: c.message, op: "pick" as const }));
    } catch {
      base = "";
      rows = [];
    } finally {
      loading = false;
    }
  }

  function toggle() {
    open = !open;
    if (open) load();
  }

  function move(i: number, delta: number) {
    const j = i + delta;
    if (j < 0 || j >= rows.length) return;
    const next = rows.slice();
    [next[i], next[j]] = [next[j], next[i]];
    rows = next;
  }

  function setOp(i: number, op: Row["op"]) {
    const next = rows.slice();
    next[i] = { ...next[i], op };
    rows = next;
  }

  async function run() {
    if (running || !base) return;
    const kept = rows.filter((r) => r.op !== "drop");
    if (kept.length === 0) { toastError("Rewrite: dropping every commit is not allowed"); return; }
    if (rows[0]?.op === "fixup") { toastError("Rewrite: the top commit has nothing above it to fold into"); return; }
    if (!confirm(`Rewrite these ${rows.length} commits? This changes history - only do it on commits you haven't pushed.`)) return;
    running = true;
    try {
      // Actions are sent in the displayed order (newest first). Git replays a
      // rebase todo oldest-first, so reverse for the backend's chronological todo.
      const actions = rows.slice().reverse().map((r) => ({ hash: r.hash, op: r.op }));
      const err = await InteractiveRebase(path, base, actions as any);
      // A conflict leaves the rebase in progress; CommitBox's conflict panel takes
      // over on the rescan, so a conflict message here is informational only.
      if (err) toastError("Rewrite " + name + ": " + err);
      else toastSuccess("Rewrote history on " + name);
      open = false;
      onChanged(path);
    } finally {
      running = false;
    }
  }
</script>

<div class="rewrite">
  <button class="btn btn-secondary btn-sm" on:click={toggle} disabled={running} title="Reorder, drop, or fixup recent local commits (interactive rebase)">
    Rewrite
  </button>
  {#if open}
    <div class="rewrite-pop">
      {#if loading}
        <div class="rewrite-empty"><span class="spinner"></span> loading</div>
      {:else if rows.length === 0 || !base}
        <div class="rewrite-empty">Not enough local commits to rewrite</div>
      {:else}
        <p class="rewrite-hint">Topmost is newest. Reorder, fold into the one above (fixup), or drop.</p>
        <ul class="rewrite-list">
          {#each rows as r, i (r.hash)}
            <li class="rewrite-row" class:dropped={r.op === "drop"}>
              <span class="rewrite-move">
                <button class="rewrite-arrow" on:click={() => move(i, -1)} disabled={i === 0 || running} title="Move up" aria-label="Move up">↑</button>
                <button class="rewrite-arrow" on:click={() => move(i, 1)} disabled={i === rows.length - 1 || running} title="Move down" aria-label="Move down">↓</button>
              </span>
              <span class="rewrite-hash mono">{r.hash.slice(0, 7)}</span>
              <span class="rewrite-msg">{r.message}</span>
              <select class="input rewrite-op" bind:value={r.op} on:change={(e) => setOp(i, (e.currentTarget as HTMLSelectElement).value as Row["op"])} disabled={running} aria-label="Action">
                <option value="pick">keep</option>
                <option value="fixup">fixup</option>
                <option value="drop">drop</option>
              </select>
            </li>
          {/each}
        </ul>
        <div class="rewrite-actions">
          <button class="btn btn-primary btn-sm" on:click={run} disabled={running}>
            {running ? "Rewriting…" : "Rewrite"}
          </button>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .rewrite { position: relative; display: inline-block; }
  .rewrite-pop {
    position: absolute;
    z-index: 20;
    top: calc(100% + 4px);
    left: 0;
    width: 420px;
    max-width: 88vw;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--r-btn);
    box-shadow: var(--shadow-pop, 0 8px 24px rgba(0, 0, 0, 0.25));
    padding: 8px;
  }
  .rewrite-hint { font-size: 11px; color: var(--muted); margin: 2px 4px 6px; }
  .rewrite-list { list-style: none; margin: 0; padding: 0; max-height: 260px; overflow-y: auto; }
  .rewrite-row {
    display: grid;
    grid-template-columns: auto auto 1fr auto;
    align-items: center;
    gap: 8px;
    padding: 3px 4px;
    border-radius: var(--r-btn);
    font-size: 12px;
  }
  .rewrite-row:hover { background: var(--accent-soft); }
  .rewrite-row.dropped .rewrite-msg { text-decoration: line-through; color: var(--faint); }
  .rewrite-move { display: flex; flex-direction: column; line-height: 1; }
  .rewrite-arrow {
    background: none; border: none; cursor: pointer; color: var(--muted);
    font-size: 10px; padding: 0 2px;
  }
  .rewrite-arrow:disabled { opacity: 0.3; cursor: default; }
  .rewrite-hash { color: var(--faint); flex: none; }
  .rewrite-msg { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .rewrite-op { flex: none; font-size: 11px; padding: 2px 4px; }
  .rewrite-actions { display: flex; justify-content: flex-end; margin-top: 8px; }
  .rewrite-empty { font-size: 12px; color: var(--muted); padding: 6px; }
</style>
