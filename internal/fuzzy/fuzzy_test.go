package fuzzy

import "testing"

func TestMatchSubsequenceHitAndMiss(t *testing.T) {
	if _, ok := Match("cbsv", "CommitBox.svelte"); !ok {
		t.Error("cbsv should be a subsequence of CommitBox.svelte")
	}
	if _, ok := Match("xyz", "CommitBox.svelte"); ok {
		t.Error("xyz is not a subsequence of CommitBox.svelte")
	}
	// Empty query matches anything (score 0).
	if _, ok := Match("", "anything"); !ok {
		t.Error("empty query should match")
	}
}

func TestMatchIsCaseInsensitive(t *testing.T) {
	if _, ok := Match("COMMIT", "commitbox.svelte"); !ok {
		t.Error("match must be case-insensitive")
	}
}

func TestContiguousOutscoresScattered(t *testing.T) {
	contig, ok1 := Match("commit", "CommitBox.svelte")
	scattered, ok2 := Match("cmtbx", "CommitBox.svelte")
	if !ok1 || !ok2 {
		t.Fatalf("both should match: %v %v", ok1, ok2)
	}
	if contig <= scattered {
		t.Errorf("contiguous 'commit' (%d) should outscore scattered 'cmtbx' (%d)", contig, scattered)
	}
}

func TestBoundaryOutscoresMidWord(t *testing.T) {
	// "box" at a camelCase boundary in CommitBox vs mid-token in "iconboxed".
	boundary, ok1 := Match("box", "CommitBox.svelte")
	mid, ok2 := Match("box", "iconboxed.go")
	if !ok1 || !ok2 {
		t.Fatalf("both should match: %v %v", ok1, ok2)
	}
	if boundary <= mid {
		t.Errorf("boundary match (%d) should outscore mid-word match (%d)", boundary, mid)
	}
}

func TestBestOfManyRanksTightMatchFirst(t *testing.T) {
	cands := []string{"internal/git/commitbox_helper.go", "frontend/src/lib/CommitBox.svelte", "docs/box.md"}
	best, bestScore := "", -1
	for _, c := range cands {
		if s, ok := Match("cbox", c); ok && s > bestScore {
			bestScore, best = s, c
		}
	}
	if best != "frontend/src/lib/CommitBox.svelte" {
		t.Errorf("cbox should rank CommitBox.svelte first, got %q", best)
	}
}
