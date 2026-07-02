<script lang="ts">
  import { onMount } from "svelte";
  import { SearchAll, OpenEditorAt } from "../../wailsjs/go/main/App";
  import { toastError } from "./toasts";

  export let onClose: () => void;

  type Hit = { repo: string; repoPath: string; file: string; line: number; text: string };
  // idx is the hit's position in the flat `hits` list, so the flat keyboard
  // highlight (selIndex) lines up with the grouped-by-repo rendering below.
  type GroupItem = { hit: Hit; idx: number };
  type Group = { repo: string; items: GroupItem[] };

  let query = "";
  let hits: Hit[] = [];
  // The query string that produced the current `hits` (or "" if none has run
  // yet). Used to tell "type to search" apart from a real "no results", and
  // to decide whether Enter should submit a fresh search or open the
  // highlighted hit.
  let lastQuery: string | null = null;
  let loading = false;
  let selIndex = 0;
  let inputEl: HTMLInputElement;
  let listEl: HTMLElement;
  let debounceTimer: ReturnType<typeof setTimeout> | undefined;
  let reqGen = 0;

  onMount(() => inputEl && inputEl.focus());

  // Group the flat hit list by repo, preserving the backend's ordering
  // (first-seen repo order, hits within a repo in their original order).
  // Each item keeps its index into the flat `hits` array so the keyboard
  // highlight (which walks the flat list) matches the grouped display.
  $: groups = (() => {
    const byRepo = new Map<string, Group>();
    hits.forEach((h, idx) => {
      let g = byRepo.get(h.repo);
      if (!g) { g = { repo: h.repo, items: [] }; byRepo.set(h.repo, g); }
      g.items.push({ hit: h, idx });
    });
    return Array.from(byRepo.values());
  })();

  // Keep the highlighted index in range as results change.
  $: if (selIndex > hits.length - 1) selIndex = Math.max(0, hits.length - 1);

  function scrollSel() {
    requestAnimationFrame(() => {
      const el = listEl && listEl.querySelector(".search-item.active");
      if (el) (el as HTMLElement).scrollIntoView({ block: "nearest" });
    });
  }

  function errText(err: any): string {
    if (!err) return "search failed";
    if (typeof err === "string") return err;
    if (err.message) return String(err.message);
    return String(err);
  }

  async function runSearch(q: string) {
    const term = q.trim();
    lastQuery = term;
    if (!term) {
      hits = [];
      loading = false;
      return;
    }
    const gen = ++reqGen;
    loading = true;
    try {
      const res = await SearchAll(term);
      if (gen !== reqGen) return;
      hits = res || [];
      selIndex = 0;
    } catch (err) {
      if (gen !== reqGen) return;
      hits = [];
      toastError("Search: " + errText(err));
    } finally {
      if (gen === reqGen) loading = false;
    }
  }

  function onQueryInput() {
    if (debounceTimer) clearTimeout(debounceTimer);
    if (!query.trim()) {
      // Clear immediately on a blank query - no call, no stale results.
      // Bump reqGen too, so a still-in-flight search from before the clear
      // cannot land afterward and repopulate `hits` behind the empty state.
      ++reqGen;
      hits = [];
      lastQuery = null;
      loading = false;
      return;
    }
    const q = query;
    debounceTimer = setTimeout(() => runSearch(q), 250);
  }

  async function openHit(h: Hit | undefined) {
    if (!h) return;
    onClose();
    const err = await OpenEditorAt(h.repoPath, h.file);
    if (err) toastError("Open editor: " + err);
  }

  function keydown(e: KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      selIndex = Math.min(selIndex + 1, hits.length - 1);
      scrollSel();
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      selIndex = Math.max(selIndex - 1, 0);
      scrollSel();
    } else if (e.key === "Enter") {
      e.preventDefault();
      const term = query.trim();
      if (term && term !== lastQuery) {
        if (debounceTimer) { clearTimeout(debounceTimer); debounceTimer = undefined; }
        runSearch(query);
      } else {
        openHit(hits[selIndex]);
      }
    } else if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    }
  }
</script>

<!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
<div class="overlay" on:click={onClose}>
  <div class="cmd-panel search-panel" on:click|stopPropagation>
    <div class="cmd-search">
      <span class="cmd-prompt">&gt;</span>
      <input
        class="cmd-input"
        type="text"
        placeholder="Search across all repos"
        bind:this={inputEl}
        bind:value={query}
        on:input={onQueryInput}
        on:keydown={keydown}
        aria-label="Cross-repo search"
      />
      {#if loading}<span class="spinner"></span>{/if}
      <span class="cmd-kbd">Esc</span>
    </div>

    <div class="search-count">
      {#if lastQuery}
        {hits.length} result{hits.length === 1 ? "" : "s"} for "{lastQuery}"
      {/if}
    </div>

    <div class="cmd-list search-list" bind:this={listEl}>
      {#if lastQuery === null}
        <div class="cmd-empty">Type to search across repos</div>
      {:else if hits.length === 0}
        <div class="cmd-empty">No results</div>
      {:else}
        {#each groups as g (g.repo)}
          <div class="search-group">
            <div class="search-group-header">{g.repo}</div>
            {#each g.items as it (it.idx)}
              <button
                class="search-item"
                class:active={it.idx === selIndex}
                on:click={() => openHit(it.hit)}
                on:mousemove={() => (selIndex = it.idx)}
              >
                <span class="search-loc">{it.hit.file}:{it.hit.line}</span>
                <span class="search-text">{it.hit.text}</span>
              </button>
            {/each}
          </div>
        {/each}
      {/if}
    </div>

    <div class="cmd-foot">
      <span><span class="cmd-kbd">up</span><span class="cmd-kbd">dn</span> navigate</span>
      <span><span class="cmd-kbd">enter</span> open</span>
      <span><span class="cmd-kbd">esc</span> close</span>
    </div>
  </div>
</div>
