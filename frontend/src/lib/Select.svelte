<script lang="ts">
  // A themed dropdown - native <select> popups can't be styled, so we render our
  // own trigger + menu that matches the app (used for status/tag/language).
  export let value: string = "";
  export let options: { value: string; label: string }[] = [];
  export let ariaLabel: string = "";
  export let onChange: (v: string) => void = () => {};

  let open = false;
  $: current = options.find((o) => o.value === value);

  function pick(v: string) {
    open = false;
    if (v !== value) {
      value = v;
      onChange(v);
    }
  }
  function onWindowClick(e: MouseEvent) {
    if (open && !(e.target as HTMLElement).closest(".sel")) open = false;
  }
  function onKey(e: KeyboardEvent) {
    if (e.key === "Escape" && open) {
      open = false;
      e.stopPropagation();
    }
  }
</script>

<svelte:window on:click={onWindowClick} />

<div class="sel">
  <button
    class="sel-trigger"
    class:open
    on:click|stopPropagation={() => (open = !open)}
    on:keydown={onKey}
    aria-haspopup="listbox"
    aria-expanded={open}
    aria-label={ariaLabel}
  >
    <span class="sel-value">{current ? current.label : ""}</span>
    <span class="sel-caret"></span>
  </button>

  {#if open}
    <div class="sel-menu" role="listbox">
      {#each options as o (o.value)}
        <button
          class="sel-item"
          class:on={o.value === value}
          on:click={() => pick(o.value)}
          role="option"
          aria-selected={o.value === value}
        >
          <span class="sel-check"></span>
          <span class="sel-item-label">{o.label}</span>
        </button>
      {/each}
    </div>
  {/if}
</div>

<style>
  .sel {
    position: relative;
    display: inline-block;
  }
  .sel-trigger {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    min-height: 32px;
    font-family: var(--font-sans);
    font-size: 13px;
    color: var(--text);
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--r-btn);
    padding: 5px 10px;
    cursor: pointer;
    transition: border-color var(--t), box-shadow var(--t), background var(--t);
  }
  .sel-trigger:hover { border-color: var(--border-hover); }
  .sel-trigger.open {
    border-color: var(--accent);
    box-shadow: var(--ring);
  }
  .sel-trigger:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  .sel-value { flex: 1; text-align: left; white-space: nowrap; }
  .sel-caret {
    flex: none;
    width: 6px;
    height: 6px;
    border-right: 1.5px solid var(--muted);
    border-bottom: 1.5px solid var(--muted);
    transform: rotate(45deg) translate(-1px, -1px);
    transition: transform var(--t);
  }
  .sel-trigger.open .sel-caret { transform: rotate(-135deg) translate(-1px, -1px); }

  .sel-menu {
    position: absolute;
    top: calc(100% + 6px);
    left: 0;
    z-index: 40;
    min-width: 100%;
    display: flex;
    flex-direction: column;
    padding: 5px;
    background: var(--raised);
    border: 1px solid var(--border);
    border-radius: var(--r-card);
    box-shadow: var(--highlight), var(--shadow-lg);
    animation: popIn 150ms cubic-bezier(0.2, 0, 0.2, 1);
  }
  .sel-item {
    display: flex;
    align-items: center;
    gap: 8px;
    text-align: left;
    white-space: nowrap;
    font: inherit;
    font-size: 13px;
    color: var(--text);
    background: transparent;
    border: none;
    border-radius: var(--r-btn);
    padding: 7px 10px 7px 8px;
    cursor: pointer;
    transition: background var(--t);
  }
  .sel-item:hover { background: var(--surface); }
  .sel-item.on { color: var(--accent); }
  .sel-check {
    flex: none;
    width: 14px;
    height: 8px;
    position: relative;
  }
  .sel-item.on .sel-check::after {
    content: "";
    position: absolute;
    left: 2px;
    top: -1px;
    width: 4px;
    height: 8px;
    border-right: 2px solid var(--accent);
    border-bottom: 2px solid var(--accent);
    transform: rotate(45deg);
  }
  .sel-item-label { flex: 1; }
</style>
