# Tier 4g - Export Import Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** an exported fleet data file can be imported back, upserting its projects and intel into the local stores, and the import sticks across devices.

**Architecture:** two bindings mirror `ExportData` in reverse. `ImportPreview` opens a native file dialog, parses `{projects, intel}`, and returns counts without writing. `ImportCommit` re-reads the path and upserts every record through `store.Update`/`intel.SetBrief`/`intel.SetChat` (all re-stamp `UpdatedAt`, so the import re-pushes and wins LWW), never deleting. The parse and write are factored into unexported helpers so tests can drive them without the dialog.

**Tech Stack:** Go 1.22, Svelte 5, wails v2.12.0.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-27-tier4g-export-import-design.md`.
- Re-stamp is the correctness anchor (same as tier 4f): import writes through `store.Update` / `intel.SetBrief` / `intel.SetChat`, never `store.Put`/`SetBriefSynced`/`SetChatSynced` (the "Synced" variants keep the source timestamp and are for pulled docs only).
- Import is **upsert, never delete**: local ids absent from the file are untouched.
- The file dialog cannot run headless: factor parse+count into `importSummary(path)` and the write into `importCommit(path)`, and test those directly.
- Do NOT run `gofmt -w` across the tree (CRLF working copy). Format only touched files; check with `git show HEAD:<file> | gofmt -d` (expect zero bytes).
- Regenerate wails bindings with `wails generate module` after adding the bindings.
- `wruntime` is imported in `app.go` as `wruntime "github.com/wailsapp/wails/v2/pkg/runtime"`; `wruntime.OpenFileDialog(ctx, wruntime.OpenDialogOptions{...})` is the open-dialog call (v2.12.0).
- `confirm(...)` is the established confirm pattern.
- Conventional Commits, no trailers. Keep the branch green on `desktop.yml`.

## File Structure

| File | Responsibility |
| --- | --- |
| `app.go` (modify) | `ImportSummary`, `ImportPreview`, `ImportCommit`, and helpers `importSummary`/`importCommit`. |
| `app_test.go` (modify) | Upsert/no-delete, re-stamp, degraded, malformed, counts, re-push tests. |
| `frontend/src/lib/SettingsModal.svelte` (modify) | Import button + preview→confirm→commit handler. |
| `CHANGELOG.md` (modify) | Note import. |

---

### Task 1: `importSummary` + `importCommit` helpers and the parse type

**Files:** Modify `app.go`, `app_test.go`

**Interfaces:**
- Produces: `type ImportSummary struct{...}`; `func (a *App) importSummary(path string) ImportSummary`; `func (a *App) importCommit(path string) error`.
- Consumes: `a.store` (Update/Snapshot/Degraded), `a.intel` (SetBrief/SetChat/SnapshotChats/Degraded), `a.triggerSync`.

- [ ] **Step 1: Write the failing tests** — add to `app_test.go`

```go
func writeExportFile(t *testing.T, path string, projects map[string]store.Record, in intel.Data) {
	t.Helper()
	body := struct {
		Projects map[string]store.Record `json:"projects"`
		Intel    intel.Data              `json:"intel"`
	}{projects, in}
	data, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestImportCommitUpsertsWithoutDeleting(t *testing.T) {
	a := newTestApp(t)
	// A local-only project the import must NOT touch, and an existing id the
	// import will overwrite.
	_ = a.store.Update("m-local", func(r *store.Record) { r.Manual = true; r.Name = "local only" })
	_ = a.store.Update("m-1", func(r *store.Record) { r.Manual = true; r.Name = "old" })

	path := filepath.Join(t.TempDir(), "export.json")
	writeExportFile(t, path, map[string]store.Record{
		"m-1": {Manual: true, Name: "new", Notes: "imported", UpdatedAt: "2020-01-01T00:00:00Z"},
		"m-2": {Manual: true, Name: "added", UpdatedAt: "2020-01-01T00:00:00Z"},
	}, intel.Data{})

	if err := a.importCommit(path); err != nil {
		t.Fatalf("importCommit: %v", err)
	}
	// Overwritten, and re-stamped newer than the file's ancient timestamp.
	if rec, _ := a.store.Get("m-1"); rec.Name != "new" || rec.Notes != "imported" {
		t.Errorf("m-1 not overwritten: %+v", rec)
	}
	if rec, _ := a.store.Get("m-1"); rec.UpdatedAt <= "2020-01-01T00:00:00Z" {
		t.Errorf("m-1 not re-stamped: %q", rec.UpdatedAt)
	}
	// Added.
	if _, ok := a.store.Get("m-2"); !ok {
		t.Error("m-2 was not added")
	}
	// Local-only survives (no delete).
	if rec, ok := a.store.Get("m-local"); !ok || rec.Name != "local only" {
		t.Error("import must not delete a local-only record")
	}
}

func TestImportCommitBringsChatsAndBrief(t *testing.T) {
	a := newTestApp(t)
	path := filepath.Join(t.TempDir(), "export.json")
	writeExportFile(t, path, map[string]store.Record{}, intel.Data{
		Brief: intel.Brief{Text: "imported brief"},
		Chats: map[string]intel.Chat{"git:x": {Turns: []intel.Turn{{Role: "user", Text: "hi"}}}},
	})
	if err := a.importCommit(path); err != nil {
		t.Fatalf("importCommit: %v", err)
	}
	if b := a.intel.Brief(); b.Text != "imported brief" {
		t.Errorf("brief not imported: %+v", b)
	}
	if turns := a.intel.Chat("git:x"); len(turns) != 1 || turns[0].Text != "hi" {
		t.Errorf("chat not imported: %+v", turns)
	}
}

func TestImportCommitEmptyBriefDoesNotWipeLocal(t *testing.T) {
	a := newTestApp(t)
	_ = a.intel.SetBrief(intel.Brief{Text: "my local brief"})
	path := filepath.Join(t.TempDir(), "export.json")
	writeExportFile(t, path, map[string]store.Record{}, intel.Data{}) // no brief
	if err := a.importCommit(path); err != nil {
		t.Fatalf("importCommit: %v", err)
	}
	if b := a.intel.Brief(); b.Text != "my local brief" {
		t.Errorf("an empty imported brief must not wipe the local one: %+v", b)
	}
}

func TestImportCommitRefusesWhenStoreDegraded(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "projects.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, _ := store.Open(p)
	is, _ := intel.Open(filepath.Join(dir, "intel.json"))
	a := &App{store: st, intel: is, dataDir: dir, syncTrigger: make(chan struct{}, 1)}

	path := filepath.Join(dir, "export.json")
	writeExportFile(t, path, map[string]store.Record{"m-1": {Manual: true, Name: "x"}}, intel.Data{})
	if err := a.importCommit(path); err == nil {
		t.Error("importCommit must refuse when the store is degraded")
	}
}

func TestImportSummaryCountsAndMalformed(t *testing.T) {
	a := newTestApp(t)
	_ = a.store.Update("m-1", func(r *store.Record) { r.Manual = true; r.Name = "existing" })

	path := filepath.Join(t.TempDir(), "export.json")
	writeExportFile(t, path, map[string]store.Record{
		"m-1": {Manual: true, Name: "a"}, // overwrites existing
		"m-2": {Manual: true, Name: "b"}, // new
	}, intel.Data{
		Brief: intel.Brief{Text: "b"},
		Chats: map[string]intel.Chat{"git:x": {Turns: []intel.Turn{{Role: "user", Text: "q"}}}},
	})
	s := a.importSummary(path)
	if s.Error != "" {
		t.Fatalf("unexpected error: %s", s.Error)
	}
	if s.Projects != 2 || s.ProjectsOverwrite != 1 {
		t.Errorf("project counts wrong: %+v", s)
	}
	if s.Chats != 1 || !s.Brief {
		t.Errorf("intel counts wrong: %+v", s)
	}

	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s := a.importSummary(bad); s.Error == "" {
		t.Error("a malformed file must report an error")
	}
}
```

`json`, `os`, `filepath`, `store`, `intel` are already imported in `app_test.go`.

- [ ] **Step 2: Run them** — `go test . -run "TestImport"` → fails to build (`importCommit`/`importSummary`/`ImportSummary` undefined).

- [ ] **Step 3: Implement** — add to `app.go`, near `ExportData`/`writeExport`:

```go
// ImportSummary is the preview of an import file: what it holds and how much of
// it would replace existing local records.
type ImportSummary struct {
	Path              string `json:"path"` // "" when the user cancelled
	Projects          int    `json:"projects"`
	ProjectsOverwrite int    `json:"projectsOverwrite"`
	Chats             int    `json:"chats"`
	ChatsOverwrite    int    `json:"chatsOverwrite"`
	Brief             bool   `json:"brief"`
	Error             string `json:"error"`
}

// importFile is the on-disk shape writeExport produces.
type importFile struct {
	Projects map[string]store.Record `json:"projects"`
	Intel    intel.Data              `json:"intel"`
}

func parseImport(path string) (importFile, error) {
	var f importFile
	data, err := os.ReadFile(path)
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return f, fmt.Errorf("not a valid fleet export: %w", err)
	}
	return f, nil
}

// importSummary parses path and counts what an import would do, without writing.
func (a *App) importSummary(path string) ImportSummary {
	f, err := parseImport(path)
	if err != nil {
		return ImportSummary{Path: path, Error: err.Error()}
	}
	s := ImportSummary{Path: path, Projects: len(f.Projects), Chats: len(f.Intel.Chats)}
	s.Brief = f.Intel.Brief.Text != "" || f.Intel.Brief.UpdatedAt != ""
	local := a.store.Snapshot()
	for id := range f.Projects {
		if _, ok := local[id]; ok {
			s.ProjectsOverwrite++
		}
	}
	localChats := a.intel.SnapshotChats()
	for id := range f.Intel.Chats {
		if _, ok := localChats[id]; ok {
			s.ChatsOverwrite++
		}
	}
	return s
}

// importCommit upserts an import file into the stores, re-stamping every record
// so it re-pushes and wins LWW. It never deletes: local ids absent from the file
// are untouched. It refuses up front if a store is degraded, so a half-import
// cannot happen.
func (a *App) importCommit(path string) error {
	if err := a.store.Degraded(); err != nil {
		return fmt.Errorf("cannot import into unreadable project data: %w", err)
	}
	if err := a.intel.Degraded(); err != nil {
		return fmt.Errorf("cannot import into unreadable intel data: %w", err)
	}
	f, err := parseImport(path)
	if err != nil {
		return err
	}
	for id, rec := range f.Projects {
		rec := rec
		if err := a.store.Update(id, func(r *store.Record) { *r = rec }); err != nil {
			return err
		}
	}
	if f.Intel.Brief.Text != "" || f.Intel.Brief.UpdatedAt != "" {
		if err := a.intel.SetBrief(f.Intel.Brief); err != nil {
			return err
		}
	}
	for id, ch := range f.Intel.Chats {
		if err := a.intel.SetChat(id, ch.Turns); err != nil {
			return err
		}
	}
	a.triggerSync()
	return nil
}
```

`fmt` is already imported in `app.go`.

- [ ] **Step 4: Green** — `go test . -run TestImport -v`. All pass.

- [ ] **Step 5: Add the re-push test** — following tier 4f's pattern (`app_test.go`):

```go
func TestImportedRecordIsRePushed(t *testing.T) {
	var pushed []cloud.Doc
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body struct {
				Docs []cloud.Doc `json:"docs"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			pushed = append(pushed, body.Docs...)
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}, "cursor": 0})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"docs": []any{}, "cursor": 0})
	}))
	defer srv.Close()

	dir := t.TempDir()
	st, _ := store.Open(filepath.Join(dir, "projects.json"))
	is, _ := intel.Open(filepath.Join(dir, "intel.json"))
	eng := syncengine.New(cloud.New(srv.URL), filepath.Join(dir, "sync.json"),
		syncengine.NewProject(st, func(string) string { return "" }, nil))
	a := &App{store: st, intel: is, dataDir: dir, engine: eng, syncTrigger: make(chan struct{}, 1)}

	path := filepath.Join(dir, "export.json")
	writeExportFile(t, path, map[string]store.Record{
		"m-1": {Manual: true, Name: "p", UpdatedAt: "2020-01-01T00:00:00Z"},
	}, intel.Data{})
	if err := a.importCommit(path); err != nil {
		t.Fatalf("importCommit: %v", err)
	}
	if err := a.engine.SyncOnce("tok"); err != nil {
		t.Fatalf("sync: %v", err)
	}
	found := false
	for _, d := range pushed {
		if d.DocID == "m-1" && !d.Deleted {
			found = true
		}
	}
	if !found {
		t.Error("an imported record must be re-pushed on the next sync")
	}
}
```

- [ ] **Step 6: Green** — `go test . -run "TestImport|TestImported" -v`.

- [ ] **Step 7: gofmt + commit**

```bash
gofmt -w app.go app_test.go
git add app.go app_test.go
git commit -m "feat(app): import an export - upsert projects and intel, re-stamped"
```

---

### Task 2: `ImportPreview` / `ImportCommit` bindings

**Files:** Modify `app.go`, `app_test.go`

**Interfaces:**
- Produces: `func (a *App) ImportPreview() ImportSummary`; `func (a *App) ImportCommit(path string) string`.

- [ ] **Step 1: Implement the bindings** — in `app.go`, after `importCommit`:

```go
// ImportPreview opens a file dialog and returns what importing the chosen file
// would do, without writing anything. A cancelled dialog returns {Path: ""}.
func (a *App) ImportPreview() ImportSummary {
	src, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title:   "Import fleet data",
		Filters: []wruntime.FileFilter{{DisplayName: "JSON", Pattern: "*.json"}},
	})
	if err != nil {
		return ImportSummary{Error: err.Error()}
	}
	if strings.TrimSpace(src) == "" {
		return ImportSummary{} // cancelled
	}
	return a.importSummary(src)
}

// ImportCommit imports the file at path (from a prior ImportPreview).
func (a *App) ImportCommit(path string) string { return errMsg(a.importCommit(path)) }
```

- [ ] **Step 2: A binding smoke test** — `ImportCommit` is a thin wrapper; assert it maps success/error to the string convention (`app_test.go`):

```go
func TestImportCommitBindingReturnsErrMsg(t *testing.T) {
	a := newTestApp(t)
	// A missing file makes importCommit error; the binding must surface it.
	if msg := a.ImportCommit(filepath.Join(t.TempDir(), "nope.json")); msg == "" {
		t.Error("ImportCommit on a missing file must return an error string")
	}
	path := filepath.Join(t.TempDir(), "ok.json")
	writeExportFile(t, path, map[string]store.Record{"m-1": {Manual: true, Name: "x"}}, intel.Data{})
	if msg := a.ImportCommit(path); msg != "" {
		t.Errorf("a valid import must return \"\", got %q", msg)
	}
}
```

- [ ] **Step 3: Green** — `go test . -run TestImportCommitBinding -v && go build ./... && go vet ./...`

- [ ] **Step 4: Regenerate bindings**

```bash
wails generate module
```

Confirm `App.d.ts` declares `ImportPreview(): Promise<main.ImportSummary>` and `ImportCommit(arg1:string): Promise<string>`, and `models.ts` has `ImportSummary`.

- [ ] **Step 5: gofmt + commit**

```bash
gofmt -w app.go app_test.go
git add app.go app_test.go frontend/wailsjs
git commit -m "feat(app): ImportPreview/ImportCommit bindings with a native file dialog"
```

---

### Task 3: Settings Import button

**Files:** Modify `frontend/src/lib/SettingsModal.svelte`

**Interfaces:**
- Consumes: `ImportPreview`, `ImportCommit`, `onSaved`.

- [ ] **Step 1: Wire the handler** — add `ImportPreview, ImportCommit` to the `../../wailsjs/go/main/App` import, and:

```ts
  let importing = false;
  async function doImport() {
    if (importing) return;
    importing = true;
    try {
      const s = await ImportPreview();
      if (!s.path) return; // cancelled
      if (s.error) { toastError("Import: " + s.error); return; }
      const parts = [`${s.projects} projects (${s.projectsOverwrite} replace existing)`, `${s.chats} chats`];
      if (s.brief) parts.push("the brief");
      if (!confirm(`Import ${parts.join(", ")} from this file? Replaced records win on all your devices.`)) return;
      const err = await ImportCommit(s.path);
      if (err) { toastError("Import: " + err); return; }
      toastSuccess("Data imported");
      onSaved(); // rescan so the imported records appear
    } finally {
      importing = false;
    }
  }
```

- [ ] **Step 2: Add the button** — beside Export (`SettingsModal.svelte`, the `.set-data-row` holding the Export button):

```svelte
            <button class="btn btn-secondary btn-sm" on:click={doImport} disabled={importing}>
              {importing ? "Importing..." : "Import data (JSON)"}
            </button>
```

- [ ] **Step 3: Green** — `npm run check --prefix frontend && npm test --prefix frontend`. Both green.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/SettingsModal.svelte
git commit -m "feat(ui): import an export from Settings, with a counts confirm"
```

---

### Task 4: Verify and ship

- [ ] **Step 1** — `cd frontend && npm ci && npm run build && cd .. && go build ./... && go vet ./... && go test ./...` — clean, no FAIL.
- [ ] **Step 2** — gofmt diff on `app.go` and `app_test.go` LF blobs: zero bytes.
- [ ] **Step 3** — CHANGELOG `[Unreleased]`, extend the export/backup area under Added:
  `- Import an exported data file - projects and intel are upserted (never deleting local-only records) and re-pushed to win on every device.`
- [ ] **Step 4** — `wails build`, launch, confirm by hand: Export to a file, change a project, Import that file, confirm the counts dialog and that the project reverts; a local-only project added after the export survives the import.
- [ ] **Step 5** — push, confirm the three `desktop` checks are green, open a PR, merge once green.

---

## Self-Review

- **Spec coverage:** §1 two-step flow (ImportSummary + ImportPreview/ImportCommit) → Tasks 1-2; §2 commit semantics (upsert, re-stamp, degraded guard, no-delete, brief-guard) → Task 1; §3 UI → Task 3. All covered.
- **Type consistency:** `ImportSummary{Path,Projects,ProjectsOverwrite,Chats,ChatsOverwrite,Brief,Error}` identical across Go, the tests, and the Svelte handler; `importSummary`/`importCommit` helpers vs `ImportPreview`/`ImportCommit` bindings kept distinct and consistent; `intel.Data`/`intel.Chat.Turns` used as tier 4e defined them.
- **Placeholder scan:** the dialog test seam (`importSummary`/`importCommit` driven directly) is explicit; the frontend SSR limit is stated with the gate named, as in tier 4f. No TBDs.
