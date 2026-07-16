// Light/dark theme: persisted in localStorage, applied as a data-theme attribute
// on the document root (the CSS palette keys off it; dark is the default when the
// attribute is absent).

import { writable } from "svelte/store";

export type Theme = "dark" | "light";

const KEY = "fleet-theme";

// A store mirroring the applied theme, so any UI (toolbar icon, etc.) reflects
// a change no matter which path triggered it (toolbar button, command palette).
export const theme = writable<Theme>("dark");

// currentTheme reads the theme currently applied to the document (default dark).
export function currentTheme(): Theme {
  return document.documentElement.getAttribute("data-theme") === "light" ? "light" : "dark";
}

export function applyTheme(t: Theme): void {
  document.documentElement.setAttribute("data-theme", t);
  theme.set(t);
}

// initTheme resolves the starting theme - a stored choice wins, else the OS
// preference - applies it, and returns it. Called once before mount.
export function initTheme(): Theme {
  // Guard the read the way toggleTheme guards the write: a throwing/absent
  // localStorage (storage disabled) must degrade to the OS default, not blow up
  // before mount and blank the app.
  let stored: string | null = null;
  try {
    stored = localStorage.getItem(KEY);
  } catch {
    /* storage unavailable - fall through to the OS preference */
  }
  let t: Theme;
  if (stored === "light" || stored === "dark") {
    t = stored;
  } else {
    t = typeof window !== "undefined" && window.matchMedia
      && window.matchMedia("(prefers-color-scheme: light)").matches
      ? "light"
      : "dark";
  }
  applyTheme(t);
  return t;
}

// toggleTheme flips dark<->light, persists the choice, applies it, and returns it.
export function toggleTheme(): Theme {
  const next: Theme = currentTheme() === "dark" ? "light" : "dark";
  applyTheme(next);
  try {
    localStorage.setItem(KEY, next);
  } catch {
    /* private-mode / storage disabled: theme still applies for this session */
  }
  return next;
}
