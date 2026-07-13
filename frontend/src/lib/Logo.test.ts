import { describe, it, expect } from "vitest";
import { render as ssrRender } from "svelte/server";
import Logo from "./Logo.svelte";

const render = (props: Record<string, unknown> = {}) => ssrRender(Logo, { props }).body as string;

describe("Logo", () => {
  it("renders the brand svg sized by prop", () => {
    const html = render({ size: 24 });
    expect(html).toContain("<svg");
    expect(html).toContain('width="24"');
    expect(html).toContain("linearGradient");
  });

  it("uses a unique gradient id per instance (no collision)", () => {
    const a = render();
    const b = render();
    const idA = a.match(/id="(fleetLogo[^"]+)"/)?.[1];
    const idB = b.match(/id="(fleetLogo[^"]+)"/)?.[1];
    expect(idA).toBeTruthy();
    expect(idA).not.toBe(idB);
  });
});
