# Tier 4f - Conflict Backup Restore Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** a record sync overwrote or deleted can be restored from its backup in one click, and the restore sticks across devices.

**Architecture:** one `RestoreBackup` binding reads the matching line from `sync-conflicts.jsonl`, writes its payload back through `store.Update` (which re-stamps `UpdatedAt` so the restore wins LWW and re-pushes), and nudges a sync. The Settings backup list gains a per-row Restore button behind a confirm.

**Tech Stack:** Go 1.22, Svelte 5, wails v2.12.0.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-25-tier4f-backup-restore-design.md`.
- The re-stamp is the correctness anchor: restore MUST write through `store.Update` (which sets `UpdatedAt = now`), never `store.Put` (which keeps the payload's old timestamp and would be re-clobbered on the next sync).
- Do NOT run `gofmt -w` across the tree (CRLF working copy). Format only touched files; check with `git show HEAD:<file> | gofmt -d` (expect zero bytes).
- Regenerate wails bindings with `wails generate module` after adding the binding.
- `confirm(...)` is the established confirm pattern (`Graph.svelte:489`).
- Conventional Commits, no trailers. Keep the branch green on `desktop.yml`.

## File Structure

| File | Responsibility |
| --- | --- |
| `app.go` (modify) | `RestoreBackup(localID, when string) string`. |
| `app_test.go` (modify) | Re-stamp, re-create-deleted, no-match, and re-push tests. |
| `frontend/src/lib/SettingsModal.svelte` (modify) | Per-row Restore button + confirm + rescan. |
| `frontend/src/lib/SettingsModal` test (see Task 2) | Restore button renders and calls the binding. |
| `CHANGELOG.md` (modify) | Note restore. |

---

### Task 1: `RestoreBackup` binding

**Files:** Modify `app.go`, `app_test.go`

**Interfaces:**
- Consumes: `a.store.Update`, `a.triggerSync`, the `sync-conflicts.jsonl` line shape `{at, localId, name, payload}`.
- Produces: `func (a *App) RestoreBackup(localID, when string) string` - `""` on success, an error string otherwise.

- [ ] **Step 1: Write the failing tests** - add to `app_test.go`

```go
func writeBackupLine(t *testing.T, dir, localID, when, name string, rec store.Record) {
	t.Helper()
	payload, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(struct {
		At      string          `json:"at"`
		LocalID string          `json:"localId"`
		Name    string          `json:"name"`
		Payload json.RawMessage `json:"payload"`
	}{At: when, LocalID: localID, Name: name, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "sync-conflicts.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreBackupReStampsSoItWinsLWW(t *testing.T) {
	a := newTestApp(t)
	when := "2026-07-01T00:00:00Z"
	writeBackupLine(t, a.dataDir, "m-1", when, "my project",
		store.Record{Manual: true, Name: "my project", Notes: "the lost note", UpdatedAt: "2026-07-01T00:00:00Z"})

	if msg := a.RestoreBackup("m-1", when); msg != "" {
		t.Fatalf("RestoreBackup: %s", msg)
	}
	rec, ok := a.store.Get("m-1")
	if !ok || rec.Notes != "the lost note" {
		t.Fatalf("restored record missing or wrong: %+v ok=%v", rec, ok)
	}
	// The re-stamp: UpdatedAt must be newer than the backup's, or the next sync
	// re-clobbers it. Comparing as strings is safe for RFC3339Nano.
	if rec.UpdatedAt <= "2026-07-01T00:00:00Z" {
		t.Errorf("UpdatedAt was not re-stamped to now: %q", rec.UpdatedAt)
	}
}

func TestRestoreBackupRecreatesADeletedRecord(t *testing.T) {
	a := newTestApp(t)
	when := "2026-07-02T00:00:00Z"
	writeBackupLine(t, a.dataDir, "m-gone", when, "deleted one",
		store.Record{Manual: true, Name: "deleted one", UpdatedAt: when})
	// m-gone is absent from the store (it was deleted).
	if _, ok := a.store.Get("m-gone"); ok {
		t.Fatal("precondition: m-gone should not be in the store")
	}
	if msg := a.RestoreBackup("m-gone", when); msg != "" {
		t.Fatalf("RestoreBackup: %s", msg)
	}
	if rec, ok := a.store.Get("m-gone"); !ok || rec.Name != "deleted one" {
		t.Errorf("a deleted record should be re-created by restore: %+v ok=%v", rec, ok)
	}
}

func TestRestoreBackupNoMatchErrorsAndWritesNothing(t *testing.T) {
	a := newTestApp(t)
	writeBackupLine(t, a.dataDir, "m-1", "2026-07-01T00:00:00Z", "x", store.Record{Manual: true, Name: "x"})
	if msg := a.RestoreBackup("m-1", "1999-01-01T00:00:00Z"); msg == "" {
		t.Error("a when that matches no line must return an error")
	}
	if _, ok := a.store.Get("m-1"); ok {
		t.Error("a non-matching restore must not write anything")
	}
}

func TestRestoreBackupPicksTheRightLineAmongMany(t *testing.T) {
	a := newTestApp(t)
	writeBackupLine(t, a.dataDir, "m-1", "2026-07-01T00:00:00Z", "v1",
		store.Record{Manual: true, Name: "m-1", Notes: "first", UpdatedAt: "2026-07-01T00:00:00Z"})
	writeBackupLine(t, a.dataDir, "m-1", "2026-07-03T00:00:00Z", "v2",
		store.Record{Manual: true, Name: "m-1", Notes: "second", UpdatedAt: "2026-07-03T00:00:00Z"})
	if msg := a.RestoreBackup("m-1", "2026-07-01T00:00:00Z"); msg != "" {
		t.Fatalf("RestoreBackup: %s", msg)
	}
	if rec, _ := a.store.Get("m-1"); rec.Notes != "first" {
		t.Errorf("restored the wrong line: got Notes=%q, want first", rec.Notes)
	}
}
```

`json`, `os`, `filepath`, `store` are already imported in `app_test.go`.

- [ ] **Step 2: Run them** - `go test . -run TestRestoreBackup` → fails to build (`a.RestoreBackup undefined`).

- [ ] **Step 3: Implement** - add to `app.go`, next to `ConflictBackups`:

```go
// RestoreBackup writes a backed-up record (identified by its localId + backup
// timestamp) back into the store, re-stamping UpdatedAt so it is newer than the
// copy that clobbered it: the next sync then re-pushes it and last-write-wins
// makes it authoritative on every device. Restoring a deleted record re-creates
// it. The append-only backup log is left unchanged.
func (a *App) RestoreBackup(localID, when string) string {
	data, err := os.ReadFile(filepath.Join(a.dataDir, "sync-conflicts.jsonl"))
	if err != nil {
		return "error: " + err.Error()
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e struct {
			At      string          `json:"at"`
			LocalID string          `json:"localId"`
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal([]byte(line), &e) != nil {
			continue // a truncated last line is expected after a crash
		}
		if e.LocalID != localID || e.At != when {
			continue
		}
		var rec store.Record
		if err := json.Unmarshal(e.Payload, &rec); err != nil {
			return "error: backup is unreadable: " + err.Error()
		}
		// Update (not Put) re-stamps UpdatedAt to now, which is the whole point.
		if err := a.store.Update(localID, func(r *store.Record) { *r = rec }); err != nil {
			return errMsg(err)
		}
		a.triggerSync()
		return ""
	}
	return "error: no backup found for that record"
}
```

- [ ] **Step 4: Green** - `go test . -run TestRestoreBackup -v`. All pass.

- [ ] **Step 5: Add the re-push test** - this one uses a real engine + fake server, following `TestDiscardCorruptStoreDoesNotTombstoneTheCloud`'s setup (`app_test.go`):

```go
func TestRestoredRecordIsRePushed(t *testing.T) {
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
	eng := syncengine.New(cloud.New(srv.URL), filepath.Join(dir, "sync.json"),
		syncengine.NewProject(st, func(string) string { return "" }, nil))
	a := &App{store: st, dataDir: dir, engine: eng, syncTrigger: make(chan struct{}, 1)}

	when := "2026-07-01T00:00:00Z"
	writeBackupLine(t, dir, "m-1", when, "p",
		store.Record{Manual: true, Name: "p", Notes: "recovered", UpdatedAt: when})
	if msg := a.RestoreBackup("m-1", when); msg != "" {
		t.Fatalf("RestoreBackup: %s", msg)
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
		t.Error("a restored record must be re-pushed on the next sync")
	}
}
```

`httptest`, `http`, `cloud`, `syncengine` are already imported in `app_test.go`.

- [ ] **Step 6: Green** - `go test . -run "TestRestore|TestRestored" -v`.

- [ ] **Step 7: Regenerate bindings + gofmt + commit**

```bash
wails generate module
gofmt -w app.go app_test.go
git add app.go app_test.go frontend/wailsjs
git commit -m "feat(app): restore a conflict backup, re-stamped so it re-pushes and wins"
```

Confirm `frontend/wailsjs/go/main/App.d.ts` now declares `RestoreBackup`.

---

### Task 2: Settings Restore button

**Files:** Modify `frontend/src/lib/SettingsModal.svelte`; add a test (see Step 3)

**Interfaces:**
- Consumes: `RestoreBackup(localId, when)`, `ConflictBackups`, `onSaved`.

- [ ] **Step 1: Wire the binding + handler** - in `SettingsModal.svelte`, add `RestoreBackup` to the `../../wailsjs/go/main/App` import, and a handler:

```ts
  let restoring = "";
  async function restore(b: main.ConflictView) {
    if (restoring) return;
    if (!confirm(`Restore "${b.name}"? This replaces the current version on all your devices with this saved copy.`)) return;
    restoring = b.localId + b.when;
    try {
      const err = await RestoreBackup(b.localId, b.when);
      if (err) { toastError("Restore: " + err); return; }
      toastSuccess(`Restored ${b.name}`);
      onSaved(); // parent rescans so the restored record reappears
    } finally {
      restoring = "";
    }
  }
```

- [ ] **Step 2: Add the button to the row** - in the `{#each backups.slice(0, 8) as b, i (...)}` list item (`SettingsModal.svelte:317-321`), add after the when span:

```svelte
                <button class="btn btn-secondary btn-sm set-backup-restore"
                  on:click={() => restore(b)} disabled={restoring === b.localId + b.when}>
                  {restoring === b.localId + b.when ? "Restoring..." : "Restore"}
                </button>
```

and a style so the button sits at the row's end:

```css
  .set-backups li { display: flex; align-items: center; gap: 8px; }
  .set-backup-restore { margin-left: auto; }
```

(If `.set-backups li` already has display rules, extend rather than duplicate them - check the existing block first.)

- [ ] **Step 3: Frontend test** - the SettingsModal is SSR-rendered in tests like the others. Add `frontend/src/lib/SettingsModal.test.ts` (or extend an existing one) that renders the modal on the integrations... no: the backup list is on the General tab, which is the default. Mock the bindings, seed `ConflictBackups` to return one row, and assert a "Restore" button is present. Because the list is populated by an async `ConflictBackups()` that SSR does not await, assert instead at the unit level: export nothing new, and rely on the Go test for behavior. **Concretely**, add a minimal test that the component renders without throwing when `ConflictBackups` returns a row, and that the string "Restore" appears once `backups` is set - done by rendering with a mocked binding that resolves synchronously is not possible under SSR, so this test asserts only that the mock is wired and the component compiles. Keep it small:

```ts
// SettingsModal loads backups async (SSR does not await), so the button's
// presence is covered by the Go RestoreBackup tests + manual check; this test
// guards that the new binding import and handler compile and the mock is used.
```

If a lightweight assertion is achievable (the project's other SSR tests show the pattern), prefer it; otherwise this task's gate is `npm run check` passing with the new code.

- [ ] **Step 4: Green** - `npm run check --prefix frontend && npm test --prefix frontend`. Both green.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/SettingsModal.svelte frontend/src/lib/SettingsModal.test.ts
git commit -m "feat(ui): restore an overwritten copy from Settings, behind a confirm"
```

---

### Task 3: Verify and ship

- [ ] **Step 1** - `cd frontend && npm ci && npm run build && cd .. && go build ./... && go vet ./... && go test ./...` - clean, no FAIL.
- [ ] **Step 2** - gofmt diff on `app.go` and `app_test.go` LF blobs: zero bytes.
- [ ] **Step 3** - CHANGELOG `[Unreleased]`, under Added:
  `- Restore a record that sync overwrote or deleted, from its backup in Settings - the restore re-pushes and wins on every device.`
- [ ] **Step 4** - `wails build`, launch, confirm by hand: overwrite a project via a second device (or simulate a backup line), click Restore in Settings, confirm the record returns and the confirm dialog appears.
- [ ] **Step 5** - push, confirm the three `desktop` checks are green, open a PR, merge once green.

---

## Self-Review

- **Spec coverage:** §1 binding → Task 1; the re-push re-stamp → Task 1 Steps 1/5; §2 UI + confirm → Task 2. All covered.
- **Type consistency:** `RestoreBackup(localID, when string) string` used identically in the binding, the Go tests, and the Svelte handler. `ConflictView.localId`/`.when` (existing) are the pair passed.
- **Placeholder scan:** Task 2 Step 3 is honest that the SSR harness cannot exercise the async-loaded list, and sets the gate as `npm run check` + the Go tests rather than a fake assertion. No TBDs.
