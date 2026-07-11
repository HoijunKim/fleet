import { describe, it, expect } from "vitest";
import { deriveChips, visibleIndices, clampSel } from "./searchFilter";

const hits = [{ repo: "a" }, { repo: "b" }, { repo: "a" }, { repo: "c" }];

describe("searchFilter", () => {
  it("derives repos in first-seen order with counts", () => {
    expect(deriveChips(hits)).toEqual([
      { repo: "a", count: 2 }, { repo: "b", count: 1 }, { repo: "c", count: 1 },
    ]);
  });
  it("visibleIndices drops hidden repos, preserves order", () => {
    expect(visibleIndices(hits, new Set(["b"]))).toEqual([0, 2, 3]);
    expect(visibleIndices(hits, new Set(["a", "c"]))).toEqual([1]);
  });
  it("clampSel returns the nearest visible index", () => {
    expect(clampSel(2, [0, 2, 3])).toBe(2);   // already visible
    expect(clampSel(1, [0, 2, 3])).toBe(0);   // 1 hidden -> nearest visible
    expect(clampSel(5, [0, 2, 3])).toBe(3);   // past end -> last visible
    expect(clampSel(0, [])).toBe(0);          // nothing visible
  });
});
