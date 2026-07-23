import { describe, it, expect } from "vitest";
import { render as ssrRender } from "svelte/server";
import StartupBanner from "./StartupBanner.svelte";

const noop = () => {};

function html(issues: any[]): string {
  return ssrRender(StartupBanner, {
    props: { issues, onReveal: noop, onDiscard: noop },
  }).body as string;
}

describe("StartupBanner", () => {
  it("renders nothing when every file loaded", () => {
    // SSR still emits hydration markers, so assert on the visible markup.
    expect(html([])).not.toContain("<div");
  });

  it("names the affected data and how to reach the kept file", () => {
    const out = html([
      { scope: "projects", path: "C:/d/projects.json", error: "not valid JSON", frozen: true },
    ]);
    expect(out).toContain("projects, tasks and notes");
    expect(out).toContain("not valid JSON");
    expect(out).toContain("Reveal folder");
  });

  it("offers Start fresh only for a store that froze, and arms it first", () => {
    const frozen = html([{ scope: "projects", path: "p", error: "bad", frozen: true }]);
    expect(frozen).toContain("Start fresh");
    // Armed only after a click: the confirming label must not be there yet.
    expect(frozen).not.toContain("discard it");
    expect(frozen).toContain("stopped saving");

    // A config failure does not freeze anything, so there is nothing to discard.
    const soft = html([{ scope: "config", path: "c", error: "bad toml", frozen: false }]);
    expect(soft).not.toContain("Start fresh");
    expect(soft).toContain("settings");
  });
});
