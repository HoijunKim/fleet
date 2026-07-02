package git

import (
	"strconv"
	"strings"
)

// GrepHit is one matching line: the repo-relative file, its 1-based line
// number, and the matched line's text.
type GrepHit struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// Grep runs `git grep -n -I -e <query>` in dir over tracked files. git grep
// exits non-zero (1) with empty output when nothing matches; that is treated as
// no hits, not an error.
func Grep(r Runner, dir, query string) ([]GrepHit, error) {
	out, err := r.Run(dir, "grep", "-n", "-I", "-e", query)
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, nil // no matches (or a benign non-zero exit)
	}
	var hits []GrepHit
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		// format: <file>:<line>:<text> - split on the first two colons only,
		// so colons inside the text are preserved.
		i1 := strings.IndexByte(line, ':')
		if i1 < 0 {
			continue
		}
		rest := line[i1+1:]
		i2 := strings.IndexByte(rest, ':')
		if i2 < 0 {
			continue
		}
		n, e := strconv.Atoi(rest[:i2])
		if e != nil {
			continue
		}
		hits = append(hits, GrepHit{File: line[:i1], Line: n, Text: rest[i2+1:]})
	}
	return hits, nil
}
