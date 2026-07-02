package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenMissingIsEmpty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "projects.json")
	s, err := Open(p)
	if err != nil {
		t.Fatalf("Open missing: %v", err)
	}
	if len(s.Snapshot()) != 0 {
		t.Errorf("expected empty store, got %v", s.Snapshot())
	}
}

func TestPutGetPersists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "projects.json")
	s, _ := Open(p)
	rec := Record{Manual: true, Name: "research", Status: "active", Priority: 2,
		Tasks: []Task{{ID: "t1", Title: "read paper", Done: false}}}
	if err := s.Put("m-1", rec); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// reload from disk
	s2, err := Open(p)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, ok := s2.Get("m-1")
	if !ok || got.Name != "research" || got.Priority != 2 || len(got.Tasks) != 1 {
		t.Errorf("reloaded record wrong: %+v", got)
	}
}

func TestDeleteRemoves(t *testing.T) {
	p := filepath.Join(t.TempDir(), "projects.json")
	s, _ := Open(p)
	_ = s.Put("x", Record{Manual: true, Name: "x"})
	if err := s.Delete("x"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("x"); ok {
		t.Error("expected x deleted")
	}
	s2, _ := Open(p)
	if _, ok := s2.Get("x"); ok {
		t.Error("delete did not persist")
	}
}

func TestOpenCorruptReturnsEmptyAndError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "projects.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(p)
	if err == nil {
		t.Error("expected error on corrupt file")
	}
	if s == nil || len(s.Snapshot()) != 0 {
		t.Error("expected usable empty store on corrupt file")
	}
}

func TestSnapshotIsCopy(t *testing.T) {
	p := filepath.Join(t.TempDir(), "projects.json")
	s, _ := Open(p)
	_ = s.Put("a", Record{Manual: true, Name: "a", Tags: []string{"x"}})
	snap := s.Snapshot()
	snap["a"] = Record{Name: "MUTATED"}
	if got, _ := s.Get("a"); got.Name != "a" {
		t.Error("Snapshot must not alias internal state")
	}
}
