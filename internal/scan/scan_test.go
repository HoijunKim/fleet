package scan

import (
	"os"
	"path/filepath"
	"testing"
)

func mkRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func mkPlain(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverFindsGitAndPlain(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, filepath.Join(root, "alpha"))
	mkRepo(t, filepath.Join(root, "beta"))
	mkPlain(t, filepath.Join(root, "notes")) // plain folder

	got := Discover([]string{root}, 2, true)
	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %d: %+v", len(got), got)
	}
	// sorted by name: alpha, beta, notes
	if got[0].Name != "alpha" || !got[0].IsGit {
		t.Errorf("entry0=%+v", got[0])
	}
	if got[2].Name != "notes" || got[2].IsGit {
		t.Errorf("entry2=%+v", got[2])
	}
}

func TestDiscoverHidesPlainWhenAsked(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, filepath.Join(root, "alpha"))
	mkPlain(t, filepath.Join(root, "notes"))

	got := Discover([]string{root}, 2, false)
	if len(got) != 1 || got[0].Name != "alpha" {
		t.Fatalf("want only alpha, got %+v", got)
	}
}

func TestDiscoverRespectsDepth(t *testing.T) {
	root := t.TempDir()
	// nested repo two levels down
	mkRepo(t, filepath.Join(root, "group", "nested"))

	shallow := Discover([]string{root}, 1, false)
	if len(shallow) != 0 {
		t.Errorf("depth 1 should not reach nested repo, got %+v", shallow)
	}
	deep := Discover([]string{root}, 2, false)
	if len(deep) != 1 || deep[0].Name != "nested" {
		t.Errorf("depth 2 should find nested, got %+v", deep)
	}
}
