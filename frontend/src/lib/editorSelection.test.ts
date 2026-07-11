import { describe, it, expect } from "vitest";
import { editorSelection } from "./editorSelection";

const opts = [{ command: "code" }, { command: "nvim" }];

describe("editorSelection", () => {
  it("selects a known command", () => {
    expect(editorSelection("code", opts)).toEqual({ selected: "code", customValue: "code" });
  });
  it("falls back to custom for an unknown command", () => {
    expect(editorSelection("mate -w", opts)).toEqual({ selected: "custom", customValue: "mate -w" });
  });
  it("empty editor -> custom, empty value", () => {
    expect(editorSelection("", opts)).toEqual({ selected: "custom", customValue: "" });
  });
});
