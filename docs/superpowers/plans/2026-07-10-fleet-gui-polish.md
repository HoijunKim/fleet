# Fleet GUI Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Polish the Fleet desktop UI — move the agentic "Ask AI" session into a wide focused overlay, add disciplined app-wide entrance motion, replace CSS-drawn icons with an inline SVG set, and ship the "Fleet" name with a designed logo.

**Architecture:** Frontend-only (Svelte-TS under `frontend/src`). New components (`AgentOverlay`, `Icon`, `Logo`) and one store module (`agentSession.ts`) that lifts the agentic run state out of `RepoChat.svelte`. Motion lives in the existing `frontend/src/lib/motion.ts` + tokens in `frontend/src/app.css`. No backend logic change; the existing agent events and bindings are reused verbatim.

**Tech Stack:** Wails v2, Svelte 3 + TypeScript, Vite, vitest (`vitest run`), single global stylesheet `app.css` with `:root` design tokens.

## Global Constraints

Every task inherits these — copy verbatim from the spec `docs/superpowers/specs/2026-07-10-fleet-gui-polish-design.md`:

- **No new runtime dependencies.** `frontend/package.json` `dependencies`/`devDependencies` must not change. Icons/logo are hand-authored inline SVG; motion uses `svelte/transition` (already present) + `motion.ts`.
- **No backend logic change.** No edits to `app.go`, `internal/**`, or Go logic; no new/changed Wails bindings or event names. Events consumed unchanged: `agent:text`, `agent:activity`, `agent:action`, `agent:done`, `agent:error`. Bindings unchanged: `AgentAvailable`, `AgentConsent`, `GiveAgentConsent`, `AgentAsk(id, q)`, `ApproveAction(id, approved)`, `CancelAgent`. **Sole permitted non-frontend edit (Task 3 only):** the app-name strings `Title` in `main.go:30` and `name` in `wails.json`, `fleet` → `Fleet`. String-only.
- **`prefers-reduced-motion: reduce` honored for every animation.** Route JS-driven motion through `motion.ts` (which already gates on `reducedMotion()`); CSS animations get a `@media (prefers-reduced-motion: reduce)` off-switch.
- **Enter-only discipline.** New motion is entrance/state-change only — no loops or bounce except the existing SyncPill `.spinner`. Micro ≈ `--t` (130ms); enter ≈ `--t-enter` (180ms). Staggered lists stay capped (`flyUp` already caps delay at index 8).
- **Preserve existing agentic guards.** The run's project-scoping (`agentStale`/`agentGenId` semantics), consent gate, fail-safe approval, single-run mutex, and per-repo chat persistence (`fleet.chat:<path>`) must not be weakened by the re-housing.
- **Green gates each task:** `npx svelte-check` 0 errors, `npx vitest run` green, and (final) `wails build` succeeds. Match house style: design tokens (`var(--…)`), existing `btn`/`btn-primary`/`btn-sm` classes, `EventsOn` for events.

---

## File Structure

- `frontend/src/lib/motion.ts` — add `fadeScaleIn()` (Task 1). Existing `reducedMotion()`, `flyUp()` reused.
- `frontend/src/app.css` — add `--t-enter` token; swap `.ic-*`/`.gear`/`.brand-dot` for components + delete dead CSS (Tasks 1–3).
- `frontend/src/lib/Icon.svelte` (new) — inline SVG glyph set, `currentColor` (Task 2).
- `frontend/src/lib/Logo.svelte` (new) — fixed-color brand mark (Task 3).
- `frontend/src/lib/agentSession.ts` (new) — agentic run store lifted from `RepoChat` (Task 4).
- `frontend/src/lib/AgentOverlay.svelte` (new) — wide focused agentic session UI (Task 5).
- `frontend/src/lib/RepoChat.svelte` — becomes launcher + single-shot fallback (Task 6).
- `frontend/src/lib/DetailPanel.svelte` — Ask AI launcher wiring + panel enter motion (Tasks 6, 8).
- `frontend/src/App.svelte` — mount `AgentOverlay` once; drive `setProject` on selection (Task 6).
- `frontend/src/lib/Toolbar.svelte` — icons + logo + "Fleet" (Tasks 2, 3).
- `frontend/src/lib/{AddProjectModal,DiffModal,SettingsModal}.svelte` — `brand-dot` → `<Logo/>` (Task 3).
- `frontend/src/lib/SyncPill.svelte` — state cross-fade (Task 7).
- `frontend/src/lib/ProjectTable.svelte`, `Graph.svelte` — entrance motion (Task 8).
- `main.go`, `wails.json` — "Fleet" strings (Task 3).
- Tests: `motion.test.ts`, `Icon.test.ts`, `Logo.test.ts`, `agentSession.test.ts`, `AgentOverlay.test.ts` (new).

---

## Task 1: Motion helpers + enter token

**Files:**
- Modify: `frontend/src/lib/motion.ts`
- Modify: `frontend/src/app.css` (`:root`)
- Test: `frontend/src/lib/motion.test.ts` (create)

**Interfaces:**
- Produces: `fadeScaleIn(): { duration: number; start: number; opacity: number }` — params for svelte's built-in `scale` transition (import `scale` from `svelte/transition`), collapsing to `{ duration: 0, start: 1, opacity: 1 }` under reduced motion. Existing `reducedMotion()` and `flyUp(i)` are reused by later tasks.
- Produces (CSS): token `--t-enter: 180ms cubic-bezier(0.2, 0, 0.2, 1)`.

- [ ] **Step 1: Write the failing test** — `frontend/src/lib/motion.test.ts`

```ts
import { describe, it, expect, vi, afterEach } from "vitest";
import { fadeScaleIn, flyUp, reducedMotion } from "./motion";

function stubReduced(reduced: boolean) {
  vi.stubGlobal("matchMedia", (q: string) => ({
    matches: reduced && q.includes("reduce"),
    media: q, addEventListener() {}, removeEventListener() {},
  }));
}
afterEach(() => vi.unstubAllGlobals());

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
```

- [ ] **Step 2: Run it, verify it fails** — Run: `cd frontend && npx vitest run src/lib/motion.test.ts`. Expected: FAIL (`fadeScaleIn` is not exported).

- [ ] **Step 3: Implement** — append to `frontend/src/lib/motion.ts`:

```ts
// fadeScaleIn returns params for svelte's `scale` transition (a soft fade +
// subtle scale-up), collapsed to instant under reduced motion. Use for
// overlays/panels: `transition:scale={fadeScaleIn()}`.
export function fadeScaleIn(): { duration: number; start: number; opacity: number } {
  if (reducedMotion()) return { duration: 0, start: 1, opacity: 1 };
  return { duration: 180, start: 0.98, opacity: 0 };
}
```

- [ ] **Step 4: Add the CSS token** — in `frontend/src/app.css`, in the `:root` block right after the `--t:` line (app.css:63), add:

```css
  --t-enter: 180ms cubic-bezier(0.2, 0, 0.2, 1);
```

- [ ] **Step 5: Run tests, verify pass** — Run: `cd frontend && npx vitest run src/lib/motion.test.ts && npx svelte-check`. Expected: tests PASS, svelte-check 0 errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/lib/motion.ts frontend/src/lib/motion.test.ts frontend/src/app.css
git commit -m "feat(motion): fadeScaleIn helper + --t-enter token (reduced-motion gated)"
```

---

## Task 2: Icon component + swap CSS-drawn icons

**Files:**
- Create: `frontend/src/lib/Icon.svelte`
- Create: `frontend/src/lib/Icon.test.ts`
- Modify: `frontend/src/lib/Toolbar.svelte` (replace `.ic-search`, `.gear`, and any `.ic-jump` spans)
- Modify: `frontend/src/app.css` (delete dead `.ic-search*`, `.ic-jump*`, `.gear*` rules; keep `.icon-btn`)

**Interfaces:**
- Produces: `Icon.svelte` — props `name: string`, `size = 16`. Renders a 24×24 inline `<svg>` with `stroke="currentColor"`, `stroke-width="1.5"`, round caps/joins, `fill="none"`, from an internal `name → path-markup` map. Unknown name → empty `<svg>` (no throw). Names available: `search`, `settings`, `external`, `chevron-down`, `activity`, `check`, `x`, `stop`, `sparkle`, `file`, `terminal`.

- [ ] **Step 1: Write the failing test** — `frontend/src/lib/Icon.test.ts`

```ts
import { describe, it, expect } from "vitest";
import Icon from "./Icon.svelte";

// Svelte 3 server render: Component.render(props) -> { html }
function render(props: Record<string, unknown>) {
  return (Icon as any).render(props).html as string;
}

describe("Icon", () => {
  it("renders an svg for a known name, sized", () => {
    const html = render({ name: "search", size: 20 });
    expect(html).toContain("<svg");
    expect(html).toContain('width="20"');
    expect(html).toContain('stroke="currentColor"');
  });

  it("renders an empty svg for an unknown name (no throw)", () => {
    const html = render({ name: "nope" });
    expect(html).toContain("<svg");
    expect(html).not.toContain("<path");
  });
});
```

- [ ] **Step 2: Run it, verify it fails** — Run: `cd frontend && npx vitest run src/lib/Icon.test.ts`. Expected: FAIL (module not found).

- [ ] **Step 3: Implement** — `frontend/src/lib/Icon.svelte`:

```svelte
<script lang="ts">
  // Inline SVG glyph set (no dependency). Lucide-style geometry, hand-authored.
  // Colored via currentColor so it inherits the surrounding text/accent color.
  export let name: string;
  export let size: number = 16;

  // Each entry is the inner markup of a 24x24 viewBox, fill="none",
  // stroke="currentColor". Unknown name -> "" (empty svg, no throw).
  const P: Record<string, string> = {
    search: '<circle cx="11" cy="11" r="7"/><path d="M21 21l-4.3-4.3"/>',
    settings:
      '<circle cx="12" cy="12" r="3.2"/><path d="M12 3v2.5M12 18.5V21M4.2 7l2.2 1.3M17.6 15.7l2.2 1.3M4.2 17l2.2-1.3M17.6 8.3l2.2-1.3"/>',
    external: '<path d="M14 5h5v5"/><path d="M19 5l-9 9"/><path d="M18 13v6H5V6h6"/>',
    "chevron-down": '<path d="M6 9l6 6 6-6"/>',
    activity: '<path d="M4 12h4l2.5-6 3 12 2.5-6H20"/>',
    check: '<path d="M5 12.5l4.5 4.5L19 7"/>',
    x: '<path d="M6 6l12 12M18 6L6 18"/>',
    stop: '<rect x="6" y="6" width="12" height="12" rx="2"/>',
    sparkle: '<path d="M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8z"/>',
    file: '<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5"/>',
    terminal: '<path d="M6 8l3.5 3.5L6 15"/><path d="M12.5 16H18"/>',
  };
  $: inner = P[name] ?? "";
</script>

<svg
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width="1.5"
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
>{@html inner}</svg>
```

- [ ] **Step 4: Run test, verify pass** — Run: `cd frontend && npx vitest run src/lib/Icon.test.ts`. Expected: PASS.

- [ ] **Step 5: Swap the header icons** — in `frontend/src/lib/Toolbar.svelte`: add `import Icon from "./Icon.svelte";` to the script. Replace `<span class="ic-search"></span>` (Toolbar.svelte:136) with `<Icon name="search" />` and `<span class="gear"></span>` (Toolbar.svelte:139) with `<Icon name="settings" />`. If a `.ic-jump` span exists in this file, replace it with `<Icon name="external" />`. Leave the `.icon-btn` wrapper buttons and their `title`/`aria-label` unchanged.

- [ ] **Step 6: Delete dead icon CSS** — in `frontend/src/app.css`, remove the now-unused rules: `.ic-search`, `.ic-search::before`, `.ic-search::after`, `.ic-jump` and their `:hover` variants (app.css:257–296 region), and `.gear`, `.gear::before` (app.css:852–866 region). Keep `.icon-btn` and `.icon-btn:hover`. Do not remove `.dot`/status-dot rules.

- [ ] **Step 7: Verify** — Run: `cd frontend && npx vitest run && npx svelte-check`. Expected: all green, 0 errors. Grep to confirm no dangling refs: `grep -rn "ic-search\|ic-jump\|\bgear\b" src` returns nothing in markup.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/lib/Icon.svelte frontend/src/lib/Icon.test.ts frontend/src/lib/Toolbar.svelte frontend/src/app.css
git commit -m "feat(ui): inline SVG Icon set; replace CSS-drawn toolbar icons"
```

---

## Task 3: Logo component + "Fleet" branding

**Files:**
- Create: `frontend/src/lib/Logo.svelte`
- Create: `frontend/src/lib/Logo.test.ts`
- Modify: `frontend/src/lib/Toolbar.svelte` (brand-dot → `<Logo/>`, "fleet" → "Fleet")
- Modify: `frontend/src/lib/AddProjectModal.svelte`, `DiffModal.svelte`, `SettingsModal.svelte` (brand-dot → `<Logo/>`)
- Modify: `frontend/src/app.css` (delete `.brand-dot`)
- Modify: `main.go` (line 30 `Title`), `wails.json` (`name`)

**Interfaces:**
- Produces: `Logo.svelte` — prop `size = 20`. Renders the fixed-color "Quiet orbit" brand mark (blue gradient squircle + white F + orbit satellite). Each instance uses a unique gradient id (module counter) so multiple `<Logo/>` on one page don't collide.

- [ ] **Step 1: Write the failing test** — `frontend/src/lib/Logo.test.ts`

```ts
import { describe, it, expect } from "vitest";
import Logo from "./Logo.svelte";

const render = (props: Record<string, unknown> = {}) => (Logo as any).render(props).html as string;

describe("Logo", () => {
  it("renders the brand svg sized by prop", () => {
    const html = render({ size: 24 });
    expect(html).toContain("<svg");
    expect(html).toContain('width="24"');
    expect(html).toContain("linearGradient");
  });

  it("uses a unique gradient id per instance (no collision)", () => {
    const a = render();
    const b = render();
    const idA = a.match(/id="(fleetLogo[^"]+)"/)?.[1];
    const idB = b.match(/id="(fleetLogo[^"]+)"/)?.[1];
    expect(idA).toBeTruthy();
    expect(idA).not.toBe(idB);
  });
});
```

- [ ] **Step 2: Run it, verify it fails** — Run: `cd frontend && npx vitest run src/lib/Logo.test.ts`. Expected: FAIL (module not found).

- [ ] **Step 3: Implement** — `frontend/src/lib/Logo.svelte`:

```svelte
<script lang="ts" context="module">
  let counter = 0;
</script>

<script lang="ts">
  // Fleet brand mark — "Quiet orbit": bold F on a blue gradient squircle with a
  // satellite tracing a short arc. Fixed brand colors (NOT currentColor): a logo
  // keeps its identity in both themes. Unique gradient id per instance.
  export let size: number = 20;
  const gid = "fleetLogo" + ++counter;
</script>

<svg width={size} height={size} viewBox="0 0 96 96" role="img" aria-label="Fleet">
  <defs>
    <linearGradient id={gid} x1="0" y1="0" x2="0.35" y2="1">
      <stop offset="0" stop-color="#7fb2ff" />
      <stop offset="1" stop-color="#3f5fd6" />
    </linearGradient>
  </defs>
  <rect width="96" height="96" rx="24" fill="url(#{gid})" />
  <rect width="96" height="52" rx="24" fill="#ffffff" opacity="0.09" />
  <path d="M40 24 A 30 30 0 0 1 72 50" fill="none" stroke="#ffffff" stroke-width="2.4" opacity="0.38" stroke-linecap="round" />
  <circle cx="72" cy="50" r="5" fill="#ffffff" />
  <g fill="#ffffff">
    <rect x="28" y="30" width="12" height="43" rx="6" />
    <rect x="28" y="30" width="31" height="12" rx="6" />
    <rect x="28" y="47" width="23" height="11" rx="5.5" />
  </g>
</svg>
```

- [ ] **Step 4: Run test, verify pass** — Run: `cd frontend && npx vitest run src/lib/Logo.test.ts`. Expected: PASS.

- [ ] **Step 5: Wire into the header** — in `frontend/src/lib/Toolbar.svelte`: add `import Logo from "./Logo.svelte";`. Replace `<span class="brand-dot"></span>` (Toolbar.svelte:54) with `<Logo size={20} />`. Change `<span class="brand-name">fleet</span>` (Toolbar.svelte:55) to `<span class="brand-name">Fleet</span>`.

- [ ] **Step 6: Wire into the three modal headers** — in each of `AddProjectModal.svelte:49`, `DiffModal.svelte:60`, `SettingsModal.svelte:140`: add `import Logo from "./Logo.svelte";` and replace `<span class="brand-dot"></span>` with `<Logo size={16} />`.

- [ ] **Step 7: Delete dead CSS + capitalize app name** — in `frontend/src/app.css` remove the `.brand-dot` rule(s). In `main.go` line 30 change `Title:  "fleet",` to `Title:  "Fleet",`. In `wails.json` change `"name": "fleet",` to `"name": "Fleet",`. Leave `"outputfilename": "fleet"` unchanged (exe stays `fleet.exe`).

- [ ] **Step 8: Verify** — Run: `cd frontend && npx vitest run && npx svelte-check`. Expected: green, 0 errors. Grep confirms no dangling `brand-dot`: `grep -rn "brand-dot" src` returns nothing. Confirm Go still builds: `cd .. && go build ./...` exit 0.

- [ ] **Step 9: Commit**

```bash
git add frontend/src/lib/Logo.svelte frontend/src/lib/Logo.test.ts frontend/src/lib/Toolbar.svelte frontend/src/lib/AddProjectModal.svelte frontend/src/lib/DiffModal.svelte frontend/src/lib/SettingsModal.svelte frontend/src/app.css main.go wails.json
git commit -m "feat(brand): Fleet logo mark + capitalized name across header, modals, window title"
```

---

## Task 4: agentSession store (lift agentic run out of RepoChat)

**Files:**
- Create: `frontend/src/lib/agentSession.ts`
- Create: `frontend/src/lib/agentSession.test.ts`

**Interfaces:**
- Consumes: bindings `AgentAvailable, AgentConsent, GiveAgentConsent, AgentAsk, ApproveAction, CancelAgent` from `../../wailsjs/go/main/App`; `EventsOn` from `../../wailsjs/runtime/runtime`.
- Produces: a module with these exports (Svelte `writable` stores + functions):
  - Stores: `available: Writable<boolean>`, `consent: Writable<boolean>`, `running: Writable<boolean>`, `stream: Writable<string>`, `activity: Writable<{tool:string; input:string}[]>`, `pending: Writable<{id:string; toolName:string; toolInput:string} | null>`, `cost: Writable<{costUsd:number; inputTokens:number; outputTokens:number} | null>`, `turns: Writable<{role:"user"|"assistant"; text:string}[]>`, `overlayOpen: Writable<boolean>`.
  - `initAgentSession(): Promise<void>` — idempotently subscribe to the five `agent:*` events once and refresh `available`/`consent`. Safe to call from `onMount`.
  - `setProject(p: {path:string; repoPath?:string; name?:string} | null): void` — on a path change, cancel any live run (`CancelAgent` + bump generation), clear run state, load that repo's saved `turns`. Null clears.
  - `giveConsent(): Promise<string>` — calls `GiveAgentConsent`; on empty (success) sets `consent=true`; returns the error string (or "").
  - `ask(q: string): Promise<void>` — append the user turn, mark running, capture run identity, call `AgentAsk(repoPath||path, q)`; on stale/error handle like RepoChat's `askAgent`.
  - `decide(approved: boolean): Promise<void>` — the `ApproveAction(id, approved)` round-trip with the double-click guard.
  - `cancel(): void` — `CancelAgent` + bump generation + clear run state.
  - `openOverlay(p): void` / `closeOverlay(): void` — set `overlayOpen`; `openOverlay` also calls `setProject(p)`. `closeOverlay` does NOT cancel the run.

This lifts, unchanged in behavior, the agentic half of `RepoChat.svelte` (lines 24–146, 166–211 for chat persistence, and the `agent:*` handlers in `onMount`). Read `RepoChat.svelte` for the exact current logic; preserve the `agentStale()` scoping (`runPath`/`runGen` vs current project + generation), the per-repo `fleet.chat:<path>` persistence (`slice(-20)`), and the fail-safe/double-click guards.

- [ ] **Step 1: Write the failing test** — `frontend/src/lib/agentSession.test.ts`

```ts
import { describe, it, expect, vi, beforeEach } from "vitest";
import { get } from "svelte/store";

// Capture EventsOn handlers so the test can emit agent:* events.
const handlers: Record<string, (d: any) => void> = {};
vi.mock("../../wailsjs/runtime/runtime", () => ({
  EventsOn: (name: string, cb: (d: any) => void) => {
    handlers[name] = cb;
    return () => delete handlers[name];
  },
}));
const calls: any[] = [];
vi.mock("../../wailsjs/go/main/App", () => ({
  AgentAvailable: async () => true,
  AgentConsent: async () => true,
  GiveAgentConsent: async () => "",
  AgentAsk: async (id: string, q: string) => { calls.push(["ask", id, q]); return ""; },
  ApproveAction: async (id: string, ok: boolean) => { calls.push(["approve", id, ok]); },
  CancelAgent: () => { calls.push(["cancel"]); },
}));

import * as S from "./agentSession";

beforeEach(() => { calls.length = 0; localStorage.clear(); });

describe("agentSession", () => {
  it("runs a question and lands the answer on agent:done", async () => {
    await S.initAgentSession();
    S.setProject({ path: "/repo/a", repoPath: "/repo/a", name: "a" });
    await S.ask("what changed?");
    expect(get(S.running)).toBe(true);
    expect(calls[0]).toEqual(["ask", "/repo/a", "what changed?"]);
    handlers["agent:text"]("partial ");
    handlers["agent:done"]({ result: "done", costUsd: 0.01, inputTokens: 5, outputTokens: 9 });
    const turns = get(S.turns);
    expect(turns[turns.length - 1]).toEqual({ role: "assistant", text: "partial" });
    expect(get(S.running)).toBe(false);
  });

  it("ignores stale events after the project switches mid-run", async () => {
    await S.initAgentSession();
    S.setProject({ path: "/repo/a", repoPath: "/repo/a" });
    await S.ask("q");
    S.setProject({ path: "/repo/b", repoPath: "/repo/b" }); // cancels + bumps gen
    expect(calls.some((c) => c[0] === "cancel")).toBe(true);
    handlers["agent:done"]({ result: "leaked", costUsd: 0, inputTokens: 0, outputTokens: 0 });
    expect(get(S.turns).some((t) => t.text === "leaked")).toBe(false);
  });

  it("decide round-trips the pending action id and clears it", async () => {
    await S.initAgentSession();
    S.setProject({ path: "/repo/a", repoPath: "/repo/a" });
    await S.ask("q");
    handlers["agent:action"]({ id: "act-1", toolName: "Edit", toolInput: { file: "x" } });
    expect(get(S.pending)?.id).toBe("act-1");
    await S.decide(true);
    expect(calls).toContainEqual(["approve", "act-1", true]);
    expect(get(S.pending)).toBe(null);
  });
});
```

- [ ] **Step 2: Run it, verify it fails** — Run: `cd frontend && npx vitest run src/lib/agentSession.test.ts`. Expected: FAIL (module not found).

- [ ] **Step 3: Implement** — `frontend/src/lib/agentSession.ts`:

```ts
import { writable, get } from "svelte/store";
import {
  AgentAvailable, AgentConsent, GiveAgentConsent, AgentAsk, ApproveAction, CancelAgent,
} from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";

type Turn = { role: "user" | "assistant"; text: string };
type Proj = { path: string; repoPath?: string; name?: string } | null;

export const available = writable(false);
export const consent = writable(false);
export const running = writable(false);
export const stream = writable("");
export const activity = writable<{ tool: string; input: string }[]>([]);
export const pending = writable<{ id: string; toolName: string; toolInput: string } | null>(null);
export const cost = writable<{ costUsd: number; inputTokens: number; outputTokens: number } | null>(null);
export const turns = writable<Turn[]>([]);
export const overlayOpen = writable(false);

let project: Proj = null;
let loadedPath = "";
let gen = 0;        // bumped on cancel/switch; late events with a stale gen are dropped
let runPath = "";
let runGen = 0;
let deciding = false;
let started = false;

function stale(): boolean {
  return !project || runPath !== project.path || runGen !== gen;
}
function fmtInput(v: any): string {
  if (typeof v === "string") return v;
  try { return JSON.stringify(v, null, 2); } catch { return String(v); }
}
function chatKey(p: string) { return "fleet.chat:" + p; }
function loadChat(p: string): Turn[] {
  if (typeof localStorage === "undefined") return [];
  try { const r = localStorage.getItem(chatKey(p)); const a = r ? JSON.parse(r) : []; return Array.isArray(a) ? a : []; }
  catch { return []; }
}
function saveChat() {
  if (typeof localStorage === "undefined" || !loadedPath) return;
  try { localStorage.setItem(chatKey(loadedPath), JSON.stringify(get(turns).slice(-20))); } catch { /* non-fatal */ }
}

export async function initAgentSession(): Promise<void> {
  if (!started) {
    started = true;
    EventsOn("agent:text", (t: any) => { if (stale()) return; stream.update((s) => s + String(t ?? "")); });
    EventsOn("agent:activity", (a: any) => { if (stale()) return; activity.update((x) => [...x, { tool: a?.tool ?? "", input: fmtInput(a?.input) }]); });
    EventsOn("agent:action", (a: any) => { if (stale()) return; pending.set({ id: a?.id ?? "", toolName: a?.toolName ?? "", toolInput: fmtInput(a?.toolInput) }); });
    EventsOn("agent:done", (d: any) => {
      if (stale()) return;
      cost.set({ costUsd: d?.costUsd ?? 0, inputTokens: d?.inputTokens ?? 0, outputTokens: d?.outputTokens ?? 0 });
      const answer = get(stream).trim() || String(d?.result ?? "(no answer)");
      turns.update((t) => [...t, { role: "assistant", text: answer }]); saveChat();
      stream.set(""); activity.set([]); pending.set(null); running.set(false);
    });
    EventsOn("agent:error", (e: any) => {
      if (stale()) return;
      turns.update((t) => [...t, { role: "assistant", text: "error: " + String(e ?? "agent failed") }]); saveChat();
      stream.set(""); pending.set(null); running.set(false);
    });
  }
  try { available.set(await AgentAvailable()); consent.set(await AgentConsent()); }
  catch { available.set(false); }
}

export function setProject(p: Proj): void {
  if (p && project && p.path === project.path) return;
  if (get(running)) { CancelAgent(); gen++; running.set(false); pending.set(null); activity.set([]); stream.set(""); cost.set(null); }
  project = p;
  loadedPath = p ? p.path : "";
  turns.set(p ? loadChat(p.path) : []);
}

export async function giveConsent(): Promise<string> {
  const msg = await GiveAgentConsent();
  if (!msg) consent.set(true);
  return msg || "";
}

export async function ask(q: string): Promise<void> {
  const text = q.trim();
  if (!text || !project || get(running)) return;
  turns.update((t) => [...t, { role: "user", text }]);
  stream.set(""); activity.set([]); pending.set(null); cost.set(null); running.set(true);
  runPath = project.path; runGen = ++gen;
  const id = project.repoPath || project.path;
  const err = await AgentAsk(id, text);
  if (stale()) return;
  if (err) { turns.update((t) => [...t, { role: "assistant", text: err }]); running.set(false); }
}

export async function decide(approved: boolean): Promise<void> {
  const p = get(pending);
  if (!p || deciding) return;
  pending.set(null);
  deciding = true;
  try { await ApproveAction(p.id, approved); } finally { deciding = false; }
}

export function cancel(): void {
  CancelAgent(); gen++; running.set(false); pending.set(null);
}

export function openOverlay(p: Proj): void { setProject(p); overlayOpen.set(true); }
export function closeOverlay(): void { overlayOpen.set(false); } // does NOT cancel the run
```

> Note for the implementer: this module intentionally omits `toastError` (a component concern). `ask`/`giveConsent` return the error string; callers (RepoChat launcher / AgentOverlay) surface toasts. Behavior otherwise mirrors `RepoChat.svelte` lines 24–211.

- [ ] **Step 4: Run tests, verify pass** — Run: `cd frontend && npx vitest run src/lib/agentSession.test.ts`. Expected: PASS (3/3).

- [ ] **Step 5: Verify types** — Run: `cd frontend && npx svelte-check`. Expected: 0 errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/lib/agentSession.ts frontend/src/lib/agentSession.test.ts
git commit -m "feat(agent): agentSession store — lift agentic run state/events out of RepoChat"
```

---

## Task 5: AgentOverlay — wide focused session UI

**Files:**
- Create: `frontend/src/lib/AgentOverlay.svelte`
- Create: `frontend/src/lib/AgentOverlay.test.ts`

**Interfaces:**
- Consumes: `agentSession` stores + `giveConsent/ask/decide/cancel/closeOverlay`; `motion.fadeScaleIn`, `motion.flyUp`; `Icon`, `Logo`; `renderBrief` from `./markdown`; `toastError` from `./toasts`.
- Produces: `AgentOverlay.svelte` — no props needed beyond reading the store; renders only when `overlayOpen` is true. Backdrop + centered panel; header (`<Logo/>` + `<name> · agentic deep-dive` + close `<Icon name="x"/>`); body = consent card (if `!consent`), activity feed (`flyUp` stagger), thread (`renderBrief`), approval card (`fadeScaleIn`), footer (cost + Cancel), input row. Esc / backdrop / ✕ → `closeOverlay()` (never cancels). Approve/Reject → `decide(true|false)`.

- [ ] **Step 1: Write the failing test** — `frontend/src/lib/AgentOverlay.test.ts`

```ts
import { describe, it, expect, vi, beforeEach } from "vitest";

const handlers: Record<string, (d: any) => void> = {};
vi.mock("../../wailsjs/runtime/runtime", () => ({
  EventsOn: (n: string, cb: (d: any) => void) => { handlers[n] = cb; return () => {}; },
}));
const calls: any[] = [];
vi.mock("../../wailsjs/go/main/App", () => ({
  AgentAvailable: async () => true, AgentConsent: async () => true,
  GiveAgentConsent: async () => "", AgentAsk: async () => "",
  ApproveAction: async (id: string, ok: boolean) => { calls.push([id, ok]); },
  CancelAgent: () => calls.push(["cancel"]),
}));

import { get } from "svelte/store";
import * as S from "./agentSession";
import AgentOverlay from "./AgentOverlay.svelte";

const render = (props: Record<string, unknown> = {}) => (AgentOverlay as any).render(props).html as string;

beforeEach(() => { calls.length = 0; S.overlayOpen.set(false); S.pending.set(null); });

describe("AgentOverlay", () => {
  it("renders nothing when closed, the panel when open", () => {
    expect(render()).not.toContain("agentic deep-dive");
    S.setProject({ path: "/r", repoPath: "/r", name: "r" });
    S.overlayOpen.set(true); S.consent.set(true);
    expect(render()).toContain("agentic deep-dive");
  });

  it("shows the approval card with the pending tool", () => {
    S.setProject({ path: "/r", repoPath: "/r", name: "r" });
    S.overlayOpen.set(true); S.consent.set(true);
    S.pending.set({ id: "act-9", toolName: "Edit", toolInput: "{}" });
    expect(render()).toContain("Edit");
  });
});
```

- [ ] **Step 2: Run it, verify it fails** — Run: `cd frontend && npx vitest run src/lib/AgentOverlay.test.ts`. Expected: FAIL (module not found).

- [ ] **Step 3: Implement** — `frontend/src/lib/AgentOverlay.svelte`. Move the agentic markup from `RepoChat.svelte` lines 365–397 (activity / stream / approval / cost) into the panel body, wire it to the `agentSession` stores, and add the overlay shell + motion. Full component:

```svelte
<script lang="ts">
  import { scale, fade } from "svelte/transition";
  import { fadeScaleIn, flyUp } from "./motion";
  import {
    available, consent, running, stream, activity, pending, cost, turns, overlayOpen,
    giveConsent, ask, decide, cancel, closeOverlay,
  } from "./agentSession";
  import Icon from "./Icon.svelte";
  import Logo from "./Logo.svelte";
  import { renderBrief } from "./markdown";
  import { toastError } from "./toasts";

  export let projectName: string = "";

  let question = "";

  async function onConsent() {
    const err = await giveConsent();
    if (err) toastError(err);
  }
  async function submit() {
    const q = question.trim();
    if (!q) return;
    question = "";
    await ask(q);
  }
  function onKey(e: KeyboardEvent) {
    if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); submit(); }
  }
  function onOverlayKey(e: KeyboardEvent) {
    if (e.key === "Escape") { e.preventDefault(); closeOverlay(); }
  }
</script>

{#if $overlayOpen}
  <div
    class="ov-backdrop"
    transition:fade={{ duration: 140 }}
    on:click|self={closeOverlay}
    on:keydown={onOverlayKey}
    role="dialog"
    aria-modal="true"
    aria-label="Agentic deep-dive"
    tabindex="-1"
  >
    <div class="ov-panel" transition:scale={fadeScaleIn()}>
      <div class="ov-head">
        <Logo size={18} />
        <span class="ov-title">{projectName} · agentic deep-dive</span>
        <button class="ov-close" on:click={closeOverlay} aria-label="Close"><Icon name="x" size={18} /></button>
      </div>

      <div class="ov-body">
        {#if $available && !$consent}
          <div class="ov-consent">
            <p>
              The agentic deep-dive lets Claude Code read files in this repo and send them to
              Anthropic under your Claude login, and can propose edits or commands (each one you
              approve here first).
            </p>
            <button class="btn btn-primary btn-sm" on:click={onConsent}>Enable agentic deep-dive</button>
          </div>
        {/if}

        {#if $activity.length}
          <div class="ov-activity">
            {#each $activity as a, i}
              <div class="ov-act" in:fly|local={flyUp(i)}>
                <Icon name="activity" size={13} /><span class="mono">{a.tool}</span> {a.input}
              </div>
            {/each}
          </div>
        {/if}

        <div class="ov-thread">
          {#each $turns as t}
            {#if t.role === "user"}
              <div class="ov-q">{t.text}</div>
            {:else}
              <div class="ov-a" class:err={t.text.startsWith("error:")}>{@html renderBrief(t.text)}</div>
            {/if}
          {/each}
          {#if $stream}<div class="ov-a ov-stream">{$stream}</div>{/if}
        </div>

        {#if $pending}
          <div class="ov-approval" transition:scale={fadeScaleIn()}>
            <div class="ov-approval-head">Approve <span class="mono">{$pending.toolName}</span>?</div>
            <pre class="ov-approval-body">{$pending.toolInput}</pre>
            <div class="ov-approval-btns">
              <button class="btn btn-primary btn-sm" on:click={() => decide(true)}><Icon name="check" size={14} /> Approve</button>
              <button class="btn btn-sm ov-reject" on:click={() => decide(false)}><Icon name="x" size={14} /> Reject</button>
            </div>
          </div>
        {/if}
      </div>

      <div class="ov-foot">
        {#if $running}
          <span class="ov-run"><span class="spinner"></span> working in the repo…</span>
          <button class="ov-cancel" on:click={cancel}><Icon name="stop" size={13} /> Cancel</button>
        {:else if $cost}
          <span class="ov-cost">cost ${$cost.costUsd.toFixed(4)} · {$cost.inputTokens} in / {$cost.outputTokens} out</span>
        {/if}
        <div class="ov-input">
          <input class="input" type="text" placeholder="Ask about this repo…" bind:value={question}
                 on:keydown={onKey} disabled={$running} aria-label="Ask about this repo" />
          <button class="btn btn-primary btn-sm" on:click={submit} disabled={$running || !question.trim()}>Ask</button>
        </div>
      </div>
    </div>
  </div>
{/if}

<style>
  .ov-backdrop {
    position: fixed; inset: 0; z-index: 50;
    background: rgba(4, 6, 10, 0.62);
    display: grid; place-items: center; padding: 24px;
  }
  .ov-panel {
    width: min(880px, 92vw); max-height: 86vh;
    display: flex; flex-direction: column;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: 14px; box-shadow: 0 24px 64px rgba(0, 0, 0, 0.5); overflow: hidden;
  }
  .ov-head { display: flex; align-items: center; gap: 9px; padding: 14px 16px; border-bottom: 1px solid var(--border); }
  .ov-title { font-size: 14px; font-weight: 600; color: var(--text); flex: 1; }
  .ov-close { display: grid; place-items: center; background: transparent; border: none; color: var(--muted); cursor: pointer; padding: 4px; border-radius: 6px; }
  .ov-close:hover { color: var(--text); background: var(--raised); }
  .ov-body { flex: 1; overflow: auto; padding: 16px; display: flex; flex-direction: column; gap: 14px; min-height: 0; }
  .ov-consent { border: 1px solid var(--border); border-radius: var(--r-btn); padding: 12px; display: flex; flex-direction: column; gap: 8px; }
  .ov-consent p { margin: 0; font-size: 12.5px; color: var(--muted); line-height: 1.5; }
  .ov-activity { display: flex; flex-direction: column; gap: 5px; }
  .ov-act { display: flex; align-items: center; gap: 7px; font-size: 12px; color: var(--faint); }
  .ov-thread { display: flex; flex-direction: column; gap: 12px; }
  .ov-q { align-self: flex-end; max-width: 80%; background: var(--accent-soft); border: 1px solid var(--accent-line); border-radius: 12px 12px 4px 12px; padding: 8px 12px; font-size: 13px; color: var(--text); white-space: pre-wrap; user-select: text; }
  .ov-a { align-self: flex-start; max-width: 94%; font-size: 13.5px; line-height: 1.6; color: var(--text); user-select: text; }
  .ov-a.err { color: var(--err); }
  .ov-a :global(p) { margin: 0 0 8px; }
  .ov-a :global(code) { font-family: var(--font-mono); font-size: 12.5px; background: var(--raised); padding: 1px 5px; border-radius: 4px; }
  .ov-a :global(pre) { overflow-x: auto; }
  .ov-stream { white-space: pre-wrap; }
  .ov-approval { border: 1px solid var(--accent-line); background: var(--accent-soft); border-radius: var(--r-btn); padding: 12px; display: flex; flex-direction: column; gap: 8px; }
  .ov-approval-head { font-size: 13px; color: var(--text); }
  .ov-approval-body { margin: 0; max-height: 320px; overflow: auto; font-family: var(--font-mono); font-size: 12px; background: var(--raised); border-radius: 4px; padding: 10px; white-space: pre-wrap; }
  .ov-approval-btns { display: flex; gap: 8px; }
  .ov-reject { border: 1px solid var(--err-line); color: var(--err); background: transparent; }
  .ov-foot { border-top: 1px solid var(--border); padding: 12px 16px; display: flex; flex-direction: column; gap: 10px; }
  .ov-run { display: flex; align-items: center; gap: 8px; font-size: 12.5px; color: var(--muted); }
  .ov-cost { font-size: 11.5px; color: var(--faint); }
  .ov-cancel, .ov-run + .ov-cancel { align-self: flex-start; font: inherit; font-size: 11.5px; display: inline-flex; align-items: center; gap: 5px; color: var(--muted); background: transparent; border: 1px solid var(--border); border-radius: var(--r-pill); padding: 2px 10px; cursor: pointer; }
  .ov-cancel:hover { color: var(--err); border-color: var(--err-line); }
  .ov-input { display: flex; gap: 8px; }
  .ov-input .input { flex: 1; }
  .mono { font-family: var(--font-mono); }
  .btn :global(svg) { vertical-align: -2px; }
</style>
```

- [ ] **Step 4: Run tests, verify pass** — Run: `cd frontend && npx vitest run src/lib/AgentOverlay.test.ts`. Expected: PASS (2/2).

- [ ] **Step 5: Verify types** — Run: `cd frontend && npx svelte-check`. Expected: 0 errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/lib/AgentOverlay.svelte frontend/src/lib/AgentOverlay.test.ts
git commit -m "feat(ui): AgentOverlay — wide focused agentic session with entrance motion"
```

---

## Task 6: RepoChat launcher + mount overlay + drive setProject

**Files:**
- Modify: `frontend/src/lib/RepoChat.svelte` (agentic path → launcher; keep single-shot fallback)
- Modify: `frontend/src/App.svelte` (mount `<AgentOverlay/>` once; call `agentSession.setProject` on selection)

**Interfaces:**
- Consumes: `agentSession` (`available, consent, running, turns, giveConsent, openOverlay, setProject`), `AgentOverlay`.
- Produces: Ask-AI tab shows a launcher when `available`; the live session opens in the overlay. Single-shot fallback (non-agentic) unchanged.

- [ ] **Step 1: RepoChat — remove the agentic run internals, add the launcher.** In `frontend/src/lib/RepoChat.svelte`:
  - Delete the agentic state and functions now owned by the store: lines 24–43 (agentic locals + `agentStale`), 45–52 keep `fmtInput` only if still used (it is not after this change — remove it), the five `agent:*` `EventsOn` pushes and the `AgentAvailable/AgentConsent` reads in `onMount` (lines 54–94 → keep `onMount` only if other setup remains; otherwise remove), `giveConsent` (98–102), `askAgent` (104–127), `decide` (129–139), `cancelAgent` (141–146), and the agentic branch of the project-switch reactive block (172–183, keep the single-shot resets).
  - Import the store: `import { available, consent, running, turns as agentTurns, giveConsent, openOverlay, setProject as agentSetProject } from "./agentSession";` and `import { initAgentSession } from "./agentSession";`. In `onMount`, call `await initAgentSession();`.
  - Keep everything for the single-shot path: `ask`, `buildContext`, `buildPrompt`, `parseTool`, `runTool`, `loadChat/saveChat/clearChat`, `turns` (single-shot), `loading`, `genId`, `cancelAsk`, `STARTERS`, `langName`.
  - Change `dispatch(text)` so the agentic branch opens the overlay instead of running inline:

```ts
  function dispatch(text: string) {
    if ($available && $consent) { agentSetProject(project); openOverlay(project); }
    else ask(text);
  }
```

  - Drive the store's project scope from this component's reactive block (so a project switch cancels a live agentic run even while the overlay is closed): in the `$: if (project && project.path !== loadedPath)` block, add `agentSetProject(project);` (alongside the existing single-shot resets).
  - Replace the markup's agentic sections (current lines 354–397) with a launcher shown when agentic is available:

```svelte
  {#if $available}
    <div class="rchat-launch">
      {#if !$consent}
        <p class="rchat-hint">Agentic deep-dive — Claude Code reads this repo (you approve every edit/command). Opens in a focused view.</p>
      {/if}
      <button class="btn btn-primary btn-sm" on:click={() => { agentSetProject(project); openOverlay(project); }}>
        {$running ? "Resume agentic deep-dive…" : "Open agentic deep-dive"}
        {#if $running}<span class="rchat-run-dot"></span>{/if}
      </button>
    </div>
  {/if}
```

  Keep the single-shot intro/thread/input markup (lines 399–447) as the fallback, but guard the "single-shot mode" note so it only shows when NOT agentic (it already keys off `!agentic`; change to `{#if !$available}`). Add minimal CSS for `.rchat-launch` (a bordered block, gap 8px) and `.rchat-run-dot` (a small pulsing `var(--accent)` dot — gate any pulse with `@media (prefers-reduced-motion: reduce) { animation: none; }`).

- [ ] **Step 2: App.svelte — mount the overlay once and scope it.** In `frontend/src/App.svelte`:
  - `import AgentOverlay from "./lib/AgentOverlay.svelte";` and `import { setProject as agentSetProject } from "./lib/agentSession";`.
  - The selected project is the existing `$: selected = projects.find((p) => p.id === selectedId) || null;` (App.svelte:147). Render the overlay once at the template top level, next to `<Toasts />` (App.svelte:760, outside the `view` conditionals so it persists across view switches): `<AgentOverlay projectName={selected?.name ?? ""} />`.
  - Add a reactive statement so changing (or clearing) the selection cancels a live run and rescopes the store: `$: agentSetProject(selected);` (`selected` is already `… || null`).

- [ ] **Step 3: Verify build + types + tests** — Run: `cd frontend && npx vitest run && npx svelte-check`. Expected: all green, 0 errors. Then confirm the whole app compiles: `cd .. && wails build`. Expected: builds `fleet.exe`.

- [ ] **Step 4: Manual smoke (documented, not automated)** — Launch `build/bin/fleet.exe`, open a project → Ask AI tab shows the launcher; clicking opens the overlay; Esc/backdrop/✕ closes without cancelling; switching project while a run is live cancels it. (This leg is inherently manual — record the result in the task report.)

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/RepoChat.svelte frontend/src/App.svelte
git commit -m "feat(ui): Ask AI launches the agentic overlay; single-shot stays inline; overlay scoped to selection"
```

---

## Task 7: SyncPill state cross-fade

**Files:**
- Modify: `frontend/src/lib/SyncPill.svelte`

**Interfaces:**
- Consumes: `svelte/transition` `fade`, `motion.reducedMotion`.
- Produces: the pill's state block cross-fades between `offline | syncing | synced | error | signedout`; the `.spinner` still spins only in `syncing`; `min-width` unchanged (no layout shift).

- [ ] **Step 1: Implement** — in `frontend/src/lib/SyncPill.svelte`: `import { fade } from "svelte/transition";` and `import { reducedMotion } from "./motion";`. Add a helper `$: fadeMs = reducedMotion() ? 0 : 140;`. Wrap each state branch's content in a keyed block so a state change triggers the transition — restructure the `{#if}` chain to key on `state`:

```svelte
{#key state}
  <span class="pill-inner" in:fade={{ duration: fadeMs }}>
    {#if state === "syncing"}
      <span class="spinner"></span><span class="pill-text">Syncing…</span>
    {:else if state === "synced"}
      <span class="dot dot-ok"></span><span class="pill-text">Synced {ago(lastSyncedUnix)}</span>
    {:else if state === "offline"}
      <span class="dot dot-warn"></span><span class="pill-text">Offline</span>
    {:else if state === "error"}
      <span class="dot dot-err"></span><span class="pill-text">Sync error</span>
      <button class="pill-action" on:click={onRetry}>Retry</button>
    {:else}
      <span class="dot dot-idle"></span>
      <button class="pill-action" on:click={onSignIn}>Sign in to sync</button>
    {/if}
  </span>
{/key}
```

Add `.pill-inner { display: inline-flex; align-items: center; gap: 7px; }` and keep the outer `.pill`'s existing `min-width`/layout so width stays stable across states.

- [ ] **Step 2: Verify** — Run: `cd frontend && npx vitest run && npx svelte-check`. Expected: green (existing tests unaffected), 0 errors. Confirm each state still renders (server-render assertion optional): the `{#key}` block must not change which literal strings appear (`Syncing…`, `Synced`, `Offline`, `Sync error`, `Sign in to sync`).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/SyncPill.svelte
git commit -m "feat(ui): SyncPill cross-fades between states (reduced-motion gated, no layout shift)"
```

---

## Task 8: List / panel / graph entrance motion

**Files:**
- Modify: `frontend/src/lib/ProjectTable.svelte`
- Modify: `frontend/src/lib/DetailPanel.svelte`
- Modify: `frontend/src/lib/Graph.svelte`

**Interfaces:**
- Consumes: `svelte/transition` (`fly`, `scale`, `fade`), `motion.flyUp`, `motion.fadeScaleIn`.

- [ ] **Step 1: ProjectTable rows** — in `frontend/src/lib/ProjectTable.svelte`: `import { fly } from "svelte/transition";` and `import { flyUp } from "./motion";`. On the repeated row element inside the `{#each}` of table rows, add `in:fly|local={flyUp(i)}` (bind the index as `{#each rows as row, i}` if not already). This staggers rows on load and on filter change; `flyUp` already caps the delay at index 8 so large lists stay snappy.

- [ ] **Step 2: DetailPanel enter** — in `frontend/src/lib/DetailPanel.svelte`: `import { scale } from "svelte/transition";` and `import { fadeScaleIn } from "./motion";`. On the root `<aside class="detail">` (DetailPanel.svelte:104), add `transition:scale={fadeScaleIn()}` so the panel eases in when a project is selected and out when cleared.

- [ ] **Step 3: Graph fade-in** — in `frontend/src/lib/Graph.svelte`: `import { fade } from "svelte/transition";` and `import { reducedMotion } from "./motion";`. On the graph's top-level container element, add `in:fade={{ duration: reducedMotion() ? 0 : 200 }}`. Do not touch the existing force-simulation code.

- [ ] **Step 4: Verify** — Run: `cd frontend && npx vitest run && npx svelte-check`. Expected: green, 0 errors. Then `cd .. && wails build` succeeds.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/ProjectTable.svelte frontend/src/lib/DetailPanel.svelte frontend/src/lib/Graph.svelte
git commit -m "feat(ui): staggered row / panel / graph entrance motion (reduced-motion gated)"
```

---

## Self-Review Notes (author)

- **Spec coverage:** W1 → Tasks 4,5,6. W2 → Tasks 1,5,7,8. W3 → Task 2. W4 → Task 3. All spec File-Structure entries appear in a task.
- **Type consistency:** store export names (`available, consent, running, stream, activity, pending, cost, turns, overlayOpen`, `initAgentSession, setProject, giveConsent, ask, decide, cancel, openOverlay, closeOverlay`) are used identically in Tasks 4, 5, 6. `fadeScaleIn`/`flyUp`/`reducedMotion` signatures match Task 1. `Icon` name set includes every name referenced by AgentOverlay (`x, activity, check, stop`) and Toolbar (`search, settings`).
- **Reduced motion:** every motion site routes through `motion.ts` helpers (`fadeScaleIn`/`flyUp` self-gate) or an explicit `reducedMotion() ? 0 : …` duration (SyncPill, Graph); the one CSS pulse (`.rchat-run-dot`) carries a `prefers-reduced-motion` off-switch.
- **App.svelte anchors (resolved):** selected project is `selected` (App.svelte:147); overlay mounts next to `<Toasts />` (App.svelte:760); DetailPanel binding at App.svelte:736. No guessing left for the implementer.
