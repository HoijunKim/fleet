import { describe, it, expect } from "vitest";
import Icon from "./Icon.svelte";

// Svelte 3 server render: Component.render(props) -> { html }
function render(props: Record<string, unknown>) {
  return (Icon as any).render(props).html as string;
}

describe("Icon", () => {
  it("renders an svg for a known name, sized", () => {
    const html = render({ name: "search", size: 20 });
    expect(html).toContain("<svg");
    expect(html).toContain('width="20"');
    expect(html).toContain('stroke="currentColor"');
  });

  it("renders an empty svg for an unknown name (no throw)", () => {
    const html = render({ name: "nope" });
    expect(html).toContain("<svg");
    expect(html).not.toContain("<path");
  });
});
