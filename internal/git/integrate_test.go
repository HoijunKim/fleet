package git

import (
	"errors"
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

func TestCloneCreatesWorkingCopy(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	base := t.TempDir()
	seed := filepath.Join(base, "seed")
	bare := filepath.Join(base, "src.git")
	dest := filepath.Join(base, "clone")

	// Seed a repo with one file and push it to a bare "remote".
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	gitOK(t, seed, "-c", "init.defaultBranch=master", "init")
	gitOK(t, seed, "config", "user.email", "s@t")
	gitOK(t, seed, "config", "user.name", "S")
	writeFile(t, seed, "README.md", "hello\n")
	gitOK(t, seed, "add", "-A")
	gitOK(t, seed, "commit", "-m", "init")
	gitOK(t, seed, "branch", "-M", "master")
	gitOK(t, base, "init", "--bare", bare)
	gitOK(t, seed, "remote", "add", "origin", bare)
	gitOK(t, seed, "push", "-u", "origin", "master")

	if err := Clone(ExecRunner{}, bare, dest); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); err != nil {
		t.Error("clone did not create a .git dir")
	}
	if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
		t.Error("clone did not check out the seeded file")
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

// A real content conflict is now KEPT, not rolled back: fleet can resolve it
// (Conflicts/ResolveConflict/ContinueOperation), so throwing the merge away
// would discard work the user can finish. Before this tier the only unwind was
// `--abort`, which is why these two tests used to assert the opposite.
func TestMergeUpstreamConflictLeavesTheMergeInProgress(t *testing.T) {
	wB := setupDiverged(t, true)
	err := MergeUpstream(ExecRunner{}, wB)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict, got: %v", err)
	}
	if op := OperationInProgress(wB); op != "merge" {
		t.Errorf("OperationInProgress = %q, want merge (the conflict must be kept)", op)
	}
	if left, _ := Conflicts(ExecRunner{}, wB); len(left) == 0 {
		t.Error("expected unmerged paths to resolve")
	}
}

func TestRebaseUpstreamConflictLeavesTheRebaseInProgress(t *testing.T) {
	wB := setupDiverged(t, true)
	err := RebaseUpstream(ExecRunner{}, wB)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict, got: %v", err)
	}
	if op := OperationInProgress(wB); op != "rebase" {
		t.Errorf("OperationInProgress = %q, want rebase (the conflict must be kept)", op)
	}
	if left, _ := Conflicts(ExecRunner{}, wB); len(left) == 0 {
		t.Error("expected unmerged paths to resolve")
	}
}

// The kept conflict is not a dead end: the user can walk out of it either way.
func TestConflictedIntegrationCanBeFinishedOrAbandoned(t *testing.T) {
	wB := setupDiverged(t, true)
	if err := MergeUpstream(ExecRunner{}, wB); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict, got: %v", err)
	}
	if err := ResolveConflict(ExecRunner{}, wB, "merge", "shared.txt", SideMine); err != nil {
		t.Fatalf("ResolveConflict: %v", err)
	}
	if err := ContinueOperation(ExecRunner{}, wB); err != nil {
		t.Fatalf("ContinueOperation: %v", err)
	}
	assertClean(t, wB)

	other := setupDiverged(t, true)
	if err := MergeUpstream(ExecRunner{}, other); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict, got: %v", err)
	}
	if err := AbortOperation(ExecRunner{}, other); err != nil {
		t.Fatalf("AbortOperation: %v", err)
	}
	assertClean(t, other)
	if got := headSubject(t, other); got != "B2" {
		t.Errorf("after abort HEAD subject = %q, want B2", got)
	}
}

// A merge the USER started - in a terminal, outside fleet - must survive a
// click on the diverged banner. The banner stays lit mid-merge (ahead/behind
// come from `# branch.ab`, which an in-progress merge does not change), so this
// is one click away, and the resolved-but-uncommitted work it would abort is
// unrecoverable: it exists only in the working tree.
func TestIntegrateRefusesWhenAnOperationIsAlreadyInProgress(t *testing.T) {
	dir := setupDiverged(t, true)

	// The user starts the merge themselves and resolves the conflict, but has
	// not committed it yet.
	if _, err := (ExecRunner{}).Run(dir, "merge", "@{u}"); err == nil {
		t.Fatal("precondition: the merge should have conflicted")
	}
	writeFile(t, dir, "shared.txt", "carefully resolved by hand\n")
	gitOK(t, dir, "add", "shared.txt")
	if OperationInProgress(dir) != "merge" {
		t.Fatal("precondition: the repo should be mid-merge")
	}

	if err := MergeUpstream(ExecRunner{}, dir); err == nil {
		t.Fatal("expected a refusal while a merge is in progress")
	} else if !strings.Contains(err.Error(), "already in progress") {
		t.Errorf("error should name the cause, got %q", err)
	}

	// The user's work is untouched: still mid-merge, still their resolution.
	if OperationInProgress(dir) != "merge" {
		t.Error("fleet aborted a merge it did not start")
	}
	data, err := os.ReadFile(filepath.Join(dir, "shared.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "carefully resolved by hand" {
		t.Errorf("the user's resolution was destroyed, file now: %q", data)
	}
	// And they can still finish it.
	gitOK(t, dir, "-c", "user.email=b@test", "-c", "user.name=B", "commit", "--no-edit")
	assertClean(t, dir)
}

func TestOperationInProgressReportsNoneOnACleanRepo(t *testing.T) {
	dir := setupDiverged(t, false)
	if op := OperationInProgress(dir); op != "" {
		t.Errorf("clean repo reported %q", op)
	}
	if err := MergeUpstream(ExecRunner{}, dir); err != nil {
		t.Fatalf("a clean diverged merge must still work: %v", err)
	}
	assertClean(t, dir)
}

// The unwind must key off "is an operation in progress", not "are there
// unmerged entries". A rejecting commit-msg hook (commitlint, husky), a broken
// commit signer, or an unset identity all make `git merge @{u}` fail AFTER the
// content is merged: the index is clean, `ls-files -u` is empty, and the repo is
// left mid-merge. Leaving that behind would also poison every later attempt,
// since the in-progress guard would then refuse a mess fleet itself made.
func TestIntegrateRollsBackWhenGitFailsAfterMerging(t *testing.T) {
	dir := setupDiverged(t, false) // no conflict: the content merges cleanly

	hooks := filepath.Join(dir, ".git", "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	// Reject the merge commit itself, the way a commit-message linter does.
	hook := filepath.Join(hooks, "commit-msg")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := MergeUpstream(ExecRunner{}, dir)
	if err == nil {
		t.Skip("git did not run the commit-msg hook here, so there is nothing to roll back")
	}
	if unmerged, _ := (ExecRunner{}).Run(dir, "ls-files", "-u"); strings.TrimSpace(unmerged) != "" {
		t.Fatal("precondition: this failure should leave a fully merged index")
	}
	assertClean(t, dir) // the repo must not be left mid-merge

	// And the next attempt is not refused by the in-progress guard.
	if err := MergeUpstream(ExecRunner{}, dir); err != nil && strings.Contains(err.Error(), "already in progress") {
		t.Errorf("fleet refused to retry because of wreckage it left itself: %v", err)
	}
}
