// Package store persists fleet's project-management data (tasks, deadlines,
// notes, status, priority) as a single JSON file, keyed by project id.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Task is one checklist item on a project.
type Task struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
	Due   string `json:"due"`
}

// Record is the stored project-management data for one project id. For code
// projects the id is the repo path and Name is left empty (the scan supplies
// it); for manual projects Manual is true and Name is authoritative.
type Record struct {
	Manual   bool     `json:"manual"`
	Name     string   `json:"name"`
	Status   string   `json:"status"`
	Priority int      `json:"priority"`
	Deadline string   `json:"deadline"`
	Notes    string   `json:"notes"`
	Tags     []string `json:"tags"`
	Tasks    []Task   `json:"tasks"`
}

// Store is a concurrency-safe, file-backed map of id -> Record.
type Store struct {
	path    string
	mu      sync.RWMutex
	records map[string]Record
}

// Open loads the store at path. A missing file yields an empty store with no
// error. A corrupt file yields an empty (usable) store AND a non-nil error.
func Open(path string) (*Store, error) {
	s := &Store{path: path, records: map[string]Record{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}
	var recs map[string]Record
	if err := json.Unmarshal(data, &recs); err != nil {
		return s, err
	}
	if recs != nil {
		s.records = recs
	}
	return s, nil
}

// Snapshot returns a copy of all records (safe to mutate by the caller).
func (s *Store) Snapshot() map[string]Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]Record, len(s.records))
	for k, v := range s.records {
		out[k] = v
	}
	return out
}

// Get returns the record for id.
func (s *Store) Get(id string) (Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[id]
	return r, ok
}

// Put sets the record for id and saves atomically.
func (s *Store) Put(id string, r Record) error {
	s.mu.Lock()
	s.records[id] = r
	s.mu.Unlock()
	return s.save()
}

// Delete removes id and saves atomically.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	delete(s.records, id)
	s.mu.Unlock()
	return s.save()
}

// save writes the store to disk atomically (temp file + rename).
func (s *Store) save() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.records, "", "  ")
	s.mu.RUnlock()
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
