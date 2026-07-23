package git

import (
	"errors"
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
