// @vitest-environment happy-dom
//
// DOM test of the Rewrite picker (tier 4q): it lists recent commits newest-first,
// up/down reorders them, and confirming sends InteractiveRebase the actions in
// git's chronological (oldest-first) order.
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, cleanup } from "@testing-library/svelte";

const calls: any[] = [];
vi.mock("../../wailsjs/go/main/App", () => ({
  RebaseCommits: async () => ({
    base: "base000",
    commits: [
      { hash: "ccccccc111", message: "add c", author: "a", when: "2026-07-03" },
      { hash: "bbbbbbb222", message: "add b", author: "a", when: "2026-07-02" },
    ],
  }),
  InteractiveRebase: async (_p: string, base: string, actions: any[]) => {
    calls.push(["rebase", base, actions]);
    return "";
  },
}));

import RebaseMenu from "./RebaseMenu.svelte";

describe("RebaseMenu (DOM)", () => {
  beforeEach(() => { calls.length = 0; vi.stubGlobal("confirm", () => true); });
  afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

  it("lists recent commits and rewrites in displayed order", async () => {
    const { getByText, findByText } = render(RebaseMenu, {
      props: { path: "/repo", name: "repo", onChanged: () => {} },
    });

    await fireEvent.click(getByText("Rewrite"));
    await findByText("add c"); // newest at top
    await findByText("add b");

    // Confirm without reordering: displayed [c, b] -> chronological [b, c].
    const runBtn = getByText("Rewrite", { selector: ".btn-primary" });
    await fireEvent.click(runBtn);

    expect(calls).toHaveLength(1);
    const [, base, actions] = calls[0];
    expect(base).toBe("base000");
    expect(actions).toEqual([
      { hash: "bbbbbbb222", op: "pick" },
      { hash: "ccccccc111", op: "pick" },
    ]);
  });

  it("moving the top commit down reverses the sent order", async () => {
    const { getByText, findByText, getAllByLabelText } = render(RebaseMenu, {
      props: { path: "/repo", name: "repo", onChanged: () => {} },
    });

    await fireEvent.click(getByText("Rewrite"));
    await findByText("add c");

    // Move the top row (add c) down: displayed becomes [b, c] -> chrono [c, b].
    const downButtons = getAllByLabelText("Move down");
    await fireEvent.click(downButtons[0]);

    await fireEvent.click(getByText("Rewrite", { selector: ".btn-primary" }));
    const [, , actions] = calls[0];
    expect(actions).toEqual([
      { hash: "ccccccc111", op: "pick" },
      { hash: "bbbbbbb222", op: "pick" },
    ]);
  });
});
