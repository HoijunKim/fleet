package store

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
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

func TestConcurrentPutNoError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "projects.json")
	s, _ := Open(p)
	var wg sync.WaitGroup
	errs := make(chan error, 50)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			errs <- s.Put("id-"+strconv.Itoa(n), Record{Manual: true, Name: "n"})
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatalf("concurrent Put returned error: %v", e)
		}
	}
	if n := len(s.Snapshot()); n != 50 {
		t.Errorf("want 50 records, got %d", n)
	}
}

func TestGetReturnsIndependentSlices(t *testing.T) {
	p := filepath.Join(t.TempDir(), "projects.json")
	s, _ := Open(p)
	_ = s.Put("a", Record{Manual: true, Tasks: []Task{{ID: "t1", Done: false}}, Tags: []string{"x"}})
	got, _ := s.Get("a")
	got.Tasks[0].Done = true
	got.Tags[0] = "MUTATED"
	again, _ := s.Get("a")
	if again.Tasks[0].Done {
		t.Error("Get must return an independent Tasks slice (internal state was mutated)")
	}
	if again.Tags[0] != "x" {
		t.Error("Get must return an independent Tags slice (internal state was mutated)")
	}
}
