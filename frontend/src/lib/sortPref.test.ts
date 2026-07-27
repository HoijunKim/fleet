import { describe, it, expect, beforeEach } from "vitest";
import { loadSortPref, saveSortPref } from "./sortPref";

function fakeLocalStorage() {
  const m = new Map<string, string>();
  return {
    getItem: (k: string) => (m.has(k) ? m.get(k)! : null),
    setItem: (k: string, v: string) => void m.set(k, v),
    removeItem: (k: string) => void m.delete(k),
    _map: m,
  };
}

describe("sortPref", () => {
  beforeEach(() => { (globalThis as any).localStorage = fakeLocalStorage(); });

  it("round-trips a key and direction", () => {
    saveSortPref("deadline", "desc");
    expect(loadSortPref()).toEqual({ key: "deadline", dir: "desc" });
  });

  it("returns the unsorted default when absent", () => {
    expect(loadSortPref()).toEqual({ key: "", dir: "asc" });
  });

  it("returns the default on a malformed value without throwing", () => {
    localStorage.setItem("fleet.sort", "{not json");
    expect(loadSortPref()).toEqual({ key: "", dir: "asc" });
  });

  it("persists an empty key so clearing the sort is remembered", () => {
    saveSortPref("name", "asc");
    saveSortPref("", "asc");
    expect(loadSortPref()).toEqual({ key: "", dir: "asc" });
  });

  it("coerces an invalid direction to asc", () => {
    localStorage.setItem("fleet.sort", JSON.stringify({ key: "name", dir: "sideways" }));
    expect(loadSortPref()).toEqual({ key: "name", dir: "asc" });
  });
});
