# Tier 4d - Intel Store Design (relocate, no sync yet)

**Goal:** move the AI brief and repo/fleet chats out of `localStorage` into a
Go-backed store keyed by a stable repo identity, so they survive a cleared
browser store, land in the data export, and are ready to sync.

This is backlog item 3, part A. The backend spec named it the prerequisite for
intel sync: "after relocating intel from `localStorage` into a Go-backed store
keyed by a stable repo identity"
(`specs/2026-07-09-fleet-backend-spine-design.md:278-279`). Part B - teaching the
sync engine to carry the intel kinds across devices - is a separate spec that
depends on this one.

## What lives in localStorage today

- **Brief** (`Today.svelte:129-162`): one fleet-wide "today" briefing, three keys
  - `fleet.brief` = `{text, at}`, `fleet.briefLang`, `fleet.briefAutoDate`.
- **Chat** (`agentSession.ts:43-55`, `RepoChat.svelte:72-97`): per-identity turn
  lists under `fleet.chat:<path>`, plus the fleet-wide `fleet.chat:__fleet__`.
  Turns are `{role, text}`, capped at the last 20. The key is the LOCAL PATH,
  which is exactly why it cannot sync: the same repo is a different path on
  another machine.

Both files carry duplicated `chatKey`/`loadChat`/`saveChat` helpers. This tier
removes both copies in favor of bindings.

## 1. `internal/intel` (new package)

A file-backed store next to `projects.json`, following the `internal/store`
pattern exactly: atomic write via `fileguard`, a read-only degraded mode that
quarantines unparseable bytes rather than overwriting them (the tier-3d integrity
rule), writes refused while degraded.

```go
type Turn struct {
	Role string `json:"role"` // "user" | "assistant"
	Text string `json:"text"`
}

type Brief struct {
	Text string `json:"text"`
	At   string `json:"at"`
	Lang string `json:"lang"`
}

// Data is the whole intel document: one global brief, chats keyed by identity.
type Data struct {
	Brief Brief             `json:"brief"`
	Chats map[string][]Turn `json:"chats"`
}
```

The brief stays a single global blob - it is the fleet-wide today briefing, one
per user, matching the current UI. Chats are a map from identity to turn list.

Store API mirrors `store.Store`:

```go
func Open(path string) (*Store, error)          // missing file -> empty; bad file -> read-only, quarantined
func (s *Store) Brief() Brief
func (s *Store) SetBrief(b Brief) error
func (s *Store) Chat(id string) []Turn
func (s *Store) SetChat(id string, turns []Turn) error   // caps to last chatCap
func (s *Store) ClearChat(id string) error
func (s *Store) Snapshot() Data                 // for export
func (s *Store) Degraded() error
```

`chatCap = 20` is enforced in `SetChat`, not the caller, so the cap holds no
matter which binding writes. A `SetChat` with an empty slice deletes the key
(the same effect as `ClearChat`), so an emptied chat does not linger as `[]`.

## 2. Stable chat identity

The identity is computed on the Go side from the repo path, using the SAME
convention as project doc-ids (`syncengine/state.go:72` `DocID`), so part B can
reuse it without re-keying:

```go
func ChatID(runner git.Runner, path string) string
```

- `path == "__fleet__"` -> `"__fleet__"` (the fleet-wide chat, a fixed id).
- a repo with a remote -> `"git:" + git.NormalizeRemote(remote)` - stable across
  machines, so part B syncs it.
- a repo with no remote -> `"local:" + shortHash(base(path))` - persists locally
  but is machine-specific; part B will not sync these, and that is stated, not
  hidden.

`git.RemoteURL(runner, path)` already resolves the remote (`app.go:130`,
`:1550`), and `git.NormalizeRemote` already strips credentials/suffixes and
lowercases (`internal/git`). The frontend never derives an identity again: it
passes the path (or `"__fleet__"`), and the binding maps it. This is the same
principle as tier 4c's ours/theirs swap - identity logic lives in exactly one
place.

## 3. Bindings

```go
func (a *App) GetBrief() intel.Brief
func (a *App) SaveBrief(text, at, lang string) string   // errMsg
func (a *App) GetChat(path string) []intel.Turn
func (a *App) SaveChat(path string, turns []intel.Turn) string
func (a *App) ClearChat(path string) string
```

`GetChat`/`SaveChat`/`ClearChat` take the repo path (or `"__fleet__"`) and call
`ChatID` internally. The mutating three return `errMsg(...)` like every other
binding. A save while the store is degraded returns the degraded error rather
than silently dropping the turns - the caller already surfaces binding errors.

`agentSession.ts` and `RepoChat.svelte` drop their `chatKey`/`loadChat`/
`saveChat` helpers and call the bindings. `Today.svelte` reads `GetBrief` on
mount and calls `SaveBrief` where it currently writes `fleet.brief` /
`fleet.briefLang`. `fleet.briefAutoDate` (a per-day "already auto-ran" guard, not
user data) stays in `localStorage`: it is device-local by nature and not worth a
store round-trip.

## 4. One-time migration

On the first `GetBrief`/`GetChat` after this ships, the store is empty but
`localStorage` may hold the old data. A migration runs once, in the frontend,
before the store becomes authoritative:

- Read `fleet.brief`, `fleet.briefLang`, every `fleet.chat:*` key.
- Write each into the store through the bindings, skipping any key the store
  already has (so a re-run cannot clobber newer store data).
- Remove the migrated `localStorage` keys.
- Guard the whole thing behind a `fleet.intelMigrated` flag so it runs at most
  once, and wrap it so a failure leaves `localStorage` intact (non-fatal): the
  old data is not destroyed until it is safely in the store.

The migration lives in one function called from app startup (`App.svelte`
`onMount`), not scattered across the three components, so its ordering against
the first store read is explicit.

## 5. Export includes intel

`writeExport` (`app.go`) currently marshals `a.store.Snapshot()` as the bare
export body. It becomes:

```go
json.MarshalIndent(struct {
	Projects map[string]store.Record `json:"projects"`
	Intel    intel.Data              `json:"intel"`
}{a.store.Snapshot(), a.intel.Snapshot()}, "", "  ")
```

Nothing consumes the export yet (import is backlog item 4), so the shape change
is free. The brief and chats now travel with a backup.

## Testing

- **Go unit** (`internal/intel/intel_test.go`): round-trip brief and chats;
  `SetChat` caps to 20 and deletes on empty; a corrupt file opens read-only,
  quarantines the bytes, and refuses writes (the tier-3d contract); `ChatID`
  returns `git:` for a remote repo, `local:` for one without, and `__fleet__`
  passes through.
- **Go unit** (`app_test.go`): `GetChat`/`SaveChat` round-trip through a real
  temp repo (remote set) and land under the `git:` identity; `writeExport`
  output parses to an object with both `projects` and `intel`.
- **Frontend** (`vitest`): the migration copies localStorage into the store via
  mocked bindings, skips keys the store already has, sets the migrated flag, and
  is a no-op on the second run; chat load/save go through the bindings.

## Out of scope (part B and later)

Syncing the intel store across devices (the sync engine still carries only
`kind:"project"`), importing an export, per-repo briefs, and any change to the
brief's content or the agent itself. This tier only moves where intel is stored
and establishes the identity part B will sync on.
