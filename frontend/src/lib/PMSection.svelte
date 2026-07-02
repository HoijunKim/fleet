<script lang="ts">
  import { AddTask, ToggleTask, DeleteTask, UpdateProject } from "../../wailsjs/go/main/App";
  import { toastSuccess, toastError } from "./toasts";
  import { ddayLabel } from "./pm";

  export let project: any;
  // Refresh this project's PM fields after a mutation.
  export let onChanged: (id: string) => void;
  // Delete a manual project (already confirmed by the caller path).
  export let onDelete: (id: string) => void;

  // Local editable copies so typing does not fight the reactive project prop.
  let lastId = "";
  let status = "active";
  let priority = 0;
  let deadline = "";
  let notes = "";

  let newTitle = "";
  let newDue = "";
  let busy = false;
  let confirming = false;
  let notesTimer: ReturnType<typeof setTimeout> | undefined;

  // Re-sync locals whenever the selected project changes.
  $: if (project && project.id !== lastId) {
    lastId = project.id;
    status = project.status || "active";
    priority = project.priority || 0;
    deadline = project.deadline || "";
    notes = project.notes || "";
    confirming = false;
    newTitle = "";
    newDue = "";
    if (notesTimer) { clearTimeout(notesTimer); notesTimer = undefined; }
  }

  $: tasks = project && project.tasks ? project.tasks : [];
  $: doneCount = tasks.filter((t: any) => t.done).length;

  async function saveMeta() {
    const err = await UpdateProject(project.id, status, Number(priority), deadline, notes);
    if (err) toastError("Update: " + err);
    else { toastSuccess("Saved " + project.name); onChanged(project.id); }
  }

  function onStatusChange(e: Event) {
    status = (e.target as HTMLSelectElement).value;
    saveMeta();
  }
  function onPriorityChange(e: Event) {
    priority = Number((e.target as HTMLSelectElement).value);
    saveMeta();
  }
  function onDeadlineChange() {
    saveMeta();
  }
  function onNotesInput() {
    if (notesTimer) clearTimeout(notesTimer);
    notesTimer = setTimeout(saveMeta, 600);
  }

  async function addTask() {
    const t = newTitle.trim();
    if (!t || busy) return;
    busy = true;
    try {
      const err = await AddTask(project.id, t, newDue);
      if (err) toastError("Add task: " + err);
      else { newTitle = ""; newDue = ""; onChanged(project.id); }
    } finally {
      busy = false;
    }
  }

  function onTaskKey(e: KeyboardEvent) {
    if (e.key === "Enter") { e.preventDefault(); addTask(); }
  }

  async function toggle(taskId: string) {
    const err = await ToggleTask(project.id, taskId);
    if (err) toastError("Toggle: " + err);
    else onChanged(project.id);
  }

  async function removeTask(taskId: string) {
    const err = await DeleteTask(project.id, taskId);
    if (err) toastError("Delete task: " + err);
    else onChanged(project.id);
  }
</script>

<div class="pm">
  <div class="pm-tasks-head">
    <span class="section-label">Tasks</span>
    <span class="pm-progress">{doneCount}/{tasks.length}</span>
  </div>

  {#if tasks.length === 0}
    <div class="pm-empty">No tasks yet</div>
  {:else}
    <ul class="pm-list">
      {#each tasks as t (t.id)}
        <li class="pm-task" class:done={t.done}>
          <button
            class="pm-check"
            class:on={t.done}
            on:click={() => toggle(t.id)}
            aria-label={t.done ? "Mark not done" : "Mark done"}
          >
            {#if t.done}<span class="pm-check-mark">x</span>{/if}
          </button>
          <span class="pm-task-title">{t.title}</span>
          {#if t.due}
            {@const d = ddayLabel(t.due)}
            <span class="pm-task-due {d ? d.cls : ''}">{d ? d.text : t.due}</span>
          {/if}
          <button class="pm-task-del" on:click={() => removeTask(t.id)} aria-label="Delete task">x</button>
        </li>
      {/each}
    </ul>
  {/if}

  <div class="pm-add">
    <input
      class="input pm-add-title"
      type="text"
      placeholder="Add a task"
      bind:value={newTitle}
      on:keydown={onTaskKey}
      aria-label="New task title"
    />
    <input
      class="input pm-add-due"
      type="date"
      bind:value={newDue}
      aria-label="New task due date"
    />
    <button class="btn btn-secondary btn-sm" on:click={addTask} disabled={busy || !newTitle.trim()}>Add</button>
  </div>

  <div class="detail-sep"></div>

  <div class="pm-grid">
    <div class="field">
      <label class="field-label" for="pm-status-{project.id}">Status</label>
      <select id="pm-status-{project.id}" class="input pm-select" bind:value={status} on:change={onStatusChange}>
        <option value="active">active</option>
        <option value="paused">paused</option>
        <option value="done">done</option>
      </select>
    </div>
    <div class="field">
      <label class="field-label" for="pm-prio-{project.id}">Priority</label>
      <select id="pm-prio-{project.id}" class="input pm-select" bind:value={priority} on:change={onPriorityChange}>
        <option value={0}>none</option>
        <option value={1}>low</option>
        <option value={2}>medium</option>
        <option value={3}>high</option>
      </select>
    </div>
    <div class="field pm-deadline-field">
      <label class="field-label" for="pm-deadline-{project.id}">Deadline</label>
      <input
        id="pm-deadline-{project.id}"
        class="input"
        type="date"
        bind:value={deadline}
        on:change={onDeadlineChange}
      />
    </div>
  </div>

  <div class="field pm-notes-field">
    <label class="field-label" for="pm-notes-{project.id}">Notes</label>
    <textarea
      id="pm-notes-{project.id}"
      class="input pm-notes"
      rows="3"
      placeholder="Notes"
      bind:value={notes}
      on:input={onNotesInput}
    ></textarea>
  </div>

  {#if project.type === "manual"}
    <div class="pm-danger">
      {#if confirming}
        <span class="pm-confirm-text">Delete this project?</span>
        <button class="btn btn-secondary btn-sm" on:click={() => (confirming = false)}>Cancel</button>
        <button class="btn btn-danger btn-sm" on:click={() => onDelete(project.id)}>Delete</button>
      {:else}
        <button class="btn btn-secondary btn-sm pm-del-btn" on:click={() => (confirming = true)}>
          Delete project
        </button>
      {/if}
    </div>
  {/if}
</div>
