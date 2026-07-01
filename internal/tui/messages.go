package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/hoijun/fleet/internal/action"
	"github.com/hoijun/fleet/internal/git"
	"github.com/hoijun/fleet/internal/meta"
	"github.com/hoijun/fleet/internal/repo"
)

// reposMsg carries the initial discovered repo list.
type reposMsg []repo.Repo

// repoLoadedMsg is one repo after git + meta finished. It aliases repo.Repo so
// the model can merge it back by Path.
type repoLoadedMsg repo.Repo

// fetchDoneMsg reports the result of a fetch/pull for a given repo path.
type fetchDoneMsg struct {
	Path string
	Err  error
}

// cmdOutputMsg carries output to show in the output pane.
type cmdOutputMsg struct {
	Title string
	Out   string
	Err   error
}

// loadRepoCmd loads git + meta for one repo off the UI goroutine.
func loadRepoCmd(r git.Runner, rp repo.Repo) tea.Cmd {
	return func() tea.Msg {
		if rp.IsGit {
			git.Load(r, &rp)
		}
		rp.Language, rp.SizeBytes, rp.HasReadme = meta.Detect(rp.Path)
		rp.Loaded = true
		return repoLoadedMsg(rp)
	}
}

func fetchCmd(r git.Runner, path string) tea.Cmd {
	return func() tea.Msg {
		return fetchDoneMsg{Path: path, Err: git.Fetch(r, path)}
	}
}

func pullCmd(r git.Runner, path string) tea.Cmd {
	return func() tea.Msg {
		return fetchDoneMsg{Path: path, Err: git.Pull(r, path)}
	}
}

func runCmd(dir, line string) tea.Cmd {
	return func() tea.Msg {
		out, err := action.RunInDir(dir, line)
		return cmdOutputMsg{Title: line, Out: out, Err: err}
	}
}

// repoFromLoaded converts a repoLoadedMsg back to a repo.Repo.
func repoFromLoaded(msg repoLoadedMsg) repo.Repo {
	return repo.Repo(msg)
}
