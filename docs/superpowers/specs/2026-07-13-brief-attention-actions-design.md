# Brief Deepening - Needs-Attention Actions - Design Spec

**Date:** 2026-07-13
**Status:** Approved for planning
**Topic:** Turn the Today brief from opaque AI prose into something you can ACT on: an actionable "Needs attention" panel that structures the same signals the brief reasons over (dirty/unpushed repos, CI failing, open PRs, near deadlines, stale WIP) into per-item rows with contextual one-click actions (open editor, push, open on GitHub, ask AI) - all in place, without leaving Today.

## Goal

The morning brief is a single AI text blob (`renderBrief` markdown) with no per-project model behind it, so you cannot act on "CI failing in repo X" from the brief - you must read the prose, then navigate away and hunt. The only structured, actionable element today is the "Easy to forget" queue, and its sole action is "Ask AI". Every per-project action already exists as a Wails binding (`OpenEditor`, `OpenTerminal`, `Push`, `OpenURL`+`GitHubURL`, `AgentAsk`) but none is surfaced on Today. This slice upgrades "Easy to forget" into a **Needs attention** panel that derives high-signal items from git state + GitHub signals + deadlines and attaches the relevant one-click action(s) to each - keeping the AI prose brief above as the narrative.

## Context

- `Today.svelte`: `generate()`/`buildPrompt()`/`projectLine()` build the AI brief; `forgotten` (lines 34-55) is a client-side derived queue (`wip`/`unpushed`/`idle`/`todo` per code repo from `p.dirty/ahead/lastWhen/todo`, sorted by staleness, top 8); rendered as `.ov-row`s with a hover-revealed "Ask AI" button (`onDrill(id)`). The AI brief renders via `{@html renderBrief(brief)}` - opaque prose. `ghByPath: Map<path,{ci,prs,issues}>` is refreshed in `generate()` but only consumed inside `projectLine` (prompt text).
- `projects` (App.svelte) carry git+PM fields per project: `id`(==repoPath for code), `name`, `path`/`repoPath`, `type`, `dirty`, `modified`, `ahead`, `behind`, `hasUpstream`, `branch`, `remote`, `lastWhen`, `todo`, `deadline`, `tasks`, `loaded`, `errMsg`, `isGit`.
- Bindings (all `app.go`, return an error string, `""`=ok): `OpenEditor(path)`, `OpenTerminal(path)`, `RevealInExplorer(path)`, `Push(path)`, `GitHubURL(remote)` (append `/actions`|`/pulls`), `OpenURL(url)`, `OpenInBrowser(remote)`. `AgentAsk` drill is via the existing `onDrill(id)` prop. `GitHubSignals()` -> `[]RepoGHSignal{repoPath,name,ci,prs,issues}`.
- Helpers/UI: `ciBit(ci)` (failing/running/none), `daysUntil(deadline)` (pm), `flyUp(i)` motion, `Icon.svelte` glyphs (`file`,`terminal`,`external`,`activity`,`sparkle`,`check`,`x`), `Toasts` store (used app-wide), the `.ov-row`/`.forgot-ask` hover-action pattern, theme tokens (`--accent`,`--err`,`--ahead`,`--dirty`,`--muted`,...).
- Drill/open: `onDrill(id)` deep-links into the DetailPanel Ask-AI tab; `onOpen(id)` selects + navigates to Projects.

## Global Constraints

- **No new runtime dependencies.** Pure frontend (Svelte/TS) + existing Wails bindings; no new Go binding, no `wailsjs` regen.
- **Non-destructive by default; the one side-effecting action (Push) is explicit.** Push appears only when `ahead>0 && hasUpstream`, is a clearly labeled button showing the branch, and reports its result via a toast. Editor/GitHub/Ask-AI/Reveal are read-only/navigational. No commit/pull/stash here (commit needs a message -> drill).
- **Best-effort, graceful:** GitHub-derived rows (CI failing / open PRs) only appear when `ghByPath` has data; with GitHub unavailable the panel still shows git/deadline items. A project with `errMsg` or not `loaded` is skipped.
- **The AI prose brief is unchanged** - this adds a structured panel next to it, and REPLACES the narrower "Easy to forget" section (its items are a subset of the new panel's).
- **Match the existing design system:** reuse `.ov-card`/`.ov-row`, the `.forgot-ask` hover-reveal action pattern, `Icon`, `flyUp`, theme tokens, and `prefers-reduced-motion` behavior.
- **GUI-verified:** after implementation, build the exe, launch it, capture the Today view (CopyFromScreen, which grabs the real window incl. any native bits), and visually confirm the Needs-attention panel renders with its rows + action buttons before merge.
- **Green gates:** `npx vitest run`, `npx svelte-check` 0 errors, `wails build`, `go build/test ./...` (Go untouched but must stay green).

## Workstream 1 - Attention deriver (`frontend/src/lib/attention.ts` + test)

- **`deriveAttention(projects, ghByPath, now) -> AttentionItem[]`**, a pure function (no Svelte, unit-tested). `AttentionItem { id, name, path, repoPath, remote, branch, reasons: Reason[], actions: ActionKind[], rank }` where `Reason { kind: "dirty"|"unpushed"|"idle"|"todo"|"ci"|"prs"|"deadline"; label: string; sev: "high"|"med"|"low" }` and `ActionKind` in `{ "editor","push","ci","prs","ask","open" }`.
- Rules (per code project, `loaded && isGit && !errMsg`):
  - `dirty` if `p.dirty` (label `"N uncommitted"` from `modified`); `unpushed` if `p.ahead>0 && p.hasUpstream` (label `"N to push"`); `idle` if last commit older than a threshold (reuse the forgotten staleness, e.g. `daysUntil(lastWhen)`-style age > 7d) AND not dirty/unpushed; `todo` if `p.todo>0`.
  - `ci` if `ciBit(gh.ci)==="CI failing"`; `prs` if `gh.prs>0` (from `ghByPath.get(p.repoPath||p.path)`).
  - `deadline` if `p.deadline` and `daysUntil(p.deadline)` within a near window (e.g. <=3 days, incl. overdue).
- Action mapping (contextual, deduped, capped ~3 per row): `ci`->`ci`+`ask`; `prs`->`prs`; `dirty`->`editor`+`ask`; `unpushed`->`push`+`editor`; `idle`/`todo`->`editor`+`ask`; `deadline`->`open`. Always ensure `ask` OR `open` is available. `push` only when `unpushed`.
- Ranking: severity-weighted (ci failing + overdue deadline highest, then unpushed/dirty, then idle/todo), stable; cap the list (e.g. top 8) with a "+N more" affordance (or just cap). Dedup one row per project merging all its reasons.
- Unit tests: each reason fires on the right field; GitHub-absent -> no ci/prs reasons; a clean on-track project yields no item; ranking orders ci/deadline first; dedup merges reasons; push action only with ahead+upstream; cap respected.

## Workstream 2 - Panel + rows in Today (`Today.svelte`, maybe a small `AttentionRow.svelte`)

- Replace the "Easy to forget" section with a **Needs attention** `.ov-card`: header + `.ov-count`, an `.ov-empty` ("All clear - nothing needs attention") when empty, else a list of rows (each `in:fly={flyUp(i)}`).
- Each row (reuse `.ov-row`): `.ov-name` (project name, click -> `onOpen(id)`), reason chips (`.ov-pill`, colored by sev via existing tokens - err for ci/overdue, ahead for unpushed, dirty for uncommitted, muted for idle/todo), and a hover-revealed action group (mirror `.forgot-ask`) of small icon+label buttons for that row's `actions`:
  - `editor` -> `OpenEditor(path)` (Icon `file`); toast on error.
  - `push` -> `Push(path)` (Icon `activity`/an up-arrow), label `"Push {branch}"`; toast `"Pushed {name}"` / the error string.
  - `ci` -> `OpenURL(GitHubURL(remote)+"/actions")` (Icon `external`).
  - `prs` -> `OpenURL(GitHubURL(remote)+"/pulls")` (Icon `external`).
  - `ask` -> `onDrill(id)` (Icon `sparkle`).
  - `open` -> `onOpen(id)`.
- Guard: actions that need a remote (`ci`/`prs`/`push`) call `GitHubURL`/`Push` and toast on empty/error; never crash a row.
- Feed the panel from `deriveAttention(projects, ghByPath, now)` reactively (`$:`), so it updates as git state / `ghByPath` refresh. Keep the existing brief prose card above it.
- A tiny `AttentionRow.svelte` is optional (extract if Today grows unwieldy); otherwise inline following the forgotten-row markup.

## Data Flow

`projects` (git+PM) + `ghByPath` (refreshed by `generate()`/`GitHubSignals()`) -> `deriveAttention()` -> ranked `AttentionItem[]` -> Needs-attention panel rows -> per-row one-click actions call existing bindings (`OpenEditor`/`Push`/`OpenURL(GitHubURL...)`/`onDrill`/`onOpen`) -> toast feedback. The AI prose brief above is unchanged.

## Error Handling / Edge Cases

- GitHub unavailable / `ghByPath` empty -> ci/prs reasons simply absent; git+deadline items still show.
- `Push` failure (e.g. rejected, no network) -> the binding's error string shown as a toast; the row stays.
- `GitHubURL(remote)==""` (non-GitHub remote) -> ci/prs actions omitted for that row (only present when a github remote exists).
- Not-loaded / errored / non-git project -> excluded from the panel.
- Empty panel -> friendly "All clear" state (a good day, not an error).
- Rapid re-derive on `projects`/`ghByPath` change is cheap (pure function over an in-memory array).

## Testing Strategy

- `attention.test.ts`: table-driven over `deriveAttention` - each reason, GitHub-present vs absent, clean project -> empty, ranking, dedup/merge, push-gating, cap. (Mirrors the existing pure-helper test style, node env.)
- Component: an SSR render assertion that a projects+ghByPath fixture renders the expected rows/labels (following the existing `(Comp as any).render(props).html` pattern) where practical; otherwise rely on the deriver tests + the GUI capture.
- **GUI capture (required before merge):** build, launch, land on Today, CopyFromScreen the window, and confirm the Needs-attention panel + rows + action buttons render correctly (and a hover state if capturable).
- Existing suites + `svelte-check` green; `wails build` + `go build/test ./...` green.

## Out of Scope (YAGNI)

- Commit / pull / stash / branch one-click actions (commit needs a message -> drill; keep the panel to attention + the safe/explicit set).
- Parsing/linking the AI prose brief (fragile; the structured panel is the actionable surface).
- A separate settings toggle for which reasons show / thresholds config (sensible fixed thresholds now).
- Bulk actions (push all, etc.).
- Reworking the DetailPanel or the agent overlay.

## File Structure

- **Create:** `frontend/src/lib/attention.ts`, `frontend/src/lib/attention.test.ts` (+ optionally `frontend/src/lib/AttentionRow.svelte`).
- **Modify:** `frontend/src/lib/Today.svelte` (replace the forgotten section with the Needs-attention panel fed by `deriveAttention`; wire the per-row actions to existing bindings + toasts; import `OpenEditor`/`Push`/`GitHubURL` as needed).
