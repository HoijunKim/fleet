// @vitest-environment happy-dom
//
// The conflict panel banner reads "Cherry-pick in progress" when GitOperation
// reports a cherry-pick (tier 4l generalized the merge/rebase-only label).
import { describe, it, expect, vi } from "vitest";
import { render } from "@testing-library/svelte";

vi.mock("../../wailsjs/go/main/App", () => {
  const ok = async () => "";
  return {
    CommitAll: ok, CommitStaged: ok, CommitAmend: ok, Push: ok, RepoDiff: ok, AskAI: ok,
    StageFile: ok, UnstageFile: ok, LastCommitMessage: ok,
    StatusFiles: async () => [{ path: "f.txt", staged: false, unstaged: true, conflict: true }],
    GitOperation: async () => "cherry-pick",
    Conflicts: async () => [{ path: "f.txt", kind: "both-modified", mode: "cherry-pick", mineLabel: "Keep mine", incomingLabel: "Keep incoming" }],
    ResolveConflict: ok, ContinueOperation: ok, AbortOperation: ok,
  };
});

import CommitBox from "./CommitBox.svelte";

describe("CommitBox cherry-pick banner (DOM)", () => {
  it("labels an in-progress cherry-pick", async () => {
    const { findByText } = render(CommitBox, {
      props: { path: "/repo", name: "repo", dirtyFiles: ["f.txt"], onChanged: () => {} },
    });
    await findByText(/Cherry-pick in progress/);
  });
});
