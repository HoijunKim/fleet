package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/hoijun/fleet/internal/config"
	"github.com/hoijun/fleet/internal/git"
	"github.com/hoijun/fleet/internal/repo"
)

// mode is the current interaction mode.
type mode int

const (
	modeList mode = iota
	modeFilter
	modeRunPrompt
	modeOutput
)

// Model is the root Bubbletea model.
type Model struct {
	cfg    config.Config
	runner git.Runner

	repos  []repo.Repo
	cursor int

	mode       mode
	filter     string
	runInput   string
	showDetail bool

	output string // last command / git output
	status string // one-line status message

	spinner spinner.Model
	loading int // number of repos still loading
	width   int
	height  int
	keys    keyMap
}

// New builds the initial model from a config, a git runner, and the already
// discovered (but not yet loaded) repos.
func New(cfg config.Config, runner git.Runner, repos []repo.Repo) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	return Model{
		cfg:     cfg,
		runner:  runner,
		repos:   repos,
		mode:    modeList,
		keys:    defaultKeys(),
		spinner: sp,
		loading: len(repos),
	}
}

// Init kicks off a load command for every repo plus the spinner tick.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick}
	for _, r := range m.repos {
		cmds = append(cmds, loadRepoCmd(m.runner, r))
	}
	return tea.Batch(cmds...)
}

// visible returns the repos matching the active filter.
func (m Model) visible() []repo.Repo {
	if m.filter == "" {
		return m.repos
	}
	needle := strings.ToLower(m.filter)
	var out []repo.Repo
	for _, r := range m.repos {
		if strings.Contains(strings.ToLower(r.Name), needle) {
			out = append(out, r)
		}
	}
	return out
}

// selected returns the repo under the cursor within the visible list.
func (m Model) selected() (repo.Repo, bool) {
	v := m.visible()
	if m.cursor < 0 || m.cursor >= len(v) {
		return repo.Repo{}, false
	}
	return v[m.cursor], true
}

// View renders the current state. This is a minimal placeholder: tea.Model
// requires a View method to satisfy the interface, but real rendering is out
// of scope for this task (Model + Update) and belongs to a dedicated
// view-rendering task not yet briefed. Without this stub the package cannot
// compile, since Update's signature returns tea.Model.
func (m Model) View() string {
	return ""
}
