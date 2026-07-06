<script lang="ts">
  import { GitHubInfo, GitHubURL, OpenURL } from "../../wailsjs/go/main/App";

  // The code project's GitHub remote URL (raw, as stored on the project).
  export let remote: string = "";
  // Stale-drop key: the repo path. Reload whenever the selected project
  // changes, mirroring the lazy-load-on-select guard used by BranchMenu /
  // HistoryList / StashPanel.
  export let path: string = "";

  let loadedPath = "";
  let requestedRemote = "";
  let info: { ci: string; prs: number; issues: number; available: boolean } | null = null;
  let baseUrl = "";

  function openGh(sub: string) {
    if (baseUrl) OpenURL(baseUrl + sub);
  }

  // Reset state whenever the selected repo (path) changes. Kept separate from
  // the load trigger below so a late-arriving remote still fires a load.
  $: if (path !== loadedPath) {
    loadedPath = path;
    requestedRemote = "";
    info = null;
    baseUrl = "";
  }

  // Fire the load once a non-empty remote is known for the current selection.
  // remote is populated asynchronously (LoadRepo / mergeGit via the concurrent
  // pool), so it may be "" at first mount and arrive after the path guard has
  // already run - keying on remote here ensures we still load when it does.
  $: if (remote && remote !== requestedRemote) {
    requestedRemote = remote;
    load();
  }

  async function load() {
    const p = path;
    try {
      const v = await GitHubInfo(remote);
      if (p !== path) return; // selection changed during await -> drop stale result
      info = v;
      if (v && v.available) {
        GitHubURL(remote).then((u) => {
          if (p === path) baseUrl = u || "";
        }).catch(() => {});
      }
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
      <button class="gh-ci {ciClass(info.ci)}" class:gh-link={!!baseUrl} on:click={() => openGh("/actions")} disabled={!baseUrl} title="Open Actions">{ciLabel(info.ci)}</button>
      {#if info.prs > 0}
        <button class="gh-chip" class:gh-link={!!baseUrl} on:click={() => openGh("/pulls")} disabled={!baseUrl} title="Open pull requests">PR <span class="gh-chip-n">{info.prs}</span></button>
      {/if}
      {#if info.issues > 0}
        <button class="gh-chip" class:gh-link={!!baseUrl} on:click={() => openGh("/issues")} disabled={!baseUrl} title="Open issues">Issues <span class="gh-chip-n">{info.issues}</span></button>
      {/if}
    </span>
  </div>
{/if}
