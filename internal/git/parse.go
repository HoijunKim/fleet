package git

import (
	"strconv"
	"strings"
	"time"

	"github.com/hoijun/fleet/internal/repo"
)

type statusResult struct {
	Branch      string
	Dirty       bool
	Modified    int
	Ahead       int
	Behind      int
	HasUpstream bool
	Files       []string
}

// parseStatus parses `git status --porcelain=v2 --branch` output.
func parseStatus(out string) statusResult {
	var r statusResult
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			r.Branch = strings.TrimSpace(strings.TrimPrefix(line, "# branch.head "))
		case strings.HasPrefix(line, "# branch.upstream "):
			r.HasUpstream = true
		case strings.HasPrefix(line, "# branch.ab "):
			fields := strings.Fields(line) // # branch.ab +2 -1
			if len(fields) == 4 {
				r.Ahead = atoiAbs(fields[2])
				r.Behind = atoiAbs(fields[3])
			}
		case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "2 "):
			r.Modified++
			r.Files = append(r.Files, changedPath(line))
		case strings.HasPrefix(line, "u "):
			r.Modified++
			r.Files = append(r.Files, changedPath(line))
		case strings.HasPrefix(line, "? "):
			r.Modified++
			r.Files = append(r.Files, strings.TrimPrefix(line, "? "))
		}
	}
	r.Dirty = r.Modified > 0
	return r
}

// changedPath extracts the path from a porcelain v2 changed entry. The number
// of fixed fields before the path depends on the entry type:
//
//	"1" (ordinary): 8 leading fields
//	"2" (rename/copy): 9 leading fields; the path field is "<new>\t<orig>"
//	"u" (unmerged): 10 leading fields
//
// For rename entries we keep the new path (before the tab).
func changedPath(line string) string {
	var lead int
	switch line[0] {
	case '1':
		lead = 8
	case '2':
		lead = 9
	case 'u':
		lead = 10
	default:
		return strings.TrimSpace(line)
	}
	fields := strings.SplitN(line, " ", lead+1)
	if len(fields) <= lead {
		return strings.TrimSpace(line)
	}
	p := fields[lead]
	if i := strings.IndexByte(p, '\t'); i >= 0 {
		p = p[:i]
	}
	return p
}

func atoiAbs(s string) int {
	s = strings.TrimPrefix(s, "+")
	s = strings.TrimPrefix(s, "-")
	n, _ := strconv.Atoi(s)
	return n
}

// parseLastCommit parses one line of `git log -1` with fields joined by \x1f:
// hash, author, ISO-8601 date, subject.
func parseLastCommit(out string) repo.Commit {
	out = strings.TrimRight(out, "\n")
	if out == "" {
		return repo.Commit{}
	}
	parts := strings.Split(out, "\x1f")
	c := repo.Commit{}
	if len(parts) > 0 {
		c.Hash = parts[0]
	}
	if len(parts) > 1 {
		c.Author = parts[1]
	}
	if len(parts) > 2 {
		if t, err := time.Parse(time.RFC3339, parts[2]); err == nil {
			c.When = t
		}
	}
	if len(parts) > 3 {
		c.Message = parts[3]
	}
	return c
}

// parseTodoCount sums the per-file counts from `git grep -cE "TODO|FIXME"`,
// whose lines look like "path:count".
func parseTodoCount(out string) int {
	total := 0
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		if i := strings.LastIndexByte(line, ':'); i >= 0 {
			n, err := strconv.Atoi(strings.TrimSpace(line[i+1:]))
			if err == nil {
				total += n
			}
		}
	}
	return total
}
