package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadRepoFileContainment(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// a secret one level up, OUTSIDE the repo
	outside := filepath.Join(filepath.Dir(dir), "secret.txt")
	_ = os.WriteFile(outside, []byte("TOP SECRET"), 0o644)
	defer os.Remove(outside)

	a := &App{}

	if got := a.ReadRepoFile(dir, "main.go"); got != "package main\n" {
		t.Errorf("in-repo read = %q", got)
	}
	// path traversal must be refused, not leak the outside file
	got := a.ReadRepoFile(dir, "../secret.txt")
	if got == "TOP SECRET" || got[:6] != "error:" {
		t.Errorf("traversal not blocked: %q", got)
	}
	if got := a.ReadRepoFile(dir, "nope.go"); got[:6] != "error:" {
		t.Errorf("missing file should error: %q", got)
	}
	if got := a.ReadRepoFile(dir, "."); got[:6] != "error:" {
		t.Errorf("directory should error: %q", got)
	}
}

type grepFake struct{ out string }

func (f grepFake) Run(dir string, args ...string) (string, error) { return f.out, nil }

func TestRepoGrepFormats(t *testing.T) {
	// git.Grep parses "file:line:text"
	a := &App{runner: grepFake{out: "auth/token.go:12:if t.ExpiresAt < now\n"}}
	got := a.RepoGrep("/x", "ExpiresAt")
	if got != "auth/token.go:12: if t.ExpiresAt < now" {
		t.Errorf("grep format = %q", got)
	}
	if got := a.RepoGrep("/x", "  "); got != "(empty query)" {
		t.Errorf("empty query = %q", got)
	}
}

func TestRepoFilesLists(t *testing.T) {
	a := &App{runner: grepFake{out: "a.go\nb.go\n"}}
	if got := a.RepoFiles("/x", ""); got != "a.go\nb.go" {
		t.Errorf("files = %q", got)
	}
	a2 := &App{runner: grepFake{out: ""}}
	if got := a2.RepoFiles("/x", ""); got != "(no tracked files)" {
		t.Errorf("empty = %q", got)
	}
}
