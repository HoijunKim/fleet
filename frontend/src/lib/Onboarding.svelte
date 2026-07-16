<script lang="ts">
  import Logo from "./Logo.svelte";

  // Open Settings (to add a scan root) / open the manual add-project modal.
  export let onAddRoot: () => void;
  export let onAddProject: () => void;
</script>

<div class="onboarding">
  <div class="onboarding-card">
    <div class="onboarding-mark"><Logo size={28} /></div>
    <h2 class="onboarding-title">Welcome to fleet</h2>
    <p class="onboarding-sub">
      Point fleet at your code, or track work by hand. Pick a way to start:
    </p>
    <div class="onboarding-actions">
      <button class="onboarding-step" on:click={onAddRoot}>
        <span class="onboarding-step-title">Add a folder to scan</span>
        <span class="onboarding-step-desc">
          fleet finds every git repo under it and keeps their status in view.
        </span>
      </button>
      <button class="onboarding-step" on:click={onAddProject}>
        <span class="onboarding-step-title">Add a project</span>
        <span class="onboarding-step-desc">
          Track tasks and deadlines for anything - no repository required.
        </span>
      </button>
    </div>
    <p class="onboarding-note">Signing in is optional; it only adds cross-device sync.</p>
  </div>
</div>

<style>
  .onboarding {
    display: flex;
    justify-content: center;
    padding: 48px 24px;
  }
  .onboarding-card {
    width: 100%;
    max-width: 560px;
    text-align: center;
  }
  .onboarding-mark { margin-bottom: 14px; }
  .onboarding-title {
    margin: 0 0 6px;
    font-size: 22px;
    font-weight: 700;
    letter-spacing: -0.01em;
    color: var(--text);
  }
  .onboarding-sub {
    margin: 0 0 22px;
    font-size: 14px;
    color: var(--muted);
  }
  .onboarding-actions {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }
  @media (max-width: 560px) {
    .onboarding-actions { grid-template-columns: 1fr; }
  }
  .onboarding-step {
    display: flex;
    flex-direction: column;
    gap: 6px;
    text-align: left;
    padding: 16px;
    border: 1px solid var(--border);
    border-radius: var(--r-card);
    background: var(--surface);
    cursor: pointer;
    transition: border-color var(--t), background var(--t);
  }
  .onboarding-step:hover { border-color: var(--accent); background: var(--raised); }
  .onboarding-step:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  .onboarding-step-title { font-size: 14px; font-weight: 600; color: var(--text); }
  .onboarding-step-desc { font-size: 12px; color: var(--muted); line-height: 1.5; }
  .onboarding-note { margin: 20px 0 0; font-size: 12px; color: var(--faint); }
</style>
