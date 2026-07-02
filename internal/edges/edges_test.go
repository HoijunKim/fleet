package edges

import (
	"os"
	"path/filepath"
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

func TestMalformedFileIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edges.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("corrupt file must not error: %v", err)
	}
	if len(s.List()) != 0 {
		t.Error("corrupt file must yield empty store")
	}
}
