import { describe, it, expect, vi } from "vitest";

// StatusFiles is async (loaded via a reactive block that SSR doesn't await), so
// these assertions target the prop-driven controls, not the fetched stage rows.
vi.mock("../../wailsjs/go/main/App", () => {
  const s = async () => "";
  return {
    CommitAll: s, CommitStaged: s, CommitAmend: s, Push: s, RepoDiff: s, AskAI: s,
    StatusFiles: async () => [], StageFile: s, UnstageFile: s, LastCommitMessage: s,
    // The conflict block is loaded async (GitOperation/Conflicts) after the
    // reactive block fires; SSR does not await it, so it stays at "no operation"
    // here - which is exactly the state the gating test below asserts.
    GitOperation: async () => "", Conflicts: async () => [],
    ResolveConflict: s, ContinueOperation: s, AbortOperation: s,
  };
});

import { render as ssrRender } from "svelte/server";
import CommitBox from "./CommitBox.svelte";

const noop = () => {};
const render = (props: Record<string, unknown>) =>
  ssrRender(CommitBox, { props: { path: "/r", name: "r", onChanged: noop, ...props } }).body as string;

describe("CommitBox", () => {
  it("shows the clean state with no changes", () => {
    const html = render({ dirtyFiles: [] });
    expect(html).toContain("clean");
    expect(html).toContain("Working tree clean");
  });

  it("offers staged / all / push commits plus amend when dirty", () => {
    const html = render({ dirtyFiles: ["a.txt", "b.txt"] });
    expect(html).toContain("2</span> changed");
    expect(html).toContain("Commit staged");
    expect(html).toContain("Commit all");
    expect(html).toContain("push");
    expect(html).toContain("Amend last commit");
    expect(html).toContain("Draft with AI");
  });

  // With no operation in progress the conflict banner and Continue/Abort must
  // stay out of the way - the block is gated entirely on the async-loaded mode.
  // (The in-conflict rendering is loaded after SSR returns and is covered by the
  // Go binding round-trip test instead, since this harness has no DOM to await.)
  it("shows no operation banner when the repo is not mid-merge", () => {
    const html = render({ dirtyFiles: ["a.txt"] });
    expect(html).not.toContain("in progress");
    expect(html).not.toContain("Continue");
    expect(html).not.toContain("Abort");
  });
});
