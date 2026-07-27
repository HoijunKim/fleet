# Tier 4d - Intel Store (relocate) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** move the AI brief and repo/fleet chats from `localStorage` into a Go-backed store keyed by a stable repo identity, included in the data export.

**Architecture:** a new `internal/intel` file-backed store mirrors `internal/store` (atomic write, quarantine-on-corrupt, writes refused while degraded). A `ChatID` function derives a stable identity from a repo path using the same convention as project doc-ids. Five bindings expose brief/chat get-save-clear; the three frontend components drop their duplicated `localStorage` helpers and call them, with a one-time migration copying the old keys over.

**Tech Stack:** Go 1.22, wails v2.12.0, Svelte 5, vitest.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-24-tier4d-intel-store-design.md`.
- The intel store MUST follow the tier-3d integrity contract exactly as `internal/store` does: a corrupt file opens read-only, the bytes are quarantined via `fileguard.Quarantine`, and every write is refused while `loadErr` is set. Do not invent a looser policy.
- Do NOT run `gofmt -w` across the tree (CRLF working copy). Format only touched files; check with `git show HEAD:<file> | gofmt -d` (expect zero bytes).
- Regenerate wails bindings with `wails generate module` after adding bindings; `frontend/wailsjs/` is committed.
- `chatCap = 20` (matches today's `turns.slice(-20)`).
- Conventional Commits, no trailers. Keep the branch green on `desktop.yml` (gofmt, vet, `go test -race`, svelte-check, vitest).
- Every new git-touching function takes a `git.Runner` first, matching the codebase.

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/intel/intel.go` (create) | The store: `Brief`, `Turn`, `Data`, `Store`, Open/Brief/SetBrief/Chat/SetChat/ClearChat/Snapshot/Degraded/Quarantined. |
| `internal/intel/intel_test.go` (create) | Round-trip, cap, corrupt-file quarantine, empty-deletes-key. |
| `internal/intel/identity.go` (create) | `ChatID(runner, path) string`. |
| `internal/intel/identity_test.go` (create) | git / local / __fleet__ cases. |
| `app.go` (modify) | `intel` field, wire in `NewApp`, five bindings, export wrapping. |
| `app_test.go` (modify) | Binding round-trip through a real repo; export shape. |
| `frontend/src/lib/intelMigrate.ts` (create) | One-time localStorage -> store migration. |
| `frontend/src/lib/intelMigrate.test.ts` (create) | Copies once, skips existing, no-op on rerun. |
| `frontend/src/App.svelte` (modify) | Call the migration in `onMount` before other loads. |
| `frontend/src/lib/agentSession.ts` (modify) | Drop `chatKey`/`loadChat`/`saveChat`; call bindings. |
| `frontend/src/lib/RepoChat.svelte` (modify) | Same. |
| `frontend/src/lib/Today.svelte` (modify) | Brief via `GetBrief`/`SaveBrief`. |

---

### Task 1: The intel store

**Files:**
- Create: `internal/intel/intel.go`, `internal/intel/intel_test.go`

**Interfaces:**
- Consumes: `internal/fileguard` (`Quarantine(path) (string, error)`).
- Produces: `intel.Store` with `Open(path) (*Store, error)`, `Brief() Brief`, `SetBrief(Brief) error`, `Chat(id string) []Turn`, `SetChat(id string, turns []Turn) error`, `ClearChat(id string) error`, `Snapshot() Data`, `Degraded() error`, `Quarantined() string`; types `Brief{Text,At,Lang string}`, `Turn{Role,Text string}`, `Data{Brief Brief; Chats map[string][]Turn}`.

- [ ] **Step 1: Write the failing test** - `internal/intel/intel_test.go`

```go
package intel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBriefRoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "intel.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetBrief(Brief{Text: "hello", At: "2026-07-24T00:00:00Z", Lang: "ko"}); err != nil {
		t.Fatal(err)
	}
	// Reopen to prove it persisted, not just cached.
	s2, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if b := s2.Brief(); b.Text != "hello" || b.Lang != "ko" {
		t.Errorf("Brief = %+v, want hello/ko", b)
	}
}

func TestChatRoundTripAndCap(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "intel.json"))
	turns := make([]Turn, 0, 25)
	for i := 0; i < 25; i++ {
		turns = append(turns, Turn{Role: "user", Text: string(rune('a' + i%26))})
	}
	if err := s.SetChat("git:x", turns); err != nil {
		t.Fatal(err)
	}
	got := s.Chat("git:x")
	if len(got) != chatCap {
		t.Fatalf("Chat len = %d, want %d (capped)", len(got), chatCap)
	}
	// The cap keeps the LAST 20, so the first kept turn is the 6th written.
	if got[0].Text != turns[len(turns)-chatCap].Text {
		t.Errorf("cap kept the wrong end: got[0]=%q", got[0].Text)
	}
}

func TestSetChatEmptyDeletesKey(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "intel.json"))
	s.SetChat("git:x", []Turn{{Role: "user", Text: "hi"}})
	if err := s.SetChat("git:x", nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Snapshot().Chats["git:x"]; ok {
		t.Error("an emptied chat should delete its key, not linger as []")
	}
}

func TestClearChat(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "intel.json"))
	s.SetChat("__fleet__", []Turn{{Role: "user", Text: "hi"}})
	if err := s.ClearChat("__fleet__"); err != nil {
		t.Fatal(err)
	}
	if len(s.Chat("__fleet__")) != 0 {
		t.Error("ClearChat left turns behind")
	}
}

func TestCorruptFileOpensReadOnlyAndQuarantines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "intel.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(p)
	if err == nil {
		t.Fatal("expected an error opening a corrupt file")
	}
	if s.Degraded() == nil {
		t.Error("Degraded should report the load failure")
	}
	if s.Quarantined() == "" {
		t.Error("the bad bytes should have been quarantined")
	}
	// Writes are refused while degraded: the empty fallback must never overwrite.
	if err := s.SetBrief(Brief{Text: "x"}); err == nil {
		t.Error("SetBrief must be refused while the store is degraded")
	}
	// The original bytes were moved aside, not destroyed.
	if _, err := os.Stat(s.Quarantined()); err != nil {
		t.Errorf("quarantined file missing: %v", err)
	}
	_ = strings.TrimSpace
}

func TestMissingFileIsEmptyNoError(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("a missing file must not error: %v", err)
	}
	if s.Brief().Text != "" || len(s.Snapshot().Chats) != 0 {
		t.Error("a missing file should yield an empty store")
	}
}
```

- [ ] **Step 2: Run it, watch it fail to build** - `go test ./internal/intel/` → `undefined: Open`.

- [ ] **Step 3: Implement** `internal/intel/intel.go`

```go
// Package intel persists fleet's AI intelligence - the fleet-wide brief and the
// per-identity chat transcripts - as a single JSON file. It follows the same
// integrity contract as internal/store: a corrupt file opens read-only with its
// bytes quarantined, and every write is refused until the data is trusted again,
// so an empty fallback can never overwrite the user's real intel.
package intel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/hoijun/fleet/internal/fileguard"
)

// chatCap bounds each chat to its most recent turns, matching the frontend's
// long-standing turns.slice(-20).
const chatCap = 20

// Turn is one message in a chat transcript.
type Turn struct {
	Role string `json:"role"` // "user" | "assistant"
	Text string `json:"text"`
}

// Brief is the fleet-wide "today" briefing: one per user.
type Brief struct {
	Text string `json:"text"`
	At   string `json:"at"`
	Lang string `json:"lang"`
}

// Data is the whole intel document.
type Data struct {
	Brief Brief             `json:"brief"`
	Chats map[string][]Turn `json:"chats"`
}

// Store is a concurrency-safe, file-backed intel document.
type Store struct {
	path        string
	mu          sync.RWMutex
	data        Data
	loadErr     error  // set when the file existed but could not be read/parsed
	quarantined string // where unparseable bytes were moved, if they were
}

// Open loads the store. A missing file yields an empty store with no error. A
// present-but-unparseable file yields a read-only store: the bytes are
// quarantined, Degraded reports the failure, and writes are refused.
func Open(path string) (*Store, error) {
	s := &Store{path: path, data: Data{Chats: map[string][]Turn{}}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		// Present but unreadable (permissions, a lock, a descriptor limit): do
		// NOT quarantine - the file is often fine on the next launch. Refusing
		// writes is enough.
		s.loadErr = err
		return s, err
	}
	var d Data
	if err := json.Unmarshal(data, &d); err != nil {
		s.loadErr = fmt.Errorf("intel.json is not valid JSON: %w", err)
		if dest, qerr := fileguard.Quarantine(path); qerr == nil {
			s.quarantined = dest
		}
		return s, s.loadErr
	}
	if d.Chats == nil {
		d.Chats = map[string][]Turn{}
	}
	s.data = d
	return s, nil
}

// Brief returns the current brief (zero value when unset).
func (s *Store) Brief() Brief {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Brief
}

// SetBrief replaces the brief.
func (s *Store) SetBrief(b Brief) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Brief = b
	return s.saveLocked()
}

// Chat returns a copy of the identity's transcript (nil-safe, empty when unset).
func (s *Store) Chat(id string) []Turn {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t := s.data.Chats[id]
	return append([]Turn(nil), t...)
}

// SetChat replaces an identity's transcript, capped to the last chatCap turns.
// An empty transcript deletes the key so it does not linger as [].
func (s *Store) SetChat(id string, turns []Turn) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(turns) == 0 {
		delete(s.data.Chats, id)
		return s.saveLocked()
	}
	if len(turns) > chatCap {
		turns = turns[len(turns)-chatCap:]
	}
	s.data.Chats[id] = append([]Turn(nil), turns...)
	return s.saveLocked()
}

// ClearChat removes an identity's transcript.
func (s *Store) ClearChat(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data.Chats, id)
	return s.saveLocked()
}

// Snapshot returns a deep copy of the whole document (for export).
func (s *Store) Snapshot() Data {
	s.mu.RLock()
	defer s.mu.RUnlock()
	chats := make(map[string][]Turn, len(s.data.Chats))
	for k, v := range s.data.Chats {
		chats[k] = append([]Turn(nil), v...)
	}
	return Data{Brief: s.data.Brief, Chats: chats}
}

// Degraded reports the load failure that put the store in read-only mode, or nil.
func (s *Store) Degraded() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadErr
}

// Quarantined returns where the unparseable bytes were moved, or "".
func (s *Store) Quarantined() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.quarantined
}

func (s *Store) saveLocked() error {
	if s.loadErr != nil {
		return fmt.Errorf("refusing to write over unreadable intel: %w", s.loadErr)
	}
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
```

Remove the stray `_ = strings.TrimSpace` and the `strings` import from the test if `go vet` flags them; they are placeholders to keep the import list honest. (If unused, delete the import line and that line.)

- [ ] **Step 4: Green** - `go test ./internal/intel/ -v`. All pass.

- [ ] **Step 5: gofmt + commit**

```bash
gofmt -w internal/intel/intel.go internal/intel/intel_test.go
git add internal/intel/intel.go internal/intel/intel_test.go
git commit -m "feat(intel): file-backed brief/chat store with the tier-3d integrity contract"
```

---

### Task 2: Stable chat identity

**Files:**
- Create: `internal/intel/identity.go`, `internal/intel/identity_test.go`

**Interfaces:**
- Consumes: `git.Runner`, `git.RemoteURL(r, path) (string, error)`, `git.NormalizeRemote(string) string`.
- Produces: `intel.ChatID(runner git.Runner, path string) string` and the constant `intel.FleetID = "__fleet__"`.

- [ ] **Step 1: Write the failing test** - `internal/intel/identity_test.go`

```go
package intel

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hoijun/fleet/internal/git"
)

func gitOK(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := (git.ExecRunner{}).Run(dir, args...); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}

func TestChatIDFleetPassesThrough(t *testing.T) {
	if got := ChatID(git.ExecRunner{}, FleetID); got != FleetID {
		t.Errorf("ChatID(__fleet__) = %q, want %q", got, FleetID)
	}
}

func TestChatIDUsesGitRemoteWhenPresent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	gitOK(t, dir, "-c", "init.defaultBranch=master", "init")
	gitOK(t, dir, "remote", "add", "origin", "git@github.com:Owner/Repo.git")
	got := ChatID(git.ExecRunner{}, dir)
	if got != "git:github.com/owner/repo" {
		t.Errorf("ChatID = %q, want git:github.com/owner/repo", got)
	}
}

func TestChatIDFallsBackToLocalWithoutRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	gitOK(t, dir, "-c", "init.defaultBranch=master", "init")
	got := ChatID(git.ExecRunner{}, dir)
	if got[:6] != "local:" {
		t.Errorf("ChatID = %q, want a local: id for a repo with no remote", got)
	}
	// Stable: the same path yields the same id.
	if again := ChatID(git.ExecRunner{}, dir); again != got {
		t.Errorf("ChatID not stable: %q != %q", got, again)
	}
	_ = os.Stat
	_ = filepath.Base
}
```

- [ ] **Step 2: Run it** - `go test ./internal/intel/ -run TestChatID` → `undefined: ChatID`.

- [ ] **Step 3: Implement** `internal/intel/identity.go`

```go
package intel

import (
	"crypto/sha1"
	"encoding/hex"
	"path/filepath"

	"github.com/hoijun/fleet/internal/git"
)

// FleetID is the identity of the fleet-wide chat: not a repo, a fixed key.
const FleetID = "__fleet__"

// ChatID derives a stable chat identity from a repo path, using the same
// convention as project doc-ids (syncengine.DocID), so a later sync tier can
// carry chats across devices without re-keying:
//
//   - the fleet-wide chat        -> "__fleet__"
//   - a repo with a git remote   -> "git:" + normalized remote (machine-stable)
//   - a repo with no remote       -> "local:" + short path hash (machine-local)
//
// The frontend passes a path (or FleetID) and never derives an identity itself.
func ChatID(runner git.Runner, path string) string {
	if path == FleetID {
		return FleetID
	}
	if remote, err := git.RemoteURL(runner, path); err == nil && remote != "" {
		return "git:" + git.NormalizeRemote(remote)
	}
	return "local:" + shortHash(filepath.Base(path))
}

func shortHash(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}
```

Delete the two `_ =` placeholder lines from the test once it builds; they only keep the imports honest while the file is first written. If `os`/`path/filepath` end up unused, drop those imports.

- [ ] **Step 4: Green** - `go test ./internal/intel/ -run TestChatID -v`.

- [ ] **Step 5: gofmt + commit**

```bash
gofmt -w internal/intel/identity.go internal/intel/identity_test.go
git add internal/intel/identity.go internal/intel/identity_test.go
git commit -m "feat(intel): stable chat identity matching the project doc-id convention"
```

---

### Task 3: Bindings and wiring

**Files:**
- Modify: `app.go` (struct field, `NewApp`, five bindings), `app_test.go`

**Interfaces:**
- Consumes: `intel.Open`, `intel.ChatID`, `intel.Brief`, `intel.Turn`, the store methods from Task 1.
- Produces: `App.GetBrief() intel.Brief`, `App.SaveBrief(text, at, lang string) string`, `App.GetChat(path string) []intel.Turn`, `App.SaveChat(path string, turns []intel.Turn) string`, `App.ClearChat(path string) string`.

- [ ] **Step 1: Write the failing test** - add to `app_test.go`

```go
func TestIntelBindingsRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	gitRun(t, dir, "-c", "init.defaultBranch=master", "init")
	gitRun(t, dir, "remote", "add", "origin", "git@github.com:Owner/Repo.git")

	is, err := intel.Open(filepath.Join(t.TempDir(), "intel.json"))
	if err != nil {
		t.Fatal(err)
	}
	a := &App{runner: git.ExecRunner{}, intel: is}

	if msg := a.SaveChat(dir, []intel.Turn{{Role: "user", Text: "hi"}}); msg != "" {
		t.Fatalf("SaveChat: %s", msg)
	}
	// Stored under the git: identity, reachable by the same path.
	if got := a.GetChat(dir); len(got) != 1 || got[0].Text != "hi" {
		t.Errorf("GetChat = %+v, want one 'hi' turn", got)
	}
	if _, ok := is.Snapshot().Chats["git:github.com/owner/repo"]; !ok {
		t.Error("chat was not stored under the normalized git identity")
	}

	if msg := a.SaveBrief("today", "2026-07-24T00:00:00Z", "ko"); msg != "" {
		t.Fatalf("SaveBrief: %s", msg)
	}
	if b := a.GetBrief(); b.Text != "today" || b.Lang != "ko" {
		t.Errorf("GetBrief = %+v", b)
	}
}
```

`gitRun` already exists in `app_test.go` (added in tier 4c). `intel` and `filepath` imports: `filepath` is already imported; add `"github.com/hoijun/fleet/internal/intel"`.

- [ ] **Step 2: Run it** - `go test . -run TestIntelBindings` → `App has no field or method intel`.

- [ ] **Step 3: Add the struct field** - in `app.go`, next to `store *store.Store` (`app.go:44`):

```go
	store    *store.Store
	intel    *intel.Store
```

Add the import `"github.com/hoijun/fleet/internal/intel"` to the block (alphabetical: after `internal/gh`, before `internal/git`... it sorts as `intel` after `git`; place it after `internal/git`).

- [ ] **Step 4: Wire it in `NewApp`** - after `st, storeErr := store.Open(storePath)` (`app.go:122`):

```go
	intelPath := filepath.Join(dir, "intel.json")
	is, _ := intel.Open(intelPath)
```

and add `intel: is,` to the returned `&App{...}` literal, next to `store: st,`. The load error is intentionally dropped here for now: intel is not yet in `StartupHealth`, and a degraded store already refuses writes on its own. (A follow-up may surface it; out of scope for this tier.)

- [ ] **Step 5: Add the bindings** - after `GetConfig` (`app.go:258`, near the other getters):

```go
// GetBrief returns the stored fleet-wide brief.
func (a *App) GetBrief() intel.Brief { return a.intel.Brief() }

// SaveBrief stores the fleet-wide brief.
func (a *App) SaveBrief(text, at, lang string) string {
	return errMsg(a.intel.SetBrief(intel.Brief{Text: text, At: at, Lang: lang}))
}

// GetChat returns the transcript for a repo path (or intel.FleetID). The path is
// mapped to a stable identity here so the frontend never derives one.
func (a *App) GetChat(path string) []intel.Turn {
	return a.intel.Chat(intel.ChatID(a.runner, path))
}

// SaveChat replaces the transcript for a repo path (or intel.FleetID).
func (a *App) SaveChat(path string, turns []intel.Turn) string {
	return errMsg(a.intel.SetChat(intel.ChatID(a.runner, path), turns))
}

// ClearChat removes the transcript for a repo path (or intel.FleetID).
func (a *App) ClearChat(path string) string {
	return errMsg(a.intel.ClearChat(intel.ChatID(a.runner, path)))
}
```

- [ ] **Step 6: Green** - `go test . -run TestIntelBindings -v && go build ./... && go vet ./...`

- [ ] **Step 7: Regenerate bindings**

```bash
wails generate module
```

Confirm `frontend/wailsjs/go/main/App.d.ts` now has `GetBrief`, `SaveBrief`, `GetChat`, `SaveChat`, `ClearChat` and `models.ts` has `intel.Brief` / `intel.Turn`.

- [ ] **Step 8: gofmt + commit**

```bash
gofmt -w app.go app_test.go
git add app.go app_test.go frontend/wailsjs
git commit -m "feat(app): intel store wiring and brief/chat bindings"
```

---

### Task 4: Export includes intel

**Files:**
- Modify: `app.go` (`writeExport`), `app_test.go`

**Interfaces:**
- Consumes: `a.intel.Snapshot()`.
- Produces: an export body `{"projects": {...}, "intel": {...}}`.

- [ ] **Step 1: Write the failing test** - add to `app_test.go`

```go
func TestExportIncludesIntel(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	is, err := intel.Open(filepath.Join(t.TempDir(), "intel.json"))
	if err != nil {
		t.Fatal(err)
	}
	is.SetBrief(intel.Brief{Text: "exported brief"})
	a := &App{store: st, intel: is}

	dest := filepath.Join(t.TempDir(), "out.json")
	if err := a.writeExport(dest); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Projects map[string]json.RawMessage `json:"projects"`
		Intel    intel.Data                 `json:"intel"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("export is not the {projects, intel} shape: %v", err)
	}
	if body.Intel.Brief.Text != "exported brief" {
		t.Errorf("intel brief missing from export: %+v", body.Intel)
	}
}
```

`json` and `os` are already imported in `app_test.go`.

- [ ] **Step 2: Run it** - `go test . -run TestExportIncludesIntel` → fails (current export is a bare project map, so `body.Projects` is empty and `body.Intel` zero, tripping the brief assertion).

- [ ] **Step 3: Implement** - replace `writeExport` (`app.go`, the 6-line function):

```go
func (a *App) writeExport(dest string) error {
	data, err := json.MarshalIndent(struct {
		Projects map[string]store.Record `json:"projects"`
		Intel    intel.Data              `json:"intel"`
	}{a.store.Snapshot(), a.intel.Snapshot()}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dest, data, 0o644)
}
```

- [ ] **Step 4: Green** - `go test . -run TestExportIncludesIntel -v`

- [ ] **Step 5: gofmt + commit**

```bash
gofmt -w app.go app_test.go
git add app.go app_test.go
git commit -m "feat(app): include intel in the data export"
```

---

### Task 5: One-time frontend migration

**Files:**
- Create: `frontend/src/lib/intelMigrate.ts`, `frontend/src/lib/intelMigrate.test.ts`
- Modify: `frontend/src/App.svelte`

**Interfaces:**
- Consumes: `SaveBrief`, `SaveChat`, `GetChat` bindings.
- Produces: `migrateIntel(): Promise<void>` - idempotent, guarded by `fleet.intelMigrated`.

- [ ] **Step 1: Write the failing test** - `frontend/src/lib/intelMigrate.test.ts`

```ts
import { describe, it, expect, vi, beforeEach } from "vitest";

const saved: Record<string, unknown> = {};
vi.mock("../../wailsjs/go/main/App", () => ({
  SaveBrief: vi.fn(async (text: string, at: string, lang: string) => { saved["brief"] = { text, at, lang }; return ""; }),
  SaveChat: vi.fn(async (path: string, turns: unknown) => { saved["chat:" + path] = turns; return ""; }),
  // Report nothing already in the store, so migration writes.
  GetChat: vi.fn(async () => []),
}));

import { migrateIntel } from "./intelMigrate";
import { SaveBrief, SaveChat } from "../../wailsjs/go/main/App";

function fakeLocalStorage(seed: Record<string, string>) {
  const m = new Map(Object.entries(seed));
  return {
    getItem: (k: string) => (m.has(k) ? m.get(k)! : null),
    setItem: (k: string, v: string) => void m.set(k, v),
    removeItem: (k: string) => void m.delete(k),
    key: (i: number) => [...m.keys()][i] ?? null,
    get length() { return m.size; },
    _map: m,
  };
}

describe("migrateIntel", () => {
  beforeEach(() => { for (const k of Object.keys(saved)) delete saved[k]; vi.clearAllMocks(); });

  it("copies brief and chats into the store, then clears the flag-guarded keys", async () => {
    const ls = fakeLocalStorage({
      "fleet.brief": JSON.stringify({ text: "hi", at: "t" }),
      "fleet.briefLang": "ko",
      "fleet.chat:/a/b": JSON.stringify([{ role: "user", text: "q" }]),
      "fleet.chat:__fleet__": JSON.stringify([{ role: "assistant", text: "a" }]),
    });
    (globalThis as any).localStorage = ls;

    await migrateIntel();

    expect(SaveBrief).toHaveBeenCalledWith("hi", "t", "ko");
    expect(SaveChat).toHaveBeenCalledWith("/a/b", [{ role: "user", text: "q" }]);
    expect(SaveChat).toHaveBeenCalledWith("__fleet__", [{ role: "assistant", text: "a" }]);
    expect(ls._map.has("fleet.chat:/a/b")).toBe(false); // migrated key removed
    expect(ls._map.get("fleet.intelMigrated")).toBe("1");
  });

  it("is a no-op on the second run", async () => {
    const ls = fakeLocalStorage({ "fleet.intelMigrated": "1", "fleet.brief": JSON.stringify({ text: "x" }) });
    (globalThis as any).localStorage = ls;
    await migrateIntel();
    expect(SaveBrief).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run it** - `npm test --prefix frontend -- intelMigrate` → fails, module not found.

- [ ] **Step 3: Implement** `frontend/src/lib/intelMigrate.ts`

```ts
import { SaveBrief, SaveChat, GetChat } from "../../wailsjs/go/main/App";

// migrateIntel moves brief/chat data out of localStorage into the Go store, once.
// It writes each key only if the store does not already have it (so a re-run
// after a partial failure cannot clobber newer store data), removes the migrated
// localStorage keys, and sets a flag so it never runs again. Any failure leaves
// localStorage intact: the old data is not destroyed until it is safely stored.
const FLAG = "fleet.intelMigrated";

export async function migrateIntel(): Promise<void> {
  if (typeof localStorage === "undefined") return;
  if (localStorage.getItem(FLAG) === "1") return;
  try {
    // Brief: a single global blob plus its language.
    const rawBrief = localStorage.getItem("fleet.brief");
    if (rawBrief) {
      const b = JSON.parse(rawBrief);
      const lang = localStorage.getItem("fleet.briefLang") || "ko";
      if (b && typeof b.text === "string") {
        const err = await SaveBrief(b.text, b.at || "", lang);
        if (err) return; // leave localStorage intact; retry next launch
      }
    }

    // Chats: every fleet.chat:<path> key. The path after the prefix is what the
    // binding maps to an identity, so "__fleet__" and real paths both work.
    const chatKeys: string[] = [];
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i);
      if (k && k.startsWith("fleet.chat:")) chatKeys.push(k);
    }
    for (const k of chatKeys) {
      const path = k.slice("fleet.chat:".length);
      const existing = await GetChat(path);
      if (existing && existing.length > 0) continue; // store already has it
      const turns = JSON.parse(localStorage.getItem(k) || "[]");
      if (Array.isArray(turns) && turns.length > 0) {
        const err = await SaveChat(path, turns);
        if (err) return;
      }
    }

    // Only now, with everything safely stored, remove the old keys.
    localStorage.removeItem("fleet.brief");
    localStorage.removeItem("fleet.briefLang");
    for (const k of chatKeys) localStorage.removeItem(k);
    localStorage.setItem(FLAG, "1");
  } catch {
    // Leave localStorage untouched; the next launch retries.
  }
}
```

- [ ] **Step 4: Green** - `npm test --prefix frontend -- intelMigrate`

- [ ] **Step 5: Call it from `App.svelte`** - in `onMount` (`App.svelte:707`), as the FIRST awaited call, before `refreshHealth`:

```ts
  onMount(async () => {
    window.addEventListener("keydown", onKey);
    await migrateIntel();
    await refreshHealth();
```

and add the import near the top of the script:

```ts
  import { migrateIntel } from "./lib/intelMigrate";
```

- [ ] **Step 6: Green** - `npm run check --prefix frontend && npm test --prefix frontend`

- [ ] **Step 7: Commit**

```bash
git add frontend/src/lib/intelMigrate.ts frontend/src/lib/intelMigrate.test.ts frontend/src/App.svelte
git commit -m "feat(frontend): one-time migration of intel from localStorage into the store"
```

---

### Task 6: Components read/write via bindings

**Files:**
- Modify: `frontend/src/lib/agentSession.ts`, `frontend/src/lib/RepoChat.svelte`, `frontend/src/lib/Today.svelte`

**Interfaces:**
- Consumes: `GetChat`, `SaveChat`, `ClearChat`, `GetBrief`, `SaveBrief`.
- Produces: nothing new; removes the `localStorage` chat/brief paths.

- [ ] **Step 1: agentSession.ts** - add the imports to the existing binding import block (`agentSession.ts:2-4`):

```ts
import {
  AgentAvailable, AgentConsent, GiveAgentConsent, AgentAsk, AgentAskFleet, ApproveAction, CancelAgent,
  GetChat, SaveChat,
} from "../../wailsjs/go/main/App";
```

Delete `chatKey` (`:43`), `loadChat` (`:44-51`), and `saveChat` (`:52-55`). Replace the call sites:

- Where `turns.set(p ? loadChat(p.path) : [])` appears (setProject, `:97`; and openOverlay reload, `:139`), the load is now async. Change those functions to await the binding. For `setProject`:

```ts
  loadedPath = p ? p.path : "";
  if (p) { GetChat(p.path).then((t) => { if (loadedPath === p.path) turns.set(t || []); }); }
  else turns.set([]);
```

  and at `:139`:

```ts
  if (p && !get(running)) { loadedPath = p.path; GetChat(p.path).then((t) => { if (loadedPath === p.path) turns.set(t || []); }); }
```

- Replace both `saveChat()` calls (`:73`, `:78`) with `void SaveChat(loadedPath, get(turns).slice(-20));` (the binding also caps, but slicing here keeps the payload small).

- [ ] **Step 2: RepoChat.svelte** - add to its binding imports:

```ts
  import { GetChat, SaveChat, ClearChat } from "../../wailsjs/go/main/App";
```

Delete `chatKey`/`loadChat`/`saveChat`/`clearChat` (`:72-97`). Replace usage:

- The load (wherever `loadChat(p)` was called to seed `turns`) becomes `turns = (await GetChat(p)) || [];`.
- `saveChat()` -> `void SaveChat(loadedPath, turns.slice(-20));`.
- `clearChat()` body -> `turns = []; void ClearChat(loadedPath);`.

Confirm the component tracks `loadedPath` the same way it does today (it does: `RepoChat.svelte:86-88`).

- [ ] **Step 3: Today.svelte** - add:

```ts
  import { GetBrief, SaveBrief } from "../../wailsjs/go/main/App";
```

- Where it reads `localStorage.getItem("fleet.brief")` (`:129`) to seed `brief`/`briefAt`, replace with:

```ts
    const b = await GetBrief();
    if (b && b.text) { brief = b.text; briefAt = b.at; }
```

- Where it writes `localStorage.setItem("fleet.brief", ...)` and `fleet.briefLang` (`:143-144`), replace both with:

```ts
    void SaveBrief(brief, briefAt, briefLang);
```

- Keep `fleet.briefAutoDate` (`:145`, `:162`) on `localStorage`: it is a per-day, per-device "already auto-ran" guard, not user data.
- The `briefLang` reactive read (`:114`) may keep its `localStorage` seed for the initial value, but its authoritative persistence now rides along in `SaveBrief`. To avoid a second source of truth, seed `briefLang` from `GetBrief().lang` on mount (falling back to `"ko"`), and drop the `localStorage.setItem("fleet.briefLang", ...)` write (`:116`).

- [ ] **Step 4: Green** - `npm run check --prefix frontend && npm test --prefix frontend`. All green, no `localStorage` chat/brief references remain:

```bash
grep -rn "fleet.chat\|fleet.brief\"" frontend/src/lib/agentSession.ts frontend/src/lib/RepoChat.svelte frontend/src/lib/Today.svelte
```

Expected: nothing (only `fleet.briefAutoDate` may remain, in Today.svelte).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/agentSession.ts frontend/src/lib/RepoChat.svelte frontend/src/lib/Today.svelte
git commit -m "feat(frontend): read and write brief/chat through the intel store, not localStorage"
```

---

### Task 7: Whole-suite verification and ship

- [ ] **Step 1** - `cd frontend && npm ci && npm run build && cd .. && go build ./... && go vet ./... && go test ./...` - clean, no FAIL.
- [ ] **Step 2** - gofmt diff on every touched Go file's LF blob: zero bytes.

```bash
for f in internal/intel/intel.go internal/intel/identity.go internal/intel/intel_test.go internal/intel/identity_test.go app.go app_test.go; do
  echo "$f: $(git show HEAD:$f | gofmt -d | wc -c)"; done
```

- [ ] **Step 3** - CHANGELOG `[Unreleased]` gains, under Changed:
  `- Brief and chat transcripts are stored in the local data directory (and included in the export) instead of the browser's localStorage.`
- [ ] **Step 4** - `wails build`, launch, confirm by hand: an existing user's brief and repo chats survive the upgrade (migration ran), a new chat persists across an app restart, and the exported JSON contains an `intel` object.
- [ ] **Step 5** - push, confirm the three `desktop` checks are green, open a PR, merge once green.

---

## Self-Review

- **Spec coverage:** §1 store → Task 1; §2 identity → Task 2; §3 bindings → Task 3; §4 migration → Task 5; §5 export → Task 4; component switch → Task 6. All covered.
- **Type consistency:** `intel.Brief{Text,At,Lang}`, `intel.Turn{Role,Text}`, `intel.Data{Brief,Chats}`, `ChatID(runner,path)`, `FleetID` used identically across Tasks 1-3 and the frontend. `SaveBrief(text,at,lang)`, `SaveChat(path,turns)`, `GetChat(path)` consistent between Task 3 bindings, Task 5 migration, and Task 6 components.
- **Placeholder scan:** the `_ =` lines in the test snippets are called out explicitly for removal, not left as silent placeholders. No TBDs.
