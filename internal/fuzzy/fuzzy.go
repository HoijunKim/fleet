// Package fuzzy scores subsequence matches for quick-open style file search:
// "cbsv" matches "CommitBox.svelte", ranked by how tight and well-placed the
// match is. It is the fzf/Ctrl-P heuristic kept small - greedy left-to-right,
// not optimal alignment.
package fuzzy

import "strings"

const (
	scoreContiguous = 8  // adjacent query chars matching adjacent candidate chars
	scoreBoundary   = 12 // match at a word/segment boundary (start, /._- space, camelCase)
	earlyMax        = 10 // earliness bonus for the first matched char, decaying by position
	basenameBonus   = 15 // a match concentrated in the file name beats one spread over the path
)

// Match reports whether query is a case-insensitive subsequence of candidate,
// and a score (higher is a better match). An empty query matches with score 0.
//
// Quick-open expects the file name to dominate, but a greedy left-to-right scan
// of the whole path can grab an early char in a directory segment (the 'c' in
// ".../src/...") and miss the boundary char in the base name. So Match scores
// the base name separately and takes the better of (full path) and (base name +
// bonus).
func Match(query, candidate string) (int, bool) {
	if query == "" {
		return 0, true
	}
	full, ok := matchIn(query, candidate)
	if base := candidate[lastSep(candidate)+1:]; base != candidate {
		if bs, bok := matchIn(query, base); bok {
			if s := bs + basenameBonus; !ok || s > full {
				return s, true
			}
		}
	}
	return full, ok
}

func lastSep(s string) int {
	if i := strings.LastIndexAny(s, "/\\"); i >= 0 {
		return i
	}
	return -1
}

func matchIn(query, candidate string) (int, bool) {
	q := strings.ToLower(query)
	c := strings.ToLower(candidate)

	score := 0
	ci := 0          // index into candidate
	prevMatch := -2  // candidate index of the previous matched char
	firstMatch := -1 // candidate index of the first matched char
	for qi := 0; qi < len(q); qi++ {
		found := -1
		for ; ci < len(c); ci++ {
			if c[ci] == q[qi] {
				found = ci
				break
			}
		}
		if found < 0 {
			return 0, false // q[qi] not present in the remainder -> not a subsequence
		}
		if firstMatch < 0 {
			firstMatch = found
		}
		if found == prevMatch+1 {
			score += scoreContiguous
		}
		if isBoundary(candidate, found) {
			score += scoreBoundary
		}
		prevMatch = found
		ci = found + 1
	}
	// Earliness: reward a match that starts near the front of the candidate.
	if firstMatch >= 0 {
		if b := earlyMax - firstMatch; b > 0 {
			score += b
		}
	}
	return score, true
}

// isBoundary reports whether the char at index i in s begins a new segment: the
// start of the string, right after a separator, or a lowercase->uppercase
// (camelCase) transition.
func isBoundary(s string, i int) bool {
	if i == 0 {
		return true
	}
	prev := s[i-1]
	switch prev {
	case '/', '\\', '.', '_', '-', ' ':
		return true
	}
	cur := s[i]
	if prev >= 'a' && prev <= 'z' && cur >= 'A' && cur <= 'Z' {
		return true // camelCase boundary
	}
	return false
}
