<script lang="ts">
  // Tag chips for a project's detail panel: shows existing tags (colored via
  // tagColor, with a remove x each) plus a small input to add a new one.
  // Works for both code and manual projects - it only touches project.tags.
  import { SetTags } from "../../wailsjs/go/main/App";
  import { toastError } from "./toasts";
  import { tagColor } from "./pm";

  export let project: any = null;
  // Refresh this project's PM fields after a mutation (same contract as
  // PMSection's onChanged).
  export let onChanged: (id: string) => void;
  // All tags in use across projects, for the add-input's autocomplete.
  export let allTags: string[] = [];

  let newTag = "";
  let busy = false;

  $: tags = (project && project.tags) || [];
  // Suggest only tags this project doesn't already have.
  $: suggestions = (allTags || []).filter((t) => !tags.includes(t));

  // Chip style derived from tagColor's "hsl(H, S%, L%)" string: full color
  // for the border/text, a low-alpha version (swap hsl -> hsla) for the
  // background. Keeps tagColor itself a plain, single-purpose function.
  function chipStyle(tag: string): string {
    const c = tagColor(tag);
    const bg = c.replace("hsl(", "hsla(").replace(/\)$/, ", 0.16)");
    return "border-color: " + c + "; background: " + bg + "; color: " + c + ";";
  }

  async function apply(next: string[]) {
    if (!project || busy) return;
    busy = true;
    try {
      const err = await SetTags(project.id, next);
      if (err) toastError("Tags: " + err);
      else onChanged(project.id);
    } finally {
      busy = false;
    }
  }

  function addTag() {
    const t = newTag.trim();
    if (!t) return;
    newTag = "";
    if (tags.includes(t)) return;
    apply([...tags, t]);
  }

  function removeTag(tag: string) {
    apply(tags.filter((t: string) => t !== tag));
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === "Enter") {
      e.preventDefault();
      addTag();
    }
  }
</script>

<div class="tag-chips">
  <span class="section-label">Tags</span>
  <div class="tag-chip-row">
    {#each tags as tag (tag)}
      <span class="tag-chip" style={chipStyle(tag)}>
        <span class="tag-chip-label">{tag}</span>
        <button
          class="tag-chip-x"
          on:click={() => removeTag(tag)}
          disabled={busy}
          aria-label={"Remove tag " + tag}
        >x</button>
      </span>
    {/each}
    <input
      class="input tag-add-input"
      type="text"
      placeholder="Add tag"
      bind:value={newTag}
      on:keydown={onKey}
      disabled={busy}
      aria-label="Add tag"
      list={"tag-suggest-" + (project ? project.id : "")}
    />
    <datalist id={"tag-suggest-" + (project ? project.id : "")}>
      {#each suggestions as s (s)}
        <option value={s}></option>
      {/each}
    </datalist>
  </div>
</div>
