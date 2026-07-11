<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { SearchAll, SearchFiles, OpenEditorAt } from "../../wailsjs/go/main/App";
  import { deriveChips, visibleIndices, clampSel } from "./searchFilter";
  import { toastError } from "./toasts";

  export let onClose: () => void;

  // Content hits carry line/text; file-name hits omit them (path only), so
  // both `SearchAll` (SearchHit) and `SearchFiles` (FileHit) results flow into
  // the same `hits` array. Keeping line/text optional lets each item render
  // by its own shape regardless of the current mode.
  type Hit = { repo: string; repoPath: string; file: string; line?: number; text?: string };
  // idx is the hit's position in the flat `hits` list, so the flat keyboard
  // highlight (selIndex) lines up with the grouped-by-repo rendering below.
  type GroupItem = { hit: Hit; idx: number };
  type Group = { repo: string; items: GroupItem[] };

  let query = "";
  let hits: Hit[] = [];
  // "content" -> SearchAll (grep), "files" -> SearchFiles (file-name search).
  let mode: "content" | "files" = "content";
  // Repos the user has toggled OFF via the chip row. Reassigned (never mutated
  // in place) so Svelte reactivity picks up membership changes.
  let hidden = new Set<string>();
  // The query string that produced the current `hits` (or null if none has
  // run yet). Set ONLY when a response lands and `hits` is assigned, so the
  // count/label and Enter's "is this current?" check always match what's
  // actually shown (never the in-flight query while a search is pending).
  let resultsQuery: string | null = null;
  let loading = false;
  let selIndex = 0;
  let inputEl: HTMLInputElement;
  let listEl: HTMLElement;
  let debounceTimer: ReturnType<typeof setTimeout> | undefined;
  let reqGen = 0;

  onMount(() => inputEl && inputEl.focus());

  // Chips list the repos present in the raw hits (first-seen order + counts),
  // independent of what is currently hidden - so a chip stays visible (to be
  // re-enabled) even while its repo is filtered out.
  $: chips = deriveChips(hits);
  // The ordered flat indices of hits whose repo is NOT hidden. This is the
  // single source of truth for what renders and for keyboard navigation.
  $: visible = visibleIndices(hits, hidden);

  // Group the VISIBLE hits by repo, preserving the backend's ordering
  // (first-seen repo order, hits within a repo in their original order).
  // Each item keeps its index into the flat `hits` array so the keyboard
  // highlight (which walks the flat list) matches the grouped display.
  $: groups = (() => {
    const byRepo = new Map<string, Group>();
    for (const idx of visible) {
      const h = hits[idx];
      let g = byRepo.get(h.repo);
      if (!g) { g = { repo: h.repo, items: [] }; byRepo.set(h.repo, g); }
      g.items.push({ hit: h, idx });
    }
    return Array.from(byRepo.values());
  })();

  // Keep the highlight on a VISIBLE hit as results/filters change. Guarded so
  // it only reassigns when selIndex has fallen off the visible set (idempotent
  // once clamped, so it never loops). Covers both a shrinking result set and
  // any change to `hidden` from toggling a chip.
  $: if (visible.length && !visible.includes(selIndex)) selIndex = clampSel(selIndex, visible);

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
    if (!term) {
      hits = [];
      loading = false;
      return;
    }
    // Snapshot the mode alongside the generation. The reqGen guard below drops
    // any response whose request is no longer the latest - which also means a
    // content result can never land while we've since switched to files mode
    // (and vice-versa), because switching mode fires a fresh runSearch that
    // bumps reqGen.
    const gen = ++reqGen;
    const searching = mode;
    loading = true;
    try {
      const res = searching === "files" ? await SearchFiles(term) : await SearchAll(term);
      if (gen !== reqGen) return;
      hits = res || [];
      // New result set -> chips default all-on (nothing hidden) for this query.
      hidden = new Set();
      resultsQuery = term;
      selIndex = 0;
    } catch (err) {
      if (gen !== reqGen) return;
      hits = [];
      toastError("Search: " + errText(err));
    } finally {
      if (gen === reqGen) loading = false;
    }
  }

  // Switch content/files mode and, if there's a live query, re-run it now
  // (bypassing the debounce) so the results reflect the new mode immediately.
  function setMode(m: "content" | "files") {
    if (m === mode) return;
    mode = m;
    if (query.trim()) {
      if (debounceTimer) { clearTimeout(debounceTimer); debounceTimer = undefined; }
      runSearch(query);
    }
  }

  // Toggle a repo's chip on/off. Reassign the Set (not mutate) so `visible`
  // and the guarded clamp above recompute.
  function toggleChip(repo: string) {
    const next = new Set(hidden);
    if (next.has(repo)) next.delete(repo); else next.add(repo);
    hidden = next;
  }

  function onQueryInput() {
    if (debounceTimer) clearTimeout(debounceTimer);
    if (!query.trim()) {
      // Clear immediately on a blank query - no call, no stale results.
      // Bump reqGen too, so a still-in-flight search from before the clear
      // cannot land afterward and repopulate `hits` behind the empty state.
      ++reqGen;
      hits = [];
      resultsQuery = null;
      loading = false;
      return;
    }
    const q = query;
    debounceTimer = setTimeout(() => runSearch(q), 250);
  }

  // Cancel any pending debounced search before closing, so a stray
  // `SearchAll` (and toast) can't fire after the overlay is gone.
  function close() {
    if (debounceTimer) { clearTimeout(debounceTimer); debounceTimer = undefined; }
    onClose();
  }

  onDestroy(() => clearTimeout(debounceTimer));

  async function openHit(h: Hit | undefined) {
    if (!h) return;
    close();
    const err = await OpenEditorAt(h.repoPath, h.file);
    if (err) toastError("Open editor: " + err);
  }

  // Move the highlight to the next/prev entry within `visible` (the ordered
  // visible flat indices) so navigation never lands on a hidden hit.
  function moveSel(delta: number) {
    if (visible.length === 0) return;
    let pos = visible.indexOf(selIndex);
    if (pos === -1) pos = visible.indexOf(clampSel(selIndex, visible));
    pos = Math.max(0, Math.min(pos + delta, visible.length - 1));
    selIndex = visible[pos];
    scrollSel();
  }

  function keydown(e: KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      moveSel(1);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      moveSel(-1);
    } else if (e.key === "Tab") {
      // Tab flips modes when there's a live query; otherwise fall through to
      // the browser's default focus move.
      if (query.trim()) {
        e.preventDefault();
        setMode(mode === "content" ? "files" : "content");
      }
    } else if (e.key === "Enter") {
      e.preventDefault();
      const term = query.trim();
      if (!loading && term === resultsQuery) {
        // Only open when the highlight is on a visible hit (guards the
        // all-hidden case where selIndex falls back to 0 over a hidden hit).
        if (visible.includes(selIndex)) openHit(hits[selIndex]);
      } else if (term) {
        if (debounceTimer) { clearTimeout(debounceTimer); debounceTimer = undefined; }
        runSearch(query);
      }
    } else if (e.key === "Escape") {
      e.preventDefault();
      close();
    }
  }
</script>

<!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
<div class="overlay" on:click={close}>
  <div class="cmd-panel search-panel" on:click|stopPropagation>
    <div class="cmd-search">
      <span class="cmd-prompt">&gt;</span>
      <input
        class="cmd-input"
        type="text"
        placeholder={mode === "files" ? "Search file names across all repos" : "Search across all repos"}
        bind:this={inputEl}
        bind:value={query}
        on:input={onQueryInput}
        on:keydown={keydown}
        aria-label="Cross-repo search"
      />
      {#if loading}<span class="spinner"></span>{/if}
      <div class="search-mode" role="group" aria-label="Search mode">
        <button
          type="button"
          class="search-mode-btn"
          class:active={mode === "content"}
          on:click={() => setMode("content")}
        >Content</button>
        <button
          type="button"
          class="search-mode-btn"
          class:active={mode === "files"}
          on:click={() => setMode("files")}
        >Files</button>
      </div>
      <span class="cmd-kbd">Esc</span>
    </div>

    <div class="search-count">
      {#if resultsQuery}
        {#if hidden.size > 0}
          {visible.length} of {hits.length} results for "{resultsQuery}"
        {:else}
          {hits.length} result{hits.length === 1 ? "" : "s"} for "{resultsQuery}"
        {/if}
      {/if}
    </div>

    {#if chips.length > 1}
      <div class="search-chips">
        {#each chips as chip (chip.repo)}
          <button
            type="button"
            class="search-chip"
            class:off={hidden.has(chip.repo)}
            on:click={() => toggleChip(chip.repo)}
            aria-pressed={!hidden.has(chip.repo)}
          >
            {chip.repo}<span class="search-chip-n">{chip.count}</span>
          </button>
        {/each}
      </div>
    {/if}

    <div class="cmd-list search-list" bind:this={listEl}>
      {#if resultsQuery === null}
        <div class="cmd-empty">Type to search across repos</div>
      {:else if hits.length === 0}
        <div class="cmd-empty">No results</div>
      {:else if visible.length === 0}
        <div class="cmd-empty">All projects hidden</div>
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
                {#if it.hit.line !== undefined}
                  <span class="search-loc">{it.hit.file}:{it.hit.line}</span>
                  <span class="search-text">{it.hit.text}</span>
                {:else}
                  <span class="search-text">{it.hit.file}</span>
                {/if}
              </button>
            {/each}
          </div>
        {/each}
      {/if}
    </div>

    <div class="cmd-foot">
      <span><span class="cmd-kbd">up</span><span class="cmd-kbd">dn</span> navigate</span>
      <span><span class="cmd-kbd">tab</span> mode</span>
      <span><span class="cmd-kbd">enter</span> open</span>
      <span><span class="cmd-kbd">esc</span> close</span>
    </div>
  </div>
</div>
