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

// cloneRecord deep-copies the slice fields (Tags, Tasks) so callers cannot
// mutate the store's internal state without going through Put.
func cloneRecord(r Record) Record {
	if r.Tags != nil {
		r.Tags = append([]string(nil), r.Tags...)
	}
	if r.Tasks != nil {
		r.Tasks = append([]Task(nil), r.Tasks...)
	}
	return r
}

// Snapshot returns a deep copy of all records (safe to mutate by the caller).
func (s *Store) Snapshot() map[string]Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]Record, len(s.records))
	for k, v := range s.records {
		out[k] = cloneRecord(v)
	}
	return out
}

// Get returns a deep copy of the record for id.
func (s *Store) Get(id string) (Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[id]
	if !ok {
		return Record{}, false
	}
	return cloneRecord(r), true
}

// Put sets the record for id and saves atomically. The whole operation holds
// the write lock, so concurrent Put/Delete calls serialize their file I/O.
func (s *Store) Put(id string, r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[id] = cloneRecord(r)
	return s.saveLocked()
}

// Delete removes id and saves atomically (under the write lock).
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, id)
	return s.saveLocked()
}

// saveLocked writes the store to disk atomically (temp file + rename). The
// caller MUST already hold s.mu (write lock); this serializes all disk writes.
func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.records, "", "  ")
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
