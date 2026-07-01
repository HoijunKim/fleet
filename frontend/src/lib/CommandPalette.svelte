<script lang="ts">
  import { onMount } from "svelte";

  interface PaletteAction {
    id: string;
    label: string;
    hint?: string;
    run: () => void;
  }

  export let repos: any[] = [];
  export let actions: PaletteAction[] = [];
  export let onClose: () => void;
  export let onJump: (r: any) => void;

  type Item = { kind: "action" | "repo"; label: string; hint: string; data: any };

  let query = "";
  let selIndex = 0;
  let inputEl: HTMLInputElement;
  let listEl: HTMLElement;

  onMount(() => inputEl && inputEl.focus());

  // Subsequence fuzzy match. Returns -1 for no match, higher is better.
  function fuzzy(text: string, q: string): number {
    const t = text.toLowerCase();
    const query2 = q.toLowerCase();
    if (!query2) return 0;
    let ti = 0;
    let score = 0;
    let streak = 0;
    for (let qi = 0; qi < query2.length; qi++) {
      const found = t.indexOf(query2[qi], ti);
      if (found === -1) return -1;
      streak = found === ti ? streak + 2 : 0;
      score += 1 + streak;
      if (found === 0) score += 2;
      ti = found + 1;
    }
    return score;
  }

  $: base = [
    ...actions.map((a) => ({ kind: "action", label: a.label, hint: a.hint || "", data: a } as Item)),
    ...repos.map((r) => ({ kind: "repo", label: r.name, hint: r.branch || "", data: r } as Item)),
  ];

  $: items = (() => {
    const q = query.trim();
    if (!q) return base.slice(0, 60);
    const scored: { it: Item; s: number }[] = [];
    for (const it of base) {
      const s = fuzzy(it.label, q);
      if (s >= 0) scored.push({ it, s });
    }
    scored.sort((a, b) => b.s - a.s);
    return scored.map((x) => x.it).slice(0, 60);
  })();

  // Keep the highlighted row in range as results change.
  $: if (selIndex > items.length - 1) selIndex = Math.max(0, items.length - 1);

  function scrollSel() {
    requestAnimationFrame(() => {
      const el = listEl && listEl.querySelector(".cmd-item.active");
      if (el) (el as HTMLElement).scrollIntoView({ block: "nearest" });
    });
  }

  function runItem(it: Item | undefined) {
    if (!it) return;
    if (it.kind === "action") it.data.run();
    else onJump(it.data);
  }

  function onQueryInput() {
    selIndex = 0;
  }

  function keydown(e: KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      selIndex = Math.min(selIndex + 1, items.length - 1);
      scrollSel();
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      selIndex = Math.max(selIndex - 1, 0);
      scrollSel();
    } else if (e.key === "Enter") {
      e.preventDefault();
      runItem(items[selIndex]);
    } else if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    }
  }
</script>

<!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
<div class="overlay" on:click={onClose}>
  <div class="cmd-panel" on:click|stopPropagation>
    <div class="cmd-search">
      <span class="cmd-prompt">&gt;</span>
      <input
        class="cmd-input"
        type="text"
        placeholder="Jump to a repo or run a command"
        bind:this={inputEl}
        bind:value={query}
        on:input={onQueryInput}
        on:keydown={keydown}
        aria-label="Command palette"
      />
      <span class="cmd-kbd">Esc</span>
    </div>

    <div class="cmd-list" bind:this={listEl}>
      {#if items.length === 0}
        <div class="cmd-empty">No matches</div>
      {:else}
        {#each items as it, i (it.kind + ":" + it.label)}
          <button
            class="cmd-item"
            class:active={i === selIndex}
            on:click={() => runItem(it)}
            on:mousemove={() => (selIndex = i)}
          >
            <span class="cmd-tag {it.kind}">{it.kind === "action" ? "run" : "repo"}</span>
            <span class="cmd-label">{it.label}</span>
            {#if it.hint}<span class="cmd-hint">{it.hint}</span>{/if}
          </button>
        {/each}
      {/if}
    </div>

    <div class="cmd-foot">
      <span><span class="cmd-kbd">up</span><span class="cmd-kbd">dn</span> navigate</span>
      <span><span class="cmd-kbd">enter</span> select</span>
      <span><span class="cmd-kbd">esc</span> close</span>
    </div>
  </div>
</div>
