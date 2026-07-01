package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/hoijun/fleet/internal/repo"
)

func TestViewListsRepoNames(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 100, 30
	out := m.View()
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if !strings.Contains(out, name) {
			t.Errorf("view missing %q", name)
		}
	}
}

func TestViewShowsHeaderCounts(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 100, 30
	m.repos[0].Dirty = true
	m.repos[0].ModifiedCount = 2
	out := m.View()
	if !strings.Contains(out, "repos 3") {
		t.Errorf("expected repo count in header, got:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "dirty 1") {
		t.Errorf("expected dirty count in header, got:\n%s", out)
	}
}

func TestViewOutputMode(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 100, 30
	m.mode = modeOutput
	m.output = "$ ls\nfoo bar"
	out := m.View()
	if !strings.Contains(out, "foo bar") {
		t.Errorf("output pane not shown: %s", out)
	}
}

func TestViewFilterPrompt(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 100, 30
	m.mode = modeFilter
	m.filter = "al"
	out := m.View()
	if !strings.Contains(out, "filter:") || !strings.Contains(out, "al") {
		t.Errorf("filter prompt not shown: %s", out)
	}
}

func TestViewDetailPanel(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 100, 30
	m.showDetail = true
	m.repos[0].Path = "/repo/alpha"
	out := m.View()
	if !strings.Contains(out, "detail:") || !strings.Contains(out, "/repo/alpha") {
		t.Errorf("detail panel not shown: %s", out)
	}
}

func TestViewRunPromptBar(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 100, 30
	m.mode = modeRunPrompt
	m.runInput = "go build"
	out := m.View()
	if !strings.Contains(out, "run in") || !strings.Contains(out, "go build") {
		t.Errorf("run prompt bar not shown: %s", out)
	}
}

var _ = repo.Repo{}

func TestTruncRuneSafe(t *testing.T) {
	got := trunc("가나다라마", 3)
	if !utf8.ValidString(got) {
		t.Errorf("trunc produced invalid UTF-8: %q", got)
	}
}

func TestViewHasNoSpecialTypographicChars(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 100, 30
	m.showDetail = true
	m.repos[0].Path = "/repo/alpha"
	out := m.View()
	for _, bad := range []rune{'—', '–', '·', '…', '─'} {
		if strings.ContainsRune(out, bad) {
			t.Errorf("view contains disallowed char %q", bad)
		}
	}
}
