package edges

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "edges.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAddListRoundTrip(t *testing.T) {
	s := newStore(t)
	e, err := s.Add("/a", "/b", "http", "calls it")
	if err != nil {
		t.Fatal(err)
	}
	if e.ID == "" || e.From != "/a" || e.To != "/b" || e.Kind != "http" || e.Note != "calls it" {
		t.Fatalf("bad edge %+v", e)
	}
	list := s.List()
	if len(list) != 1 || list[0].ID != e.ID {
		t.Fatalf("list=%+v", list)
	}
}

func TestAddRejectsInvalid(t *testing.T) {
	s := newStore(t)
	if _, err := s.Add("", "/b", "http", ""); err == nil {
		t.Error("empty from must error")
	}
	if _, err := s.Add("/a", "", "http", ""); err == nil {
		t.Error("empty to must error")
	}
	if _, err := s.Add("/a", "/a", "http", ""); err == nil {
		t.Error("self-edge must error")
	}
	if _, err := s.Add("/a", "/b", "bogus", ""); err == nil {
		t.Error("invalid kind must error")
	}
	if len(s.List()) != 0 {
		t.Error("no invalid edge should persist")
	}
}

func TestRemove(t *testing.T) {
	s := newStore(t)
	e, _ := s.Add("/a", "/b", "db", "")
	if err := s.Remove("nope"); err != nil {
		t.Errorf("removing missing id must be a no-op, got %v", err)
	}
	if err := s.Remove(e.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 0 {
		t.Error("edge not removed")
	}
}

func TestPersistAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edges.json")
	s1, _ := Open(path)
	e, _ := s1.Add("/a", "/b", "related", "note")
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	list := s2.List()
	if len(list) != 1 || list[0].ID != e.ID || list[0].Note != "note" {
		t.Fatalf("reopened list=%+v", list)
	}
}

func TestUniqueIDs(t *testing.T) {
	s := newStore(t)
	a, _ := s.Add("/a", "/b", "http", "")
	b, _ := s.Add("/a", "/c", "http", "")
	if a.ID == b.ID {
		t.Error("ids must be unique")
	}
}

// A malformed file must be reported, not shrugged off: the store falls back to
// an empty slice, and a caller that never learns of the failure would persist
// that emptiness over the user's real edges on the next Add.
func TestMalformedFileIsQuarantinedAndReported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edges.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err == nil {
		t.Fatal("corrupt file must return an error")
	}
	if len(s.List()) != 0 {
		t.Error("corrupt file must yield empty store")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("corrupt file must be moved aside, not left in place")
	}
	found := false
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "edges.json.corrupt-") {
			data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			if string(data) != "{not json" {
				t.Errorf("quarantined bytes altered: %q", data)
			}
			found = true
		}
	}
	if !found {
		t.Error("corrupt bytes must survive in a .corrupt-* file")
	}
	// Quarantine leaves nothing at path to clobber, so writing is safe again.
	if _, err := s.Add("/a", "/b", "http", ""); err != nil {
		t.Errorf("Add after quarantine must succeed: %v", err)
	}
}

// An unreadable (not unparseable) file is NOT quarantined - renaming is as
// likely to fail, and the bytes may be perfectly good - so the store must
// refuse to write instead, or it would destroy edges it never loaded.
func TestUnreadableFileRefusesToPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edges.json")
	// A directory at the edges path is readable-as-a-name but not as a file, on
	// every OS, which makes it a portable stand-in for an I/O failure.
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err == nil {
		t.Fatal("unreadable file must return an error")
	}
	if _, err := s.Add("/a", "/b", "http", ""); err == nil {
		t.Error("Add must refuse to write over data that failed to load")
	}
}
