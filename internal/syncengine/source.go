package syncengine

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/hoijun/fleet/internal/intel"
	"github.com/hoijun/fleet/internal/store"
)

// Item is one syncable document's current local state. LocalID is the key the
// source stores it under, which can differ from the doc_id (a code project's
// doc_id is "git:<remote>" but its local key is the repo path).
type Item struct {
	LocalID   string
	Payload   []byte
	UpdatedAt string
}

// Source is one family of syncable documents. The engine drives push, tombstone
// and pull through it, so each source owns its snapshot, its live-set and its
// degraded policy. A source serves exactly one kind (pull routes by kind).
type Source interface {
	Kind() string
	// Snapshot returns the source's current docs keyed by doc_id. prev is the
	// engine's persisted state slice for this kind, which the project source
	// consults to keep a detached record's doc_id stable; intel ignores it.
	Snapshot(prev map[string]DocState) map[string]Item
	// Degraded reports that this source's store is unreadable; the engine skips
	// it entirely this cycle so it can propagate no emptiness.
	Degraded() error
	// Reconcilable guards the tombstone loop: a non-nil return (given the number
	// of docs still tracked for this kind) means the snapshot cannot be trusted
	// to derive a live-set, so nothing is tombstoned from it this cycle.
	Reconcilable(tracked int) error
	// Apply upserts a pulled doc under localID. Remove deletes it.
	Apply(localID string, payload []byte) error
	Remove(localID string) error
	// BacksUpClobbered reports whether a pulled overwrite/delete of an unsynced
	// local edit should be backed up first (projects yes, intel no).
	BacksUpClobbered() bool
	// DisplayName extracts a human name from a payload for the backup record.
	DisplayName(payload []byte) string
}

// --- project source: today's engine logic behind the interface ---------------

type projectSource struct {
	store    *store.Store
	remoteOf func(string) string
	degraded func() error
}

// NewProject wraps the PM store as a sync source. remoteOf resolves a code
// project's git remote from its local id; degraded reports whether the store
// failed to load (nil = always healthy).
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

func (p *projectSource) Snapshot(prev map[string]DocState) map[string]Item {
	out := map[string]Item{}
	for localID, rec := range p.store.Snapshot() {
		remote := ""
		if !rec.Manual && p.remoteOf != nil {
			remote = p.remoteOf(localID)
		}
		id := DocID(localID, rec, remote)
		// A detached record (a pulled code-project doc with no local repo on this
		// machine) is stored under its own doc_id as the local key. Keep that
		// identity instead of re-deriving one, so it is neither re-pushed under a
		// fresh "local:" id nor wrongly tombstoned under its original id.
		if ds, ok := prev[localID]; ok && ds.LocalID == localID {
			id = localID
		}
		payload, err := json.Marshal(rec)
		if err != nil {
			continue
		}
		out[id] = Item{LocalID: localID, Payload: payload, UpdatedAt: rec.UpdatedAt}
	}
	return out
}

// Reconcilable refuses to derive a live-set from an empty snapshot whose backing
// file is gone: a real delete leaves projects.json holding an empty map, so a
// missing file means the data went somewhere fleet did not send it (a
// half-restored machine). Tombstoning from that would push the loss to every
// device. An empty snapshot WITH the file present (the last project was deleted)
// is fine and returns nil.
func (p *projectSource) Reconcilable(tracked int) error {
	if len(p.store.Snapshot()) == 0 && p.storeFileMissing() && tracked > 0 {
		return fmt.Errorf("%d synced projects are tracked, but projects.json is gone", tracked)
	}
	return nil
}

func (p *projectSource) storeFileMissing() bool {
	path := p.store.Path()
	if path == "" {
		return false // an in-memory store has no file to lose
	}
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}

func (p *projectSource) Apply(localID string, payload []byte) error {
	var rec store.Record
	if err := json.Unmarshal(payload, &rec); err != nil {
		return err
	}
	return p.store.Put(localID, rec)
}

func (p *projectSource) Remove(localID string) error { return p.store.Delete(localID) }

func (p *projectSource) BacksUpClobbered() bool { return true }

func (p *projectSource) DisplayName(payload []byte) string {
	var rec store.Record
	if json.Unmarshal(payload, &rec) == nil {
		return rec.Name
	}
	return ""
}

// --- intel sources -----------------------------------------------------------

// briefSource syncs the single fleet-wide brief as doc_id "__brief__".
type briefSource struct{ store *intel.Store }

// NewBrief wraps the brief in the intel store as a sync source.
func NewBrief(is *intel.Store) Source { return &briefSource{store: is} }

func (b *briefSource) Kind() string              { return "brief" }
func (b *briefSource) Degraded() error           { return b.store.Degraded() }
func (b *briefSource) Reconcilable(int) error    { return nil }
func (b *briefSource) BacksUpClobbered() bool    { return false }
func (b *briefSource) DisplayName([]byte) string { return "" }

const briefDocID = "__brief__"

func (b *briefSource) Snapshot(map[string]DocState) map[string]Item {
	br := b.store.Brief()
	if br.Text == "" && br.UpdatedAt == "" {
		return map[string]Item{}
	}
	payload, err := json.Marshal(br)
	if err != nil {
		return map[string]Item{}
	}
	return map[string]Item{briefDocID: {LocalID: briefDocID, Payload: payload, UpdatedAt: br.UpdatedAt}}
}

func (b *briefSource) Apply(_ string, payload []byte) error {
	var br intel.Brief
	if err := json.Unmarshal(payload, &br); err != nil {
		return err
	}
	return b.store.SetBriefSynced(br)
}

func (b *briefSource) Remove(string) error { return b.store.SetBriefSynced(intel.Brief{}) }

// chatSource syncs each transcript under its identity as the doc_id.
type chatSource struct{ store *intel.Store }

// NewChat wraps the chats in the intel store as a sync source.
func NewChat(is *intel.Store) Source { return &chatSource{store: is} }

func (c *chatSource) Kind() string              { return "chat" }
func (c *chatSource) Degraded() error           { return c.store.Degraded() }
func (c *chatSource) Reconcilable(int) error    { return nil }
func (c *chatSource) BacksUpClobbered() bool    { return false }
func (c *chatSource) DisplayName([]byte) string { return "" }

func (c *chatSource) Snapshot(map[string]DocState) map[string]Item {
	out := map[string]Item{}
	for id, ch := range c.store.SnapshotChats() {
		payload, err := json.Marshal(ch)
		if err != nil {
			continue
		}
		out[id] = Item{LocalID: id, Payload: payload, UpdatedAt: ch.UpdatedAt}
	}
	return out
}

func (c *chatSource) Apply(localID string, payload []byte) error {
	var ch intel.Chat
	if err := json.Unmarshal(payload, &ch); err != nil {
		return err
	}
	return c.store.SetChatSynced(localID, ch)
}

func (c *chatSource) Remove(localID string) error { return c.store.ClearChat(localID) }
