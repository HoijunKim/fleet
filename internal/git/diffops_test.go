package git

import (
	"strings"
	"testing"
)

type diffopsFake struct{ head, cached string }

func (f diffopsFake) Run(dir string, args ...string) (string, error) {
	if len(args) >= 2 && args[0] == "diff" && args[1] == "HEAD" {
		return f.head, nil
	}
	if len(args) >= 2 && args[0] == "diff" && args[1] == "--cached" {
		return f.cached, nil
	}
	return "", nil
}

func TestDiffAndStaged(t *testing.T) {
	f := diffopsFake{head: "  working change  ", cached: "staged change"}
	if got := Diff(f, "/x"); got != "working change" {
		t.Errorf("Diff = %q", got)
	}
	if got := StagedDiff(f, "/x"); got != "staged change" {
		t.Errorf("StagedDiff = %q", got)
	}
}

func TestDiffCleanIsEmpty(t *testing.T) {
	if got := Diff(diffopsFake{}, "/x"); got != "" {
		t.Errorf("clean tree Diff should be empty, got %q", got)
	}
}

func TestDiffTruncates(t *testing.T) {
	big := strings.Repeat("x", 20000)
	got := Diff(diffopsFake{head: big}, "/x")
	if len(got) >= 20000 || !strings.HasSuffix(got, "(truncated)") {
		t.Errorf("expected truncated output, len=%d", len(got))
	}
}
