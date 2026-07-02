// Package edges provides a small JSON-file backed store for manual
// repo-to-repo graph edges that a user draws in the UI. These edges
// capture relationships that code cannot express, such as an HTTP
// call, a shared database, a deploy ordering, or a generic relation.
package edges

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Edge is a single manual relationship between two repos.
type Edge struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
	Note string `json:"note"`
}

// Store is a mutex-guarded, file-persisted collection of Edge values.
type Store struct {
	path  string
	mu    sync.Mutex
	edges []Edge
}

// Open loads the store from path. A missing file or a malformed file
// both yield an empty store with a nil error; the store never fails
// to open because of a corrupt scratch file on disk.
func Open(path string) (*Store, error) {
	s := &Store{path: path}

	data, err := os.ReadFile(path)
	if err != nil {
		return s, nil
	}

	var loaded []Edge
	if err := json.Unmarshal(data, &loaded); err != nil {
		return s, nil
	}

	s.edges = loaded
	return s, nil
}

// List returns a fresh copy of the stored edges. It never returns nil.
func (s *Store) List() []Edge {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Edge, len(s.edges))
	copy(out, s.edges)
	return out
}

// Add validates and appends a new edge, persists the store, and
// returns the stored edge. from/to are trimmed of surrounding
// whitespace before validation. Invalid input returns a non-nil
// error and does not persist.
func (s *Store) Add(from, to, kind, note string) (Edge, error) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	kind = strings.TrimSpace(kind)
	note = strings.TrimSpace(note)

	if from == "" {
		return Edge{}, fmt.Errorf("edges: from must not be empty")
	}
	if to == "" {
		return Edge{}, fmt.Errorf("edges: to must not be empty")
	}
	if from == to {
		return Edge{}, fmt.Errorf("edges: from and to must differ")
	}
	if !AllowedKind(kind) {
		return Edge{}, fmt.Errorf("edges: invalid kind %q", kind)
	}

	id, err := newID()
	if err != nil {
		return Edge{}, fmt.Errorf("edges: generate id: %w", err)
	}

	e := Edge{ID: id, From: from, To: to, Kind: kind, Note: note}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.edges = append(s.edges, e)
	if err := s.persistLocked(); err != nil {
		return Edge{}, err
	}

	return e, nil
}

// Remove deletes the edge with the given id and persists the store.
// Removing a missing id is a no-op and returns a nil error.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	for i, e := range s.edges {
		if e.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return nil
	}

	s.edges = append(s.edges[:idx], s.edges[idx+1:]...)
	return s.persistLocked()
}

// AllowedKind reports whether kind is one of the recognized edge kinds.
func AllowedKind(kind string) bool {
	switch kind {
	case "http", "db", "deploy-after", "related":
		return true
	default:
		return false
	}
}

// persistLocked marshals the current edges and atomically writes them
// to s.path. Callers must hold s.mu.
func (s *Store) persistLocked() error {
	data, err := json.MarshalIndent(s.edges, "", "  ")
	if err != nil {
		return fmt.Errorf("edges: marshal: %w", err)
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("edges: write temp file: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("edges: rename temp file: %w", err)
	}

	return nil
}

// newID generates a random 16-character hex identifier.
func newID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
