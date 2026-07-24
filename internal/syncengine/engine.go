package syncengine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hoijun/fleet/internal/cloud"
)

// Engine syncs one or more local document sources against the backend. All local
// domain logic stays in the sources; the server is a dumb versioned document
// store keyed by (user, kind, doc_id).
type Engine struct {
	client    *cloud.Client
	statePath string
	sources   []Source

	mu              sync.Mutex
	state           State
	loaded          bool
	lastConflict    bool
	lostLocalEdit   string   // "", "overwritten", or "deleted"
	skippedDegraded []string // kinds skipped last cycle because their source was unreadable
}

// New builds an Engine over the given sources (project, brief, chat, ...). Each
// source owns its own snapshot, live-set and degraded policy; the engine drives
// push/tombstone/pull across all of them against one shared cursor.
func New(client *cloud.Client, statePath string, sources ...Source) *Engine {
	return &Engine{
		client:    client,
		statePath: statePath,
		sources:   sources,
		state:     State{Docs: map[string]map[string]DocState{}},
	}
}

// sourceFor returns the source serving a kind, or nil.
func (e *Engine) sourceFor(kind string) Source {
	for _, s := range e.sources {
		if s.Kind() == kind {
			return s
		}
	}
	return nil
}

// kindDocs returns (creating if needed) the state slice for a kind.
func (e *Engine) kindDocs(kind string) map[string]DocState {
	if e.state.Docs[kind] == nil {
		e.state.Docs[kind] = map[string]DocState{}
	}
	return e.state.Docs[kind]
}

// SkippedDegraded returns and clears the kinds skipped last cycle because their
// source was unreadable, so the UI can show a paused pill without the whole sync
// aborting.
func (e *Engine) SkippedDegraded() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	v := e.skippedDegraded
	e.skippedDegraded = nil
	return v
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
	e.state = State{Docs: map[string]map[string]DocState{}}
	e.loaded = false
	e.lastConflict = false
	e.lostLocalEdit = ""
	_ = os.Remove(e.statePath)
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
func (e *Engine) backupConflict(localID, name string, payload []byte) {
	line, err := json.Marshal(struct {
		At      string          `json:"at"`
		LocalID string          `json:"localId"`
		Name    string          `json:"name"`
		Payload json.RawMessage `json:"payload"`
	}{At: time.Now().UTC().Format(time.RFC3339), LocalID: localID, Name: name, Payload: payload})
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

	e.skippedDegraded = nil

	// snaps holds each healthy source's current docs, keyed by kind then doc_id,
	// so the pull phase can find the local item a pulled doc would overwrite.
	snaps := map[string]map[string]Item{}

	// --- push + tombstone, per source -----------------------------------------
	var dirty []cloud.Doc
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, src := range e.sources {
		kind := src.Kind()
		// A degraded source is skipped entirely - no push, no tombstone - so it
		// can propagate no emptiness. Its kind is reported so the UI can warn.
		if err := src.Degraded(); err != nil {
			e.skippedDegraded = append(e.skippedDegraded, kind)
			continue
		}
		kd := e.kindDocs(kind)
		snap := src.Snapshot(kd)
		snaps[kind] = snap

		live := map[string]bool{}
		for id, it := range snap {
			live[id] = true
			h := payloadHash(it.Payload)
			prev, ok := kd[id]
			if ok && prev.Hash == h && !prev.Deleted {
				continue
			}
			dirty = append(dirty, cloud.Doc{Kind: kind, DocID: id, Payload: it.Payload, UpdatedAt: it.UpdatedAt, Deleted: false})
			prev.LocalID = it.LocalID // remember mapping for accepted pushes
			kd[id] = prev
		}

		// Tombstones: docs tracked for this kind that vanished from the snapshot.
		// Only when the source can trust its snapshot to derive a live-set: a
		// half-restored project store must not tombstone from an empty snapshot.
		tracked := 0
		for _, ds := range kd {
			if !ds.Deleted {
				tracked++
			}
		}
		if err := src.Reconcilable(tracked); err != nil {
			e.skippedDegraded = append(e.skippedDegraded, kind)
			delete(snaps, kind)
			continue
		}
		for id, ds := range kd {
			if live[id] || ds.Deleted {
				continue
			}
			dirty = append(dirty, cloud.Doc{Kind: kind, DocID: id, Payload: json.RawMessage("{}"), UpdatedAt: now, Deleted: true})
		}
	}

	if len(dirty) > 0 {
		results, _, err := e.client.Push(dirty, access)
		if err != nil {
			return err
		}
		byKey := make(map[string]cloud.Doc, len(dirty))
		for _, d := range dirty {
			byKey[d.Kind+"\x00"+d.DocID] = d
		}
		for _, r := range results {
			d, ok := byKey[r.Kind+"\x00"+r.DocID]
			if !ok || !r.Accepted {
				continue // stale push; the pull below reconciles it
			}
			kd := e.kindDocs(d.Kind)
			ds := kd[d.DocID]
			ds.Hash = payloadHash(d.Payload)
			ds.UpdatedAt = d.UpdatedAt
			ds.Deleted = d.Deleted
			kd[d.DocID] = ds
		}
		// Do NOT advance the cursor from the push response: it is the server's
		// GLOBAL max version, and adopting it when this device is behind on
		// pulls would skip remote versions we never pulled (Pull is monotonic),
		// causing silent lost updates and preventing a rejected push from ever
		// reconciling. Only the pull below advances e.state.Cursor.
	}

	// --- pull, routed to each source by kind ----------------------------------
	docs, cursor, err := e.client.Pull(e.state.Cursor, access)
	if err != nil {
		return err
	}
	for _, d := range docs {
		src := e.sourceFor(d.Kind)
		// An unknown kind (a newer client's data) or a degraded/skipped source
		// is passed over: applying it would fail or is unwanted, and the cursor
		// still advances so it is not re-fetched forever.
		if src == nil || src.Degraded() != nil {
			continue
		}
		kd := e.kindDocs(d.Kind)
		snap := snaps[d.Kind]

		localID := d.DocID
		if ds, ok := kd[d.DocID]; ok && ds.LocalID != "" {
			localID = ds.LocalID
		}
		local, hasLocal := snap[d.DocID]
		localUpdated := ""
		if hasLocal {
			localUpdated = local.UpdatedAt
		} else if ds, ok := kd[d.DocID]; ok {
			localUpdated = ds.UpdatedAt
		}
		if !newer(d.UpdatedAt, localUpdated) {
			continue // local is newer or equal: LWW keeps local
		}
		if d.Deleted {
			// A pulled delete destroys the local record as thoroughly as a
			// clobbering update; back it up the same way (project only). No hash
			// check: a tombstone carries no payload to compare against.
			if hasLocal && src.BacksUpClobbered() {
				e.backupConflict(localID, src.DisplayName(local.Payload), local.Payload)
				e.lostLocalEdit = "deleted"
			}
			if err := src.Remove(localID); err != nil {
				return err
			}
		} else {
			// If the local record has UNSYNCED changes (its current hash differs
			// from the last-synced hash), this newer remote would clobber them.
			if hasLocal && src.BacksUpClobbered() && payloadHash(local.Payload) != kd[d.DocID].Hash {
				e.backupConflict(localID, src.DisplayName(local.Payload), local.Payload)
				if e.lostLocalEdit == "" {
					e.lostLocalEdit = "overwritten" // "deleted" outranks it
				}
			}
			if err := src.Apply(localID, d.Payload); err != nil {
				return err
			}
		}
		if localUpdated != "" {
			e.lastConflict = true // overwrote an existing local edit
		}
		kd[d.DocID] = DocState{LocalID: localID, Hash: payloadHash(d.Payload), UpdatedAt: d.UpdatedAt, Deleted: d.Deleted}
	}
	if cursor > e.state.Cursor {
		e.state.Cursor = cursor
	}

	return saveState(e.statePath, e.state)
}
