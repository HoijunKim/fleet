// @vitest-environment happy-dom
//
// A real DOM test (happy-dom) of the conflict-resolution click wiring that SSR
// cannot reach: mount CommitBox in a merge-conflict state, click a resolve
// button, and assert the binding fires. This automates the click half of the
// tier-4c manual GUI check.
import { describe, it, expect, vi } from "vitest";
import { render, fireEvent } from "@testing-library/svelte";

const calls: any[] = [];
vi.mock("../../wailsjs/go/main/App", () => {
  const ok = async () => "";
  return {
    CommitAll: ok, CommitStaged: ok, CommitAmend: ok, Push: ok, RepoDiff: ok, AskAI: ok,
    StageFile: ok, UnstageFile: ok, LastCommitMessage: ok,
    // One conflicted file, mid-merge.
    StatusFiles: async () => [{ path: "both.txt", staged: false, unstaged: true, conflict: true }],
    GitOperation: async () => "merge",
    Conflicts: async () => [{ path: "both.txt", kind: "both-modified", mode: "merge", mineLabel: "Keep mine", incomingLabel: "Keep incoming" }],
    ResolveConflict: async (path: string, file: string, side: string) => { calls.push(["resolve", path, file, side]); return ""; },
    ContinueOperation: async (path: string) => { calls.push(["continue", path]); return ""; },
    AbortOperation: async (path: string) => { calls.push(["abort", path]); return ""; },
  };
});

import CommitBox from "./CommitBox.svelte";

describe("CommitBox conflict flow (DOM)", () => {
  it("shows the merge banner and wires the resolve buttons", async () => {
    calls.length = 0;
    const { findByText, getByText } = render(CommitBox, {
      props: { path: "/repo", name: "repo", dirtyFiles: ["both.txt"], onChanged: () => {} },
    });

    // The async load (StatusFiles/GitOperation/Conflicts) resolves, then the
    // banner and per-row controls render. Wait for a per-row button, which only
    // appears once Conflicts() has loaded (mode is set one await earlier).
    const mine = await findByText("Keep mine");

    // Continue is disabled while a conflict remains.
    const cont = getByText("Continue") as HTMLButtonElement;
    expect(cont.disabled).toBe(true);

    // Click "Keep mine" -> ResolveConflict(path, file, "mine").
    await fireEvent.click(mine);
    expect(calls).toContainEqual(["resolve", "/repo", "both.txt", "mine"]);
  });

  it("wires the Abort button", async () => {
    calls.length = 0;
    const { findByText } = render(CommitBox, {
      props: { path: "/repo", name: "repo", dirtyFiles: ["both.txt"], onChanged: () => {} },
    });
    const abort = await findByText("Abort");
    await fireEvent.click(abort);
    expect(calls).toContainEqual(["abort", "/repo"]);
  });
});
