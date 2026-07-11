# Fleet Editor Picker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Replace the bare Editor text field in Settings with an auto-detecting dropdown (installed editors marked) + a Custom command option.

**Architecture:** A `DetectEditors()` binding probes known editor commands with `exec.LookPath`; a pure `editorSelection.ts` helper maps the current `cfg.Editor` to a dropdown selection (known command or "custom"); `SettingsModal.svelte` renders the picker.

**Tech Stack:** Go 1.22 (stdlib), Wails v2, Svelte-TS, vitest, Go testing.

## Global Constraints

From the spec `docs/superpowers/specs/2026-07-12-fleet-editor-picker-design.md`:
- **No new runtime dependencies** (Go `os/exec` stdlib; `frontend/package.json` unchanged).
- **No change to how the editor is launched** (`OpenEditorAt`/`EditorCmd` untouched); a custom command still works.
- **Detection best-effort, never blocks Settings**; current value preserved on failure.
- **Green gates:** `go build`/`go vet`/`go test ./...`, `npx svelte-check` 0 errors, `npx vitest run`, `wails build`.

## Commit authorship (all tasks)
`git -c user.name=hoijun -c user.email=hoijun.kim00@gmail.com commit -m "..."` - NO Co-Authored-By/Claude trailer.

---

## Task 1: Editor detection backend (`DetectEditors`)

**Files:**
- Modify: `app.go` (add `EditorOption`, `DetectEditors`, a testable `detectEditors(lookPath)` helper)
- Modify: `app_test.go` (test with a stubbed lookPath)

**Interfaces:**
- Produces: `EditorOption{Name, Command string; Installed bool}` (json `name`/`command`/`installed`) and `DetectEditors() []EditorOption`.

- [ ] **Step 1: Write the failing test** - in `app_test.go` add:

```go
func TestDetectEditors(t *testing.T) {
	installed := map[string]bool{"code": true, "nvim": true}
	look := func(cmd string) (string, error) {
		if installed[cmd] {
			return "/usr/bin/" + cmd, nil
		}
		return "", errors.New("not found")
	}
	got := detectEditors(look)
	// full known list returned; installed ones flagged + sorted first
	if len(got) < 5 {
		t.Fatalf("expected the full known list, got %d", len(got))
	}
	if !got[0].Installed || !got[1].Installed {
		t.Fatalf("installed editors should sort first: %+v", got[:2])
	}
	var code EditorOption
	for _, e := range got {
		if e.Command == "code" {
			code = e
		}
	}
	if code.Name != "VS Code" || !code.Installed {
		t.Fatalf("code: %+v", code)
	}
}
```

(Add `"errors"` to the test imports if not present.)

- [ ] **Step 2: Run it, verify it fails** - `go test ./ -run TestDetectEditors`. Expected: FAIL (undefined: detectEditors).

- [ ] **Step 3: Implement** - in `app.go` (near the other config/editor bindings):

```go
// EditorOption is a known editor and whether its command is on PATH.
type EditorOption struct {
	Name      string `json:"name"`
	Command   string `json:"command"`
	Installed bool   `json:"installed"`
}

// knownEditors is the curated name->command list shown in the picker (display
// order when equally installed).
var knownEditors = []EditorOption{
	{Name: "VS Code", Command: "code"},
	{Name: "Cursor", Command: "cursor"},
	{Name: "Windsurf", Command: "windsurf"},
	{Name: "Sublime Text", Command: "subl"},
	{Name: "Zed", Command: "zed"},
	{Name: "Neovim", Command: "nvim"},
	{Name: "Vim", Command: "vim"},
	{Name: "IntelliJ IDEA", Command: "idea"},
	{Name: "WebStorm", Command: "webstorm"},
	{Name: "Emacs", Command: "emacs"},
	{Name: "Notepad++", Command: "notepad++"},
}

// DetectEditors returns the known editors, marking the ones on PATH installed
// and sorting installed-first (stable within each group).
func (a *App) DetectEditors() []EditorOption { return detectEditors(exec.LookPath) }

func detectEditors(lookPath func(string) (string, error)) []EditorOption {
	var installed, missing []EditorOption
	for _, e := range knownEditors {
		e := e
		if _, err := lookPath(e.Command); err == nil {
			e.Installed = true
			installed = append(installed, e)
		} else {
			missing = append(missing, e)
		}
	}
	return append(installed, missing...)
}
```

(Ensure `os/exec` is imported in app.go - it may already be.)

- [ ] **Step 4: Run test, verify pass** - `go test ./ -run TestDetectEditors`. Expected: PASS.

- [ ] **Step 5: Verify + commit** - `go build ./... && go vet ./... && go test ./...` (green). Then:

```bash
git add app.go app_test.go
git commit -m "feat(settings): DetectEditors - probe known editors on PATH for the editor picker"
```

---

## Task 2: Editor picker UI (`editorSelection.ts` + SettingsModal)

**Files:**
- Create: `frontend/src/lib/editorSelection.ts`, `frontend/src/lib/editorSelection.test.ts`
- Modify: `frontend/src/lib/SettingsModal.svelte`
- Modify (generated): `frontend/wailsjs/**` (regenerate for `DetectEditors`/`EditorOption` via `wails generate module`)
- Modify: `frontend/src/app.css` if a picker style is needed

**Interfaces:**
- Consumes: `DetectEditors` (Task 1).
- Produces: `editorSelection(cfgEditor: string, options: {command:string}[]) -> { selected: string; customValue: string }` - `selected` is the matching known `command`, or `"custom"` when the value is empty or not a known command; `customValue` is the raw `cfgEditor` (for the custom text field).

- [ ] **Step 1: Write the failing test** - `frontend/src/lib/editorSelection.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { editorSelection } from "./editorSelection";

const opts = [{ command: "code" }, { command: "nvim" }];

describe("editorSelection", () => {
  it("selects a known command", () => {
    expect(editorSelection("code", opts)).toEqual({ selected: "code", customValue: "code" });
  });
  it("falls back to custom for an unknown command", () => {
    expect(editorSelection("mate -w", opts)).toEqual({ selected: "custom", customValue: "mate -w" });
  });
  it("empty editor -> custom, empty value", () => {
    expect(editorSelection("", opts)).toEqual({ selected: "custom", customValue: "" });
  });
});
```

- [ ] **Step 2: Run it, verify it fails** - `cd frontend && npx vitest run src/lib/editorSelection.test.ts`. Expected: FAIL (module not found).

- [ ] **Step 3: Implement** - `frontend/src/lib/editorSelection.ts`:

```ts
// editorSelection maps the saved editor command to a dropdown selection:
// the matching known command, or "custom" (with the raw value) otherwise.
export function editorSelection(
  cfgEditor: string,
  options: { command: string }[],
): { selected: string; customValue: string } {
  const editor = (cfgEditor ?? "").trim();
  const known = options.some((o) => o.command === editor);
  return { selected: editor && known ? editor : "custom", customValue: cfgEditor ?? "" };
}
```

- [ ] **Step 4: Run tests, verify pass** - `cd frontend && npx vitest run src/lib/editorSelection.test.ts`. Expected: PASS (3/3).

- [ ] **Step 5: Wire the picker into SettingsModal.** Read the CURRENT `frontend/src/lib/SettingsModal.svelte` (the Editor `<input>` at ~line 187, and how the modal loads config on open). Then:
  - Import `DetectEditors` from the bindings and `editorSelection` from `./editorSelection`. Add `let editors: {name:string;command:string;installed:boolean}[] = [];` and load them in the modal's on-open/onMount (`editors = await DetectEditors();`), non-blocking (a failure just leaves `editors = []`).
  - Add `$: sel = editorSelection(cfg.Editor, editors);` and a local `let editorChoice = sel.selected;` initialized from it; keep them in sync when the modal opens / `cfg.Editor` loads.
  - Replace the Editor `<input>` with: a `<select bind:value={editorChoice} on:change={...}>` whose options are each editor as `<option value={e.command}>{e.name}{e.installed ? " (installed)" : ""}</option>` plus a final `<option value="custom">Custom...</option>`. On change: if `editorChoice === "custom"` leave `cfg.Editor` as-is (show the text field); else `cfg.Editor = editorChoice`. When `editorChoice === "custom"`, render the existing `<input class="input" bind:value={cfg.Editor} placeholder="code" />` beneath the select so a raw command still works. If a known command is in `cfg.Editor` but not installed, still show it selected (the `<option>` exists in the known list regardless of `installed`).
  - Leave the Terminal field and everything else unchanged.
  - Keep the field label `Editor` and the `field`/`input` classes.

- [ ] **Step 6: Regenerate Wails bindings.** From `frontend/` (or repo root as the project uses), run `wails generate module` so `DetectEditors`/`EditorOption` appear in `frontend/wailsjs/go/main/App.{d.ts,js}` + `models.ts`. Commit those additive generated changes together with the UI. (If `wails generate module` is unavailable, hand-add the `DetectEditors` export mirroring an existing no-arg binding + the `EditorOption` model, matching the generated style.)

- [ ] **Step 7: Verify** - `cd frontend && npx vitest run && npx svelte-check` (green, 0 errors), then `cd .. && wails build` (succeeds). Manual-smoke note in the report: Settings shows the dropdown with installed editors marked; picking one sets the command; Custom reveals the text field; a pre-existing custom command shows as Custom.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/lib/editorSelection.ts frontend/src/lib/editorSelection.test.ts frontend/src/lib/SettingsModal.svelte frontend/src/app.css frontend/wailsjs
git commit -m "feat(settings): editor picker - auto-detected dropdown + Custom command"
```

---

## Self-Review Notes (author)

- **Spec coverage:** W1 -> Task 1; W2 -> Task 2. Every File-Structure entry appears in a task.
- **Type consistency:** `EditorOption{name,command,installed}` (Task 1) is consumed by `editorSelection`'s `{command}[]` and the SettingsModal `<option>` (Task 2). `editorSelection(cfgEditor, options) -> {selected, customValue}` is used identically in the UI.
- **Non-destructive:** the picker never overwrites `cfg.Editor` on open (only on an explicit pick); a blank or custom value maps to "Custom..." with the raw text preserved; a known-but-not-installed command stays selected.
