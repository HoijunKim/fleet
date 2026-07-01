package main

import (
	"context"

	"github.com/hoijun/fleet/internal/action"
	"github.com/hoijun/fleet/internal/config"
	"github.com/hoijun/fleet/internal/git"
	"github.com/hoijun/fleet/internal/meta"
	"github.com/hoijun/fleet/internal/repo"
	"github.com/hoijun/fleet/internal/scan"
)

// App is the Wails binding layer exposed to the front end.
type App struct {
	ctx    context.Context
	cfg    config.Config
	runner git.Runner
}

// NewApp builds the App with the real git runner and loaded config.
func NewApp() *App {
	cfg, _, _ := config.Load()
	return &App{cfg: cfg, runner: git.ExecRunner{}}
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// RepoView is the JS-serializable view of a repo (repo.Repo's error field does
// not serialize, so it becomes ErrMsg; last-commit fields are flattened).
type RepoView struct {
	Name        string   `json:"name"`
	Path        string   `json:"path"`
	IsGit       bool     `json:"isGit"`
	Branch      string   `json:"branch"`
	Dirty       bool     `json:"dirty"`
	Modified    int      `json:"modified"`
	Ahead       int      `json:"ahead"`
	Behind      int      `json:"behind"`
	HasUpstream bool     `json:"hasUpstream"`
	Remote      string   `json:"remote"`
	DirtyFiles  []string `json:"dirtyFiles"`
	LastHash    string   `json:"lastHash"`
	LastMsg     string   `json:"lastMsg"`
	LastAuthor  string   `json:"lastAuthor"`
	LastWhen    string   `json:"lastWhen"`
	Language    string   `json:"language"`
	SizeBytes   int64    `json:"sizeBytes"`
	Todo        int      `json:"todo"`
	ErrMsg      string   `json:"errMsg"`
	Loaded      bool     `json:"loaded"`
}

func toView(r repo.Repo) RepoView {
	v := RepoView{
		Name: r.Name, Path: r.Path, IsGit: r.IsGit, Branch: r.Branch,
		Dirty: r.Dirty, Modified: r.ModifiedCount, Ahead: r.Ahead, Behind: r.Behind,
		HasUpstream: r.HasUpstream, Remote: r.RemoteURL, DirtyFiles: r.DirtyFiles,
		LastHash: r.Last.Hash, LastMsg: r.Last.Message, LastAuthor: r.Last.Author,
		Language: r.Language, SizeBytes: r.SizeBytes, Todo: r.TodoCount, Loaded: r.Loaded,
	}
	if !r.Last.When.IsZero() {
		v.LastWhen = r.Last.When.Format("2006-01-02")
	}
	if r.Err != nil {
		v.ErrMsg = r.Err.Error()
	}
	return v
}

// ScanRepos discovers repos under the configured roots (skeleton views only).
func (a *App) ScanRepos() []RepoView {
	repos := scan.Discover(a.cfg.Roots, a.cfg.ScanDepth, a.cfg.ShowNonGit)
	out := make([]RepoView, 0, len(repos))
	for _, r := range repos {
		out = append(out, toView(r))
	}
	return out
}

// LoadRepo loads one repo's git + meta data and returns the full view.
func (a *App) LoadRepo(path string) RepoView {
	r := repo.Repo{Path: path, Name: baseName(path), IsGit: isGitDir(path)}
	if r.IsGit {
		git.Load(a.runner, &r)
	}
	r.Language, r.SizeBytes, r.HasReadme = meta.Detect(path)
	r.Loaded = true
	return toView(r)
}

func (a *App) Fetch(path string) string        { return errMsg(git.Fetch(a.runner, path)) }
func (a *App) Pull(path string) string         { return errMsg(git.Pull(a.runner, path)) }
func (a *App) OpenEditor(path string) string   { return errMsg(action.EditorCmd(a.cfg.Editor, path).Start()) }
func (a *App) OpenTerminal(path string) string { return errMsg(action.TerminalCmd(a.cfg.Terminal, path).Start()) }

// RunCommand runs a command line in the repo and returns combined output (or the
// error text if it failed).
func (a *App) RunCommand(path, line string) string {
	out, err := action.RunInDir(path, line)
	if err != nil {
		return out + "\n[error: " + err.Error() + "]"
	}
	return out
}

func (a *App) GetConfig() config.Config { return a.cfg }

// SaveConfig persists the config and updates the in-memory copy.
func (a *App) SaveConfig(c config.Config) string {
	p, err := config.Path()
	if err != nil {
		return err.Error()
	}
	if err := c.Save(p); err != nil {
		return err.Error()
	}
	a.cfg = c
	return ""
}

func errMsg(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}
