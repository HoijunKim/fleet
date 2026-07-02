<script lang="ts">
  import { GitHubInfo } from "../../wailsjs/go/main/App";

  // The code project's GitHub remote URL (raw, as stored on the project).
  export let remote: string = "";
  // Stale-drop key: the repo path. Reload whenever the selected project
  // changes, mirroring the lazy-load-on-select guard used by BranchMenu /
  // HistoryList / StashPanel.
  export let path: string = "";

  let loadedPath = "";
  let info: { ci: string; prs: number; issues: number; available: boolean } | null = null;

  // Lazily (re)load GitHub status whenever the selected repo changes. Guarded
  // by path (not remote), since path is what uniquely identifies the current
  // selection.
  $: if (path !== loadedPath) {
    loadedPath = path;
    info = null;
    if (remote) load();
  }

  async function load() {
    const p = path;
    try {
      const v = await GitHubInfo(remote);
      if (p !== path) return; // selection changed during await -> drop stale result
      info = v;
    } catch {
      if (p !== path) return;
      info = null;
    }
  }

  // CI conclusion/status -> badge color class.
  function ciClass(ci: string): string {
    switch (ci) {
      case "success":
        return "ok";
      case "failure":
      case "cancelled":
      case "timed_out":
      case "startup_failure":
        return "err";
      default:
        // in_progress, queued, pending, "" (no runs yet)
        return "neutral";
    }
  }

  function ciLabel(ci: string): string {
    return ci ? ci.replace(/_/g, " ") : "CI";
  }

  // Render nothing when there is no remote, the call hasn't resolved yet, or
  // the backend reports the repo is not on GitHub / gh failed - graceful,
  // no error shown.
  $: show = !!remote && !!info && info.available;
</script>

{#if show && info}
  <div class="dl-row">
    <span class="dl-label">GitHub</span>
    <span class="gh-badge">
      <span class="gh-ci {ciClass(info.ci)}">{ciLabel(info.ci)}</span>
      {#if info.prs > 0}
        <span class="gh-chip">PR <span class="gh-chip-n">{info.prs}</span></span>
      {/if}
      {#if info.issues > 0}
        <span class="gh-chip">Issues <span class="gh-chip-n">{info.issues}</span></span>
      {/if}
    </span>
  </div>
{/if}
