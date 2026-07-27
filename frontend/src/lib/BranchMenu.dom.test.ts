// @vitest-environment happy-dom
//
// DOM test of the force-delete wiring (tier 4i): a safe delete refused as
// "not fully merged" surfaces a force control that calls DeleteBranchForce.
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/svelte";

const calls: any[] = [];
vi.mock("../../wailsjs/go/main/App", () => ({
  Branches: async () => ({ current: "master", all: ["master", "wip"] }),
  Checkout: async () => "",
  CreateBranch: async () => "",
  DeleteBranch: async (_p: string, b: string) => { calls.push(["safe", b]); return "error: The branch '" + b + "' is not fully merged."; },
  DeleteBranchForce: async (_p: string, b: string) => { calls.push(["force", b]); return ""; },
}));

import BranchMenu from "./BranchMenu.svelte";

describe("BranchMenu force delete (DOM)", () => {
  beforeEach(() => {
    calls.length = 0;
    vi.stubGlobal("confirm", () => true);
  });

  it("offers force delete after the safe delete is refused, and wires it", async () => {
    const { getByTitle, findByText, getByText } = render(BranchMenu, {
      props: { path: "/repo", name: "repo", onChanged: () => {} },
    });

    // Wait for Branches() to load (the trigger is disabled while all is empty).
    await findByText("master");
    // Open the popover.
    await fireEvent.click(getByTitle("Switch branch"));

    // Arm delete on the non-current branch (its "x").
    await fireEvent.click(getByText("x"));
    // Confirm -> safe delete, which the mock refuses as unmerged.
    await fireEvent.click(getByText("del?"));
    expect(calls).toContainEqual(["safe", "wip"]);

    // A force control appears; clicking it (confirm stubbed) force-deletes.
    const force = await findByText("force?");
    await fireEvent.click(force);
    expect(calls).toContainEqual(["force", "wip"]);
  });
});
