package git

import (
	"os/exec"
	"strings"
	"testing"
)

// newRepo makes a temp git repo with one committed file and returns its dir.
// Reuses gitOK/writeFile from integrate_test.go (same package).
func newRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	gitOK(t, dir, "-c", "init.defaultBranch=master", "init")
	gitOK(t, dir, "config", "user.email", "t@t")
	gitOK(t, dir, "config", "user.name", "T")
	writeFile(t, dir, "base.txt", "base\n")
	gitOK(t, dir, "add", "-A")
	gitOK(t, dir, "commit", "-m", "init")
	gitOK(t, dir, "branch", "-M", "master")
	return dir
}

func statusByPath(fs []FileStatus) map[string]FileStatus {
	m := map[string]FileStatus{}
	for _, f := range fs {
		m[f.Path] = f
	}
	return m
}

func TestParseStatusFiles(t *testing.T) {
	// Hand-built porcelain v2 lines covering the XY decodings.
	out := strings.Join([]string{
		"1 M. N... 100644 100644 100644 aaa bbb staged.txt",              // index-only -> staged
		"1 .M N... 100644 100644 100644 aaa bbb worktree.txt",            // worktree-only -> unstaged
		"1 MM N... 100644 100644 100644 aaa bbb both.txt",                // both
		"2 R. N... 100644 100644 100644 aaa bbb R100 new.txt\told.txt",   // rename, keeps new path
		"u UU N... 100644 100644 100644 100644 aaa bbb ccc conflict.txt", // unmerged
		"? untracked.txt",
	}, "\n")
	m := statusByPath(parseStatusFiles(out))

	check := func(path string, staged, unstaged bool) {
		g, ok := m[path]
		if !ok {
			t.Errorf("%s missing from parse", path)
			return
		}
		if g.Staged != staged || g.Unstaged != unstaged {
			t.Errorf("%s = {staged:%v unstaged:%v}, want {%v %v}", path, g.Staged, g.Unstaged, staged, unstaged)
		}
	}
	check("staged.txt", true, false)
	check("worktree.txt", false, true)
	check("both.txt", true, true)
	check("new.txt", true, false)
	check("conflict.txt", false, true)
	check("untracked.txt", false, true)
}

func TestStatusFilesStagedVsUnstaged(t *testing.T) {
	dir := newRepo(t)
	// staged.txt: new, staged. dirty.txt: tracked, modified, NOT staged. new.txt: untracked.
	writeFile(t, dir, "staged.txt", "s\n")
	gitOK(t, dir, "add", "staged.txt")
	writeFile(t, dir, "base.txt", "base changed\n") // tracked, unstaged
	writeFile(t, dir, "new.txt", "u\n")             // untracked

	fs, err := StatusFiles(ExecRunner{}, dir)
	if err != nil {
		t.Fatalf("StatusFiles: %v", err)
	}
	m := statusByPath(fs)
	if g := m["staged.txt"]; !g.Staged || g.Unstaged {
		t.Errorf("staged.txt = %+v, want staged only", g)
	}
	if g := m["base.txt"]; g.Staged || !g.Unstaged {
		t.Errorf("base.txt = %+v, want unstaged only", g)
	}
	if g := m["new.txt"]; g.Staged || !g.Unstaged {
		t.Errorf("new.txt = %+v, want unstaged (untracked)", g)
	}
}

func TestStageUnstageRoundTrip(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "base.txt", "changed\n")

	if err := StageFile(ExecRunner{}, dir, "base.txt"); err != nil {
		t.Fatalf("StageFile: %v", err)
	}
	if g := statusByPath(mustStatus(t, dir))["base.txt"]; !g.Staged {
		t.Errorf("after stage, base.txt = %+v, want staged", g)
	}
	if err := UnstageFile(ExecRunner{}, dir, "base.txt"); err != nil {
		t.Fatalf("UnstageFile: %v", err)
	}
	if g := statusByPath(mustStatus(t, dir))["base.txt"]; g.Staged || !g.Unstaged {
		t.Errorf("after unstage, base.txt = %+v, want unstaged only", g)
	}
}

func mustStatus(t *testing.T, dir string) []FileStatus {
	t.Helper()
	fs, err := StatusFiles(ExecRunner{}, dir)
	if err != nil {
		t.Fatalf("StatusFiles: %v", err)
	}
	return fs
}

func TestCommitStagedCommitsOnlyStaged(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "a.txt", "a\n")
	writeFile(t, dir, "b.txt", "b\n")
	gitOK(t, dir, "add", "a.txt") // stage only a.txt

	if err := CommitStaged(ExecRunner{}, dir, "add a"); err != nil {
		t.Fatalf("CommitStaged: %v", err)
	}
	// b.txt is still untracked/uncommitted.
	if g := statusByPath(mustStatus(t, dir))["b.txt"]; g.Staged {
		t.Errorf("b.txt should not have been committed: %+v", g)
	}
	// a.txt made it into the new HEAD.
	out := gitOK(t, dir, "show", "--name-only", "--pretty=format:", "HEAD")
	if !strings.Contains(out, "a.txt") || strings.Contains(out, "b.txt") {
		t.Errorf("HEAD should contain only a.txt, got:\n%s", out)
	}
}

func TestCommitAmendAndLastMessage(t *testing.T) {
	dir := newRepo(t)
	if msg, err := LastCommitMessage(ExecRunner{}, dir); err != nil || msg != "init" {
		t.Fatalf("LastCommitMessage = %q, %v; want \"init\"", msg, err)
	}
	before := strings.TrimSpace(gitOK(t, dir, "rev-list", "--count", "HEAD"))
	if err := CommitAmend(ExecRunner{}, dir, "init reworded"); err != nil {
		t.Fatalf("CommitAmend: %v", err)
	}
	if msg, _ := LastCommitMessage(ExecRunner{}, dir); msg != "init reworded" {
		t.Errorf("after amend, message = %q, want \"init reworded\"", msg)
	}
	if after := strings.TrimSpace(gitOK(t, dir, "rev-list", "--count", "HEAD")); after != before {
		t.Errorf("amend changed commit count %s -> %s", before, after)
	}
}

func TestStashApplyKeepsDropRemoves(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "base.txt", "wip\n")
	gitOK(t, dir, "stash", "push")

	// Apply restores the change AND keeps the entry.
	if err := StashApply(ExecRunner{}, dir, 0); err != nil {
		t.Fatalf("StashApply: %v", err)
	}
	if !strings.Contains(gitOK(t, dir, "stash", "list"), "stash@{0}") {
		t.Error("StashApply should keep the entry")
	}
	// Reset the working change so drop's precondition is clean, then drop.
	gitOK(t, dir, "checkout", "--", "base.txt")
	if err := StashDrop(ExecRunner{}, dir, 0); err != nil {
		t.Fatalf("StashDrop: %v", err)
	}
	if strings.TrimSpace(gitOK(t, dir, "stash", "list")) != "" {
		t.Error("StashDrop should remove the entry")
	}
}

func TestConflictFlaggedAndCommitStagedRefused(t *testing.T) {
	dir := newRepo(t)
	// Diverge base.txt on two branches so a merge conflicts.
	gitOK(t, dir, "checkout", "-b", "left")
	writeFile(t, dir, "base.txt", "left change\n")
	gitOK(t, dir, "commit", "-am", "left")
	gitOK(t, dir, "checkout", "master")
	gitOK(t, dir, "checkout", "-b", "right")
	writeFile(t, dir, "base.txt", "right change\n")
	gitOK(t, dir, "commit", "-am", "right")
	if _, err := (ExecRunner{}).Run(dir, "merge", "left"); err == nil {
		t.Fatal("expected the merge to conflict")
	}

	// StatusFiles marks the unmerged file as a conflict.
	if g := statusByPath(mustStatus(t, dir))["base.txt"]; !g.Conflict {
		t.Errorf("base.txt should be flagged Conflict: %+v", g)
	}
	// CommitStaged refuses while the path is unmerged (markers can't slip in).
	if err := CommitStaged(ExecRunner{}, dir, "sneak"); err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Errorf("CommitStaged should refuse an unmerged tree, got: %v", err)
	}
}

func TestCreateAndDeleteBranch(t *testing.T) {
	dir := newRepo(t)
	if err := CreateBranch(ExecRunner{}, dir, "feature"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if cur := strings.TrimSpace(gitOK(t, dir, "rev-parse", "--abbrev-ref", "HEAD")); cur != "feature" {
		t.Errorf("CreateBranch did not switch: on %q", cur)
	}
	// A merged branch deletes cleanly.
	gitOK(t, dir, "checkout", "master")
	if err := DeleteBranch(ExecRunner{}, dir, "feature"); err != nil {
		t.Errorf("DeleteBranch of a merged branch: %v", err)
	}

	// An unmerged branch is refused by safe-delete.
	gitOK(t, dir, "checkout", "-b", "wip")
	writeFile(t, dir, "base.txt", "unmerged work\n")
	gitOK(t, dir, "commit", "-am", "wip commit")
	gitOK(t, dir, "checkout", "master")
	if err := DeleteBranch(ExecRunner{}, dir, "wip"); err == nil {
		t.Error("DeleteBranch should refuse an unmerged branch")
	}
}
