package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildRebaseTodo(t *testing.T) {
	todo := BuildRebaseTodo([]RebaseAction{
		{Hash: "aaa", Op: "pick"},
		{Hash: "bbb", Op: "fixup"},
		{Hash: "ccc", Op: "drop"},
		{Hash: "ddd", Op: "pick"},
	})
	want := "pick aaa\nfixup bbb\npick ddd\n"
	if todo != want {
		t.Errorf("todo = %q, want %q", todo, want)
	}
}

func TestBuildRebaseTodoAllDropIsEmpty(t *testing.T) {
	if todo := BuildRebaseTodo([]RebaseAction{{Hash: "a", Op: "drop"}}); todo != "" {
		t.Errorf("all-drop should yield empty todo, got %q", todo)
	}
}

// copySeqEditor is a portable GIT_SEQUENCE_EDITOR for tests: git appends the
// todo path, so `cp "$FLEET_REBASE_TODO"` copies our todo over git's. It stands
// in for the fleet --rebase-seq sentinel (which needs the real binary).
const copySeqEditor = `cp "$FLEET_REBASE_TODO"`

func setupRebaseRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	gitOK(t, dir, "-c", "init.defaultBranch=master", "init")
	gitOK(t, dir, "config", "gc.auto", "0")
	gitOK(t, dir, "config", "maintenance.auto", "false")
	gitOK(t, dir, "config", "user.email", "t@t")
	gitOK(t, dir, "config", "user.name", "T")
	for _, n := range []string{"a", "b", "c"} {
		writeFile(t, dir, n+".txt", n+"\n")
		gitOK(t, dir, "add", "-A")
		gitOK(t, dir, "commit", "-m", "add "+n)
	}
	return dir
}

func headSubjects(t *testing.T, dir string, n int) []string {
	t.Helper()
	out := gitOK(t, dir, "log", "-n", "99", "--format=%s")
	all := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if n < len(all) {
		all = all[:n]
	}
	return all
}

func TestInteractiveRebaseReorder(t *testing.T) {
	dir := setupRebaseRepo(t)
	// Rebase the top two commits (add b, add c) onto "add a", swapping them.
	base := strings.TrimSpace(gitOK(t, dir, "rev-parse", "HEAD~2"))
	cHash := strings.TrimSpace(gitOK(t, dir, "rev-parse", "HEAD"))
	bHash := strings.TrimSpace(gitOK(t, dir, "rev-parse", "HEAD~1"))

	// Desired order: c before b (reversed).
	err := InteractiveRebase(dir, base, copySeqEditor, []RebaseAction{
		{Hash: cHash, Op: "pick"},
		{Hash: bHash, Op: "pick"},
	})
	if err != nil {
		t.Fatalf("InteractiveRebase: %v", err)
	}
	subs := headSubjects(t, dir, 3)
	if subs[0] != "add b" || subs[1] != "add c" {
		t.Errorf("after reorder top subjects = %v, want [add b, add c, ...]", subs)
	}
}

func TestInteractiveRebaseDrop(t *testing.T) {
	dir := setupRebaseRepo(t)
	base := strings.TrimSpace(gitOK(t, dir, "rev-parse", "HEAD~2"))
	cHash := strings.TrimSpace(gitOK(t, dir, "rev-parse", "HEAD"))
	bHash := strings.TrimSpace(gitOK(t, dir, "rev-parse", "HEAD~1"))

	// Drop "add b", keep "add c".
	err := InteractiveRebase(dir, base, copySeqEditor, []RebaseAction{
		{Hash: bHash, Op: "drop"},
		{Hash: cHash, Op: "pick"},
	})
	if err != nil {
		t.Fatalf("InteractiveRebase: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); !os.IsNotExist(err) {
		t.Error("dropped commit's file b.txt should be gone")
	}
	if _, err := os.Stat(filepath.Join(dir, "c.txt")); err != nil {
		t.Error("kept commit's file c.txt should be present")
	}
}

func TestInteractiveRebaseFixup(t *testing.T) {
	dir := setupRebaseRepo(t)
	base := strings.TrimSpace(gitOK(t, dir, "rev-parse", "HEAD~2"))
	cHash := strings.TrimSpace(gitOK(t, dir, "rev-parse", "HEAD"))
	bHash := strings.TrimSpace(gitOK(t, dir, "rev-parse", "HEAD~1"))

	countBefore := len(headSubjects(t, dir, 99))
	// Fixup c into b: two commits become one.
	err := InteractiveRebase(dir, base, copySeqEditor, []RebaseAction{
		{Hash: bHash, Op: "pick"},
		{Hash: cHash, Op: "fixup"},
	})
	if err != nil {
		t.Fatalf("InteractiveRebase: %v", err)
	}
	countAfter := len(headSubjects(t, dir, 99))
	if countAfter != countBefore-1 {
		t.Errorf("fixup should reduce commit count by one: %d -> %d", countBefore, countAfter)
	}
	// Both files are still present in the combined commit.
	for _, f := range []string{"b.txt", "c.txt"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s should be present after fixup: %v", f, err)
		}
	}
}

func TestApplyRebaseSeq(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "desired.txt")
	dst := filepath.Join(dir, "git-todo.txt")
	if err := os.WriteFile(src, []byte("pick aaa\nfixup bbb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("pick original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyRebaseSeq(src, dst); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "pick aaa\nfixup bbb\n" {
		t.Errorf("dst = %q, want the src todo", got)
	}
}
