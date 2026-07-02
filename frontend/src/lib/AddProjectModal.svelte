<script lang="ts">
  import { onMount } from "svelte";
  import { AddProject } from "../../wailsjs/go/main/App";
  import { toastSuccess, toastError } from "./toasts";

  export let onClose: () => void;
  // Called with the new project id after a successful add.
  export let onAdded: (id: string) => void;

  let name = "";
  let busy = false;
  let inputEl: HTMLInputElement;

  onMount(() => inputEl && inputEl.focus());

  async function submit() {
    const n = name.trim();
    if (!n || busy) return;
    busy = true;
    try {
      const id = await AddProject(n);
      if (!id) {
        toastError("Could not add project");
        return;
      }
      toastSuccess("Added " + n);
      onAdded(id);
      onClose();
    } finally {
      busy = false;
    }
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === "Enter") {
      e.preventDefault();
      submit();
    } else if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    }
  }
</script>

<!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
<div class="overlay" on:click={onClose}>
  <div class="modal add-modal" on:click|stopPropagation>
    <div class="modal-head">
      <span class="brand-dot"></span>
      <h3 class="modal-title">New project</h3>
      <button class="btn btn-secondary btn-sm modal-close" on:click={onClose} aria-label="Close">x</button>
    </div>

    <div class="modal-body">
      <div class="field">
        <label class="field-label" for="add-name">Name</label>
        <input
          id="add-name"
          class="input"
          type="text"
          placeholder="Project name"
          bind:value={name}
          bind:this={inputEl}
          on:keydown={onKey}
        />
      </div>
    </div>

    <div class="modal-foot">
      <button class="btn btn-secondary" on:click={onClose}>Cancel</button>
      <button class="btn btn-primary" on:click={submit} disabled={busy || !name.trim()}>
        {#if busy}<span class="spinner"></span> adding{:else}Add project{/if}
      </button>
    </div>
  </div>
</div>
