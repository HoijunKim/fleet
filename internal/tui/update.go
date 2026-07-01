package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/hoijun/fleet/internal/action"
)

// Update handles all incoming messages and key presses.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case repoLoadedMsg:
		m.mergeLoaded(msg)
		return m, nil

	case fetchDoneMsg:
		if msg.Err != nil {
			m.status = fmt.Sprintf("fetch/pull failed for %s: %v", msg.Path, msg.Err)
		} else {
			m.status = "fetch/pull done: " + msg.Path
			// refresh that repo
			for _, r := range m.repos {
				if r.Path == msg.Path {
					return m, loadRepoCmd(m.runner, r)
				}
			}
		}
		return m, nil

	case cmdOutputMsg:
		m.mode = modeOutput
		if msg.Err != nil {
			m.output = fmt.Sprintf("$ %s\n%s\n[error: %v]", msg.Title, msg.Out, msg.Err)
		} else {
			m.output = fmt.Sprintf("$ %s\n%s", msg.Title, msg.Out)
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) mergeLoaded(msg repoLoadedMsg) {
	for i := range m.repos {
		if m.repos[i].Path == msg.Path {
			m.repos[i] = repoFromLoaded(msg)
			if m.loading > 0 {
				m.loading--
			}
			return
		}
	}
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Text-input modes consume keys first.
	switch m.mode {
	case modeFilter:
		return m.handleFilterKey(msg)
	case modeRunPrompt:
		return m.handleRunKey(msg)
	case modeOutput:
		if msg.String() == "esc" || msg.String() == "q" {
			m.mode = modeList
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Down):
		if m.cursor < len(m.visible())-1 {
			m.cursor++
		}
		return m, nil
	case key.Matches(msg, m.keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case key.Matches(msg, m.keys.Detail):
		m.showDetail = !m.showDetail
		return m, nil
	case key.Matches(msg, m.keys.Filter):
		m.mode = modeFilter
		return m, nil
	case key.Matches(msg, m.keys.Run):
		m.mode = modeRunPrompt
		m.runInput = ""
		return m, nil
	case key.Matches(msg, m.keys.Fetch):
		if r, ok := m.selected(); ok {
			if r.IsGit {
				m.status = "fetching " + r.Name + "..."
				return m, fetchCmd(m.runner, r.Path)
			}
			m.status = r.Name + " is not a git repo"
		}
	case key.Matches(msg, m.keys.FetchAll):
		var cmds []tea.Cmd
		for _, r := range m.repos {
			if r.IsGit {
				cmds = append(cmds, fetchCmd(m.runner, r.Path))
			}
		}
		m.status = "fetching all..."
		return m, tea.Batch(cmds...)
	case key.Matches(msg, m.keys.Pull):
		if r, ok := m.selected(); ok {
			if r.IsGit {
				m.status = "pulling " + r.Name + "..."
				return m, pullCmd(m.runner, r.Path)
			}
			m.status = r.Name + " is not a git repo"
		}
	case key.Matches(msg, m.keys.Refresh):
		if r, ok := m.selected(); ok {
			return m, loadRepoCmd(m.runner, r)
		}
	case key.Matches(msg, m.keys.RefreshAll):
		var cmds []tea.Cmd
		for _, r := range m.repos {
			cmds = append(cmds, loadRepoCmd(m.runner, r))
		}
		m.loading = len(m.repos)
		m.status = "refreshing all..."
		return m, tea.Batch(cmds...)
	case key.Matches(msg, m.keys.Edit):
		if r, ok := m.selected(); ok {
			if err := action.EditorCmd(m.cfg.Editor, r.Path).Start(); err != nil {
				m.status = "editor failed: " + err.Error()
			} else {
				m.status = "opened editor: " + r.Name
			}
		}
		return m, nil
	case key.Matches(msg, m.keys.Term):
		if r, ok := m.selected(); ok {
			if err := action.TerminalCmd(m.cfg.Terminal, r.Path).Start(); err != nil {
				m.status = "terminal failed: " + err.Error()
			} else {
				m.status = "opened terminal: " + r.Name
			}
		}
		return m, nil
	}
	return m, nil
}

func (m Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.filter = ""
	case "enter":
		m.mode = modeList
	case "backspace":
		if r := []rune(m.filter); len(r) > 0 {
			m.filter = string(r[:len(r)-1])
		}
	default:
		if len(msg.Runes) > 0 {
			m.filter += string(msg.Runes)
		}
	}
	if m.cursor >= len(m.visible()) {
		m.cursor = 0
	}
	return m, nil
}

func (m Model) handleRunKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.runInput = ""
	case "enter":
		line := m.runInput
		m.mode = modeList
		m.runInput = ""
		if r, ok := m.selected(); ok && line != "" {
			return m, runCmd(r.Path, line)
		}
	case "backspace":
		if r := []rune(m.runInput); len(r) > 0 {
			m.runInput = string(r[:len(r)-1])
		}
	default:
		if len(msg.Runes) > 0 {
			m.runInput += string(msg.Runes)
		}
	}
	return m, nil
}
