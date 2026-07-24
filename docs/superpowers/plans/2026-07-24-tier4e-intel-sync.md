# Tier 4e - Intel Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** the AI brief and chat transcripts sync across a user's devices, the same way projects already do.

**Architecture:** the sync engine is generalized from one hard-wired project store to a list of `Source`s. Today's project logic moves behind a `projectSource` unchanged - the existing engine tests are the proof it did not change. Two intel sources (brief, chat) are added. The sync state is re-keyed by `(kind, doc_id)` to stop the project/chat `git:` collision, and a degraded source is skipped (not aborted) with the skip reported to the UI.

**Tech Stack:** Go 1.22, wails v2.12.0.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-24-tier4e-intel-sync-design.md`.
- `internal/syncengine` is the one file where a bug is silent multi-device data loss. The ten non-degraded engine tests MUST stay green with their assertions unchanged; that is the contract that the `projectSource` refactor preserved behavior. Do not edit their bodies.
- Do NOT run `gofmt -w` across the tree (CRLF working copy). Format only touched files; check with `git show HEAD:<file> | gofmt -d` (expect zero bytes).
- The intel store's clock is an injectable `func() time.Time` defaulting to `time.Now`; tests inject a fixed clock.
- LWW timestamps are RFC3339Nano (`newer()` in `state.go:96` parses that).
- Regenerate wails bindings with `wails generate module` only if a binding signature changes (it does not in this tier - `GetChat`/`SaveChat` keep their shapes).
- Conventional Commits, no trailers. Keep the branch green on `desktop.yml`.

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/intel/intel.go` (modify) | Per-doc `updatedAt`, injectable clock, back-compat `Chat` unmarshal, `BriefUpdatedAt`/`ChatUpdatedAt`. |
| `internal/intel/intel_test.go` (modify) | Timestamp stamping, old-shape unmarshal. |
| `internal/syncengine/source.go` (create) | `Source` interface, `Item`, `projectSource`, `briefSource`, `chatSource`. |
| `internal/syncengine/source_test.go` (create) | Source-level unit tests for the intel sources. |
| `internal/syncengine/state.go` (modify) | Nested `State.Docs`, migration in `loadState`. |
| `internal/syncengine/state_test.go` (modify) | Migration test. |
| `internal/syncengine/engine.go` (modify) | Drive push/tombstone/pull through sources; `SkippedDegraded`; drop `ErrLocalDataUnsafe`. |
| `internal/syncengine/engine_test.go` (modify) | `newEngine` + direct constructions use `NewProject`; nested state access; rewrite the two degraded tests; add intel/collision tests; `fakeSrv` keys by `(kind,doc_id)`. |
| `app.go` (modify) | Build both sources, new `New` signature. |
| `authsync.go` (modify) | Move the "paused" surfacing to a post-success `SkippedDegraded` check. |
| `CHANGELOG.md` (modify) | Note cross-device intel sync. |

---

### Task 1: Intel store write timestamps

**Files:** Modify `internal/intel/intel.go`, `internal/intel/intel_test.go`

**Interfaces:**
- Produces: `intel.Chat{Turns []Turn; UpdatedAt string}`; `Brief` gains `UpdatedAt string`; `Store.SetClock(func() time.Time)`; `Store.BriefUpdatedAt() string`; `Store.ChatUpdatedAt(id string) string`. `Chat` has a custom `UnmarshalJSON` accepting the old `[]Turn` shape. `Data.Chats` becomes `map[string]Chat`.

- [ ] **Step 1: Write the failing test** — add to `internal/intel/intel_test.go`

```go
func TestSetChatStampsUpdatedAt(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "intel.json"))
	s.SetClock(func() time.Time { return time.Date(2026, 7, 24, 1, 2, 3, 0, time.UTC) })
	if err := s.SetChat("git:x", []Turn{{Role: "user", Text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	if got := s.ChatUpdatedAt("git:x"); got != "2026-07-24T01:02:03Z" {
		t.Errorf("ChatUpdatedAt = %q, want the stamped time", got)
	}
}

func TestSetBriefStampsUpdatedAt(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "intel.json"))
	s.SetClock(func() time.Time { return time.Date(2026, 7, 24, 1, 2, 3, 0, time.UTC) })
	if err := s.SetBrief(Brief{Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if got := s.BriefUpdatedAt(); got != "2026-07-24T01:02:03Z" {
		t.Errorf("BriefUpdatedAt = %q, want the stamped time", got)
	}
}

func TestOpenAcceptsOldBareArrayChatShape(t *testing.T) {
	p := filepath.Join(t.TempDir(), "intel.json")
	// The tier-4d on-disk shape: chats are bare [Turn] arrays.
	old := `{"brief":{"text":"b"},"chats":{"git:x":[{"role":"user","text":"q"}]}}`
	if err := os.WriteFile(p, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(p)
	if err != nil {
		t.Fatalf("Open must accept the old shape: %v", err)
	}
	got := s.Chat("git:x")
	if len(got) != 1 || got[0].Text != "q" {
		t.Errorf("old-shape chat not loaded: %+v", got)
	}
	if s.ChatUpdatedAt("git:x") != "" {
		t.Error("an old chat should load with an empty updatedAt (older-than-anything)")
	}
}
```

Add `"time"` to the test imports.

- [ ] **Step 2: Run it** — `go test ./internal/intel/ -run "Stamp|OldBare"` → fails to build (`SetClock` undefined, `Chat` type changed).

- [ ] **Step 3: Implement** — in `intel.go`:

Change the types:

```go
// Chat is one identity's transcript with the time it last changed locally, for
// last-write-wins sync.
type Chat struct {
	Turns     []Turn `json:"turns"`
	UpdatedAt string `json:"updatedAt"`
}

// UnmarshalJSON accepts either the current object shape or the tier-4d shape,
// where a chat was a bare array of turns. An old chat loads with an empty
// UpdatedAt, which sync treats as older than anything.
func (c *Chat) UnmarshalJSON(b []byte) error {
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		return json.Unmarshal(b, &c.Turns)
	}
	type raw Chat
	var r raw
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	*c = Chat(r)
	return nil
}
```

Add `UpdatedAt` to `Brief`:

```go
type Brief struct {
	Text      string `json:"text"`
	At        string `json:"at"`
	Lang      string `json:"lang"`
	UpdatedAt string `json:"updatedAt"`
}
```

Change `Data.Chats` to `map[string]Chat`, and update `Store`:

```go
type Store struct {
	path        string
	mu          sync.RWMutex
	data        Data
	now         func() time.Time
	loadErr     error
	quarantined string
}
```

In `Open`, initialize `s.now = time.Now` and `data: Data{Chats: map[string]Chat{}}`; keep the `if d.Chats == nil` guard.

Add:

```go
// SetClock overrides the timestamp source (tests inject a fixed clock).
func (s *Store) SetClock(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

func (s *Store) stamp() string { return s.now().UTC().Format(time.RFC3339Nano) }
```

Rewrite `Chat`/`SetChat`/`ClearChat`/`Brief`/`SetBrief` for the new shape:

```go
func (s *Store) Chat(id string) []Turn {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Turn(nil), s.data.Chats[id].Turns...)
}

func (s *Store) ChatUpdatedAt(id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Chats[id].UpdatedAt
}

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
	s.data.Chats[id] = Chat{Turns: append([]Turn(nil), turns...), UpdatedAt: s.stamp()}
	return s.saveLocked()
}

func (s *Store) BriefUpdatedAt() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Brief.UpdatedAt
}

func (s *Store) SetBrief(b Brief) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b.UpdatedAt = s.stamp()
	s.data.Brief = b
	return s.saveLocked()
}
```

Update `Snapshot`'s chat copy to `make(map[string]Chat, ...)`. Add `"bytes"` and `"time"` imports.

- [ ] **Step 4: Green** — `go test ./internal/intel/ -v`. All pass (the tier-4d round-trip tests still pass because `Chat(id)`/`SetChat` keep their `[]Turn` external shape).

- [ ] **Step 5: gofmt + commit**

```bash
gofmt -w internal/intel/intel.go internal/intel/intel_test.go
git add internal/intel/intel.go internal/intel/intel_test.go
git commit -m "feat(intel): stamp a per-document updatedAt for last-write-wins sync"
```

---

### Task 2: Nested sync state + migration

**Files:** Modify `internal/syncengine/state.go`, `internal/syncengine/state_test.go`

**Interfaces:**
- Produces: `State.Docs` is `map[string]map[string]DocState` (kind → doc_id → state). `loadState` migrates a flat pre-4e file into the `"project"` kind.

- [ ] **Step 1: Write the failing test** — add to `state_test.go`

```go
func TestLoadStateMigratesFlatToNested(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sync.json")
	// A pre-4e sync.json: Docs is a flat doc_id -> DocState map.
	flat := `{"cursor":5,"docs":{"m-1":{"localId":"m-1","hash":"h","updatedAt":"t","deleted":false}}}`
	if err := os.WriteFile(p, []byte(flat), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := loadState(p)
	if err != nil {
		t.Fatalf("loadState should migrate, not error: %v", err)
	}
	if s.Cursor != 5 {
		t.Errorf("cursor lost in migration: %d", s.Cursor)
	}
	ds, ok := s.Docs["project"]["m-1"]
	if !ok || ds.Hash != "h" {
		t.Errorf("flat doc not migrated into the project kind: %+v", s.Docs)
	}
}

func TestLoadStateReadsNested(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sync.json")
	nested := `{"cursor":2,"docs":{"chat":{"git:x":{"localId":"git:x","hash":"h","updatedAt":"t"}}}}`
	if err := os.WriteFile(p, []byte(nested), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := loadState(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Docs["chat"]["git:x"]; !ok {
		t.Errorf("nested state not read: %+v", s.Docs)
	}
}
```

- [ ] **Step 2: Run it** — `go test ./internal/syncengine/ -run TestLoadState` → fails (nested indexing on a flat map / compile error once `State` changes).

- [ ] **Step 3: Implement** — in `state.go`:

```go
// State is the persisted sync bookkeeping (sync.json). Docs is keyed by kind,
// then doc_id, because a project and a chat can share a doc_id (both "git:<remote>")
// and must not collide in the bookkeeping the way they cannot on the server.
type State struct {
	Cursor int64                          `json:"cursor"`
	Docs   map[string]map[string]DocState `json:"docs"`
}
```

Rewrite `loadState` to migrate:

```go
func loadState(path string) (State, error) {
	empty := State{Docs: map[string]map[string]DocState{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return empty, nil
		}
		return empty, err
	}
	// Try the current nested shape first.
	var s State
	if err := json.Unmarshal(data, &s); err == nil && s.Docs != nil {
		return s, nil
	}
	// Fall back to the pre-4e flat shape: every doc it holds is a project, since
	// projects were the only synced kind before this tier.
	var flat struct {
		Cursor int64               `json:"cursor"`
		Docs   map[string]DocState `json:"docs"`
	}
	if err := json.Unmarshal(data, &flat); err != nil {
		return empty, err
	}
	out := State{Cursor: flat.Cursor, Docs: map[string]map[string]DocState{}}
	if len(flat.Docs) > 0 {
		out.Docs["project"] = flat.Docs
	}
	return out, nil
}
```

Note: a nested-shape file unmarshals into `State` cleanly; a flat-shape file also unmarshals into `State` without error but leaves `s.Docs` values as `map[string]map...` of the wrong depth — so the guard is `s.Docs != nil` AND the nested values decode. To be unambiguous, detect the shape by trial: attempt nested, and if any value fails to be a map, fall back. Simplify by trying flat first when the nested decode yields entries whose inner type is not a map. **Concretely:** since `DocState` and `map[string]DocState` do not unmarshal-interchange cleanly, the first `Unmarshal` into `State` will error on a flat file (a `DocState` object cannot decode into `map[string]DocState`), so the `err == nil` guard already routes correctly. Keep the code above.

- [ ] **Step 4: Green** — `go test ./internal/syncengine/ -run TestLoadState -v`. (The engine won't compile yet because `engine.go` still uses the flat map; that is Task 3. Run just this file's state tests by building the test binary is not possible mid-refactor — so this step's green is deferred to Task 3 Step 6, and Step 2's "fails" is observed as a compile error naming the `State.Docs` shape.)

- [ ] **Step 5: Commit** (code compiles as a unit once Task 3 lands; commit state.go with Task 3). Skip a standalone commit here — fold into Task 3 to keep the tree buildable. Mark this task done when Task 3's suite is green.

---

### Task 3: Source interface + projectSource (behavior-preserving)

**Files:** Create `internal/syncengine/source.go`; modify `internal/syncengine/engine.go`, `internal/syncengine/engine_test.go`

**Interfaces:**
- Produces: `Source` interface; `Item{Payload []byte; UpdatedAt string}`; `NewProject(st *store.Store, remoteOf func(string) string, degraded func() error) Source`; `New(client *cloud.Client, statePath string, sources ...Source) *Engine`.
- Consumes: nested `State` from Task 2.

- [ ] **Step 1: Write `source.go`** — the interface plus `projectSource`, which reproduces today's engine logic exactly.

```go
package syncengine

import (
	"encoding/json"

	"github.com/hoijun/fleet/internal/store"
)

// Item is one syncable document's current local state.
type Item struct {
	Payload   []byte
	UpdatedAt string
}

// Source is one family of syncable documents. The engine drives push, tombstone
// and pull through it, so each source owns its snapshot, live-set and degraded
// policy. A source serves exactly one kind (pull routes by kind).
type Source interface {
	Kind() string
	// Snapshot returns the source's current docs. prev is the engine's persisted
	// state slice for this kind, which the project source consults to keep a
	// detached record's doc_id stable; the intel sources ignore it.
	Snapshot(prev map[string]DocState) map[string]Item
	Degraded() error
	// Apply upserts a pulled doc. prevLocal is the local payload the doc is about
	// to overwrite (nil when none), so the source can back it up before writing;
	// the project source does, the intel sources do not.
	Apply(docID string, it Item, prevLocal []byte) error
	Remove(docID string, prevLocal []byte) error
	// Reconcilable guards the tombstone loop: a non-nil return (given the number
	// of docs the engine still tracks for this kind) means the snapshot cannot be
	// trusted to derive a live-set, so the cycle must not tombstone from it.
	Reconcilable(tracked int) error
	// LocalIDFor maps a pulled doc_id to the local key the source stores it under.
	// The project source may return a different local id (detached records); the
	// intel sources return the doc_id itself.
	LocalIDFor(docID string, knownLocalID string) string
}

// projectSource wraps the PM store. Its Snapshot reproduces the doc-id
// derivation, detached-record retention and no-remote fallback the engine used
// to do inline, so the existing engine tests prove behavior is unchanged.
type projectSource struct {
	store    *store.Store
	remoteOf func(string) string
	degraded func() error
	// idOf remembers, per local id, the doc_id last used, so a detached record
	// keeps its identity (set by the engine via LocalIDFor round-tripping).
}

func NewProject(st *store.Store, remoteOf func(string) string, degraded func() error) Source {
	return &projectSource{store: st, remoteOf: remoteOf, degraded: degraded}
}

func (p *projectSource) Kind() string { return "project" }

func (p *projectSource) Degraded() error {
	if p.degraded == nil {
		return nil
	}
	return p.degraded()
}
```

> **Refactor note for the implementer:** the pieces below are *moved*, not rewritten. Cut the doc-id/detached logic out of `SyncOnce` (`engine.go:197-228`) and the apply/backup logic (`engine.go:270-320`) and place them in `projectSource.Snapshot`, `.Apply`, `.Remove`, keeping every comment. The detached-id retention (`engine.go:212-214`) depends on the engine's per-kind state slice, so `Snapshot` takes the current state slice for this kind as an argument. Update the `Source` interface so `Snapshot(prev map[string]DocState) map[string]Item` and `Apply`/`Remove` receive what they need to run the existing backup calls. Preserve the `backupConflict` behavior for the project source only.

Because the exact cut spans large blocks, implement `projectSource` by moving the code verbatim and adjusting only the receiver and the state access. The test suite (Step 5) is the acceptance gate: if any of the ten behavior tests fail, the move changed behavior and must be corrected.

- [ ] **Step 2: Refactor `engine.go`** — the `Engine` holds `sources []Source`; `SyncOnce` loops over them. Keep, verbatim, the degraded/reconcilable guards as a whole-cycle abort for now (Task 5 changes the policy), so the two degraded tests stay green:

```go
type Engine struct {
	client    *cloud.Client
	statePath string
	sources   []Source

	mu            sync.Mutex
	state         State
	loaded        bool
	lastConflict  bool
	lostLocalEdit string
}

func New(client *cloud.Client, statePath string, sources ...Source) *Engine {
	return &Engine{
		client:    client,
		statePath: statePath,
		sources:   sources,
		state:     State{Docs: map[string]map[string]DocState{}},
	}
}
```

`SyncOnce` becomes: load state; for each source, run the existing degraded/reconcile guards (returning `ErrLocalDataUnsafe` as today); build dirty+tombstones from `state.Docs[kind]`; push; record accepted under `state.Docs[kind]`. Then one `Pull`, routing each doc to the source whose `Kind()` matches and applying via the source. Cursor from pull only. Persist.

Keep `ErrLocalDataUnsafe`, `TookRemoteEdit`, `LostLocalEdit`, `Reset`, `conflictsPath`, `backupConflict`, `payloadHash`, `newer` unchanged in this task.

- [ ] **Step 3: Update `engine_test.go` construction and internal-state access** — mechanical, not behavioral:
  - `newEngine` (`:91`): `e := New(cloud.New(url), statePath, NewProject(st, func(string) string { return "" }, nil))`
  - Reset (`:208`), Detached (`:245`,`:258`), degraded (`:472`): same `New(client, path, NewProject(st, remoteOf, degraded))` shape.
  - `:380`: `eB.state.Docs["m-1"]` → `eB.state.Docs["project"]["m-1"]`.

  Do NOT touch any assertion in the ten behavior tests.

- [ ] **Step 4: Update `fakeSrv` to key by (kind, doc_id)** — the server stub at `engine_test.go:23-84` keys `f.docs` by `d.DocID`; the real server keys by `(user, kind, doc_id)`. Change the map key to `d.Kind + "\x00" + d.DocID` in the POST branch, and add a helper so existing tests that read `f.docs["m-1"]` / `f.docs["git:github.com/o/app"]` keep working:

```go
func (f *fakeSrv) get(kind, id string) (cloud.Doc, bool) { d, ok := f.docs[kind+"\x00"+id]; return d, ok }
```

  Replace the tests' direct `f.docs["m-1"]` reads (`:251`, `:460`, `:477`) with `f.get("project", "m-1")` etc. The GET branch iterates all values, so it is unaffected.

- [ ] **Step 5: Green — the whole suite, assertions unchanged**

```bash
go test ./internal/syncengine/ ./internal/intel/ -v 2>&1 | grep -E "^(--- FAIL|FAIL|ok)"
```

Expected: every test passes, including the two degraded ones (still global-abort in this task) and `TestLoadStateMigratesFlatToNested`.

- [ ] **Step 6: gofmt + commit**

```bash
gofmt -w internal/syncengine/source.go internal/syncengine/engine.go internal/syncengine/state.go internal/syncengine/engine_test.go internal/syncengine/state_test.go
git add internal/syncengine/
git commit -m "refactor(sync): drive the engine through a Source interface; project logic unchanged"
```

---

### Task 4: Intel sources + sync tests

**Files:** Create `internal/syncengine/source_test.go`; modify `internal/syncengine/source.go`, `internal/syncengine/engine_test.go`

**Interfaces:**
- Produces: `NewBrief(is *intel.Store) Source`, `NewChat(is *intel.Store) Source`.

- [ ] **Step 1: Write the failing test** — `internal/syncengine/source_test.go` and additions to `engine_test.go`.

In `engine_test.go`, a collision + LWW test:

```go
func TestProjectAndChatShareGitIdSafely(t *testing.T) {
	f := newFake()
	ts := httptest.NewServer(f)
	defer ts.Close()

	dir := t.TempDir()
	st, _ := store.Open(filepath.Join(dir, "projects.json"))
	is, _ := intel.Open(filepath.Join(dir, "intel.json"))
	is.SetClock(func() time.Time { return time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC) })
	e := New(cloud.New(ts.URL), filepath.Join(dir, "sync.json"),
		NewProject(st, func(string) string { return "git@github.com:o/app.git" }, nil),
		NewChat(is))

	// A project and a chat for the SAME repo -> same doc_id "git:github.com/o/app".
	_ = st.Update("C:/repos/app", func(r *store.Record) { r.Name = "app" })
	_ = is.SetChat("git:github.com/o/app", []intel.Turn{{Role: "user", Text: "hi"}})

	if err := e.SyncOnce("tok"); err != nil {
		t.Fatal(err)
	}
	// Both land on the server under their own kind, neither tombstoned.
	if d, ok := f.get("project", "git:github.com/o/app"); !ok || d.Deleted {
		t.Errorf("project doc missing or tombstoned: %+v", d)
	}
	if d, ok := f.get("chat", "git:github.com/o/app"); !ok || d.Deleted {
		t.Errorf("chat doc missing or tombstoned: %+v", d)
	}
}

func TestChatSyncsAcrossDevicesLWW(t *testing.T) {
	f := newFake()
	ts := httptest.NewServer(f)
	defer ts.Close()

	mk := func(dir string, at time.Time) (*Engine, *intel.Store) {
		is, _ := intel.Open(filepath.Join(dir, "intel.json"))
		is.SetClock(func() time.Time { return at })
		e := New(cloud.New(ts.URL), filepath.Join(dir, "sync.json"), NewChat(is))
		return e, is
	}
	eA, isA := mk(t.TempDir(), time.Date(2026, 7, 24, 1, 0, 0, 0, time.UTC))
	_ = isA.SetChat("git:x", []intel.Turn{{Role: "user", Text: "fromA"}})
	if err := eA.SyncOnce("tok"); err != nil {
		t.Fatal(err)
	}
	eB, isB := mk(t.TempDir(), time.Date(2026, 7, 24, 2, 0, 0, 0, time.UTC))
	if err := eB.SyncOnce("tok"); err != nil {
		t.Fatal(err)
	}
	if got := isB.Chat("git:x"); len(got) != 1 || got[0].Text != "fromA" {
		t.Errorf("device B did not receive the chat: %+v", got)
	}
}
```

`engine_test.go` needs the `intel` import.

- [ ] **Step 2: Run it** — fails: `undefined: NewChat`.

- [ ] **Step 3: Implement the intel sources** in `source.go`:

```go
// briefSource syncs the single fleet-wide brief as doc_id "__brief__".
type briefSource struct{ store *intel.Store }

func NewBrief(is *intel.Store) Source { return &briefSource{store: is} }

func (b *briefSource) Kind() string { return "brief" }
func (b *briefSource) Degraded() error { return b.store.Degraded() }
func (b *briefSource) Reconcilable(int) error { return nil } // one fixed doc; nothing to reconcile
func (b *briefSource) LocalIDFor(docID, _ string) string { return docID }

func (b *briefSource) Snapshot(map[string]DocState) map[string]Item {
	br := b.store.Brief()
	if br.Text == "" && br.UpdatedAt == "" {
		return map[string]Item{}
	}
	payload, _ := json.Marshal(br)
	return map[string]Item{"__brief__": {Payload: payload, UpdatedAt: b.store.BriefUpdatedAt()}}
}

func (b *briefSource) Apply(_ string, it Item, _ []byte) error {
	var br intel.Brief
	if err := json.Unmarshal(it.Payload, &br); err != nil {
		return err
	}
	return b.store.SetBriefSynced(br) // sets fields without re-stamping updatedAt
}

func (b *briefSource) Remove(string, []byte) error { return b.store.SetBriefSynced(intel.Brief{}) }

// chatSource syncs each transcript under its identity as the doc_id.
type chatSource struct{ store *intel.Store }

func NewChat(is *intel.Store) Source { return &chatSource{store: is} }

func (c *chatSource) Kind() string { return "chat" }
func (c *chatSource) Degraded() error { return c.store.Degraded() }
func (c *chatSource) Reconcilable(int) error { return nil }
func (c *chatSource) LocalIDFor(docID, _ string) string { return docID }

func (c *chatSource) Snapshot(map[string]DocState) map[string]Item {
	out := map[string]Item{}
	for id, ch := range c.store.SnapshotChats() {
		payload, _ := json.Marshal(ch)
		out[id] = Item{Payload: payload, UpdatedAt: ch.UpdatedAt}
	}
	return out
}

func (c *chatSource) Apply(docID string, it Item, _ []byte) error {
	var ch intel.Chat
	if err := json.Unmarshal(it.Payload, &ch); err != nil {
		return err
	}
	return c.store.SetChatSynced(docID, ch)
}

func (c *chatSource) Remove(docID string, _ []byte) error { return c.store.ClearChat(docID) }
```

This needs three new intel-store methods that write WITHOUT re-stamping `updatedAt` (a pulled doc keeps the remote's timestamp, else LWW oscillates): `SetBriefSynced(Brief) error`, `SetChatSynced(id string, ch Chat) error`, and `SnapshotChats() map[string]Chat`. Add them to `intel.go`:

```go
// SetChatSynced writes a chat verbatim (updatedAt from the source), used when
// applying a pulled doc so the remote's timestamp is preserved for LWW.
func (s *Store) SetChatSynced(id string, ch Chat) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(ch.Turns) == 0 {
		delete(s.data.Chats, id)
		return s.saveLocked()
	}
	s.data.Chats[id] = ch
	return s.saveLocked()
}

func (s *Store) SetBriefSynced(b Brief) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Brief = b
	return s.saveLocked()
}

func (s *Store) SnapshotChats() map[string]Chat {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]Chat, len(s.data.Chats))
	for k, v := range s.data.Chats {
		out[k] = v
	}
	return out
}
```

Also add `Source` interface methods `Apply`/`Remove`/`Reconcilable`/`Snapshot` to the interface as finalized here, and make `projectSource` satisfy the same set (from Task 3). If the interface signatures shifted between Task 3 and here, reconcile them now so all three sources implement one interface.

- [ ] **Step 4: Green** — `go test ./internal/syncengine/ ./internal/intel/ -v 2>&1 | grep -E "FAIL|^ok"`. All pass.

- [ ] **Step 5: gofmt + commit**

```bash
gofmt -w internal/syncengine/source.go internal/syncengine/source_test.go internal/syncengine/engine_test.go internal/intel/intel.go
git add internal/syncengine/ internal/intel/intel.go
git commit -m "feat(sync): brief and chat sources; project and chat share a git id safely"
```

---

### Task 5: Per-source degraded skip + report

**Files:** Modify `internal/syncengine/engine.go`, `internal/syncengine/engine_test.go`

**Interfaces:**
- Produces: `func (e *Engine) SkippedDegraded() []string` (returns and clears). Removes `ErrLocalDataUnsafe`.

- [ ] **Step 1: Rewrite the two degraded tests** — replace the bodies of `TestSyncRefusesWhenStoreDegraded` (`:450`) and `TestSyncRefusesWhenStoreFileVanished` (`:485`) to assert the new contract. For the degraded one:

```go
	e2 := New(cloud.New(ts.URL), statePath, NewProject(broken, func(string) string { return "" }, broken.Degraded))
	if err := e2.SyncOnce("tok"); err != nil {
		t.Fatalf("a degraded source must skip, not error: %v", err)
	}
	if d, ok := f.get("project", "m-1"); !ok || d.Deleted {
		t.Error("a degraded store must never tombstone the server copy")
	}
	if skipped := e2.SkippedDegraded(); len(skipped) != 1 || skipped[0] != "project" {
		t.Errorf("SkippedDegraded = %v, want [project]", skipped)
	}
```

The file-vanished test similarly asserts `SyncOnce` returns nil, pushes zero project docs (compare `f.pushes` before/after, or assert the server doc is unchanged), and reports `"project"`.

- [ ] **Step 2: Run** — the two rewritten tests fail (`SkippedDegraded` undefined; `SyncOnce` still returns `ErrLocalDataUnsafe`).

- [ ] **Step 3: Implement the policy** — in `engine.go`:
  - Add `skippedDegraded []string` to `Engine` and:

```go
// SkippedDegraded returns and clears the kinds skipped last cycle because their
// source was unreadable, so the UI can show a paused pill without the whole
// sync aborting.
func (e *Engine) SkippedDegraded() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	v := e.skippedDegraded
	e.skippedDegraded = nil
	return v
}
```

  - In `SyncOnce`, reset `e.skippedDegraded = nil` at the top. For each source: `if err := src.Degraded(); err != nil { e.skippedDegraded = append(e.skippedDegraded, src.Kind()); continue }` and likewise skip its `Reconcilable` failure (append + continue). In the pull loop, if the target source is degraded or unknown, skip that doc.
  - Delete `ErrLocalDataUnsafe` and its doc comment.

- [ ] **Step 4: Green** — `go test ./internal/syncengine/ -v 2>&1 | grep -E "FAIL|^ok"`. All pass, including the ten unchanged behavior tests.

- [ ] **Step 5: gofmt + commit**

```bash
gofmt -w internal/syncengine/engine.go internal/syncengine/engine_test.go
git add internal/syncengine/
git commit -m "feat(sync): skip a degraded source and report it instead of aborting the cycle"
```

---

### Task 6: Wire the app and the paused pill

**Files:** Modify `app.go`, `authsync.go`

**Interfaces:**
- Consumes: `NewProject`, `NewBrief`, `NewChat`, `SkippedDegraded`.

- [ ] **Step 1: Build the engine from sources** — in `app.go` `NewApp`, replace the `syncengine.New(...)` call (`app.go:136`):

```go
	eng := syncengine.New(cl, syncPath,
		syncengine.NewProject(st, func(path string) string {
			u, _ := git.RemoteURL(git.ExecRunner{}, path)
			return u
		}, st.Degraded),
		syncengine.NewBrief(intelStore),
		syncengine.NewChat(intelStore),
	)
```

- [ ] **Step 2: Move the paused surfacing** — in `authsync.go`, delete the `ErrLocalDataUnsafe` branch (`:386-389`) and the `ErrLocalDataUnsafe` no-retry condition (`:470-474`, leaving the rest of that condition intact). After `a.setSyncState("synced", "")` (`:397`), before the lost/remote block:

```go
	if skipped := a.engine.SkippedDegraded(); len(skipped) > 0 {
		a.setSyncState("paused", "some local data is unreadable; its sync is paused: "+strings.Join(skipped, ", "))
		return nil
	}
```

Confirm `strings` is imported in `authsync.go` (it is used elsewhere; if not, add it). Remove the now-unused `syncengine.ErrLocalDataUnsafe` references and the `errors` import if it becomes unused (it will still be used for `cloud.ErrRefreshFailed`, so keep it).

- [ ] **Step 3: Verify** — `go build ./... && go vet ./... && go test ./...` — clean, no FAIL. Grep confirms the sentinel is gone:

```bash
grep -rn "ErrLocalDataUnsafe" --include=*.go . ; echo "exit: expect no matches"
```

- [ ] **Step 4: gofmt + commit**

```bash
gofmt -w app.go authsync.go
git add app.go authsync.go
git commit -m "feat(app): sync intel alongside projects; paused pill from SkippedDegraded"
```

---

### Task 7: Whole-suite verification and ship

- [ ] **Step 1** — `cd frontend && npm ci && npm run build && cd .. && go build ./... && go vet ./... && go test ./...` — clean, no FAIL.
- [ ] **Step 2** — gofmt diff on every touched Go file's LF blob: zero bytes.

```bash
for f in internal/intel/intel.go internal/intel/intel_test.go internal/syncengine/source.go internal/syncengine/source_test.go internal/syncengine/state.go internal/syncengine/state_test.go internal/syncengine/engine.go internal/syncengine/engine_test.go app.go authsync.go; do
  echo "$f: $(git show HEAD:$f | gofmt -d | wc -c)"; done
```

- [ ] **Step 3** — CHANGELOG `[Unreleased]`, under Changed, extend the intel line:
  `- The AI brief and chat transcripts sync across your devices (last-write-wins), alongside projects.`
- [ ] **Step 4** — `wails build`, launch, confirm by hand: sign in on one machine, brief/chat appear on a second signed-in machine after a sync; a corrupt `intel.json` shows the paused pill but projects still sync.
- [ ] **Step 5** — push, confirm the three `desktop` checks are green, open a PR, merge once green.

---

## Self-Review

- **Spec coverage:** §1 store timestamps → Task 1; §2 Source interface + project/brief/chat sources → Tasks 3-4; §3 nested state + migration → Task 2, engine loop → Tasks 3+5, degraded report → Task 5; §4 paused wiring → Task 6. All covered.
- **Type consistency:** `Item{Payload,UpdatedAt}`, `Source` with `Kind/Snapshot/Degraded/Apply/Remove/Reconcilable`, `NewProject/NewBrief/NewChat`, `SkippedDegraded() []string`, nested `State.Docs map[string]map[string]DocState`, `intel.Chat{Turns,UpdatedAt}`, `SetChatSynced/SetBriefSynced/SnapshotChats` used consistently across tasks. The `Source` interface is finalized in Task 4 Step 3 — Task 3 must adopt the same signatures (flagged there).
- **Placeholder scan:** the one soft spot is Task 3's "move verbatim" refactor note; it is explicit that the ten behavior tests are the acceptance gate rather than leaving the move underspecified. No TBDs.
- **Risk:** Task 3 is the dangerous one; it is structured so the assertions of the ten behavior tests do not change, making any behavior drift a test failure.
