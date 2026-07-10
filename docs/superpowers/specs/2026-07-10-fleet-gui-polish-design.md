# fleet GUI Polish — Design Spec

**Date:** 2026-07-10
**Status:** Approved for planning
**Topic:** Three-workstream frontend polish pass: agentic deep-dive overlay, app-wide motion, inline SVG icon set.

## Goal

Raise the fleet desktop UI from "solid, uneven" to "polished and coherent" by fixing three concrete gaps found in a live render review: (1) the agentic "Ask AI" surface is cramped in a fixed 400px detail panel, (2) motion is present but uneven — the newest components (RepoChat, SyncPill) have no enter/state animation, (3) icons are hand-drawn CSS shapes rather than a crisp, consistent set.

## Context

- Wails v2 desktop app; Svelte-TS frontend under `frontend/src`, single global stylesheet `frontend/src/app.css`, design tokens in `:root`.
- `frontend/src/lib/motion.ts` **already exists** and is the motion home: `reducedMotion()` reads `prefers-reduced-motion: reduce`, and `flyUp(i)` returns staggered `{y, duration, delay}` that collapses to `{0,0,0}` under reduced motion. Used today by `AgendaCard.svelte`, `Today.svelte`.
- Motion token today: `--t: 130ms cubic-bezier(0.2, 0, 0.2, 1)` (app.css:63), used for hover/color micro-transitions across the app.
- The agentic chat lives in `RepoChat.svelte` (532 lines), rendered by `DetailPanel.svelte` inside `<aside class="detail">` which is `flex: 0 0 400px; width: 400px` (app.css:603) — the source of the cramping. `DetailPanel` tabs: Overview / Git / Tasks / Symbols / Ask AI.
- Icons today: CSS-drawn `.ic-search`, `.ic-jump`, `.gear` (app.css:257–296, 852–866); a dropdown chevron as an inline-SVG `data:` background (app.css:186); status dots as CSS circles (`.dot` variants).

**No backend change.** All Go bindings and events already exist and are reused verbatim.

## Global Constraints

Copy these verbatim into the plan's Global Constraints; every task inherits them.

- **No new runtime dependencies.** Icons are hand-authored inline SVG; motion uses Svelte's built-in `svelte/transition` + `svelte/animate` (already in the toolchain) and the existing `motion.ts`. No icon library, no animation library. `frontend/package.json` dependencies must not change.
- **No backend / Go change.** No edits to `app.go`, `internal/agent/*`, or any `.go` file. No new/changed Wails bindings or event names. The three existing agent event names (`agent:text`, `agent:activity`, `agent:action`, `agent:done`, `agent:error`) and bindings (`AgentAvailable`, `AgentConsent`, `GiveAgentConsent`, `AgentAsk(id, q)`, `ApproveAction(id, approved)`, `CancelAgent`) are consumed unchanged.
- **`prefers-reduced-motion: reduce` is honored for every animation added.** Route all new enter/stagger motion through `motion.ts` helpers (or an equivalent guard) so reduced-motion users get instant, motionless rendering. No un-guarded transitions.
- **Enter-only discipline.** New motion is entrance/state-change only: no looping, bouncing, or continuous movement except the single existing SyncPill spinner. Durations stay short (micro ≈130ms via `--t`; enter ≈180ms). Staggered lists are capped so large lists never feel slow.
- **Preserve existing behavior and guards.** The agentic run's project-scoping (`agentStale()` / `agentGenId`), consent gate (server + UI), fail-safe approval, and single-run mutex are not weakened by the re-housing.
- **Green gates.** `wails build` succeeds, `npx svelte-check` reports 0 errors, existing `vitest` and Go suites stay green.

## Workstream 1 — Agentic Deep-Dive Overlay

**Problem:** the agentic session (streaming activity feed + chat + code blocks + approval card + cost/cancel) is squeezed into ~370px usable width.

**Approach:** move the live agentic session into a wide, focused overlay. The "Ask AI" tab becomes a slim launcher; the session runs in the overlay.

### Components

- **New `lib/AgentOverlay.svelte`** — the overlay shell + the full agentic session UI (consent card, activity feed, chat transcript, approval card, footer). It owns the same state and event wiring that `RepoChat.svelte` has today for agentic mode.
- **`RepoChat.svelte` becomes the launcher + single-shot fallback.** In the Ask AI tab it renders: consent state, a one-line "last conversation" summary if any, and a primary **"Open agentic deep-dive"** button that opens the overlay. When the agent is unavailable/non-Claude, it keeps today's inline single-shot chat fallback (no overlay).
- Decision on code organization: the shared agentic logic (event subscription, `agentStale`/`agentGenId` scoping, `AgentAsk`/`ApproveAction`/`CancelAgent` calls, message/activity/approval state) is extracted into a small **`lib/agentSession.ts`** store/module consumed by `AgentOverlay.svelte`, so the overlay and any launcher affordance share one source of truth and there is no duplicated event wiring. `RepoChat.svelte`'s single-shot fallback path is unaffected by this module.

### Overlay layout & behavior

- Backdrop: full-window dimmed layer; panel centered, `width: min(880px, 92vw)`, `max-height: 86vh`, internal scroll; header row: `<project> · agentic deep-dive` + close (✕) using the new SVG icon.
- Sections top→bottom: consent card (only if not consented) → activity feed (streaming tool calls, now full width) → chat transcript (markdown + code, wide) → approval card when an action is pending → footer (cost / input+output tokens, and a Cancel button while running).
- **Close safety (explicit):**
  - Esc, backdrop click, and ✕ all **hide** the overlay. They do **not** cancel a running agent (closing is non-destructive; only the explicit Cancel button / project-switch cancels).
  - While a run is in flight, the Ask AI tab shows a small "running" indicator so the user can reopen and see live state; reopening restores the current activity/chat/approval.
  - A pending approval stays gated while the overlay is hidden (fail-safe: no auto-approve, no auto-deny; it resolves on user decision or the existing hook timeout). The running indicator signals that a decision is waiting.
  - Switching projects still cancels the run and clears state exactly as today (`CancelAgent()` + `agentGenId++`), and closes the overlay.
- Single-run mutex and one-overlay-at-a-time are preserved: opening the overlay for a different project than an active run is prevented the same way the current UI prevents cross-repo runs.

### Reuse

- No binding/event changes. The overlay subscribes to the same `agent:*` events and calls the same bindings. `ApproveAction(id, approved)` still round-trips the action `id`.

## Workstream 2 — App-Wide Motion Pass

**Approach:** extend `motion.ts` with a couple of guarded helpers and apply entrance/state motion across the app, disciplined by the Global Constraints (reduced-motion, enter-only, capped stagger).

### motion.ts additions

- `fadeScaleIn()` → params for an overlay/panel entrance (opacity 0→1, subtle scale 0.98→1, ~180ms), returning `{duration:0}` under reduced motion.
- `staggerCap(i, opts?)` → like `flyUp` but with an explicit index cap so a list of N rows never staggers beyond a fixed total (e.g. delay stops growing after ~10 items); collapses under reduced motion.
- New CSS token `--t-enter: 180ms cubic-bezier(0.2, 0, 0.2, 1)` in `:root` (app.css), alongside `--t`.

### Application

- **AgentOverlay:** backdrop fade-in; panel `fadeScaleIn`; activity-feed items stagger-in (`staggerCap`); approval card enters with a short scale+fade to draw attention; chat messages fade-in on append.
- **SyncPill:** cross-fade the pill contents between `offline | syncing | synced | error | signedout` so state changes read smoothly rather than snapping. The existing `.spinner` continues to spin only in `syncing`. `min-width` stays so there is no layout shift.
- **Projects table (`ProjectTable.svelte`):** subtle staggered fly-in of rows on load and on filter change, via `staggerCap` (capped so 20 rows stay snappy).
- **Detail panel (`DetailPanel.svelte`):** slide/fade-in when a project is selected.
- **Graph (`Graph.svelte`):** already force-animates; add only a light fade-in on view enter, nothing more.

### Over-motion guards (restated as acceptance)

- Every added transition is gated by `reducedMotion()` (or a `prefers-reduced-motion` CSS media query) — verified by a test that stubs the media query.
- No animation loops except the pre-existing SyncPill spinner.
- Stagger totals are bounded; no list entrance exceeds ~600ms end-to-end regardless of item count.

## Workstream 3 — Inline SVG Icon Set (no dependency)

**Approach:** one small hand-authored inline-SVG icon set, no library.

### Component

- **New `lib/Icon.svelte`** — props `name: string`, `size = 16`. Renders an inline `<svg>` (24×24 viewBox, `fill="none"`, `stroke="currentColor"`, `stroke-width="1.5"`, round caps/joins) from an internal `name → path` map. Color inherits via `currentColor`; size via prop. Lucide-style geometry, hand-authored (no copied library file, no dependency).
- Icon set (initial): `search`, `settings` (gear), `external` (jump), `chevron-down`, plus agentic icons `activity`, `check`, `x`, `stop`, `sparkle`, `file`, `terminal`. Unknown `name` renders nothing (empty svg) rather than throwing.

### Replacements

- Replace `.ic-search`, `.ic-jump`, `.gear` usages (currently empty `<span class="…">`) with `<Icon name="…"/>`; remove the now-dead CSS icon rules from app.css.
- Unify the dropdown chevron: replace the `data:` background-image (app.css:186) with `<Icon name="chevron-down"/>` where the chevron is a real element; where it must stay a CSS background (e.g. native `<select>`), leave the existing data-URI (documented exception).
- **Status dots stay CSS** (`.dot` variants) — they are semantic color indicators, not glyph icons; not in scope for SVG conversion.
- New agentic icons are used by the AgentOverlay (activity feed marker, approve/deny/cancel buttons, AI header mark).

## Data Flow

Unchanged from today. Frontend only: components subscribe to existing `agent:*` events and call existing bindings; the overlay is a re-housing of state that already flows through `RepoChat`. No new IPC, no new persisted state beyond what `RepoChat` already saves per repo.

## Error Handling / Edge Cases

- Agent unavailable / non-Claude: no overlay; `RepoChat` shows today's single-shot fallback + note.
- Overlay closed mid-run: run continues; reopening restores state; pending approval stays gated (fail-safe).
- Reduced motion: all added motion collapses to instant.
- Large lists: stagger capped; no perceptible slowdown.
- Unknown icon name: renders empty, no crash.

## Testing Strategy

- `Icon.svelte`: renders an `<svg>` for a known name; renders empty (no throw) for an unknown name; respects `size`.
- Motion guard: with `matchMedia('(prefers-reduced-motion: reduce)')` stubbed truthy, `fadeScaleIn()`/`staggerCap()`/`flyUp()` return zero-duration params.
- `AgentOverlay` open/close: opens from launcher; Esc/backdrop/✕ hide without calling `CancelAgent`; project-switch calls `CancelAgent` and closes; approval `id` round-trips to `ApproveAction`.
- SyncPill: renders each of the five states; spinner present only in `syncing`.
- Gates: `wails build`, `svelte-check` 0 errors, `vitest`, Go suites all green.

## Out of Scope (YAGNI)

- No new agent capabilities, tools, or backend changes.
- No theming/skinning beyond existing tokens; no light-mode redesign.
- No icon conversion for status dots.
- No animation on the Graph beyond a light fade-in.
- No multi-overlay / multi-session concurrency (single run preserved).

## File Structure

- **Create:** `frontend/src/lib/AgentOverlay.svelte`, `frontend/src/lib/agentSession.ts`, `frontend/src/lib/Icon.svelte`, and tests `frontend/src/lib/Icon.test.ts`, `frontend/src/lib/motion.test.ts` (motion guards), `frontend/src/lib/AgentOverlay.test.ts`.
- **Modify:** `frontend/src/lib/RepoChat.svelte` (launcher + fallback; extract agentic logic to `agentSession.ts`), `frontend/src/lib/SyncPill.svelte` (state cross-fade), `frontend/src/lib/ProjectTable.svelte` (row stagger), `frontend/src/lib/DetailPanel.svelte` (panel enter + Ask AI launcher wiring), `frontend/src/lib/Graph.svelte` (fade-in), `frontend/src/lib/motion.ts` (add `fadeScaleIn`, `staggerCap`), `frontend/src/app.css` (add `--t-enter`; swap `.ic-*`/`.gear` for `<Icon>`; remove dead icon CSS), and any component using `.ic-search`/`.ic-jump`/`.gear` (e.g. `Toolbar.svelte`).
