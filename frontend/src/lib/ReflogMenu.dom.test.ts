// @vitest-environment happy-dom
//
// DOM test of the reflog recovery picker (tier 4m): it lists HEAD movements and
// clicking one (confirm stubbed) calls RestoreReflog with that entry's ref.
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/svelte";

const calls: any[] = [];
vi.mock("../../wailsjs/go/main/App", () => ({
  Reflog: async () => [
    { hash: "aaaa111", ref: "HEAD@{0}", subject: "commit: latest", when: "2026-07-27 10:00" },
    { hash: "bbbb222", ref: "HEAD@{1}", subject: "reset: moving to HEAD~1", when: "2026-07-27 09:00" },
  ],
  RestoreReflog: async (_p: string, ref: string) => { calls.push(["restore", ref]); return ""; },
}));

import ReflogMenu from "./ReflogMenu.svelte";

describe("ReflogMenu (DOM)", () => {
  beforeEach(() => { calls.length = 0; vi.stubGlobal("confirm", () => true); });

  it("lists reflog entries and restores the clicked one by ref", async () => {
    const { getByText, findByText } = render(ReflogMenu, {
      props: { path: "/repo", name: "repo", onChanged: () => {} },
    });
    await fireEvent.click(getByText("History"));
    const entry = await findByText("reset: moving to HEAD~1");
    await fireEvent.click(entry);
    expect(calls).toContainEqual(["restore", "HEAD@{1}"]);
  });
});
