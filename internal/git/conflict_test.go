package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// These tests drive real git. They cover the half of conflict handling fleet
// used to send the user to a terminal for: seeing what conflicts, choosing a
// side, and finishing or unwinding the operation.

// setupMergeConflicts builds a repo whose in-progress merge carries one of every
// conflict kind at once, and returns its working dir:
//
//	both.txt   - edited on both sides            -> both-modified
//	new.txt    - created on both sides           -> both-added
//	theirs.txt - edited here, deleted by them     -> deleted-by-them
//	ours.txt   - deleted here, edited by them     -> deleted-by-us
func setupMergeConflicts(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	gitOK(t, dir, "-c", "init.defaultBranch=master", "init")
	gitOK(t, dir, "config", "user.email", "t@t")
	gitOK(t, dir, "config", "user.name", "T")

	writeFile(t, dir, "both.txt", "base\n")
	writeFile(t, dir, "theirs.txt", "base\n")
	writeFile(t, dir, "ours.txt", "base\n")
	gitOK(t, dir, "add", "-A")
	gitOK(t, dir, "commit", "-m", "base")

	// The other side.
	gitOK(t, dir, "checkout", "-b", "other")
	writeFile(t, dir, "both.txt", "other\n")
	writeFile(t, dir, "new.txt", "other new\n")
	writeFile(t, dir, "ours.txt", "other edit\n")
	if err := os.Remove(filepath.Join(dir, "theirs.txt")); err != nil {
		t.Fatal(err)
	}
	gitOK(t, dir, "add", "-A")
	gitOK(t, dir, "commit", "-m", "other side")

	// Our side.
	gitOK(t, dir, "checkout", "master")
	writeFile(t, dir, "both.txt", "mine\n")
	writeFile(t, dir, "new.txt", "mine new\n")
	writeFile(t, dir, "theirs.txt", "my edit\n")
	if err := os.Remove(filepath.Join(dir, "ours.txt")); err != nil {
		t.Fatal(err)
	}
	gitOK(t, dir, "add", "-A")
	gitOK(t, dir, "commit", "-m", "my side")

	// Conflicting merge: expected to fail, which is the point.
	if _, err := (ExecRunner{}).Run(dir, "merge", "other"); err == nil {
		t.Fatal("merge was expected to conflict")
	}
	if op := OperationInProgress(dir); op != "merge" {
		t.Fatalf("OperationInProgress = %q, want merge", op)
	}
	return dir
}

func TestConflictsReportsEveryKind(t *testing.T) {
	dir := setupMergeConflicts(t)

	got, err := Conflicts(ExecRunner{}, dir)
	if err != nil {
		t.Fatalf("Conflicts: %v", err)
	}

	want := map[string]string{
		"both.txt":   "both-modified",
		"new.txt":    "both-added",
		"theirs.txt": "deleted-by-them",
		"ours.txt":   "deleted-by-us",
	}
	if len(got) != len(want) {
		// One entry per path, not one per index stage.
		t.Fatalf("Conflicts returned %d entries, want %d: %+v", len(got), len(want), got)
	}
	for _, c := range got {
		if w, ok := want[c.Path]; !ok {
			t.Errorf("unexpected conflicted path %q", c.Path)
		} else if c.Kind != w {
			t.Errorf("%s: kind = %q, want %q", c.Path, c.Kind, w)
		}
	}

	paths := make([]string, len(got))
	for i, c := range got {
		paths[i] = c.Path
	}
	if !sort.StringsAreSorted(paths) {
		t.Errorf("paths not sorted: %v", paths)
	}
}

func TestConflictsEmptyOnACleanRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	gitOK(t, dir, "-c", "init.defaultBranch=master", "init")
	gitOK(t, dir, "config", "user.email", "t@t")
	gitOK(t, dir, "config", "user.name", "T")
	writeFile(t, dir, "a.txt", "a\n")
	gitOK(t, dir, "add", "-A")
	gitOK(t, dir, "commit", "-m", "init")

	got, err := Conflicts(ExecRunner{}, dir)
	if err != nil {
		t.Fatalf("Conflicts on a clean repo: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Conflicts = %+v, want empty", got)
	}
}

// A path with a space exercises the tab split: ls-files -u separates metadata
// from the path with a tab, so splitting on whitespace would truncate the name.
func TestConflictsHandlesPathsWithSpaces(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	gitOK(t, dir, "-c", "init.defaultBranch=master", "init")
	gitOK(t, dir, "config", "user.email", "t@t")
	gitOK(t, dir, "config", "user.name", "T")
	writeFile(t, dir, "my file.txt", "base\n")
	gitOK(t, dir, "add", "-A")
	gitOK(t, dir, "commit", "-m", "base")
	gitOK(t, dir, "checkout", "-b", "other")
	writeFile(t, dir, "my file.txt", "other\n")
	gitOK(t, dir, "commit", "-am", "other")
	gitOK(t, dir, "checkout", "master")
	writeFile(t, dir, "my file.txt", "mine\n")
	gitOK(t, dir, "commit", "-am", "mine")
	if _, err := (ExecRunner{}).Run(dir, "merge", "other"); err == nil {
		t.Fatal("merge was expected to conflict")
	}

	got, err := Conflicts(ExecRunner{}, dir)
	if err != nil {
		t.Fatalf("Conflicts: %v", err)
	}
	if len(got) != 1 || got[0].Path != "my file.txt" {
		t.Fatalf("Conflicts = %+v, want one entry for %q", got, "my file.txt")
	}
	if !strings.Contains(got[0].Kind, "modified") {
		t.Errorf("kind = %q, want both-modified", got[0].Kind)
	}
}
