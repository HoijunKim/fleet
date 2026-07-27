// @vitest-environment happy-dom
//
// DOM test of the case-sensitivity toggle (tier 4j): content search defaults to
// ignoreCase=true, and the "Aa" toggle flips it to false.
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/svelte";

const calls: any[] = [];
vi.mock("../../wailsjs/go/main/App", () => ({
  SearchAll: async (q: string, ignoreCase: boolean) => { calls.push(["all", q, ignoreCase]); return []; },
  SearchFiles: async () => [],
  OpenEditorAt: async () => "",
}));

import SearchOverlay from "./SearchOverlay.svelte";

describe("SearchOverlay case toggle (DOM)", () => {
  beforeEach(() => { calls.length = 0; });

  it("defaults to ignoreCase and flips it with the Aa toggle", async () => {
    const { getByLabelText } = render(SearchOverlay, { props: { onClose: () => {} } });

    const input = getByLabelText("Cross-repo search") as HTMLInputElement;
    await fireEvent.input(input, { target: { value: "todo" } });
    // Debounced 250ms; wait it out.
    await new Promise((r) => setTimeout(r, 320));
    expect(calls.at(-1)).toEqual(["all", "todo", true]); // ignoreCase default

    // Toggle "Match case" -> re-runs immediately with ignoreCase=false.
    await fireEvent.click(getByLabelText("Match case"));
    await Promise.resolve();
    expect(calls.at(-1)).toEqual(["all", "todo", false]);
  });
});
