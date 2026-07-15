import { describe, it, expect, vi } from "vitest";

// DetailPanel imports a raft of child components (each pulling Wails bindings) and
// the runtime event bus. Stub them so the panel renders under SSR. SSR runs the
// instance script but not onMount/handlers, where the real binding calls live, so
// no-op stubs are enough. (An explicit object, not a Proxy: a Proxy whose get trap
// answers `then` makes the module namespace a thenable and `await import()` never
// resolves - it deadlocks the whole test file.)
vi.mock("../../wailsjs/go/main/App", () => {
  const b = async () => ""; // inline: vi.mock factory is hoisted above module-scope consts
  return {
    Fetch: b, Pull: b, OpenEditor: b, OpenTerminal: b, RenameProject: b,
    AddTask: b, EditTask: b, SetTaskStatus: b, ReorderTasks: b, DeleteTask: b,
    UpdateProject: b, SetTags: b,
  };
});
vi.mock("../../wailsjs/runtime/runtime", () => ({
  EventsOn: () => () => {},
  EventsOff: () => {},
  EventsEmit: () => {},
}));

import { render as ssrRender } from "svelte/server";
import DetailPanel from "./DetailPanel.svelte";

const noop = () => {};
const render = (project: any) =>
  ssrRender(DetailPanel, {
    props: {
      project,
      onRepoChanged: noop,
      onProjectChanged: noop,
      onDeleteProject: noop,
    },
  }).body as string;

describe("DetailPanel header", () => {
  it("renders a manual project's name as a click-to-rename control inside a heading", () => {
    const html = render({ id: "m1", name: "Roadmap", type: "manual", tasks: [] });
    expect(html).toContain("detail-title-btn");
    expect(html).toContain("Click to rename");
    // Heading semantics are preserved: the rename control lives in an <h3>.
    expect(html).toMatch(/<h3[^>]*class="detail-title"[^>]*>\s*<button[^>]*detail-title-btn/);
    expect(html).toContain("Roadmap");
  });

  it("renders a code project's name as a static heading with no rename control", () => {
    const html = render({ id: "/repo", name: "repo", type: "code", tasks: [], isGit: true });
    expect(html).not.toContain("detail-title-btn");
    expect(html).not.toContain("Click to rename");
    expect(html).toContain("repo");
  });
});
