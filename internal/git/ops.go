package git

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hoijun/fleet/internal/repo"
)

// Diff returns the working-tree changes against HEAD (all files), truncated so
// an AI prompt stays bounded. Empty string when the tree is clean.
func Diff(r Runner, dir string) string {
	out, _ := r.Run(dir, "diff", "HEAD")
	return capText(out, 12000)
}

// StagedDiff returns the staged changes (git diff --cached), truncated. Used to
// draft a commit message from what is about to be committed.
func StagedDiff(r Runner, dir string) string {
	out, _ := r.Run(dir, "diff", "--cached")
	return capText(out, 12000)
}

// capText trims whitespace and truncates to max bytes with a marker.
func capText(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) > max {
		return s[:max] + "\n...(truncated)"
	}
	return s
}

// Branches returns the current branch and all local branch names.
func Branches(r Runner, dir string) (current string, all []string, err error) {
	cur, err := r.Run(dir, "branch", "--show-current")
	if err != nil {
		return "", nil, err
	}
	current = strings.TrimSpace(cur)
	// A single positional arg keeps this call unambiguous for callers that key
	// on subcommand alone. Parsing below tolerates both a plain list of short
	// names and git's raw default "<hash> <type> <refname>" output, so it
	// works whether or not a caller-side --format is layered on top.
	out, err := r.Run(dir, "for-each-ref")
	if err != nil {
		return current, nil, err
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		name := fields[len(fields)-1]
		switch {
		case strings.HasPrefix(name, "refs/heads/"):
			name = strings.TrimPrefix(name, "refs/heads/")
		case strings.HasPrefix(name, "refs/"):
			continue // skip tags/remotes/etc.
		}
		all = append(all, name)
	}
	return current, all, nil
}

// CurrentBranch returns dir's current branch, resolved live via the real git
// binary. Returns "" on error or detached HEAD (git prints nothing for
// `branch --show-current` in that case).
func CurrentBranch(dir string) string {
	out, err := (ExecRunner{}).Run(dir, "branch", "--show-current")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Checkout switches to branch.
func Checkout(r Runner, dir, branch string) error {
	_, err := r.Run(dir, "checkout", branch)
	return err
}

// CommitAll stages everything then commits with msg.
func CommitAll(r Runner, dir, msg string) error {
	// Refuse to commit with unresolved merge conflicts: `add -A` would otherwise
	// stage the conflict markers. `ls-files -u` lists unmerged (conflicted) entries.
	if u, err := r.Run(dir, "ls-files", "-u"); err == nil && strings.TrimSpace(u) != "" {
		return errors.New("resolve merge conflicts before committing")
	}
	if _, err := r.Run(dir, "add", "-A"); err != nil {
		return err
	}
	_, err := r.Run(dir, "commit", "-m", msg)
	return err
}

// Push runs git push.
func Push(r Runner, dir string) error { _, err := r.Run(dir, "push"); return err }

// Clone clones url into dest. The runner's dir is irrelevant to `git clone`, so
// it runs from dest's parent; git creates dest itself.
func Clone(r Runner, url, dest string) error {
	_, err := r.Run(filepath.Dir(dest), "clone", url, dest)
	return err
}

// MergeUpstream merges the tracked upstream (@{u}) into the current branch,
// creating a merge commit when the histories diverged. See integrateUpstream
// for the conflict contract.
func MergeUpstream(r Runner, dir string) error { return integrateUpstream(r, dir, "merge") }

// RebaseUpstream replays the current branch's local commits on top of the
// tracked upstream (@{u}). See integrateUpstream for the conflict contract.
func RebaseUpstream(r Runner, dir string) error { return integrateUpstream(r, dir, "rebase") }

// OperationInProgress reports the git operation the repo is currently in the
// middle of - "merge", "rebase", or "" for none - by looking for the marker
// paths git itself uses. It is deliberately filesystem-based rather than a git
// invocation: it must be cheap enough to call alongside every repo load, and it
// stays correct for an operation started outside fleet.
func OperationInProgress(dir string) string {
	gitDir := filepath.Join(dir, ".git")
	// A worktree or submodule has a .git FILE pointing at the real directory.
	if fi, err := os.Stat(gitDir); err == nil && !fi.IsDir() {
		if data, err := os.ReadFile(gitDir); err == nil {
			if p, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir: "); ok {
				gitDir = p
			}
		}
	}
	for _, m := range []struct{ path, op string }{
		{"MERGE_HEAD", "merge"},
		{"rebase-merge", "rebase"},
		{"rebase-apply", "rebase"},
	} {
		if _, err := os.Stat(filepath.Join(gitDir, m.path)); err == nil {
			return m.op
		}
	}
	return ""
}

// integrateUpstream runs `git <mode> @{u}` (mode is "merge" or "rebase").
//
// It refuses to start when the repo is ALREADY mid-merge or mid-rebase: starting
// a second operation on top of one in progress is wrong regardless of who
// started the first. The diverged banner that leads here stays lit mid-merge
// (ahead/behind come from `# branch.ab`, which the in-progress merge does not
// change), so this is reachable in one click, not a corner case. The user
// finishes the first operation through ContinueOperation/AbortOperation.
//
// When fleet's OWN invocation fails, the outcome depends on the failure: a real
// content conflict is left in place and reported as ErrConflict, because the
// conflict panel can now resolve it; anything else is rolled back with
// `<mode> --abort` so the tree is never stranded with nothing to act on.
func integrateUpstream(r Runner, dir, mode string) error {
	if op := OperationInProgress(dir); op != "" {
		return fmt.Errorf("a %s is already in progress here; finish or abort it in a terminal first", op)
	}
	_, err := r.Run(dir, mode, "@{u}")
	if err == nil {
		return nil
	}
	// What to do with a failure depends on whether a human can act on it, and
	// `ls-files -u` is what separates the two cases.
	unmerged, _ := r.Run(dir, "ls-files", "-u")
	if strings.TrimSpace(unmerged) != "" {
		// A real content conflict is KEPT. fleet can list it, resolve each path
		// to either side, and continue or abort (conflict.go), so unwinding here
		// would throw away work the user is able to finish. The sentinel lets
		// the caller open the conflict panel instead of reporting a dead end.
		return fmt.Errorf("%s stopped on a conflict: %w", mode, ErrConflict)
	}
	// No unmerged paths and yet git failed: it applied the content and only
	// failed to write the commit - a rejecting commit-msg/pre-merge-commit hook,
	// commit.gpgsign with a broken signer, an unset user identity. There is
	// nothing for a human to resolve, and leaving the repo mid-merge (mid-rebase:
	// on a detached HEAD) would poison every later attempt, because the guard
	// above would then refuse a mess fleet itself made.
	//
	// The guard proved nothing was in progress before this call, so whatever is
	// in progress now is fleet's own and is safe to roll back.
	if OperationInProgress(dir) != "" {
		_, _ = r.Run(dir, mode, "--abort")
	}
	// Not a conflict (dirty tree, no upstream, a hook, a signing failure, ...).
	// Surface git's own diagnostic, which the runner already wrapped into err.
	return err
}

// DiffFile returns a diff for a single file. It diffs against HEAD so both
// staged and unstaged changes to tracked files show up; for an untracked (new)
// file - which has no HEAD diff - it falls back to showing the file's full
// content as an all-added diff via --no-index against /dev/null.
func DiffFile(r Runner, dir, file string) (string, error) {
	out, err := r.Run(dir, "diff", "HEAD", "--", file)
	if err == nil && strings.TrimSpace(out) != "" {
		return out, nil
	}
	// Untracked file, or a repo with no commits (unborn HEAD): show the whole
	// file as added. --no-index exits non-zero when the files differ, which is
	// the normal case here, so the error is ignored in favor of the output.
	if alt, _ := r.Run(dir, "diff", "--no-index", "/dev/null", file); strings.TrimSpace(alt) != "" {
		return alt, nil
	}
	return out, err
}

// WorktreeDiff returns the full, uncapped working-tree diff for the human "view
// all changes" modal. Unlike Diff, which caps output for AI prompts, this is
// uncapped so the reader sees everything - and it includes untracked (new)
// files, which `git diff HEAD` alone omits, so it matches what the per-file
// DiffFile view shows for every file in the Changed list.
func WorktreeDiff(r Runner, dir string) (string, error) {
	// Tracked changes vs HEAD. On an unborn HEAD (a repo with no commits) this
	// errors; the error is intentionally ignored so untracked files below still
	// render, mirroring DiffFile's own unborn-HEAD handling.
	tracked, _ := r.Run(dir, "diff", "HEAD")

	var b strings.Builder
	b.WriteString(strings.TrimRight(tracked, "\n"))

	// Append each untracked file as an all-added diff (--no-index against
	// /dev/null), the same fallback DiffFile uses. Without this a repo whose only
	// changes are new files would render an empty combined diff.
	others, _ := r.Run(dir, "ls-files", "--others", "--exclude-standard")
	for _, f := range strings.Split(strings.ReplaceAll(others, "\r\n", "\n"), "\n") {
		if f = strings.TrimSpace(f); f == "" {
			continue
		}
		alt, _ := r.Run(dir, "diff", "--no-index", "/dev/null", f)
		if strings.TrimSpace(alt) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(strings.TrimRight(alt, "\n"))
	}
	return b.String(), nil
}

// Log returns the last n commits.
func Log(r Runner, dir string, n int) ([]repo.Commit, error) {
	out, err := r.Run(dir, "log", "-n", strconv.Itoa(n), "--format=%H%x1f%an%x1f%cI%x1f%s")
	if err != nil {
		return nil, err
	}
	var commits []repo.Commit
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		commits = append(commits, parseLastCommit(line))
	}
	return commits, nil
}

// StashList returns the stash entries.
func StashList(r Runner, dir string) ([]string, error) {
	out, err := r.Run(dir, "stash", "list")
	if err != nil {
		return nil, err
	}
	var list []string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line != "" {
			list = append(list, line)
		}
	}
	return list, nil
}

// Stash saves the working tree; StashPop restores the latest.
func Stash(r Runner, dir string) error    { _, err := r.Run(dir, "stash", "push"); return err }
func StashPop(r Runner, dir string) error { _, err := r.Run(dir, "stash", "pop"); return err }

// StashApply restores stash entry i (stash@{i}) WITHOUT removing it; StashDrop
// deletes entry i. i is the entry's index in StashList (0 is newest).
func StashApply(r Runner, dir string, i int) error {
	_, err := r.Run(dir, "stash", "apply", "stash@{"+strconv.Itoa(i)+"}")
	return err
}
func StashDrop(r Runner, dir string, i int) error {
	_, err := r.Run(dir, "stash", "drop", "stash@{"+strconv.Itoa(i)+"}")
	return err
}

// CreateBranch creates branch name and switches to it (git checkout -b).
func CreateBranch(r Runner, dir, name string) error {
	_, err := r.Run(dir, "checkout", "-b", name)
	return err
}

// DeleteBranch deletes branch name with git's safe delete (-d), which refuses an
// unmerged branch; that refusal surfaces as the returned error.
func DeleteBranch(r Runner, dir, name string) error {
	_, err := r.Run(dir, "branch", "-d", name)
	return err
}

// DeleteBranchForce deletes branch name with git's force delete (-D), removing
// even an unmerged branch and losing its unmerged commits. It is separate from
// DeleteBranch so the force path is explicit at every call site, never a flag
// reachable by accident; the UI offers it only after the safe delete is refused.
func DeleteBranchForce(r Runner, dir, name string) error {
	_, err := r.Run(dir, "branch", "-D", name)
	return err
}

// StageFile stages a single path; UnstageFile removes a path from the index,
// leaving the working-tree change intact.
func StageFile(r Runner, dir, file string) error {
	_, err := r.Run(dir, "add", "--", file)
	return err
}
func UnstageFile(r Runner, dir, file string) error {
	_, err := r.Run(dir, "restore", "--staged", "--", file)
	return err
}

// CommitStaged commits only what is already staged (no implicit add). Git errors
// when nothing is staged, which surfaces to the caller. It also refuses while any
// path is unmerged, so a conflict cannot be committed with its markers.
func CommitStaged(r Runner, dir, msg string) error {
	if u, err := r.Run(dir, "ls-files", "-u"); err == nil && strings.TrimSpace(u) != "" {
		return errors.New("resolve merge conflicts before committing")
	}
	_, err := r.Run(dir, "commit", "-m", msg)
	return err
}

// CommitAmend replaces the last commit, folding in whatever is currently staged
// and using msg as the new message.
func CommitAmend(r Runner, dir, msg string) error {
	_, err := r.Run(dir, "commit", "--amend", "-m", msg)
	return err
}

// LastCommitMessage returns HEAD's full commit message (for prefilling an amend).
func LastCommitMessage(r Runner, dir string) (string, error) {
	out, err := r.Run(dir, "log", "-1", "--pretty=%B")
	return strings.TrimRight(out, "\n"), err
}

// FileStatus is a single changed path with its index (staged) and worktree
// (unstaged) state, from `git status --porcelain=v2`. Conflict marks an unmerged
// path: it must be resolved externally before it can be staged/committed (staging
// it blindly would commit the conflict markers).
type FileStatus struct {
	Path     string `json:"path"`
	Staged   bool   `json:"staged"`
	Unstaged bool   `json:"unstaged"`
	Conflict bool   `json:"conflict"`
}

// StatusFiles returns per-file staged/unstaged state for the staging UI. Unlike
// parseStatus (which flattens to a dirty list) this preserves the XY codes.
func StatusFiles(r Runner, dir string) ([]FileStatus, error) {
	out, err := r.Run(dir, "status", "--porcelain=v2")
	if err != nil {
		return nil, err
	}
	return parseStatusFiles(out), nil
}

// parseStatusFiles decodes porcelain v2 entries into per-file staging state.
func parseStatusFiles(out string) []FileStatus {
	var files []FileStatus
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case '1', '2':
			// "1 XY ..." / "2 XY ..." - XY is the second whitespace field.
			fields := strings.SplitN(line, " ", 3)
			if len(fields) < 3 || len(fields[1]) < 2 {
				continue
			}
			xy := fields[1]
			files = append(files, FileStatus{
				Path:     changedPath(line),
				Staged:   xy[0] != '.',
				Unstaged: xy[1] != '.',
			})
		case 'u':
			// Unmerged: must be resolved externally before it can be committed.
			files = append(files, FileStatus{Path: changedPath(line), Unstaged: true, Conflict: true})
		case '?':
			files = append(files, FileStatus{Path: strings.TrimPrefix(line, "? "), Unstaged: true})
		}
	}
	return files
}
