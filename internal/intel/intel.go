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
