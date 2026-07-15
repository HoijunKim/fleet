package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests drive real git against throwaway repos. They verify that
// MergeUpstream/RebaseUpstream integrate a diverged upstream, and - critically -
// that a conflict never strands the working tree mid-operation.

func gitOK(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := ExecRunner{}.Run(dir, args...)
	if err != nil {
		t.Fatalf("git %s (in %s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return out
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setupDiverged builds a clone whose master has diverged from its upstream: one
// commit locally (B2) and one commit upstream (A2), both descending from a
// shared base. When conflict is true, A2 and B2 edit the same line of the same
// file so integrating them conflicts; otherwise they touch different files and
// integrate cleanly. Returns the diverged clone's working dir.
func setupDiverged(t *testing.T, conflict bool) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	base := t.TempDir()
	wA := filepath.Join(base, "a")
	wB := filepath.Join(base, "b")
	bare := filepath.Join(base, "remote.git")
	if err := os.MkdirAll(wA, 0o755); err != nil {
		t.Fatal(err)
	}

	// Author A's repo with an initial commit.
	gitOK(t, wA, "-c", "init.defaultBranch=master", "init")
	gitOK(t, wA, "config", "user.email", "a@test")
	gitOK(t, wA, "config", "user.name", "A")
	writeFile(t, wA, "shared.txt", "base line\n")
	gitOK(t, wA, "add", "-A")
	gitOK(t, wA, "commit", "-m", "init")
	gitOK(t, wA, "branch", "-M", "master")

	// Bare remote, seeded from A.
	gitOK(t, base, "init", "--bare", bare)
	gitOK(t, wA, "remote", "add", "origin", bare)
	gitOK(t, wA, "push", "-u", "origin", "master")

	// Author B clones the seeded remote.
	gitOK(t, base, "clone", bare, wB)
	gitOK(t, wB, "config", "user.email", "b@test")
	gitOK(t, wB, "config", "user.name", "B")

	// A2: upstream advances.
	if conflict {
		writeFile(t, wA, "shared.txt", "A change\n")
	} else {
		writeFile(t, wA, "onlyA.txt", "a\n")
	}
	gitOK(t, wA, "add", "-A")
	gitOK(t, wA, "commit", "-m", "A2")
	gitOK(t, wA, "push", "origin", "master")

	// B2: local diverges from the shared base.
	if conflict {
		writeFile(t, wB, "shared.txt", "B change\n")
	} else {
		writeFile(t, wB, "onlyB.txt", "b\n")
	}
	gitOK(t, wB, "add", "-A")
	gitOK(t, wB, "commit", "-m", "B2")

	// B learns about A2 (updates origin/master) without integrating it: now
	// ahead 1, behind 1.
	gitOK(t, wB, "fetch", "origin")
	return wB
}

func headSubject(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(gitOK(t, dir, "log", "-1", "--pretty=%s"))
}

// assertClean fails if the repo is mid-merge/mid-rebase or has unmerged entries.
func assertClean(t *testing.T, dir string) {
	t.Helper()
	if unmerged, _ := (ExecRunner{}).Run(dir, "ls-files", "-u"); strings.TrimSpace(unmerged) != "" {
		t.Errorf("expected no unmerged index entries, got:\n%s", unmerged)
	}
	gitDir := filepath.Join(dir, ".git")
	for _, p := range []string{"MERGE_HEAD", "rebase-merge", "rebase-apply"} {
		if _, err := os.Stat(filepath.Join(gitDir, p)); err == nil {
			t.Errorf("expected no %s (operation left in progress)", p)
		}
	}
}

func TestWorktreeDiffIncludesTrackedAndUntracked(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	gitOK(t, dir, "-c", "init.defaultBranch=master", "init")
	gitOK(t, dir, "config", "user.email", "t@t")
	gitOK(t, dir, "config", "user.name", "T")
	writeFile(t, dir, "tracked.txt", "one\n")
	gitOK(t, dir, "add", "-A")
	gitOK(t, dir, "commit", "-m", "init")

	// Modify a tracked file AND drop in an untracked one.
	writeFile(t, dir, "tracked.txt", "one\ntwo\n")
	writeFile(t, dir, "brand_new.txt", "hello from a new file\n")

	out, err := WorktreeDiff(ExecRunner{}, dir)
	if err != nil {
		t.Fatalf("WorktreeDiff: %v", err)
	}
	// Tracked modification shows.
	if !strings.Contains(out, "tracked.txt") || !strings.Contains(out, "+two") {
		t.Errorf("combined diff missing the tracked change:\n%s", out)
	}
	// Untracked file shows - the case `git diff HEAD` alone omits.
	if !strings.Contains(out, "brand_new.txt") || !strings.Contains(out, "hello from a new file") {
		t.Errorf("combined diff missing the untracked file:\n%s", out)
	}
}

func TestMergeUpstreamCleanDiverge(t *testing.T) {
	wB := setupDiverged(t, false)
	if err := MergeUpstream(ExecRunner{}, wB); err != nil {
		t.Fatalf("MergeUpstream on a non-conflicting diverge: %v", err)
	}
	// A merge commit now sits on top; both sides' files are present.
	if _, err := os.Stat(filepath.Join(wB, "onlyA.txt")); err != nil {
		t.Error("upstream commit A2 was not merged in (onlyA.txt missing)")
	}
	if _, err := os.Stat(filepath.Join(wB, "onlyB.txt")); err != nil {
		t.Error("local commit B2 was lost")
	}
	assertClean(t, wB)
}

func TestRebaseUpstreamCleanDiverge(t *testing.T) {
	wB := setupDiverged(t, false)
	if err := RebaseUpstream(ExecRunner{}, wB); err != nil {
		t.Fatalf("RebaseUpstream on a non-conflicting diverge: %v", err)
	}
	// B2 replayed on top of A2: HEAD is still B2, and A2's file is present.
	if got := headSubject(t, wB); got != "B2" {
		t.Errorf("after rebase HEAD subject = %q, want B2", got)
	}
	if _, err := os.Stat(filepath.Join(wB, "onlyA.txt")); err != nil {
		t.Error("rebase did not replay onto A2 (onlyA.txt missing)")
	}
	assertClean(t, wB)
}

func TestMergeUpstreamConflictAborts(t *testing.T) {
	wB := setupDiverged(t, true)
	err := MergeUpstream(ExecRunner{}, wB)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected a conflict error, got: %v", err)
	}
	// The abort must have restored a clean tree still pointing at B2.
	assertClean(t, wB)
	if got := headSubject(t, wB); got != "B2" {
		t.Errorf("after aborted merge HEAD subject = %q, want B2", got)
	}
}

func TestRebaseUpstreamConflictAborts(t *testing.T) {
	wB := setupDiverged(t, true)
	err := RebaseUpstream(ExecRunner{}, wB)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected a conflict error, got: %v", err)
	}
	assertClean(t, wB)
	if got := headSubject(t, wB); got != "B2" {
		t.Errorf("after aborted rebase HEAD subject = %q, want B2", got)
	}
}
