<script lang="ts">
  import Logo from "./Logo.svelte";
  import { BrowserOpenURL } from "../../wailsjs/runtime/runtime";

  export let onClose: () => void;

  function openURL(url: string) {
    BrowserOpenURL(url);
  }

  // Each section explains one surface of the app, in the order a new user meets
  // it. Kept as data so the markup stays a simple loop.
  const sections: { title: string; body: string }[] = [
    {
      title: "Today & the brief",
      body:
        "The landing view. It surfaces what needs attention now - overdue tasks, dirty or behind repos, failing CI - and can generate an AI brief that reasons over all of it. The brief and per-repo chats sync across your signed-in devices.",
    },
    {
      title: "Projects table",
      body:
        "Every git repo under your scan roots, plus any manual projects, in one list. Columns show branch, dirty/ahead/behind state, tasks and deadlines. Click a column header to sort (the sort is remembered); click a row to open its detail panel.",
    },
    {
      title: "Detail panel - git",
      body:
        "Per-repo git without a terminal: stage files, commit, amend, push, pull, and merge or rebase a diverged upstream. Switch or delete branches (an unmerged branch offers a force delete). Cherry-pick a commit from another branch. 'History' moves the branch back to a previous point via the reflog. A conflict opens an inline resolve panel - pick a side per file, then continue or abort.",
    },
    {
      title: "Detail panel - project management",
      body:
        "Tasks with status and progress, deadlines, tags, and notes - for code repos and manual projects alike. The cross-project Agenda gathers deadlines and in-progress work across everything.",
    },
    {
      title: "AI: brief, repo chat, agent",
      body:
        "Ask about a repo in its chat, or run an agentic deep-dive that can read, grep and run commands under per-action approval. A fleet-wide launcher asks across every project at once. The default provider shells out to the claude CLI; OpenAI and Gemini are configured in Settings.",
    },
    {
      title: "Search & command palette",
      body:
        "Ctrl+Shift+F searches file contents (case-insensitive by default, with an 'Aa' toggle) and file names across every repo, grouped by repo. Ctrl+K opens the command palette to jump and run actions by keyboard.",
    },
    {
      title: "Sync & data",
      body:
        "Signing in (GitHub) is optional and adds cross-device sync of your projects and intel, last-write-wins. Settings can export everything to a JSON file and import one back, and lists any records sync overwrote so you can restore them.",
    },
    {
      title: "Settings",
      body:
        "Scan roots and depth, editor and terminal commands, AI providers, and integrations. The footer shows the build version. Toggle light/dark from the toolbar.",
    },
  ];
</script>

<!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
<div class="overlay" on:click={onClose}>
  <div class="help-panel" on:click|stopPropagation>
    <div class="help-head">
      <Logo size={18} />
      <h2 class="help-title">What does everything do?</h2>
      <button class="btn btn-secondary btn-sm help-close" on:click={onClose} aria-label="Close help">Close</button>
    </div>
    <div class="help-body">
      {#each sections as s (s.title)}
        <section class="help-section">
          <h3 class="help-section-title">{s.title}</h3>
          <p class="help-section-body">{s.body}</p>
        </section>
      {/each}
    </div>
    <div class="help-credit">
      <span>fleet <span class="v">v0.1.0</span> &middot; Made by <b>H.K</b></span>
      <span class="help-credit-links">
        <button type="button" on:click={() => openURL("https://github.com/hoijun-kim/fleet")}>GitHub</button>
        <button type="button" on:click={() => openURL("https://github.com/hoijun-kim/fleet/blob/master/LICENSE")}>PolyForm NC 1.0.0</button>
      </span>
    </div>
    <p class="help-foot">Press <span class="cmd-kbd">Esc</span> or click outside to close.</p>
  </div>
</div>

<svelte:window on:keydown={(e) => { if (e.key === "Escape") onClose(); }} />

<style>
  .help-panel {
    width: 720px;
    max-width: 92vw;
    max-height: 86vh;
    display: flex;
    flex-direction: column;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--r-modal, 12px);
    box-shadow: var(--shadow-pop, 0 12px 40px rgba(0, 0, 0, 0.35));
    overflow: hidden;
  }
  .help-head {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 14px 18px;
    border-bottom: 1px solid var(--hairline, var(--border));
  }
  .help-title { flex: 1; margin: 0; font-size: 15px; font-weight: 600; color: var(--text); }
  .help-body { padding: 8px 18px 4px; overflow-y: auto; }
  .help-section { padding: 10px 0; border-bottom: 1px solid var(--hairline, var(--border)); }
  .help-section:last-child { border-bottom: none; }
  .help-section-title { margin: 0 0 4px; font-size: 13px; font-weight: 600; color: var(--accent, var(--text)); }
  .help-section-body { margin: 0; font-size: 12.5px; line-height: 1.55; color: var(--muted); }
  .help-credit { display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap; padding: 12px 18px 0; font-size: 12px; color: var(--muted); border-top: 1px solid var(--hairline, var(--border)); margin-top: 4px; }
  .help-credit b { color: var(--text); }
  .help-credit .v { color: var(--faint); font-variant-numeric: tabular-nums; }
  .help-credit-links { display: flex; gap: 12px; }
  .help-credit-links button { background: 0; border: 0; padding: 0; font: inherit; font-size: 12px; color: var(--accent, var(--text)); cursor: pointer; }
  .help-credit-links button:hover { text-decoration: underline; }
  .help-foot { margin: 0; padding: 8px 18px 14px; font-size: 11.5px; color: var(--faint); }
</style>
