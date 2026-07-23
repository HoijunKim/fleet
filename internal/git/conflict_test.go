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

// setupRebaseConflict leaves the repo mid-rebase of a local commit onto an
// upstream that touched the same line. During a rebase HEAD is the UPSTREAM and
// the commit being replayed is "theirs", which is the trap this fixture exists
// to catch: local content is reachable as --theirs, not --ours.
func setupRebaseConflict(t *testing.T) string {
	t.Helper()
	dir := setupDiverged(t, true)
	if _, err := (ExecRunner{}).Run(dir, "rebase", "@{u}"); err == nil {
		t.Fatal("rebase was expected to conflict")
	}
	if op := OperationInProgress(dir); op != "rebase" {
		t.Fatalf("OperationInProgress = %q, want rebase", op)
	}
	return dir
}

// fileContent reads a working-tree file with line endings normalized: a checkout
// on a machine with core.autocrlf=true writes CRLF, and these tests assert which
// SIDE won, not how the platform spells a newline.
func fileContent(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func TestResolveConflictMineInRebaseKeepsLocalWork(t *testing.T) {
	dir := setupRebaseConflict(t)

	if err := ResolveConflict(ExecRunner{}, dir, "rebase", "shared.txt", "mine"); err != nil {
		t.Fatalf("ResolveConflict: %v", err)
	}
	// "B change" is the local commit being replayed. Mapping "mine" to --ours
	// here would leave "A change" - the upstream - and silently drop the user's
	// work, which is the whole reason the mapping is mode-aware.
	if got := fileContent(t, dir, "shared.txt"); got != "B change\n" {
		t.Errorf("shared.txt = %q, want the local %q", got, "B change\n")
	}
	if left, _ := Conflicts(ExecRunner{}, dir); len(left) != 0 {
		t.Errorf("resolving must stage the file, still unmerged: %+v", left)
	}
}

func TestResolveConflictIncomingInRebaseTakesUpstream(t *testing.T) {
	dir := setupRebaseConflict(t)

	if err := ResolveConflict(ExecRunner{}, dir, "rebase", "shared.txt", "incoming"); err != nil {
		t.Fatalf("ResolveConflict: %v", err)
	}
	if got := fileContent(t, dir, "shared.txt"); got != "A change\n" {
		t.Errorf("shared.txt = %q, want the upstream %q", got, "A change\n")
	}
}

func TestResolveConflictInMergeUsesOursForMine(t *testing.T) {
	dir := setupMergeConflicts(t)

	if err := ResolveConflict(ExecRunner{}, dir, "merge", "both.txt", "mine"); err != nil {
		t.Fatalf("ResolveConflict mine: %v", err)
	}
	if got := fileContent(t, dir, "both.txt"); got != "mine\n" {
		t.Errorf("both.txt = %q, want %q", got, "mine\n")
	}

	if err := ResolveConflict(ExecRunner{}, dir, "merge", "new.txt", "incoming"); err != nil {
		t.Fatalf("ResolveConflict incoming: %v", err)
	}
	if got := fileContent(t, dir, "new.txt"); got != "other new\n" {
		t.Errorf("new.txt = %q, want %q", got, "other new\n")
	}
}

func TestResolveConflictDeletedByUsKeepingMineStagesTheDeletion(t *testing.T) {
	dir := setupMergeConflicts(t)

	// ours.txt: this side deleted it, the other side edited it. Keeping "mine"
	// means keeping the deletion - there is no stage to check out, and a naive
	// `checkout --ours` fails here.
	if err := ResolveConflict(ExecRunner{}, dir, "merge", "ours.txt", "mine"); err != nil {
		t.Fatalf("ResolveConflict: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ours.txt")); !os.IsNotExist(err) {
		t.Errorf("ours.txt should stay deleted, stat err = %v", err)
	}
	if left, _ := conflictKind(ExecRunner{}, dir, "ours.txt"); left != "" {
		t.Errorf("ours.txt still unmerged as %q", left)
	}
}

func TestResolveConflictDeletedByThemKeepingIncomingStagesTheDeletion(t *testing.T) {
	dir := setupMergeConflicts(t)

	// theirs.txt: edited here, deleted by them. Taking "incoming" is a deletion.
	if err := ResolveConflict(ExecRunner{}, dir, "merge", "theirs.txt", "incoming"); err != nil {
		t.Fatalf("ResolveConflict: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "theirs.txt")); !os.IsNotExist(err) {
		t.Errorf("theirs.txt should be deleted, stat err = %v", err)
	}
	if left, _ := conflictKind(ExecRunner{}, dir, "theirs.txt"); left != "" {
		t.Errorf("theirs.txt still unmerged as %q", left)
	}
}

func TestResolveConflictWorktreeStagesWhatIsOnDisk(t *testing.T) {
	dir := setupMergeConflicts(t)

	// The user resolved it in their editor: fleet stages the file as-is.
	writeFile(t, dir, "both.txt", "hand merged\n")
	if err := ResolveConflict(ExecRunner{}, dir, "merge", "both.txt", "worktree"); err != nil {
		t.Fatalf("ResolveConflict: %v", err)
	}
	if left, _ := conflictKind(ExecRunner{}, dir, "both.txt"); left != "" {
		t.Errorf("both.txt still unmerged as %q", left)
	}
	staged := gitOK(t, dir, "show", ":both.txt")
	if staged != "hand merged\n" {
		t.Errorf("staged content = %q, want %q", staged, "hand merged\n")
	}
}

func TestResolveConflictRejectsAnUnknownSide(t *testing.T) {
	dir := setupMergeConflicts(t)
	if err := ResolveConflict(ExecRunner{}, dir, "merge", "both.txt", "yours"); err == nil {
		t.Error("an unknown side must error rather than silently staging something")
	}
}

func TestContinueOperationRefusesWhileAConflictRemains(t *testing.T) {
	dir := setupMergeConflicts(t)

	err := ContinueOperation(ExecRunner{}, dir)
	if err == nil {
		t.Fatal("ContinueOperation must refuse while paths are unmerged")
	}
	// Naming the file is the point: git's own message says "you must edit all
	// merge conflicts" without saying which.
	if !strings.Contains(err.Error(), "both.txt") {
		t.Errorf("error %q should name a conflicted file", err)
	}
	if op := OperationInProgress(dir); op != "merge" {
		t.Errorf("a refused continue must leave the merge in progress, got %q", op)
	}
}

func TestContinueOperationFinishesAResolvedMerge(t *testing.T) {
	dir := setupMergeConflicts(t)
	for _, f := range []string{"both.txt", "new.txt", "theirs.txt", "ours.txt"} {
		if err := ResolveConflict(ExecRunner{}, dir, "merge", f, "mine"); err != nil {
			t.Fatalf("resolve %s: %v", f, err)
		}
	}

	if err := ContinueOperation(ExecRunner{}, dir); err != nil {
		t.Fatalf("ContinueOperation: %v", err)
	}
	assertClean(t, dir)
	// A merge commit has two parents; anything else means the merge did not
	// actually conclude.
	parents := strings.Fields(strings.TrimSpace(gitOK(t, dir, "log", "-1", "--pretty=%P")))
	if len(parents) != 2 {
		t.Errorf("HEAD has %d parents, want 2 (a merge commit): %v", len(parents), parents)
	}
}

func TestContinueOperationFinishesAResolvedRebase(t *testing.T) {
	dir := setupRebaseConflict(t)
	if err := ResolveConflict(ExecRunner{}, dir, "rebase", "shared.txt", "mine"); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if err := ContinueOperation(ExecRunner{}, dir); err != nil {
		t.Fatalf("ContinueOperation: %v", err)
	}
	assertClean(t, dir)
	if got := headSubject(t, dir); got != "B2" {
		t.Errorf("HEAD subject = %q, want the replayed local commit B2", got)
	}
}

func TestAbortOperationRestoresThePreMergeHead(t *testing.T) {
	dir := setupMergeConflicts(t)
	before := strings.TrimSpace(gitOK(t, dir, "rev-parse", "HEAD"))

	if err := AbortOperation(ExecRunner{}, dir); err != nil {
		t.Fatalf("AbortOperation: %v", err)
	}
	assertClean(t, dir)
	if after := strings.TrimSpace(gitOK(t, dir, "rev-parse", "HEAD")); after != before {
		t.Errorf("HEAD = %s, want the pre-merge %s", after, before)
	}
}

func TestAbortOperationUnwindsARebase(t *testing.T) {
	dir := setupRebaseConflict(t)

	if err := AbortOperation(ExecRunner{}, dir); err != nil {
		t.Fatalf("AbortOperation: %v", err)
	}
	assertClean(t, dir)
	if got := headSubject(t, dir); got != "B2" {
		t.Errorf("HEAD subject = %q, want the pre-rebase local commit B2", got)
	}
}

func TestContinueAndAbortErrorWithNothingInProgress(t *testing.T) {
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

	// A no-op would leave a UI showing buttons for an operation that is not
	// there; erroring surfaces the bug instead of hiding it.
	if err := ContinueOperation(ExecRunner{}, dir); err == nil {
		t.Error("ContinueOperation on a clean repo must error")
	}
	if err := AbortOperation(ExecRunner{}, dir); err == nil {
		t.Error("AbortOperation on a clean repo must error")
	}
}
