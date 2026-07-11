<script lang="ts">
  import { scale, fade, fly } from "svelte/transition";
  import { fadeScaleIn, flyUp } from "./motion";
  import {
    available, consent, running, stream, activity, pending, cost, turns, overlayOpen,
    giveConsent, ask, decide, cancel, closeOverlay,
  } from "./agentSession";
  import { parseAction } from "./agentAction";
  import Icon from "./Icon.svelte";
  import Logo from "./Logo.svelte";
  import { renderBrief } from "./markdown";
  import { toastError } from "./toasts";

  export let projectName: string = "";

  let question = "";

  $: action = $pending ? parseAction($pending.category, $pending.toolName, $pending.toolInput) : null;

  async function onConsent() {
    const err = await giveConsent();
    if (err) toastError(err);
  }
  async function submit() {
    const q = question.trim();
    if (!q) return;
    question = "";
    await ask(q);
  }
  function onKey(e: KeyboardEvent) {
    if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); submit(); }
  }
  function onOverlayKey(e: KeyboardEvent) {
    // Svelte 3 forbids <svelte:window> inside a block, so the tag itself must
    // stay top-level; gate its effect here so Escape only closes the overlay
    // while it's actually open (never app-wide).
    if (!$overlayOpen) return;
    if (e.key === "Escape") { e.preventDefault(); closeOverlay(); }
  }
</script>

<svelte:window on:keydown={onOverlayKey} />

{#if $overlayOpen}
  <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
  <div
    class="ov-backdrop"
    transition:fade={{ duration: 140 }}
    on:click|self={closeOverlay}
    role="dialog"
    aria-modal="true"
    aria-label="Agentic deep-dive"
    tabindex="-1"
  >
    <div class="ov-panel" transition:scale={fadeScaleIn()}>
      <div class="ov-head">
        <Logo size={18} />
        <span class="ov-title">{projectName} · agentic deep-dive</span>
        <button class="ov-close" on:click={closeOverlay} aria-label="Close"><Icon name="x" size={18} /></button>
      </div>

      <div class="ov-body">
        {#if $available && !$consent}
          <div class="ov-consent">
            <p>
              The agentic deep-dive lets Claude Code read files in this repo and send them to
              Anthropic under your Claude login, and can propose edits or commands (each one you
              approve here first).
            </p>
            <button class="btn btn-primary btn-sm" on:click={onConsent}>Enable agentic deep-dive</button>
          </div>
        {/if}

        {#if $activity.length}
          <div class="ov-activity">
            {#each $activity as a, i}
              <div class="ov-act" class:ov-act-ok={a.tool === "approved"} class:ov-act-rej={a.tool === "rejected"} in:fly|local={flyUp(i)}>
                {#if a.tool === "approved"}
                  <Icon name="check" size={13} />
                {:else if a.tool === "rejected"}
                  <Icon name="x" size={13} />
                {:else}
                  <Icon name="activity" size={13} />
                {/if}
                <span class="mono">{a.tool}</span> {a.input}
              </div>
            {/each}
          </div>
        {/if}

        <div class="ov-thread">
          {#each $turns as t}
            {#if t.role === "user"}
              <div class="ov-q">{t.text}</div>
            {:else}
              <div class="ov-a" class:err={t.text.startsWith("error:")}>{@html renderBrief(t.text)}</div>
            {/if}
          {/each}
          {#if $stream}<div class="ov-a ov-stream">{$stream}</div>{/if}
        </div>

        {#if $pending}
          <div class="ov-approval sev-{$pending.severity}" transition:scale={fadeScaleIn()}>
            <div class="ov-approval-head">
              <span class="ov-cat ov-cat-{$pending.category}">{$pending.category}</span>
              <span class="ov-summary">{$pending.summary || $pending.toolName}</span>
            </div>
            {#if action && action.kind === "diff"}
              <div class="ov-diff mono">
                <div class="ov-diff-file">{action.file}</div>
                {#each action.removed as l}<div class="ov-del">- {l}</div>{/each}
                {#each action.added as l}<div class="ov-add">+ {l}</div>{/each}
              </div>
            {:else if action && action.kind === "write"}
              <div class="ov-diff mono">
                <div class="ov-diff-file">{action.file}</div>
                {#each action.preview as l}<div class="ov-add">+ {l}</div>{/each}
              </div>
            {:else if action && action.kind === "command"}
              <pre class="ov-cmd">{action.command}</pre>
            {:else if action && action.kind === "raw"}
              <pre class="ov-approval-body">{action.json}</pre>
            {/if}
            <div class="ov-approval-btns">
              <button class="btn btn-primary btn-sm" on:click={() => decide(true)}><Icon name="check" size={14} /> Approve</button>
              <button class="btn btn-sm ov-reject" on:click={() => decide(false)}><Icon name="x" size={14} /> Reject</button>
            </div>
          </div>
        {/if}
      </div>

      <div class="ov-foot">
        {#if $running}
          <span class="ov-run"><span class="spinner"></span> working in the repo…</span>
          <button class="ov-cancel" on:click={cancel}><Icon name="stop" size={13} /> Cancel</button>
        {:else if $cost}
          <span class="ov-cost">cost ${$cost.costUsd.toFixed(4)} · {$cost.inputTokens} in / {$cost.outputTokens} out</span>
        {/if}
        <div class="ov-input">
          <input class="input" type="text" placeholder="Ask about this repo…" bind:value={question}
                 on:keydown={onKey} disabled={$running} aria-label="Ask about this repo" />
          <button class="btn btn-primary btn-sm" on:click={submit} disabled={$running || !question.trim()}>Ask</button>
        </div>
      </div>
    </div>
  </div>
{/if}

<style>
  .ov-backdrop {
    position: fixed; inset: 0; z-index: 50;
    background: rgba(4, 6, 10, 0.62);
    display: grid; place-items: center; padding: 24px;
  }
  .ov-panel {
    width: min(880px, 92vw); max-height: 86vh;
    display: flex; flex-direction: column;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: 14px; box-shadow: 0 24px 64px rgba(0, 0, 0, 0.5); overflow: hidden;
  }
  .ov-head { display: flex; align-items: center; gap: 9px; padding: 14px 16px; border-bottom: 1px solid var(--border); }
  .ov-title { font-size: 14px; font-weight: 600; color: var(--text); flex: 1; }
  .ov-close { display: grid; place-items: center; background: transparent; border: none; color: var(--muted); cursor: pointer; padding: 4px; border-radius: 6px; }
  .ov-close:hover { color: var(--text); background: var(--raised); }
  .ov-body { flex: 1; overflow: auto; padding: 16px; display: flex; flex-direction: column; gap: 14px; min-height: 0; }
  .ov-consent { border: 1px solid var(--border); border-radius: var(--r-btn); padding: 12px; display: flex; flex-direction: column; gap: 8px; }
  .ov-consent p { margin: 0; font-size: 12.5px; color: var(--muted); line-height: 1.5; }
  .ov-activity { display: flex; flex-direction: column; gap: 5px; }
  .ov-act { display: flex; align-items: center; gap: 7px; font-size: 12px; color: var(--faint); }
  .ov-act-ok { color: var(--accent); }
  .ov-act-rej { color: var(--muted); }
  .ov-thread { display: flex; flex-direction: column; gap: 12px; }
  .ov-q { align-self: flex-end; max-width: 80%; background: var(--accent-soft); border: 1px solid var(--accent-line); border-radius: 12px 12px 4px 12px; padding: 8px 12px; font-size: 13px; color: var(--text); white-space: pre-wrap; user-select: text; }
  .ov-a { align-self: flex-start; max-width: 94%; font-size: 13.5px; line-height: 1.6; color: var(--text); user-select: text; }
  .ov-a.err { color: var(--err); }
  .ov-a :global(p) { margin: 0 0 8px; }
  .ov-a :global(code) { font-family: var(--font-mono); font-size: 12.5px; background: var(--raised); padding: 1px 5px; border-radius: 4px; }
  .ov-a :global(pre) { overflow-x: auto; }
  .ov-stream { white-space: pre-wrap; }
  .ov-approval { border: 1px solid var(--accent-line); background: var(--accent-soft); border-radius: var(--r-btn); padding: 12px; display: flex; flex-direction: column; gap: 8px; }
  .ov-approval-head { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--text); }
  .ov-summary { flex: 1; }
  .ov-approval-body { margin: 0; max-height: 320px; overflow: auto; font-family: var(--font-mono); font-size: 12px; background: var(--raised); border-radius: 4px; padding: 10px; white-space: pre-wrap; }
  .ov-approval-btns { display: flex; gap: 8px; }
  .ov-reject { border: 1px solid var(--err-line); color: var(--err); background: transparent; }
  .ov-foot { border-top: 1px solid var(--border); padding: 12px 16px; display: flex; flex-direction: column; gap: 10px; }
  .ov-run { display: flex; align-items: center; gap: 8px; font-size: 12.5px; color: var(--muted); }
  .ov-cost { font-size: 11.5px; color: var(--faint); }
  .ov-cancel, .ov-run + .ov-cancel { align-self: flex-start; font: inherit; font-size: 11.5px; display: inline-flex; align-items: center; gap: 5px; color: var(--muted); background: transparent; border: 1px solid var(--border); border-radius: var(--r-pill); padding: 2px 10px; cursor: pointer; }
  .ov-cancel:hover { color: var(--err); border-color: var(--err-line); }
  .ov-input { display: flex; gap: 8px; }
  .ov-input .input { flex: 1; }
  .mono { font-family: var(--font-mono); }
  .btn :global(svg) { vertical-align: -2px; }
</style>
