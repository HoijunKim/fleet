// Package scan discovers repositories under configured root directories.
package scan

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/hoijun/fleet/internal/repo"
)

// Discover walks each root up to depth levels deep, returning one Repo per
// immediate/nested directory. A directory containing a ".git" entry is marked
// IsGit and its subtree is not descended into further. Plain (non-git) folders
// are included only when showNonGit is true. Results are sorted by Name.
//
// depth is measured from a root's direct children: depth 1 = direct children
// only, depth 2 = children and grandchildren, etc.
func Discover(roots []string, depth int, showNonGit bool) []repo.Repo {
	seen := map[string]bool{}
	var out []repo.Repo
	for _, root := range roots {
		walk(root, 1, depth, showNonGit, seen, &out)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func walk(dir string, level, maxDepth int, showNonGit bool, seen map[string]bool, out *[]repo.Repo) {
	if level > maxDepth {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == ".git" {
			continue
		}
		full := filepath.Join(dir, e.Name())
		if isGit(full) {
			if !seen[full] {
				seen[full] = true
				*out = append(*out, repo.Repo{Name: e.Name(), Path: full, IsGit: true})
			}
			continue // do not descend into a repo
		}
		// plain directory: optionally record, then descend
		if showNonGit && !seen[full] {
			seen[full] = true
			*out = append(*out, repo.Repo{Name: e.Name(), Path: full, IsGit: false})
		}
		walk(full, level+1, maxDepth, showNonGit, seen, out)
	}
}

func isGit(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.IsDir()
}
