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

// GrepOpts selects how the query is interpreted. The default is a fixed-string
// (literal) search - what an interactive search should do; Regex switches to an
// extended regular expression, WholeWord matches only whole words, and
// IgnoreCase folds case.
type GrepOpts struct {
	IgnoreCase bool
	Regex      bool
	WholeWord  bool
}

// Grep runs `git grep -n -I [flags] -e <query>` in dir over tracked files. git
// grep exits non-zero (1) with empty output when nothing matches; that is treated
// as no hits, not an error.
func Grep(r Runner, dir, query string, opts GrepOpts) ([]GrepHit, error) {
	args := []string{"grep", "-n", "-I"}
	if opts.IgnoreCase {
		args = append(args, "-i")
	}
	if opts.WholeWord {
		args = append(args, "-w")
	}
	if opts.Regex {
		args = append(args, "-E") // extended regex
	} else {
		args = append(args, "-F") // fixed string: literal, the default
	}
	args = append(args, "-e", query)
	out, err := r.Run(dir, args...)
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
