package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hoijun/fleet/internal/config"
	"github.com/hoijun/fleet/internal/repo"
)

type stubErr struct{ msg string }

func (e *stubErr) Error() string { return e.msg }

func newTestModel() Model {
	repos := []repo.Repo{
		{Name: "alpha", Path: "/a", IsGit: true},
		{Name: "beta", Path: "/b", IsGit: true},
		{Name: "gamma", Path: "/g", IsGit: true},
	}
	return New(config.Default(), okRunner{}, repos)
}

func send(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

func TestCursorDownUp(t *testing.T) {
	m := newTestModel()
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if m.cursor != 1 {
		t.Errorf("cursor=%d want 1", m.cursor)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if m.cursor != 0 {
		t.Errorf("cursor=%d want 0", m.cursor)
	}
}

func TestCursorClampsAtEnds(t *testing.T) {
	m := newTestModel()
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")}) // already at top
	if m.cursor != 0 {
		t.Errorf("cursor=%d want 0", m.cursor)
	}
	for i := 0; i < 10; i++ {
		m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	}
	if m.cursor != 2 {
		t.Errorf("cursor=%d want 2 (clamped)", m.cursor)
	}
}

func TestRepoLoadedMergesByPath(t *testing.T) {
	m := newTestModel()
	loaded := repo.Repo{Name: "beta", Path: "/b", IsGit: true, Branch: "dev", Loaded: true}
	m = send(m, repoLoadedMsg(loaded))
	if m.repos[1].Branch != "dev" || !m.repos[1].Loaded {
		t.Errorf("merge failed: %+v", m.repos[1])
	}
	// other repos untouched
	if m.repos[0].Loaded {
		t.Error("alpha should be untouched")
	}
}

func TestFilterNarrowsVisible(t *testing.T) {
	m := newTestModel()
	m.filter = "ga"
	v := m.visible()
	if len(v) != 1 || v[0].Name != "gamma" {
		t.Errorf("visible=%+v", v)
	}
}

func TestFetchDoneRecordsError(t *testing.T) {
	m := newTestModel()
	m = send(m, fetchDoneMsg{Path: "/a", Err: &stubErr{msg: "network down"}})
	if m.status == "" {
		t.Error("expected status line to report fetch error")
	}
}

func TestQuitKey(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("expected QuitMsg, got %T", cmd())
	}
}

func TestFilterBackspaceIsRuneSafe(t *testing.T) {
	m := newTestModel()
	m.mode = modeFilter
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("가")})
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = send(m, tea.KeyMsg{Type: tea.KeyBackspace})
	if m.filter != "가" {
		t.Errorf("filter=%q want 가 (one rune removed, not one byte)", m.filter)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyBackspace})
	if m.filter != "" {
		t.Errorf("filter=%q want empty; must not leave an invalid UTF-8 tail", m.filter)
	}
}
