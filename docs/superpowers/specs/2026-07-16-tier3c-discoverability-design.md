# Tier 3c - Discoverability Design

**Goal:** Make the app's surface discoverable and comfortable: a light/dark
theme toggle, a command palette that reaches every view and system action, and
a first-run onboarding path so an empty install isn't a dead end.

## 1. Light / dark theme

- `frontend/src/lib/theme.ts`:
  - `type Theme = "dark" | "light"`.
  - `initTheme()`: read `localStorage["fleet-theme"]`; if absent, fall back to the
    OS preference (`matchMedia("(prefers-color-scheme: light)")`); apply it.
  - `applyTheme(t)`: `document.documentElement.setAttribute("data-theme", t)`.
  - `toggleTheme()`: flip the current theme, persist, apply; returns the new value.
  - `currentTheme()`: read the applied attribute (default "dark").
- `app.css`: keep the existing dark palette on `:root` (default). Add
  `:root[data-theme="light"] { ... }` overriding every surface/text/accent
  neutral plus `color-scheme: light`, and darkened status hues for contrast on a
  light ground. Dark stays the default when no attribute is set.
- `main.ts` calls `initTheme()` before mount so there's no flash.
- Toolbar: a new theme-toggle `icon-btn` (sun in dark mode, moon in light) that
  calls `toggleTheme()`; a small reactive `theme` state keeps the icon in sync.

## 2. Command palette expansion

App's `actions` array gains, alongside the existing add/refresh/fetch/pull/push/
settings:
- View navigation: Go to Today / Overview / Projects / Graph (set `view`).
- Toggle theme.
- Sync now - only when signed in.
- Sign in - only when signed out; Sign out - only when signed in.

Actions are built reactively so the auth-dependent ones appear/disappear with
state. The palette already fuzzy-matches labels, so no palette-component change
is needed beyond feeding it the fuller list.

## 3. First-run onboarding

- New `Onboarding.svelte`: shown when the install is empty - no scan roots
  configured AND no projects discovered/created. Two clear next steps:
  "Add a folder to scan" (opens Settings) and "Add a project" (opens the manual
  add modal), plus a one-line note that sign-in is optional.
- Rendered on the Projects view (and reachable from Today) in place of the empty
  table when `rootsConfigured === false && projects.length === 0`. The existing
  "No projects match your filters" state is unchanged for the normal case.
- App exposes whether roots are configured (from the loaded config) to decide
  between onboarding and the ordinary empty state.

## Accessibility & polish

- The theme toggle has an `aria-label` reflecting the action ("Switch to light
  theme" / "Switch to dark theme") and a visible focus ring.
- Light palette keeps text/accent contrast >= the dark theme's; status colors
  stay distinguishable.
- Respect `prefers-reduced-motion` (no new animations added).

## Testing

- **Frontend unit**: `theme.ts` - init falls back to OS preference, toggle flips
  and persists, applyTheme sets the attribute (jsdom/document stub).
- **Frontend SSR**: `Onboarding` renders both next-step actions; the palette
  action-list builder yields the auth-conditional entries for signed-in vs
  signed-out.
- **GUI**: toggle to light and back; open the palette and jump to a view; the
  onboarding card on an empty install.

## Out of scope

Per-component theme overrides, additional themes beyond light/dark, customizable
accent color, a guided multi-step tour.
