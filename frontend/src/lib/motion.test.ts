import { describe, it, expect, vi, afterEach } from "vitest";
import { fadeScaleIn, flyUp, reducedMotion } from "./motion";

function stubReduced(reduced: boolean) {
  vi.stubGlobal("matchMedia", (q: string) => ({
    matches: reduced && q.includes("reduce"),
    media: q, addEventListener() {}, removeEventListener() {},
  }));
}
afterEach(() => {
  vi.unstubAllGlobals();
});

describe("motion helpers", () => {
  it("fadeScaleIn animates when motion is allowed", () => {
    stubReduced(false);
    const p = fadeScaleIn();
    expect(p.duration).toBeGreaterThan(0);
    expect(p.start).toBeLessThan(1);
    expect(p.opacity).toBe(0);
  });

  it("fadeScaleIn is instant under reduced motion", () => {
    stubReduced(true);
    expect(fadeScaleIn()).toEqual({ duration: 0, start: 1, opacity: 1 });
    expect(reducedMotion()).toBe(true);
  });

  it("flyUp collapses under reduced motion", () => {
    stubReduced(true);
    expect(flyUp(3)).toEqual({ y: 0, duration: 0, delay: 0 });
  });
});
