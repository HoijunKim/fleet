import { describe, it, expect } from "vitest";
import { render as ssrRender } from "svelte/server";
import Onboarding from "./Onboarding.svelte";

const noop = () => {};

describe("Onboarding", () => {
  it("offers both first-run next steps and the optional-sign-in note", () => {
    const html = ssrRender(Onboarding, { props: { onAddRoot: noop, onAddProject: noop } }).body as string;
    expect(html).toContain("Welcome to fleet");
    expect(html).toContain("Add a folder to scan");
    expect(html).toContain("Add a project");
    expect(html).toContain("optional");
  });
});
