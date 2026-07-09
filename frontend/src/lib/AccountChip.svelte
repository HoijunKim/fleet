<script lang="ts">
  export let login: string = "";
  export let avatarUrl: string = "";
  export let onSignOut: () => void;
  export let onSyncNow: () => void;

  let open = false;
  let imgOk = true;
  function toggle() {
    open = !open;
  }
  function onWindowClick(e: MouseEvent) {
    if (!open) return;
    const t = e.target as HTMLElement;
    if (!t.closest(".acct")) open = false;
  }
  function onWindowKey(e: KeyboardEvent) {
    if (open && e.key === "Escape") open = false;
  }
  function initial(): string {
    return (login || "?").slice(0, 1).toUpperCase();
  }
</script>

<svelte:window on:click={onWindowClick} on:keydown={onWindowKey} />

<div class="acct">
  <button class="acct-btn" on:click|stopPropagation={toggle} aria-expanded={open} aria-haspopup="menu" title={login || "Signed in"}>
    {#if avatarUrl && imgOk}
      <img class="acct-av" src={avatarUrl} alt="" on:error={() => (imgOk = false)} />
    {:else}
      <span class="acct-av acct-av-fallback">{initial()}</span>
    {/if}
    <span class="acct-login">{login || "Signed in"}</span>
    <span class="acct-caret"></span>
  </button>
  {#if open}
    <div class="acct-menu" role="menu">
      <button class="acct-item" role="menuitem" on:click={() => { open = false; onSyncNow(); }}>Sync now</button>
      <div class="acct-div"></div>
      <button class="acct-item" role="menuitem" on:click={() => { open = false; onSignOut(); }}>Sign out</button>
    </div>
  {/if}
</div>

<style>
  .acct { position: relative; }
  .acct-btn {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    height: 28px;
    padding: 0 8px 0 4px;
    border: 1px solid var(--border);
    border-radius: var(--r-pill);
    background: var(--surface);
    color: var(--text);
    font: inherit;
    font-size: 12px;
    cursor: pointer;
    transition: border-color var(--t), background var(--t);
  }
  .acct-btn:hover { border-color: var(--accent-line); background: var(--accent-soft); }
  .acct-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .acct-av { width: 20px; height: 20px; border-radius: 50%; flex: none; object-fit: cover; }
  .acct-av-fallback {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--accent-soft);
    color: var(--accent);
    font-size: 11px;
    font-weight: 700;
  }
  .acct-login { max-width: 120px; overflow: hidden; text-overflow: ellipsis; }
  .acct-caret {
    width: 0; height: 0;
    border-left: 4px solid transparent;
    border-right: 4px solid transparent;
    border-top: 4px solid var(--muted);
  }
  .acct-menu {
    position: absolute;
    top: 34px;
    right: 0;
    min-width: 140px;
    background: var(--raised);
    border: 1px solid var(--border);
    border-radius: var(--r-btn);
    padding: 4px;
    z-index: 50;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
  }
  .acct-item {
    display: block;
    width: 100%;
    text-align: left;
    font: inherit;
    font-size: 13px;
    color: var(--text);
    background: transparent;
    border: none;
    border-radius: 6px;
    padding: 7px 10px;
    cursor: pointer;
  }
  .acct-item:hover, .acct-item:focus-visible { background: var(--accent-soft); color: var(--accent); }
  .acct-item:focus-visible { outline: none; }
  .acct-div { height: 1px; background: var(--border); margin: 4px 2px; }
</style>
