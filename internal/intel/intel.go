// Package intel persists fleet's AI intelligence - the fleet-wide brief and the
// per-identity chat transcripts - as a single JSON file. It follows the same
// integrity contract as internal/store: a corrupt file opens read-only with its
// bytes quarantined, and every write is refused until the data is trusted again,
// so an empty fallback can never overwrite the user's real intel.
package intel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

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
	At   string `json:"at"` // the display string the UI shows
	Lang string `json:"lang"`
	// UpdatedAt is an RFC3339Nano stamp for last-write-wins sync, distinct from
	// At (which is a human display string and not comparable).
	UpdatedAt string `json:"updatedAt"`
}

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

// Data is the whole intel document.
type Data struct {
	Brief Brief           `json:"brief"`
	Chats map[string]Chat `json:"chats"`
}

// Store is a concurrency-safe, file-backed intel document.
type Store struct {
	path        string
	mu          sync.RWMutex
	data        Data
	now         func() time.Time // timestamp source; overridable in tests
	loadErr     error            // set when the file existed but could not be read/parsed
	quarantined string           // where unparseable bytes were moved, if they were
}

// Open loads the store. A missing file yields an empty store with no error. A
// present-but-unparseable file yields a read-only store: the bytes are
// quarantined, Degraded reports the failure, and writes are refused.
func Open(path string) (*Store, error) {
	s := &Store{path: path, now: time.Now, data: Data{Chats: map[string]Chat{}}}
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
		d.Chats = map[string]Chat{}
	}
	s.data = d
	return s, nil
}

// SetClock overrides the timestamp source (tests inject a fixed clock).
func (s *Store) SetClock(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

func (s *Store) stamp() string { return s.now().UTC().Format(time.RFC3339Nano) }

// Brief returns the current brief (zero value when unset).
func (s *Store) Brief() Brief {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Brief
}

// BriefUpdatedAt returns the brief's last-change timestamp, for sync LWW.
func (s *Store) BriefUpdatedAt() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Brief.UpdatedAt
}

// SetBrief replaces the brief, stamping a fresh updatedAt for a local edit.
func (s *Store) SetBrief(b Brief) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b.UpdatedAt = s.stamp()
	s.data.Brief = b
	return s.saveLocked()
}

// SetBriefSynced writes a brief verbatim (updatedAt from the source), used when
// applying a pulled doc so the remote's timestamp is preserved for LWW.
func (s *Store) SetBriefSynced(b Brief) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Brief = b
	return s.saveLocked()
}

// Chat returns a copy of the identity's transcript (nil-safe, empty when unset).
func (s *Store) Chat(id string) []Turn {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Turn(nil), s.data.Chats[id].Turns...)
}

// ChatUpdatedAt returns the chat's last-change timestamp, for sync LWW.
func (s *Store) ChatUpdatedAt(id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Chats[id].UpdatedAt
}

// SetChat replaces an identity's transcript, capped to the last chatCap turns,
// stamping a fresh updatedAt for a local edit. An empty transcript deletes the
// key so it does not linger as [].
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

// ClearChat removes an identity's transcript.
func (s *Store) ClearChat(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data.Chats, id)
	return s.saveLocked()
}

// SnapshotChats returns a shallow copy of the chats map (values are immutable
// Chat structs), for the sync engine to enumerate.
func (s *Store) SnapshotChats() map[string]Chat {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]Chat, len(s.data.Chats))
	for k, v := range s.data.Chats {
		out[k] = v
	}
	return out
}

// Snapshot returns a deep copy of the whole document (for export).
func (s *Store) Snapshot() Data {
	s.mu.RLock()
	defer s.mu.RUnlock()
	chats := make(map[string]Chat, len(s.data.Chats))
	for k, v := range s.data.Chats {
		chats[k] = v
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
