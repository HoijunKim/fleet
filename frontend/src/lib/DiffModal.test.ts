import { describe, it, expect, vi } from "vitest";

// The diff fetch runs in onMount (skipped under SSR), so these stubs are only to
// satisfy the import graph; the assertions target the header title, which is
// reactive from props.
vi.mock("../../wailsjs/go/main/App", () => ({
  DiffFile: async () => "",
  DiffAll: async () => "",
}));

import { render as ssrRender } from "svelte/server";
import DiffModal from "./DiffModal.svelte";

const noop = () => {};
const render = (props: Record<string, unknown>) =>
  ssrRender(DiffModal, { props: { path: "/r", onClose: noop, ...props } }).body as string;

describe("DiffModal title", () => {
  it("titles a single-file diff with the file name", () => {
    expect(render({ file: "src/app.go" })).toContain("src/app.go");
  });

  it("titles the whole-tree diff 'All changes'", () => {
    const html = render({ file: "", all: true });
    expect(html).toContain("All changes");
  });
});
