// @vitest-environment happy-dom
//
// DOM test of the cherry-pick picker (tier 4l): it lists a source branch's
// commits and clicking one calls CherryPick with that hash.
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/svelte";

const calls: any[] = [];
vi.mock("../../wailsjs/go/main/App", () => ({
  Branches: async () => ({ current: "master", all: ["master", "feature"] }),
  LogRef: async (_p: string, ref: string) => { calls.push(["log", ref]); return [
    { hash: "abcdef1234567890", message: "feature commit", author: "a", when: "2026-07-01" },
  ]; },
  CherryPick: async (_p: string, hash: string) => { calls.push(["pick", hash]); return ""; },
}));

import CherryPickMenu from "./CherryPickMenu.svelte";

describe("CherryPickMenu (DOM)", () => {
  beforeEach(() => { calls.length = 0; });

  it("lists a non-current branch's commits and cherry-picks the clicked one", async () => {
    const { getByText, findByText } = render(CherryPickMenu, {
      props: { path: "/repo", name: "repo", onChanged: () => {} },
    });

    await fireEvent.click(getByText("Cherry-pick"));
    // The source branch is the non-current one; its commits load via LogRef.
    const commit = await findByText("feature commit");
    expect(calls).toContainEqual(["log", "feature"]);

    await fireEvent.click(commit);
    expect(calls).toContainEqual(["pick", "abcdef1234567890"]);
  });
});
