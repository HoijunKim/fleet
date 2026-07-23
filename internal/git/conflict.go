package git

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrConflict marks an operation that stopped on a real content conflict, as
// opposed to one that failed for any other reason. The distinction decides
// whether fleet unwinds (nothing for a human to resolve) or hands the tree to
// the conflict UI, so callers branch on errors.Is rather than on message text.
var ErrConflict = errors.New("merge conflict")

// Conflict kinds. They are not cosmetic: which sides carry content decides what
// "keep mine" can even mean for a path.
const (
	ConflictBothModified  = "both-modified"   // stages 1,2,3 - both sides edited
	ConflictBothAdded     = "both-added"      // stages 2,3 - both sides created it
	ConflictDeletedByThem = "deleted-by-them" // stages 1,2 - mine has content, theirs deleted
	ConflictDeletedByUs   = "deleted-by-us"   // stages 1,3 - mine deleted, theirs has content
	ConflictUnknown       = "unmerged"        // any stage combination git invents later
)

// Conflict is one unmerged path.
type Conflict struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

// Conflicts lists the repo's unmerged paths, one entry per path, sorted.
//
// `git ls-files -u` emits one line per index stage - up to three for the same
// path - as "<mode> <sha> <stage>\t<path>". The stage set is the only thing that
// distinguishes a both-edited file from a delete/modify pair, so the stages are
// collected per path rather than counted.
func Conflicts(r Runner, dir string) ([]Conflict, error) {
	out, err := r.Run(dir, "ls-files", "-u")
	if err != nil {
		return nil, err
	}
	stages := map[string]map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		// The path is everything after the first tab: it can contain spaces.
		meta, path, ok := strings.Cut(strings.TrimRight(line, "\r"), "\t")
		if !ok || path == "" {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) < 3 {
			continue
		}
		if stages[path] == nil {
			stages[path] = map[string]bool{}
		}
		stages[path][fields[2]] = true
	}

	out2 := make([]Conflict, 0, len(stages))
	for path, s := range stages {
		out2 = append(out2, Conflict{Path: path, Kind: kindFromStages(s)})
	}
	sort.Slice(out2, func(i, j int) bool { return out2[i].Path < out2[j].Path })
	return out2, nil
}

func kindFromStages(s map[string]bool) string {
	switch {
	case s["1"] && s["2"] && s["3"]:
		return ConflictBothModified
	case !s["1"] && s["2"] && s["3"]:
		return ConflictBothAdded
	case s["2"] && !s["3"]:
		return ConflictDeletedByThem
	case !s["2"] && s["3"]:
		return ConflictDeletedByUs
	default:
		return ConflictUnknown
	}
}

// Sides a user can choose, in the user's words rather than git's.
const (
	SideMine     = "mine"     // what this branch had
	SideIncoming = "incoming" // what is being merged in / rebased onto
	SideWorktree = "worktree" // whatever is on disk: the user edited it by hand
)

// checkoutFlag maps a user-facing side to git's --ours/--theirs for the given
// operation.
//
// During a REBASE the two are swapped relative to a merge: a rebase checks out
// the upstream and replays your commits onto it, so HEAD - git's "ours" - is the
// upstream, and the commit being applied - "theirs" - is yours. Mapping "mine"
// to --ours is right for a merge and silently discards the user's work in a
// rebase, which is why this lives in one tested function and the UI never
// derives it.
func checkoutFlag(mode, side string) (string, error) {
	rebase := mode == "rebase"
	switch side {
	case SideMine:
		if rebase {
			return "--theirs", nil
		}
		return "--ours", nil
	case SideIncoming:
		if rebase {
			return "--ours", nil
		}
		return "--theirs", nil
	default:
		return "", fmt.Errorf("unknown side %q", side)
	}
}

// sideIsDeletion reports whether the chosen side is the one that deleted the
// path, in which case there is no stage to check out and the resolution is a
// staged removal instead. The kinds are named from the merge point of view
// ("us" is HEAD), so under a rebase they refer to the opposite user-facing side.
func sideIsDeletion(kind, mode, side string) bool {
	rebase := mode == "rebase"
	switch kind {
	case ConflictDeletedByUs: // HEAD deleted it
		if rebase {
			return side == SideIncoming
		}
		return side == SideMine
	case ConflictDeletedByThem: // the other side deleted it
		if rebase {
			return side == SideMine
		}
		return side == SideIncoming
	default:
		return false
	}
}

// ResolveConflict resolves one unmerged path to the side the user picked and
// stages the result, so the file leaves the conflict list immediately.
//
// side is "mine", "incoming", or "worktree" for a file the user merged by hand
// in their editor. mode comes from OperationInProgress and decides the
// ours/theirs mapping.
func ResolveConflict(r Runner, dir, mode, file, side string) error {
	if side == SideWorktree {
		if _, err := r.Run(dir, "add", "--", file); err != nil {
			return err
		}
		return nil
	}
	flag, err := checkoutFlag(mode, side)
	if err != nil {
		return err
	}
	kind, err := conflictKind(r, dir, file)
	if err != nil {
		return err
	}
	if kind == "" {
		return fmt.Errorf("%s is not conflicted", file)
	}
	if sideIsDeletion(kind, mode, side) {
		// `checkout --ours` on a path with no such stage fails with a bare
		// error; keeping a deletion is a removal, not a checkout.
		if _, err := r.Run(dir, "rm", "-q", "--", file); err != nil {
			return err
		}
		return nil
	}
	if _, err := r.Run(dir, "checkout", flag, "--", file); err != nil {
		return err
	}
	_, err = r.Run(dir, "add", "--", file)
	return err
}

// ContinueOperation finishes the merge or rebase currently in progress.
//
// The mode is read from the on-disk markers rather than taken as an argument:
// the markers are the truth, and a stale mode from the UI is how `rebase
// --abort` gets run against a merge.
//
// Runner has no environment hook, so the editor git would open for the commit
// message is suppressed with `-c core.editor=true` - a no-op "editor" that exits
// 0, leaving git's default merge/rebase message.
func ContinueOperation(r Runner, dir string) error {
	mode := OperationInProgress(dir)
	if mode == "" {
		return errors.New("no merge or rebase in progress")
	}
	left, err := Conflicts(r, dir)
	if err != nil {
		return err
	}
	if len(left) > 0 {
		// git's own message ("you must edit all merge conflicts") does not say
		// which files; fleet knows, so it says.
		paths := make([]string, len(left))
		for i, c := range left {
			paths[i] = c.Path
		}
		return fmt.Errorf("%w: resolve %s first", ErrConflict, strings.Join(paths, ", "))
	}
	_, err = r.Run(dir, "-c", "core.editor=true", mode, "--continue")
	return err
}

// AbortOperation unwinds the merge or rebase currently in progress, restoring
// the state from before it started.
func AbortOperation(r Runner, dir string) error {
	mode := OperationInProgress(dir)
	if mode == "" {
		return errors.New("no merge or rebase in progress")
	}
	_, err := r.Run(dir, mode, "--abort")
	return err
}

// conflictKind returns the kind recorded for one path, or "" when the path is
// not conflicted.
func conflictKind(r Runner, dir, file string) (string, error) {
	cs, err := Conflicts(r, dir)
	if err != nil {
		return "", err
	}
	for _, c := range cs {
		if c.Path == file {
			return c.Kind, nil
		}
	}
	return "", nil
}
