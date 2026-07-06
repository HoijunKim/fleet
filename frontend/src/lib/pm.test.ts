import { describe, it, expect } from "vitest";
import { daysUntil, ddayLabel, deadlineSort } from "./pm";

// Build a "YYYY-MM-DD" string offset by n days from today, using LOCAL date
// parts (daysUntil parses "YYYY-MM-DD" as local midnight, so toISOString/UTC
// would be off by a day in non-UTC zones).
function dayOffset(n: number): string {
  const d = new Date();
  d.setHours(0, 0, 0, 0);
  d.setDate(d.getDate() + n);
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

describe("daysUntil", () => {
  it("returns null for empty or unparseable input", () => {
    expect(daysUntil("")).toBeNull();
    expect(daysUntil("not a date")).toBeNull();
  });

  it("is 0 for today, positive for future, negative for overdue", () => {
    expect(daysUntil(dayOffset(0))).toBe(0);
    expect(daysUntil(dayOffset(5))).toBe(5);
    expect(daysUntil(dayOffset(-3))).toBe(-3);
  });
});

describe("ddayLabel", () => {
  it("labels future, d-day and overdue", () => {
    expect(ddayLabel(dayOffset(4))?.text).toBe("D-4");
    expect(ddayLabel(dayOffset(0))?.text).toBe("D-DAY");
    expect(ddayLabel(dayOffset(-2))?.text).toBe("D+2");
    expect(ddayLabel("")).toBeNull();
  });
});

describe("deadlineSort", () => {
  it("sorts sooner first, no-deadline last", () => {
    expect(deadlineSort(dayOffset(1))).toBeLessThan(deadlineSort(dayOffset(9)));
    expect(deadlineSort("")).toBe(Number.POSITIVE_INFINITY);
  });
});
