# Tier 4e - Intel Sync Design (part B)

**Goal:** the AI brief and chat transcripts sync across a user's devices, the same
way projects already do.

This is backlog item 3, part B. Part A (tier 4d) put intel in a Go store keyed by
a stable identity; this tier teaches the sync engine to carry that store's
documents. It is the riskiest change in this batch: `internal/syncengine` is the
one place a bug becomes silent multi-device data loss, so the design is built so
the existing engine tests keep pinning project behavior unchanged.

## The three problems part B has to solve

1. **doc_id collision.** A project for a repo with a remote has doc_id
   `git:github.com/o/r`. That repo's chat identity is the *same string*. The
   server keys documents by `(user, kind, doc_id)` so they never collide there,
   but the engine's `state.Docs` is keyed by doc_id *alone*
   (`state.go:22`, `map[string]DocState`). Two kinds sharing a doc_id would
   overwrite each other's bookkeeping. → the sync state must be keyed by
   `(kind, doc_id)`.

2. **one tombstone loop, many stores.** `SyncOnce` derives a `live` set from the
   project snapshot and tombstones every tracked doc_id not in it
   (`engine.go:232-237`). Feed intel docs through that same loop and a chat,
   absent from the project snapshot, gets tombstoned. → the live-set / tombstone
   logic must be per-source.

3. **LWW needs a comparable timestamp.** `newer()` parses RFC3339Nano
   (`state.go:96`). The brief's `at` is a locale display string
   (`Today.svelte` sets it with `toLocaleString()`), and a chat has no timestamp
   at all. → the intel store must stamp an RFC3339 `updatedAt` on every write.

## 1. Intel store gains write timestamps

`internal/intel` grows a per-document `updatedAt`, stamped by the store on every
mutation so no caller can forget it:

```go
type Chat struct {
	Turns     []Turn `json:"turns"`
	UpdatedAt string `json:"updatedAt"`
}

type Brief struct {
	Text      string `json:"text"`
	At        string `json:"at"`        // unchanged: the display string the UI shows
	Lang      string `json:"lang"`
	UpdatedAt string `json:"updatedAt"` // new: RFC3339Nano, for LWW
}

type Data struct {
	Brief Brief            `json:"brief"`
	Chats map[string]Chat  `json:"chats"`
}
```

- `SetBrief`/`SetChat` stamp `UpdatedAt` with the store's clock. The clock is an
  injectable `func() time.Time` (defaulting to `time.Now`), so tests are
  deterministic and the workflow-script clock ban is irrelevant here.
- **Back-compat unmarshal.** Tier 4d shipped `Chats map[string][]Turn`, and a
  user's `intel.json` (or a just-run localStorage migration) is in that shape. A
  custom `UnmarshalJSON` on `Chat` accepts either a bare `[]Turn` (old) or the
  `{turns, updatedAt}` object (new); an old chat loads with an empty
  `UpdatedAt`, which `newer()` treats as older-than-anything, so the first sync
  pulls the remote copy if there is one and otherwise pushes the local with a
  fresh stamp on its next edit. No data is lost either way.
- The `intel.Data` returned by `Snapshot()` (used by the export) keeps the new
  shape; the export gaining `updatedAt` fields is harmless.

A new accessor the intel source needs:

```go
func (s *Store) BriefUpdatedAt() string
func (s *Store) ChatUpdatedAt(id string) string
```

## 2. The Source interface

```go
// Item is one syncable document's current local state.
type Item struct {
	Payload   []byte
	UpdatedAt string
}

// Source is one family of syncable documents (projects, or intel). The engine
// drives push, tombstone and pull through this interface, so each source keeps
// its own snapshot, its own live-set, and its own degraded policy.
type Source interface {
	Kind() string                 // "project", "brief", "chat"
	Snapshot() map[string]Item    // doc_id -> current local item
	Degraded() error              // non-nil: skip this source entirely this cycle
	Apply(docID string, it Item) error // pull upsert
	Remove(docID string) error          // pull tombstone
	// Reconcilable guards the tombstone loop: a nil return means the snapshot can
	// be trusted to derive a live-set. The project source returns non-nil for the
	// "empty snapshot but projects.json is gone" case (a half-restored machine),
	// exactly as engine.go:185-195 does today.
	Reconcilable(tracked int) error
}
```

Two implementations:

- **`projectSource`** wraps `*store.Store` + `remoteOf` + the degraded func. Its
  `Snapshot` reproduces today's logic verbatim: `DocID(localID, rec, remote)`,
  the detached-record retention (`state.Docs[localID].LocalID == localID` keeps
  its own id, `engine.go:212-214`), and the manual/no-remote fallbacks. `Apply`
  and `Remove` unmarshal to `store.Record` and call `store.Put`/`store.Delete`,
  including the clobbered-edit and pulled-delete backups
  (`engine.go:288-316`). Because this source *is* today's code moved behind an
  interface, the existing engine tests are the proof it did not change.
- **`briefSource`** presents the brief as one doc (kind `"brief"`, doc_id
  `"__brief__"`), backed by the intel store.
- **`chatSource`** presents each chat as one doc (kind `"chat"`, doc_id = the
  identity the chat is already stored under), backed by the same intel store.

Both intel sources have no manual/detached/scan complexity: the store key *is*
the doc_id, so `Snapshot` is a direct enumeration and `Apply`/`Remove` write
straight through. They are two sources rather than one because pull routes by
kind and a source serves exactly one kind. Neither keeps a clobbered-edit backup
(a chat/brief is lower-stakes and the store already holds the newest; LWW is
enough), which the spec states rather than silently omits. Both report the same
`intel.Store.Degraded()`, so a corrupt `intel.json` skips brief and chat together
while projects sync on.

## 3. Engine changes

- **State schema.** `State.Docs` becomes `map[string]map[string]DocState` -
  kind → doc_id → state. `loadState` migrates an old flat
  `map[string]DocState`: an absent nested shape is read as the `"project"` kind
  (every doc_id an old state file holds is a project, since projects were the
  only synced kind). `saveState` always writes the nested shape.
- **Construction.** `New` takes the sources instead of a single store:

  ```go
  func New(client *cloud.Client, statePath string, sources ...Source) *Engine
  ```

  `app.go` builds a `projectSource` (from the existing store/remoteOf/degraded)
  and an `intelSource` (from the intel store) and passes both. The `remoteOf`
  and `degraded` closures move into `projectSource`.
- **SyncOnce loop.**
  - For each source whose `Degraded()` is nil: build its dirty set and
    tombstones against *its own* snapshot and *its own* slice of the state
    (`state.Docs[src.Kind()]`), push them, and record accepted pushes under
    `(kind, doc_id)`.
  - A source whose `Degraded()` is non-nil is skipped for push and tombstone
    entirely - its state slice is left untouched, so it can propagate no
    emptiness (the tombstone-count-zero invariant). Its kind is recorded in a
    `skippedDegraded` set.
  - One `Pull(cursor)` as today; each returned doc is routed to the source whose
    `Kind()` matches `doc.Kind`. A doc for a degraded (or unknown) source is
    skipped, not applied and not errored, so a degraded intel store cannot make
    a project pull fail.
  - The cursor still advances only from the pull (`engine.go:259-263`).
- **Degraded reporting.** `func (e *Engine) SkippedDegraded() []string` returns
  and clears the kinds skipped last cycle, mirroring `LostLocalEdit`/
  `TookRemoteEdit`. `SyncOnce` no longer returns `ErrLocalDataUnsafe`; the
  "paused" surfacing moves to a post-success check (see §4). The sentinel is
  removed.

## 4. Wiring the paused state

`authsync.go:378-396` currently reads `ErrLocalDataUnsafe` from `SyncOnce`'s
return to set the `"paused"` pill. With per-source skip, `SyncOnce` returns nil
on a degraded-skip, so the check moves next to the other post-success flags
(`authsync.go:398-411`):

```go
a.setSyncState("synced", "")
if skipped := a.engine.SkippedDegraded(); len(skipped) > 0 {
	a.setSyncState("paused", "some local data is unreadable; its sync is paused: "+strings.Join(skipped, ", "))
}
```

The no-retry backoff branch (`authsync.go:470-474`) keyed off `ErrLocalDataUnsafe`
is dropped: a degraded skip is no longer an error return, and the next timer tick
retries harmlessly (it re-skips until the data is fixed, pushing nothing). The
`"paused"` pill still tells the user their data needs attention.

## Testing

- **The twelve existing engine tests stay green unchanged**, except the two that
  assert a whole-cycle refusal:
  - `TestSyncRefusesWhenStoreDegraded` and `TestSyncRefusesWhenStoreFileVanished`
    are rewritten to assert the new contract: `SyncOnce` returns nil, pushes
    **zero** docs for the project kind (the safety invariant - verified by a fake
    client that records every pushed doc), and reports `"project"` from
    `SkippedDegraded()`.
  - The other ten (push-once, LWW, tombstone, offline, reset, detached,
    cursor-only-from-pull, clobbered-edit backup, tombstone-after-last-delete,
    pulled-tombstone backup) must pass with no change to their assertions. That
    is the guarantee the `projectSource` refactor preserved behavior.
- **Intel unit** (`internal/intel`): the back-compat unmarshal reads an old
  `[]Turn` chat; `SetChat`/`SetBrief` stamp `updatedAt` from an injected clock;
  `ChatUpdatedAt`/`BriefUpdatedAt` return it.
- **New engine tests**:
  - a chat and a project for the *same* `git:` identity sync independently -
    neither tombstones the other (the collision test).
  - a dirty brief and chat push once, then not again until edited (LWW).
  - a remote chat newer than local is applied; older is kept.
  - a degraded intel source skips intel but a healthy project source still
    pushes and pulls in the same cycle (the availability guarantee).
  - `loadState` migrates a flat pre-4e `sync.json` into the `project` kind.
- **app / bindings**: `NewApp` wires both sources; a smoke test that a brief
  saved through the binding appears in the project source's peer without cross-
  contamination is covered by the engine tests, so app-level testing is limited
  to construction not panicking.

## Out of scope

Real-time push (still interval/on-change polling), field-level or turn-level
merge (still whole-document LWW), a per-device chat history view, and syncing the
`fleet.briefAutoDate` guard (device-local by design). Importing an export remains
backlog item 4.
