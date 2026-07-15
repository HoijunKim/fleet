<script lang="ts">
  import { onDestroy } from "svelte";
  import { AddTask, EditTask, SetTaskStatus, ReorderTasks, DeleteTask, UpdateProject } from "../../wailsjs/go/main/App";
  import { toastSuccess, toastError } from "./toasts";
  import { ddayLabel } from "./pm";
  import TagChips from "./TagChips.svelte";

  export let project: any;
  // Refresh this project's PM fields after a mutation.
  export let onChanged: (id: string) => void;
  // Delete a manual project (already confirmed by the caller path).
  export let onDelete: (id: string) => void;

  // Local editable copies so typing does not fight the reactive project prop.
  let lastId = "";
  let lastName = "";
  let status = "active";
  let priority = 0;
  let deadline = "";
  let notes = "";

  let newTitle = "";
  let newDue = "";
  let busy = false;
  let confirming = false;

  // Inline task edit: the task whose title/due is open for editing (null = none),
  // plus working copies of its fields so typing does not fight the reactive prop.
  let editId: string | null = null;
  let editTitle = "";
  let editDue = "";
  let notesTimer: ReturnType<typeof setTimeout> | undefined;

  // Re-sync locals whenever the selected project changes.
  $: if (project && project.id !== lastId) {
    if (notesTimer) {
      // A debounced notes save was still pending for the OUTGOING project.
      // Flush it against that project's id/fields (captured before we
      // overwrite the locals below) so the edit is not silently dropped.
      clearTimeout(notesTimer);
      notesTimer = undefined;
      saveProjectMeta(lastId, status, priority, deadline, notes, lastName);
    }
    lastId = project.id;
    lastName = project.name;
    status = project.status || "active";
    priority = project.priority || 0;
    deadline = project.deadline || "";
    notes = project.notes || "";
    confirming = false;
    newTitle = "";
    newDue = "";
    editId = null;
  }

  $: tasks = project && project.tasks ? project.tasks : [];
  // Prefer the backend-computed counts (kept in sync by GetProject after every
  // mutation); fall back to a client-side count off t.status for safety.
  $: taskCount = project && typeof project.taskCount === "number" ? project.taskCount : tasks.length;
  $: doneCount =
    project && typeof project.doneCount === "number"
      ? project.doneCount
      : tasks.filter((t: any) => t.status === "done").length;
  $: progressPct = taskCount > 0 ? Math.round((doneCount / taskCount) * 100) : 0;

  onDestroy(() => clearTimeout(notesTimer));

  // Persist an explicit id/fields combination. Used both for the normal
  // "save the currently selected project" path and for flushing a pending
  // save against a project we're navigating away from.
  async function saveProjectMeta(
    id: string,
    s: string,
    p: number,
    d: string,
    n: string,
    label?: string
  ) {
    if (!id) return;
    const err = await UpdateProject(id, s, Number(p), d, n);
    if (err) toastError("Update: " + err);
    else {
      toastSuccess("Saved" + (label ? " " + label : ""));
      onChanged(id);
    }
  }

  async function saveMeta() {
    if (!project) return;
    await saveProjectMeta(project.id, status, priority, deadline, notes, project.name);
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

  // Per-task status control: click cycles todo -> doing -> done -> todo.
  const STATUS_ORDER = ["todo", "doing", "done"];

  async function cycleStatus(t: any) {
    const cur = t.status === "doing" || t.status === "done" ? t.status : "todo";
    const next = STATUS_ORDER[(STATUS_ORDER.indexOf(cur) + 1) % STATUS_ORDER.length];
    const err = await SetTaskStatus(project.id, t.id, next);
    if (err) toastError("Status: " + err);
    else onChanged(project.id);
  }

  async function removeTask(taskId: string) {
    const err = await DeleteTask(project.id, taskId);
    if (err) toastError("Delete task: " + err);
    else onChanged(project.id);
  }

  // Open the inline editor for a task, seeding the working copies from its
  // current fields.
  function startEdit(t: any) {
    editId = t.id;
    editTitle = t.title || "";
    editDue = t.due || "";
  }
  function cancelEdit() {
    editId = null;
  }
  async function saveEdit(taskId: string) {
    const title = editTitle.trim();
    if (!title) { toastError("Task title cannot be empty"); return; }
    const err = await EditTask(project.id, taskId, title, editDue);
    if (err) toastError("Edit task: " + err);
    else { editId = null; onChanged(project.id); }
  }
  function onEditKey(e: KeyboardEvent, taskId: string) {
    if (e.key === "Enter") { e.preventDefault(); saveEdit(taskId); }
    else if (e.key === "Escape") { e.preventDefault(); cancelEdit(); }
  }

  // Drag-to-reorder: native HTML5 DnD, no library. Guard against empty/
  // single-item lists and no-op drops (same id, or dropped outside a task).
  let dragId: string | null = null;

  function onDragStart(e: DragEvent, id: string) {
    dragId = id;
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = "move";
      e.dataTransfer.setData("text/plain", id);
    }
  }

  function onDragOver(e: DragEvent) {
    e.preventDefault();
  }

  function onDragEnd() {
    dragId = null;
  }

  async function onDrop(e: DragEvent, targetId: string) {
    e.preventDefault();
    const srcId = dragId;
    dragId = null;
    if (!srcId || srcId === targetId || tasks.length < 2) return;
    // Move the dragged task to just before the drop target.
    const ids = tasks.map((t: any) => t.id).filter((id: string) => id !== srcId);
    const idx = ids.indexOf(targetId);
    if (idx === -1) return;
    ids.splice(idx, 0, srcId);
    const err = await ReorderTasks(project.id, ids);
    if (err) toastError("Reorder: " + err);
    else onChanged(project.id);
  }
</script>

<div class="pm">
  <TagChips {project} {onChanged} />

  <div class="detail-sep"></div>

  <div class="pm-tasks-head">
    <span class="section-label">Tasks</span>
    <span class="pm-progress">{doneCount}/{taskCount} ({progressPct}%)</span>
  </div>

  {#if tasks.length === 0}
    <div class="pm-empty">No tasks yet</div>
  {:else}
    <div
      class="pm-progress-bar"
      role="progressbar"
      aria-valuenow={progressPct}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-label="Task progress"
    >
      <div class="pm-progress-fill" style="width: {progressPct}%"></div>
    </div>
    <ul class="pm-list">
      {#each tasks as t (t.id)}
        <li
          class="pm-task"
          class:done={t.status === "done"}
          class:dragging={dragId === t.id}
          class:editing={editId === t.id}
          draggable={editId !== t.id}
          on:dragstart={(e) => onDragStart(e, t.id)}
          on:dragover={onDragOver}
          on:drop={(e) => onDrop(e, t.id)}
          on:dragend={onDragEnd}
        >
          <button
            class="pm-status-pill status-{t.status || 'todo'}"
            on:click={() => cycleStatus(t)}
            aria-label={"Status: " + (t.status || "todo") + " (click to cycle)"}
          >{t.status || "todo"}</button>
          {#if editId === t.id}
            <!-- svelte-ignore a11y_autofocus -->
            <input
              class="input pm-edit-title"
              type="text"
              bind:value={editTitle}
              on:keydown={(e) => onEditKey(e, t.id)}
              aria-label="Edit task title"
              autofocus
            />
            <input
              class="input pm-edit-due"
              type="date"
              bind:value={editDue}
              on:keydown={(e) => onEditKey(e, t.id)}
              aria-label="Edit task due date"
            />
            <button class="btn btn-secondary btn-sm" on:click={() => saveEdit(t.id)}>Save</button>
            <button class="pm-task-del pm-edit-cancel" on:click={cancelEdit} aria-label="Cancel edit">x</button>
          {:else}
            <button class="pm-task-title" on:click={() => startEdit(t)} title="Click to edit">{t.title}</button>
            {#if t.due}
              {@const d = ddayLabel(t.due)}
              <span class="pm-task-due {d ? d.cls : ''}">{d ? d.text : t.due}</span>
            {/if}
            <button class="pm-task-del" on:click={() => removeTask(t.id)} aria-label="Delete task">x</button>
          {/if}
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
