package git

import (
	"strings"
	"testing"
)

// diffFake returns canned output distinguishing `git diff HEAD -- file` (the
// tracked-change path) from `git diff --no-index /dev/null file` (untracked).
type diffFake struct {
	head    string // output for "diff HEAD ..."
	noIndex string // output for "diff --no-index ..."
}

func (f diffFake) Run(dir string, args ...string) (string, error) {
	if len(args) >= 2 && args[1] == "--no-index" {
		return f.noIndex, nil
	}
	return f.head, nil
}

func TestDiffFileTrackedUsesHead(t *testing.T) {
	f := diffFake{head: "diff --git a/x b/x\n-old\n+new\n"}
	got, err := DiffFile(f, "/r", "x.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "+new") {
		t.Errorf("tracked file should show HEAD diff, got %q", got)
	}
}

func TestDiffFileUntrackedFallsBackToNoIndex(t *testing.T) {
	// HEAD diff is empty (untracked file is not in HEAD); fall back to --no-index.
	f := diffFake{head: "", noIndex: "diff --git a/new b/new\n+hello world\n"}
	got, err := DiffFile(f, "/r", "new.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "+hello world") {
		t.Errorf("untracked file should fall back to no-index content, got %q", got)
	}
}
