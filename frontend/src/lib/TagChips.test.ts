import { describe, it, expect, vi } from "vitest";

vi.mock("../../wailsjs/go/main/App", () => ({ SetTags: async () => "" }));

import { render as ssrRender } from "svelte/server";
import TagChips from "./TagChips.svelte";

const noop = () => {};
const render = (props: Record<string, unknown>) =>
  ssrRender(TagChips, { props: { onChanged: noop, ...props } }).body as string;

describe("TagChips autocomplete", () => {
  it("suggests every in-use tag the project doesn't already have", () => {
    const html = render({
      project: { id: "p1", tags: ["web"] },
      allTags: ["web", "backend", "urgent"],
    });
    expect(html).toContain("<datalist");
    expect(html).toContain('value="backend"');
    expect(html).toContain('value="urgent"');
    // "web" is already on the project, so it isn't offered as a suggestion.
    expect(html).not.toContain('value="web"');
  });

  it("renders no suggestions when there are none", () => {
    const html = render({ project: { id: "p2", tags: [] }, allTags: [] });
    expect(html).toContain("<datalist");
    expect(html).not.toContain("<option");
  });
});
