import { describe, it, expect } from "vitest";
import { ciBit } from "./ciBit";

describe("ciBit", () => {
  it("no bit for success/neutral/empty", () => {
    for (const c of ["", "success", "neutral", "skipped"]) expect(ciBit(c)).toBe("");
  });
  it("running for in-progress/queued", () => {
    for (const c of ["in_progress", "queued", "pending"]) expect(ciBit(c)).toBe("CI running");
  });
  it("failing for failure/timed_out/cancelled/action_required", () => {
    for (const c of ["failure", "timed_out", "cancelled", "action_required"]) expect(ciBit(c)).toBe("CI failing");
  });
});
