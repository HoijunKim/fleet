import { describe, it, expect, beforeEach } from "vitest";
import { initTheme, applyTheme, toggleTheme, currentTheme } from "./theme";

// The project has no jsdom, so stub the DOM/storage globals theme.ts touches.
// The functions read these at call time, so stubbing in beforeEach is enough.
let attrs: Record<string, string>;
let store: Record<string, string>;
let prefersLight: boolean;

beforeEach(() => {
  attrs = {};
  store = {};
  prefersLight = false;
  (globalThis as any).document = {
    documentElement: {
      getAttribute: (k: string) => attrs[k] ?? null,
      setAttribute: (k: string, v: string) => { attrs[k] = v; },
      removeAttribute: (k: string) => { delete attrs[k]; },
    },
  };
  (globalThis as any).localStorage = {
    getItem: (k: string) => store[k] ?? null,
    setItem: (k: string, v: string) => { store[k] = v; },
    clear: () => { store = {}; },
  };
  (globalThis as any).window = {
    matchMedia: (q: string) => ({ matches: prefersLight && q.includes("light") }),
  };
});

describe("theme", () => {
  it("applyTheme sets data-theme and currentTheme reads it", () => {
    applyTheme("light");
    expect(attrs["data-theme"]).toBe("light");
    expect(currentTheme()).toBe("light");
    applyTheme("dark");
    expect(currentTheme()).toBe("dark");
  });

  it("initTheme prefers a stored choice over the OS preference", () => {
    store["fleet-theme"] = "light";
    prefersLight = false; // OS prefers dark
    expect(initTheme()).toBe("light");
    expect(currentTheme()).toBe("light");
  });

  it("initTheme falls back to the OS preference when unset", () => {
    prefersLight = true;
    expect(initTheme()).toBe("light");
    prefersLight = false;
    expect(initTheme()).toBe("dark");
  });

  it("toggleTheme flips and persists", () => {
    applyTheme("dark");
    expect(toggleTheme()).toBe("light");
    expect(currentTheme()).toBe("light");
    expect(store["fleet-theme"]).toBe("light");
    expect(toggleTheme()).toBe("dark");
    expect(store["fleet-theme"]).toBe("dark");
  });
});
