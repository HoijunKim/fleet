package git

import (
	"strconv"
	"strings"

	"github.com/hoijun/fleet/internal/repo"
)

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

// Checkout switches to branch.
func Checkout(r Runner, dir, branch string) error { _, err := r.Run(dir, "checkout", branch); return err }

// CommitAll stages everything then commits with msg.
func CommitAll(r Runner, dir, msg string) error {
	if _, err := r.Run(dir, "add", "-A"); err != nil {
		return err
	}
	_, err := r.Run(dir, "commit", "-m", msg)
	return err
}

// Push runs git push.
func Push(r Runner, dir string) error { _, err := r.Run(dir, "push"); return err }

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
