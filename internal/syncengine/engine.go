package syncengine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hoijun/fleet/internal/cloud"
	"github.com/hoijun/fleet/internal/store"
)

// Engine syncs the local PM store against the backend. All local domain logic
// stays here; the server is a dumb versioned document store.
type Engine struct {
	store     *store.Store
	client    *cloud.Client
	statePath string
	remoteOf  func(path string) string
	degraded  func() error

	mu            sync.Mutex
	state         State
	loaded        bool
	lastConflict  bool
	lostLocalEdit string // "", "overwritten", or "deleted"
}

// ErrLocalDataUnsafe aborts a sync cycle because the local store cannot be
// trusted as the source of truth. Pushing from it would tombstone the user's
// documents on the server - and, on the next pull, on every other device.
var ErrLocalDataUnsafe = errors.New("local data is unreadable; sync paused so it cannot be propagated")

// New builds an Engine. remoteOf resolves a code project's git remote URL from
// its local id (repo path); return "" when there is no remote. degraded reports
// whether the local store failed to load (nil error means healthy); a nil
// degraded is treated as always-healthy, which suits tests with a store built
// in memory.
func New(st *store.Store, client *cloud.Client, statePath string, remoteOf func(string) string, degraded func() error) *Engine {
	return &Engine{
		store:     st,
		client:    client,
		statePath: statePath,
		remoteOf:  remoteOf,
		degraded:  degraded,
		state:     State{Docs: map[string]DocState{}},
	}
}

// TookRemoteEdit returns (and clears) whether the last sync applied a remote
// change over an existing local edit.
func (e *Engine) TookRemoteEdit() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	v := e.lastConflict
	e.lastConflict = false
	return v
}

// LostLocalEdit returns (and clears) how the last sync destroyed a local
// record: "overwritten" when a newer remote clobbered UNSYNCED local changes,
// "deleted" when a remote tombstone removed the record outright, "" when
// neither happened. In both non-empty cases the local version was backed up
// (see conflictsPath) so the user can recover it. "deleted" wins when both
// occurred in one cycle: it is the less recoverable of the two, since the
// record is gone from the UI entirely.
func (e *Engine) LostLocalEdit() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	v := e.lostLocalEdit
	e.lostLocalEdit = ""
	return v
}

// Reset discards all sync bookkeeping - the in-memory doc map and the persisted
// state file - so the next SyncOnce starts from an empty cursor and treats every
// local record as new. Used after account deletion: the next sign-in is a
// different server user, and the stale "already synced" hashes would otherwise
// make SyncOnce skip pushing the local records to the new (empty) account.
func (e *Engine) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.state = State{Docs: map[string]DocState{}}
	e.loaded = false
	e.lastConflict = false
	e.lostLocalEdit = ""
	_ = os.Remove(e.statePath)
}

// storeFileMissing reports whether the store's backing file is absent. Any stat
// error other than not-exist counts as present: this only ever adds a refusal,
// so it must not fire on a transient I/O hiccup.
func (e *Engine) storeFileMissing() bool {
	p := e.store.Path()
	if p == "" {
		return false // an in-memory store has no file to lose
	}
	_, err := os.Stat(p)
	return os.IsNotExist(err)
}

// conflictsMaxBytes is the size past which the conflicts file is rotated to
// sync-conflicts.1.jsonl before the next append.
const conflictsMaxBytes = 1 << 20

// conflictsPath is where clobbered local records are appended (one JSON object
// per line) for recovery, next to the sync state file.
func (e *Engine) conflictsPath() string {
	return filepath.Join(filepath.Dir(e.statePath), "sync-conflicts.jsonl")
}

// backupConflict appends the about-to-be-overwritten local record to the
// conflicts file so a clobbered unsynced edit is recoverable, never silently
// lost. Best-effort: a write failure does not abort the sync.
func (e *Engine) backupConflict(localID string, rec store.Record, payload []byte) {
	line, err := json.Marshal(struct {
		At      string          `json:"at"`
		LocalID string          `json:"localId"`
		Name    string          `json:"name"`
		Payload json.RawMessage `json:"payload"`
	}{At: time.Now().UTC().Format(time.RFC3339), LocalID: localID, Name: rec.Name, Payload: payload})
	if err != nil {
		return
	}
	p := e.conflictsPath()
	// Rotate rather than append forever: this file is written on every clobbered
	// edit and every pulled delete, and an unbounded one is both slow to read
	// back and a poor recovery surface. One generation is kept.
	if fi, err := os.Stat(p); err == nil && fi.Size() > conflictsMaxBytes {
		_ = os.Rename(p, filepath.Join(filepath.Dir(p), "sync-conflicts.1.jsonl"))
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

// SyncOnce performs one push/pull cycle: push dirty docs (and tombstones for
// locally deleted docs), pull since the cursor, apply LWW, then persist state.
// A network error returns without writing sync.json, so local data is never
// corrupted by a failed sync.
func (e *Engine) SyncOnce(access string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.loaded {
		st, err := loadState(e.statePath)
		if err != nil {
			return err
		}
		e.state = st
		e.loaded = true
	}

	snap := e.store.Snapshot()

	// Refuse to sync from a store that is not the user's real data. The
	// tombstone loop below deletes, on the server and on every other device,
	// every tracked doc missing from this snapshot - so an empty snapshot from a
	// store that merely failed to PARSE is a total, silent, multi-device wipe.
	// Both conditions abort the whole cycle before any push: the pull half is
	// no safer, since it would apply remote records into a store whose writes
	// are refused anyway.
	if e.degraded != nil {
		if err := e.degraded(); err != nil {
			return fmt.Errorf("%w: %v", ErrLocalDataUnsafe, err)
		}
	}
	// An empty snapshot while we still track live documents is ambiguous on its
	// own: deleting your last project produces exactly that, and pausing sync
	// for it would be wrong. What is NOT ambiguous is the same emptiness with no
	// projects.json behind it - a real delete leaves the file there holding an
	// empty map, so a missing file means the data went somewhere fleet did not
	// send it (a half-restored machine, a cleaned APPDATA, a synced folder that
	// lost a race). Tombstoning from that would push the loss to every device.
	//
	// Docs already tombstoned do not count: once the user really has deleted
	// everything, their tombstones live in the state file forever, and counting
	// those would pause sync permanently for a legitimately empty store.
	if len(snap) == 0 && e.storeFileMissing() {
		tracked := 0
		for _, ds := range e.state.Docs {
			if !ds.Deleted {
				tracked++
			}
		}
		if tracked > 0 {
			return fmt.Errorf("%w: %d synced projects are tracked, but projects.json is gone", ErrLocalDataUnsafe, tracked)
		}
	}

	// Build local docs; a doc is dirty when its payload hash changed.
	var dirty []cloud.Doc
	live := map[string]bool{}
	for localID, rec := range snap {
		remote := ""
		if !rec.Manual && e.remoteOf != nil {
			remote = e.remoteOf(localID)
		}
		id := DocID(localID, rec, remote)
		// A detached record (a pulled code-project doc with no local repo on
		// this machine) is stored under its own doc_id as the local key. Keep
		// that identity instead of re-deriving one, so it is neither re-pushed
		// under a fresh "local:" id nor wrongly tombstoned under its original
		// id - the spec requires detached records be retained, never dropped
		// or duplicated.
		if ds, ok := e.state.Docs[localID]; ok && ds.LocalID == localID {
			id = localID
		}
		live[id] = true
		payload, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		h := payloadHash(payload)
		prev, ok := e.state.Docs[id]
		if ok && prev.Hash == h && !prev.Deleted {
			continue
		}
		dirty = append(dirty, cloud.Doc{Kind: "project", DocID: id, Payload: payload, UpdatedAt: rec.UpdatedAt, Deleted: false})
		prev.LocalID = localID // remember mapping for accepted pushes
		e.state.Docs[id] = prev
	}

	// Tombstones: docs we tracked that have vanished from the store.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for id, ds := range e.state.Docs {
		if live[id] || ds.Deleted {
			continue
		}
		dirty = append(dirty, cloud.Doc{Kind: "project", DocID: id, Payload: json.RawMessage("{}"), UpdatedAt: now, Deleted: true})
	}

	if len(dirty) > 0 {
		results, _, err := e.client.Push(dirty, access)
		if err != nil {
			return err
		}
		byID := make(map[string]cloud.Doc, len(dirty))
		for _, d := range dirty {
			byID[d.DocID] = d
		}
		for _, r := range results {
			if !r.Accepted {
				continue // stale push; the pull below reconciles it
			}
			d := byID[r.DocID]
			ds := e.state.Docs[r.DocID]
			ds.Hash = payloadHash(d.Payload)
			ds.UpdatedAt = d.UpdatedAt
			ds.Deleted = d.Deleted
			e.state.Docs[r.DocID] = ds
		}
		// Do NOT advance the cursor from the push response: it is the server's
		// GLOBAL max version, and adopting it when this device is behind on
		// pulls would skip remote versions we never pulled (Pull is monotonic),
		// causing silent lost updates and preventing a rejected push from ever
		// reconciling. Only the pull below advances e.state.Cursor.
	}

	docs, cursor, err := e.client.Pull(e.state.Cursor, access)
	if err != nil {
		return err
	}
	for _, d := range docs {
		localID := e.localIDForDoc(d)
		localUpdated := ""
		if rec, ok := snap[localID]; ok {
			localUpdated = rec.UpdatedAt
		} else if ds, ok := e.state.Docs[d.DocID]; ok {
			localUpdated = ds.UpdatedAt
		}
		if !newer(d.UpdatedAt, localUpdated) {
			continue // local is newer or equal: LWW keeps local
		}
		if d.Deleted {
			// A delete pulled from another device destroys the local record just
			// as thoroughly as a clobbering update does - store.Delete is a hard
			// delete with no trash - so back it up the same way. Unlike the
			// update branch there is no hash check: a tombstone carries no
			// payload to compare against, and every deleted record is worth
			// keeping, synced or not.
			if local, ok := snap[localID]; ok {
				if lp, err := json.Marshal(local); err == nil {
					e.backupConflict(localID, local, lp)
					e.lostLocalEdit = "deleted"
				}
			}
			if err := e.store.Delete(localID); err != nil {
				return err
			}
		} else {
			var rec store.Record
			if err := json.Unmarshal(d.Payload, &rec); err != nil {
				return err
			}
			// If the local record has UNSYNCED changes (its current hash differs
			// from the last-synced hash), this newer remote would clobber them.
			// Back up the local version so the loss is recoverable, not silent.
			if local, ok := snap[localID]; ok {
				if lp, err := json.Marshal(local); err == nil && payloadHash(lp) != e.state.Docs[d.DocID].Hash {
					e.backupConflict(localID, local, lp)
					if e.lostLocalEdit == "" {
						e.lostLocalEdit = "overwritten" // "deleted" outranks it
					}
				}
			}
			if err := e.store.Put(localID, rec); err != nil {
				return err
			}
		}
		if localUpdated != "" {
			e.lastConflict = true // overwrote an existing local edit
		}
		e.state.Docs[d.DocID] = DocState{LocalID: localID, Hash: payloadHash(d.Payload), UpdatedAt: d.UpdatedAt, Deleted: d.Deleted}
	}
	if cursor > e.state.Cursor {
		e.state.Cursor = cursor
	}

	return saveState(e.statePath, e.state)
}

// localIDForDoc maps a doc_id back to a local store key. A known mapping wins;
// a manual doc_id is itself the local id; an unmatched code doc is retained
// (detached) under its doc_id until a scan reconciles it.
func (e *Engine) localIDForDoc(d cloud.Doc) string {
	if ds, ok := e.state.Docs[d.DocID]; ok && ds.LocalID != "" {
		return ds.LocalID
	}
	return d.DocID
}
