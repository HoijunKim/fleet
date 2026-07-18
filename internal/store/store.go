// Package store persists fleet's project-management data (tasks, deadlines,
// notes, status, priority) as a single JSON file, keyed by project id.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hoijun/fleet/internal/fileguard"
)

// Task is one checklist item on a project.
type Task struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Done   bool   `json:"done"`
	Status string `json:"status"`
	Due    string `json:"due"`
}

// Record is the stored project-management data for one project id. For code
// projects the id is the repo path and Name is left empty (the scan supplies
// it); for manual projects Manual is true and Name is authoritative.
type Record struct {
	Manual    bool     `json:"manual"`
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	Priority  int      `json:"priority"`
	Deadline  string   `json:"deadline"`
	Notes     string   `json:"notes"`
	Tags      []string `json:"tags"`
	Tasks     []Task   `json:"tasks"`
	UpdatedAt string   `json:"updatedAt"`
}

// Store is a concurrency-safe, file-backed map of id -> Record.
type Store struct {
	path    string
	mu      sync.RWMutex
	records map[string]Record

	// loadErr is set when the file existed but could not be read or parsed. The
	// store stays usable for READS (an empty map), but every write is refused
	// while it is set: writing would replace the user's real data with the empty
	// map we fell back to. See saveLocked.
	loadErr error
	// quarantined is where the unparseable bytes were moved, when they were.
	quarantined string
}

// Open loads the store at path. A missing file yields an empty store with no
// error. A file that exists but cannot be read or parsed yields a read-only
// store: the bytes are quarantined (never overwritten), Degraded reports the
// failure, and every write is refused until DiscardAndReset.
func Open(path string) (*Store, error) {
	s := &Store{path: path, records: map[string]Record{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		// Unreadable but present (permissions, a locked file, a backup agent
		// holding it, a descriptor limit): do NOT quarantine. The file itself is
		// very often perfectly good and the next launch will read it fine -
		// renaming it here would turn a transient failure into a file the user
		// has to go find. Refusing to write is enough; DiscardAndReset does the
		// quarantine if the user ever chooses to move on without it.
		s.loadErr = err
		return s, err
	}
	var recs map[string]Record
	if err := json.Unmarshal(data, &recs); err != nil {
		s.loadErr = fmt.Errorf("projects.json is not valid JSON: %w", err)
		if dest, qerr := fileguard.Quarantine(path); qerr == nil {
			s.quarantined = dest
		}
		return s, s.loadErr
	}
	migrate(recs)
	if recs != nil {
		s.records = recs
	}
	return s, nil
}

// migrate normalizes each task's Status/Done pair for legacy data that only
// carries the "done" bool. If Status is unset, it is derived from Done.
// Status (whether freshly derived or already present) is then the source of
// truth: Done is re-mirrored from it so the two fields cannot disagree.
// Idempotent: running it twice on already-migrated data is a no-op.
func migrate(recs map[string]Record) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for k, record := range recs {
		for i, t := range record.Tasks {
			if t.Status == "" {
				if t.Done {
					t.Status = "done"
				} else {
					t.Status = "todo"
				}
			}
			t.Done = t.Status == "done"
			record.Tasks[i] = t
		}
		if record.UpdatedAt == "" {
			record.UpdatedAt = now
		}
		recs[k] = record
	}
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

// Update atomically reads the record for id, applies fn to a copy of it, and
// saves - all under the write lock - so overlapping updates cannot lose each
// other's changes. fn receives a pointer to the current record (a zero Record
// if id is new).
func (s *Store) Update(id string, fn func(*Record)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := cloneRecord(s.records[id])
	fn(&rec)
	rec.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.records[id] = rec
	return s.saveLocked()
}

// Delete removes id and saves atomically (under the write lock).
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, id)
	return s.saveLocked()
}

// Path returns the file backing this store.
func (s *Store) Path() string { return s.path }

// Degraded returns the load failure when the store opened in read-only mode, or
// nil when it holds the real data. Callers that must not act on an empty store
// (the sync engine, above all) check this first.
func (s *Store) Degraded() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadErr
}

// Quarantined returns where the unparseable file was moved, or "" if it was not
// moved (a read failure, or a healthy store).
func (s *Store) Quarantined() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.quarantined
}

// DiscardAndReset clears the degraded flag and writes the current (empty) map,
// re-enabling writes. It is destructive by design and must only be called on an
// explicit user opt-in, never automatically: the whole point of the degraded
// state is that fleet does not decide on its own to move on without the data.
//
// "Discard" must still mean "set aside", never "erase". Open quarantines only
// what it could PARSE and fail - a file it could not READ (a permission
// problem, a backup agent holding it open) is left untouched and is very often
// perfectly good. Overwriting that file here would destroy intact data while
// the UI promises the original was kept, so it is moved aside first and the
// reset is abandoned if that cannot be done.
func (s *Store) DiscardAndReset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.quarantined == "" {
		dest, err := fileguard.Quarantine(s.path)
		if err != nil {
			return fmt.Errorf("cannot set the unreadable file aside, so it will not be replaced: %w", err)
		}
		s.quarantined = dest
	}
	s.loadErr = nil
	return s.saveLocked()
}

// saveLocked writes the store to disk atomically (temp file + rename). The
// caller MUST already hold s.mu (write lock); this serializes all disk writes.
//
// A degraded store refuses to save: s.records is the empty fallback map, not the
// user's data, so writing it would replace a file we merely failed to PARSE with
// one that is genuinely empty - turning a recoverable problem into permanent
// loss on the user's first edit.
func (s *Store) saveLocked() error {
	if s.loadErr != nil {
		return fmt.Errorf("refusing to write over unreadable data: %w", s.loadErr)
	}
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
