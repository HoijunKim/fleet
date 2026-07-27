// @vitest-environment happy-dom
//
// DOM test of the content-search mode toggles (tiers 4j, 4p): case (Aa), regex
// (.*), and whole-word (\b), threaded into SearchAll(term, !matchCase, regex,
// wholeWord).
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, cleanup } from "@testing-library/svelte";

const calls: any[] = [];
vi.mock("../../wailsjs/go/main/App", () => ({
  SearchAll: async (q: string, ignoreCase: boolean, regex: boolean, wholeWord: boolean) => {
    calls.push(["all", q, ignoreCase, regex, wholeWord]);
    return [];
  },
  SearchFiles: async () => [],
  OpenEditorAt: async () => "",
}));

import SearchOverlay from "./SearchOverlay.svelte";

async function typeQuery(getByLabelText: any) {
  const input = getByLabelText("Cross-repo search") as HTMLInputElement;
  await fireEvent.input(input, { target: { value: "todo" } });
  await new Promise((r) => setTimeout(r, 320)); // debounce
}

describe("SearchOverlay content modes (DOM)", () => {
  beforeEach(() => { calls.length = 0; });
  afterEach(() => cleanup());

  it("defaults to ignoreCase, no regex, no whole-word", async () => {
    const { getByLabelText } = render(SearchOverlay, { props: { onClose: () => {} } });
    await typeQuery(getByLabelText);
    expect(calls.at(-1)).toEqual(["all", "todo", true, false, false]);
  });

  it("Aa flips ignoreCase off", async () => {
    const { getByLabelText } = render(SearchOverlay, { props: { onClose: () => {} } });
    await typeQuery(getByLabelText);
    await fireEvent.click(getByLabelText("Match case"));
    await Promise.resolve();
    expect(calls.at(-1)).toEqual(["all", "todo", false, false, false]);
  });

  it(".* turns on regex, \\b turns on whole-word", async () => {
    const { getByLabelText } = render(SearchOverlay, { props: { onClose: () => {} } });
    await typeQuery(getByLabelText);
    await fireEvent.click(getByLabelText("Regular expression"));
    await Promise.resolve();
    expect(calls.at(-1)).toEqual(["all", "todo", true, true, false]);
    await fireEvent.click(getByLabelText("Whole word"));
    await Promise.resolve();
    expect(calls.at(-1)).toEqual(["all", "todo", true, true, true]);
  });
});
