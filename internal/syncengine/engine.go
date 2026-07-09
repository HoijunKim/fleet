package syncengine

import (
	"encoding/json"
	"strings"
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

	mu           sync.Mutex
	state        State
	loaded       bool
	lastConflict bool
}

// New builds an Engine. remoteOf resolves a code project's git remote URL from
// its local id (repo path); return "" when there is no remote.
func New(st *store.Store, client *cloud.Client, statePath string, remoteOf func(string) string) *Engine {
	return &Engine{
		store:     st,
		client:    client,
		statePath: statePath,
		remoteOf:  remoteOf,
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
		results, cursor, err := e.client.Push(dirty, access)
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
		if cursor > e.state.Cursor {
			e.state.Cursor = cursor
		}
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
			if err := e.store.Delete(localID); err != nil {
				return err
			}
		} else {
			var rec store.Record
			if err := json.Unmarshal(d.Payload, &rec); err != nil {
				return err
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
	if !strings.HasPrefix(d.DocID, "git:") && !strings.HasPrefix(d.DocID, "local:") {
		return d.DocID
	}
	return d.DocID
}
