import { describe, it, expect, vi } from "vitest";

// PMSection pulls in several Wails bindings (its own + TagChips's SetTags); stub
// them all as no-ops so the component renders under SSR without a backend.
vi.mock("../../wailsjs/go/main/App", () => ({
  AddTask: async () => "",
  EditTask: async () => "",
  SetTaskStatus: async () => "",
  ReorderTasks: async () => "",
  DeleteTask: async () => "",
  UpdateProject: async () => "",
  SetTags: async () => "",
}));

import { render as ssrRender } from "svelte/server";
import PMSection from "./PMSection.svelte";

const noop = () => {};
const render = (project: any) =>
  ssrRender(PMSection, { props: { project, onChanged: noop, onDelete: noop } }).body as string;

const manual = (tasks: any[], extra: Record<string, unknown> = {}) => ({
  id: "p1",
  name: "Alpha",
  type: "manual",
  tasks,
  ...extra,
});

describe("PMSection", () => {
  it("renders each task title as a click-to-edit control", () => {
    const html = render(manual([{ id: "t1", title: "Wire the API", status: "todo" }]));
    expect(html).toContain("Wire the API");
    // The title is an inline-edit affordance, not a static span.
    expect(html).toContain('title="Click to edit"');
  });

  it("shows the empty state when there are no tasks", () => {
    expect(render(manual([]))).toContain("No tasks yet");
  });

  it("reflects done/total progress from the backend counts", () => {
    const html = render(
      manual([{ id: "t1", title: "a", status: "done" }, { id: "t2", title: "b", status: "todo" }], {
        taskCount: 2,
        doneCount: 1,
      }),
    );
    expect(html).toContain("1/2");
    expect(html).toContain("50%");
  });

  it("offers project deletion only for manual projects", () => {
    expect(render(manual([]))).toContain("Delete project");
    const code = render({ id: "p2", name: "repo", type: "code", tasks: [] });
    expect(code).not.toContain("Delete project");
  });
});
