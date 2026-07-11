# Fleet Search Enhancements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Add file-name search (Content/Files mode toggle) and per-project filter chips to the cross-repo search overlay.

**Architecture:** A new `git.ListFiles` + `SearchFiles` binding power a Files mode in `SearchOverlay.svelte`; a pure, tested `searchFilter.ts` helper derives the project chips and applies the on/off filter so both modes share the grouping/nav/count logic.

**Tech Stack:** Go 1.22 (stdlib), Wails v2, Svelte-TS, vitest, Go testing.

## Global Constraints

Copy verbatim from the spec `docs/superpowers/specs/2026-07-11-fleet-search-enhancements-design.md`:
- **No new runtime dependencies** (`frontend/package.json` unchanged; Go stdlib).
- **Keyboard nav operates over VISIBLE (filtered) hits only**; `selIndex` never points at a hidden hit.
- **`prefers-reduced-motion` honored** for chip/toggle motion.
- **No regression** to existing content search (grep, grouping, keyboard nav, open, 250ms debounce, blank-query clear, `reqGen` generation guard).
- **Green gates:** `go build ./...` + `go vet ./...` clean, `go test ./...` green, `npx svelte-check` 0 errors, `npx vitest run` green, `wails build` succeeds.

## Commit authorship (all tasks)
`git -c user.name=hoijun -c user.email=hoijun.kim00@gmail.com commit -m "..."` - NO Co-Authored-By/Claude trailer.

---

## Task 1: File search backend (`git.ListFiles` + `SearchFiles`)

**Files:**
- Create: `internal/git/lsfiles.go`, `internal/git/lsfiles_test.go`
- Modify: `app.go` (add `FileHit` + `SearchFiles`)

**Interfaces:**
- Produces: `git.ListFiles(r Runner, dir string) ([]string, error)` - tracked repo-relative paths via `git ls-files`. And `app.go` `SearchFiles(query string) []FileHit` where `FileHit{Repo, RepoPath, File string}` (json `repo`/`repoPath`/`file`).

- [ ] **Step 1: Write the failing test** - `internal/git/lsfiles_test.go` (mirror `grep_test.go`'s fake-Runner style; read `internal/git/grep_test.go` first for the exact fake `Runner` type used):

```go
package git

import "testing"

func TestListFiles(t *testing.T) {
	r := fakeRunner{out: "a.go\ndir/b.ts\n\n"} // trailing blank tolerated
	got, err := ListFiles(r, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "a.go" || got[1] != "dir/b.ts" {
		t.Fatalf("got %v", got)
	}
}

func TestListFilesNoFilesNonZero(t *testing.T) {
	r := fakeRunner{out: "", err: errFake} // non-zero, empty output -> no files, no error
	got, err := ListFiles(r, "/repo")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v err %v", got, err)
	}
}
```

Note: reuse whatever fake `Runner`/sentinel error `grep_test.go` already defines. If `grep_test.go` names them differently, match those names (do NOT introduce a second fake).

- [ ] **Step 2: Run it, verify it fails** - `go test ./internal/git/ -run TestListFiles`. Expected: FAIL (undefined: ListFiles).

- [ ] **Step 3: Implement** - `internal/git/lsfiles.go`:

```go
package git

import "strings"

// ListFiles returns the repo's tracked files (repo-relative paths) via
// `git ls-files`. A non-zero exit with empty output is treated as no files,
// not an error (mirrors Grep's tolerance).
func ListFiles(r Runner, dir string) ([]string, error) {
	out, err := r.Run(dir, "ls-files")
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}
```

- [ ] **Step 4: Run test, verify pass** - `go test ./internal/git/ -run TestListFiles`. Expected: PASS.

- [ ] **Step 5: Add `SearchFiles` to app.go** - near `SearchAll` (app.go:856) add:

```go
// FileHit is one cross-repo file-name search result.
type FileHit struct {
	Repo     string `json:"repo"`
	RepoPath string `json:"repoPath"`
	File     string `json:"file"`
}

// SearchFiles finds tracked files across all discovered repos whose repo-
// relative path contains query (case-insensitive), capped for a responsive UI.
func (a *App) SearchFiles(query string) []FileHit {
	out := []FileHit{}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return out
	}
	cfg := a.cfgSnapshot()
	for _, r := range scan.Discover(cfg.Roots, cfg.ScanDepth, false) {
		files, _ := git.ListFiles(a.runner, r.Path)
		for _, f := range files {
			if strings.Contains(strings.ToLower(f), q) {
				out = append(out, FileHit{Repo: r.Name, RepoPath: r.Path, File: f})
				if len(out) >= 500 {
					return out
				}
			}
		}
	}
	return out
}
```

- [ ] **Step 6: Verify + commit** - `go build ./... && go vet ./... && go test ./...` (green). Then:

```bash
git add internal/git/lsfiles.go internal/git/lsfiles_test.go app.go
git commit -m "feat(search): SearchFiles - case-insensitive file-name search across repos"
```

---

## Task 2: `searchFilter.ts` helper (chips + filter + clamp)

**Files:**
- Create: `frontend/src/lib/searchFilter.ts`, `frontend/src/lib/searchFilter.test.ts`

**Interfaces:**
- Produces (pure, DOM-free):
  - `type RepoChip = { repo: string; count: number }`
  - `deriveChips(hits: {repo:string}[]): RepoChip[]` - repos in first-seen order with counts.
  - `visibleIndices(hits: {repo:string}[], hidden: Set<string>): number[]` - the flat indices of hits whose repo is NOT hidden, in order.
  - `clampSel(sel: number, visible: number[]): number` - the visible index nearest to `sel` (returns a value that is one of `visible`, or 0 if none).

- [ ] **Step 1: Write the failing test** - `frontend/src/lib/searchFilter.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { deriveChips, visibleIndices, clampSel } from "./searchFilter";

const hits = [{ repo: "a" }, { repo: "b" }, { repo: "a" }, { repo: "c" }];

describe("searchFilter", () => {
  it("derives repos in first-seen order with counts", () => {
    expect(deriveChips(hits)).toEqual([
      { repo: "a", count: 2 }, { repo: "b", count: 1 }, { repo: "c", count: 1 },
    ]);
  });
  it("visibleIndices drops hidden repos, preserves order", () => {
    expect(visibleIndices(hits, new Set(["b"]))).toEqual([0, 2, 3]);
    expect(visibleIndices(hits, new Set(["a", "c"]))).toEqual([1]);
  });
  it("clampSel returns the nearest visible index", () => {
    expect(clampSel(2, [0, 2, 3])).toBe(2);   // already visible
    expect(clampSel(1, [0, 2, 3])).toBe(0);   // 1 hidden -> nearest visible
    expect(clampSel(5, [0, 2, 3])).toBe(3);   // past end -> last visible
    expect(clampSel(0, [])).toBe(0);          // nothing visible
  });
});
```

- [ ] **Step 2: Run it, verify it fails** - `cd frontend && npx vitest run src/lib/searchFilter.test.ts`. Expected: FAIL (module not found).

- [ ] **Step 3: Implement** - `frontend/src/lib/searchFilter.ts`:

```ts
export type RepoChip = { repo: string; count: number };

// deriveChips lists repos present in hits, in first-seen order, with counts.
export function deriveChips(hits: { repo: string }[]): RepoChip[] {
  const order: string[] = [];
  const counts = new Map<string, number>();
  for (const h of hits) {
    if (!counts.has(h.repo)) order.push(h.repo);
    counts.set(h.repo, (counts.get(h.repo) ?? 0) + 1);
  }
  return order.map((repo) => ({ repo, count: counts.get(repo) ?? 0 }));
}

// visibleIndices returns the flat indices of hits whose repo is not hidden.
export function visibleIndices(hits: { repo: string }[], hidden: Set<string>): number[] {
  const out: number[] = [];
  hits.forEach((h, i) => { if (!hidden.has(h.repo)) out.push(i); });
  return out;
}

// clampSel returns the visible index nearest to sel (or 0 when none visible).
export function clampSel(sel: number, visible: number[]): number {
  if (visible.length === 0) return 0;
  if (visible.includes(sel)) return sel;
  let best = visible[0], bestDist = Math.abs(visible[0] - sel);
  for (const v of visible) {
    const d = Math.abs(v - sel);
    if (d < bestDist) { best = v; bestDist = d; }
  }
  return best;
}
```

- [ ] **Step 4: Run tests, verify pass** - `cd frontend && npx vitest run src/lib/searchFilter.test.ts`. Expected: PASS (3/3).

- [ ] **Step 5: Verify types + commit** - `cd frontend && npx svelte-check` (0 errors). Then:

```bash
git add frontend/src/lib/searchFilter.ts frontend/src/lib/searchFilter.test.ts
git commit -m "feat(search): searchFilter helper - project chips, visible-index filter, selection clamp"
```

---

## Task 3: SearchOverlay - mode toggle + file rendering + chip row

**Files:**
- Modify: `frontend/src/lib/SearchOverlay.svelte`, `frontend/src/app.css`

**Interfaces:**
- Consumes: `SearchFiles` (Task 1), `searchFilter` helpers (Task 2), existing `SearchAll`/`OpenEditorAt`.

Read the CURRENT `frontend/src/lib/SearchOverlay.svelte` fully first - preserve all existing behavior (debounce, `reqGen` guard, blank-query clear, keyboard nav, open, Esc). Make these additions:

- [ ] **Step 1: Unify the hit type + add mode.** Import `SearchFiles` alongside `SearchAll`, and `{ deriveChips, visibleIndices, clampSel }` from `./searchFilter`. Change the hit type to carry optional content fields: `type Hit = { repo: string; repoPath: string; file: string; line?: number; text?: string }`. Add `let mode: "content" | "files" = "content";` and `let hidden = new Set<string>();`.

- [ ] **Step 2: Route the search by mode.** In `runSearch(q)`, call `mode === "files" ? await SearchFiles(term) : await SearchAll(term)` and assign into `hits` (the file results have no `line`/`text`). Keep the `reqGen` generation guard unchanged. On a new results assignment, reset `hidden = new Set()` (chips default all-on per query). When `mode` changes, re-run the current query: add a `function setMode(m)` that sets `mode` and, if there is a non-blank `query`, calls `runSearch(query)` immediately (bypassing debounce).

- [ ] **Step 3: Derive chips + visible list.** Add reactive: `$: chips = deriveChips(hits);` `$: visible = visibleIndices(hits, hidden);` and rebuild `groups` from the VISIBLE hits only (map each visible flat index through `hits`, grouped by repo, each item keeping its original flat `idx` for `selIndex` alignment). Keyboard nav (ArrowUp/Down/Enter) must walk `visible` (the ordered visible flat indices), not the raw `hits` - e.g. move `selIndex` to the previous/next entry in `visible`. After any `hidden` change, `selIndex = clampSel(selIndex, visible)`.

- [ ] **Step 4: Mode toggle + chip row markup.** In the header (near `.cmd-search`), add a segmented toggle: two buttons `Content` / `Files` (`class:active` on the current mode, `on:click={() => setMode(...)}`). Add `Tab` handling in `keydown` (when not blank): `e.key === "Tab"` -> `e.preventDefault(); setMode(mode === "content" ? "files" : "content");`. Below `.search-count`, render the chip row when `chips.length > 1`: for each `chip`, a toggle button showing `{chip.repo}` + `{chip.count}`, `class:off={hidden.has(chip.repo)}`, `on:click` toggling membership in `hidden` (reassign the Set for reactivity: `hidden = new Set(hidden); hidden.has(repo) ? hidden.delete(repo) : hidden.add(repo)`). Update the count line to "N of M results" when `hidden.size > 0`. In each result item, render `it.hit.file` + (content mode) `:{it.hit.line}` and the `it.hit.text`; in files mode show just the path. All-projects-hidden -> an "all projects hidden" empty state (distinct from "No results"). Update the input `placeholder` per mode.

- [ ] **Step 5: Styles.** In `frontend/src/app.css`, add `.search-mode` segmented-toggle styles (two segments, active state via accent) and `.search-chips`/`.search-chip`/`.search-chip.off` (small pills, count in a muted span, off = dimmed/strikethrough-free muted). Reduced-motion: any chip transition uses `var(--t)` (already reduced-motion-safe as CSS) or none; add no JS motion.

- [ ] **Step 6: Verify** - `cd frontend && npx vitest run && npx svelte-check` (green, 0 errors), then `cd .. && wails build` succeeds. Manual-smoke note in the report: content search unchanged; Files mode lists files; Tab toggles; chips hide/show a project and keyboard nav skips hidden hits.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/lib/SearchOverlay.svelte frontend/src/app.css
git commit -m "feat(search): Content/Files mode toggle + per-project filter chips in the search overlay"
```

---

## Self-Review Notes (author)

- **Spec coverage:** W1 -> Tasks 1+3; W2 -> Tasks 2+3. Every File-Structure entry appears in a task.
- **Type consistency:** `FileHit{repo,repoPath,file}` (Task 1) matches the frontend `Hit` (line/text optional) it maps into (Task 3). `deriveChips`/`visibleIndices`/`clampSel` signatures (Task 2) are consumed identically in Task 3.
- **No-regression anchors:** Task 3 explicitly preserves `reqGen`, debounce, blank-query clear, Esc/open; keyboard nav is redefined over `visible` so it never lands on a hidden hit (the spec's binding constraint).
