package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/hoijun/fleet/internal/action"
	"github.com/hoijun/fleet/internal/config"
	"github.com/hoijun/fleet/internal/git"
	"github.com/hoijun/fleet/internal/meta"
	"github.com/hoijun/fleet/internal/repo"
	"github.com/hoijun/fleet/internal/scan"
	"github.com/hoijun/fleet/internal/store"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails binding layer exposed to the front end.
type App struct {
	ctx    context.Context
	mu     sync.RWMutex
	cfg    config.Config
	runner git.Runner
	store  *store.Store
}

// NewApp builds the App with the real git runner and loaded config.
func NewApp() *App {
	cfg, cfgPath, _ := config.Load()
	storePath := filepath.Join(filepath.Dir(cfgPath), "projects.json")
	st, _ := store.Open(storePath) // empty store on error; UI still works
	return &App{cfg: cfg, runner: git.ExecRunner{}, store: st}
}

// cfgSnapshot returns a copy of the current config, safe to call from any
// goroutine (each Wails-bound method call runs on its own goroutine).
func (a *App) cfgSnapshot() config.Config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg
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
	cfg := a.cfgSnapshot()
	repos := scan.Discover(cfg.Roots, cfg.ScanDepth, cfg.ShowNonGit)
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

func (a *App) Fetch(path string) string { return errMsg(git.Fetch(a.runner, path)) }
func (a *App) Pull(path string) string  { return errMsg(git.Pull(a.runner, path)) }
func (a *App) OpenEditor(path string) string {
	return errMsg(action.EditorCmd(a.cfgSnapshot().Editor, path).Start())
}
func (a *App) OpenTerminal(path string) string {
	return errMsg(action.TerminalCmd(a.cfgSnapshot().Terminal, path).Start())
}

// RunCommand runs a command line in the repo and returns combined output (or the
// error text if it failed).
func (a *App) RunCommand(path, line string) string {
	out, err := action.RunInDir(path, line)
	if err != nil {
		return out + "\n[error: " + err.Error() + "]"
	}
	return out
}

func (a *App) GetConfig() config.Config { return a.cfgSnapshot() }

// SaveConfig persists the config and updates the in-memory copy.
func (a *App) SaveConfig(c config.Config) string {
	p, err := config.Path()
	if err != nil {
		return err.Error()
	}
	if err := c.Save(p); err != nil {
		return err.Error()
	}
	a.mu.Lock()
	a.cfg = c
	a.mu.Unlock()
	return ""
}

func errMsg(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}

// BranchInfo is the current + all-local branches for a repo.
type BranchInfo struct {
	Current string   `json:"current"`
	All     []string `json:"all"`
}

// CommitView is a JS-serializable commit.
type CommitView struct {
	Hash    string `json:"hash"`
	Message string `json:"message"`
	Author  string `json:"author"`
	When    string `json:"when"`
}

func (a *App) Branches(path string) BranchInfo {
	c, all, err := git.Branches(a.runner, path)
	if err != nil {
		return BranchInfo{All: []string{}}
	}
	if all == nil {
		all = []string{}
	}
	return BranchInfo{Current: c, All: all}
}

func (a *App) Checkout(path, branch string) string {
	return errMsg(git.Checkout(a.runner, path, branch))
}
func (a *App) CommitAll(path, msg string) string { return errMsg(git.CommitAll(a.runner, path, msg)) }
func (a *App) Push(path string) string           { return errMsg(git.Push(a.runner, path)) }

func (a *App) DiffFile(path, file string) string {
	out, err := git.DiffFile(a.runner, path, file)
	if err != nil {
		return out + "\n[error: " + err.Error() + "]"
	}
	return out
}

func (a *App) Log(path string, n int) []CommitView {
	commits, err := git.Log(a.runner, path, n)
	if err != nil {
		return []CommitView{}
	}
	out := make([]CommitView, 0, len(commits))
	for _, c := range commits {
		w := ""
		if !c.When.IsZero() {
			w = c.When.Format("2006-01-02")
		}
		out = append(out, CommitView{Hash: c.Hash, Message: c.Message, Author: c.Author, When: w})
	}
	return out
}

func (a *App) StashList(path string) []string {
	l, err := git.StashList(a.runner, path)
	if err != nil || l == nil {
		return []string{}
	}
	return l
}
func (a *App) Stash(path string) string    { return errMsg(git.Stash(a.runner, path)) }
func (a *App) StashPop(path string) string { return errMsg(git.StashPop(a.runner, path)) }

// OpenInBrowser opens the repo's remote (converted to https) in the default browser.
func (a *App) OpenInBrowser(remote string) string {
	url := remoteToHTTPS(remote)
	if url == "" {
		return "no browsable url for remote"
	}
	wruntime.BrowserOpenURL(a.ctx, url)
	return ""
}

// RevealInExplorer opens the OS file manager at path.
func (a *App) RevealInExplorer(path string) string {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return errMsg(cmd.Start())
}

// TaskView is the JS-facing task.
type TaskView struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
	Due   string `json:"due"`
}

// ProjectView is the JS-facing unified project (project-management fields only;
// live git status is merged in by the front end via LoadRepo for code projects).
type ProjectView struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Type     string     `json:"type"` // "code" | "manual"
	RepoPath string     `json:"repoPath"`
	Status   string     `json:"status"`
	Priority int        `json:"priority"`
	Deadline string     `json:"deadline"`
	Notes    string     `json:"notes"`
	Tags     []string   `json:"tags"`
	Tasks    []TaskView `json:"tasks"`
}

// DayCountView is the JS-facing per-day commit count.
type DayCountView struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

func toTaskViews(ts []store.Task) []TaskView {
	out := make([]TaskView, 0, len(ts))
	for _, t := range ts {
		out = append(out, TaskView{ID: t.ID, Title: t.Title, Done: t.Done, Due: t.Due})
	}
	return out
}

func recordToView(id, name, typ, repoPath string, r store.Record) ProjectView {
	tags := r.Tags
	if tags == nil {
		tags = []string{}
	}
	return ProjectView{
		ID: id, Name: name, Type: typ, RepoPath: repoPath,
		Status: r.Status, Priority: r.Priority, Deadline: r.Deadline,
		Notes: r.Notes, Tags: tags, Tasks: toTaskViews(r.Tasks),
	}
}

// ListProjects assembles discovered repos (code) plus manual store records.
func (a *App) ListProjects() []ProjectView {
	cfg := a.cfgSnapshot()
	snap := a.store.Snapshot()
	out := []ProjectView{}
	// code projects from the scan
	for _, r := range scan.Discover(cfg.Roots, cfg.ScanDepth, cfg.ShowNonGit) {
		rec := snap[r.Path] // zero Record if none
		out = append(out, recordToView(r.Path, r.Name, "code", r.Path, rec))
	}
	// manual projects from the store
	for id, rec := range snap {
		if rec.Manual {
			out = append(out, recordToView(id, rec.Name, "manual", "", rec))
		}
	}
	return out
}

// AddProject creates a manual project and returns its id.
func (a *App) AddProject(name string) string {
	id := "m-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	_ = a.store.Put(id, store.Record{Manual: true, Name: name, Status: "active"})
	return id
}

// UpdateProject sets a project's status/priority/deadline/notes.
func (a *App) UpdateProject(id, status string, priority int, deadline, notes string) string {
	rec, _ := a.store.Get(id)
	rec.Status = status
	rec.Priority = priority
	rec.Deadline = deadline
	rec.Notes = notes
	return errMsg(a.store.Put(id, rec))
}

// DeleteProject removes a project's stored data (manual project disappears; a
// code project loses its project-management data but is still discovered).
func (a *App) DeleteProject(id string) string { return errMsg(a.store.Delete(id)) }

// AddTask appends a task to a project.
func (a *App) AddTask(projectID, title, due string) string {
	rec, _ := a.store.Get(projectID)
	tid := "t-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	rec.Tasks = append(rec.Tasks, store.Task{ID: tid, Title: title, Due: due})
	return errMsg(a.store.Put(projectID, rec))
}

// ToggleTask flips a task's done state.
func (a *App) ToggleTask(projectID, taskID string) string {
	rec, _ := a.store.Get(projectID)
	for i := range rec.Tasks {
		if rec.Tasks[i].ID == taskID {
			rec.Tasks[i].Done = !rec.Tasks[i].Done
		}
	}
	return errMsg(a.store.Put(projectID, rec))
}

// DeleteTask removes a task from a project.
func (a *App) DeleteTask(projectID, taskID string) string {
	rec, _ := a.store.Get(projectID)
	kept := rec.Tasks[:0]
	for _, t := range rec.Tasks {
		if t.ID != taskID {
			kept = append(kept, t)
		}
	}
	rec.Tasks = kept
	return errMsg(a.store.Put(projectID, rec))
}

// CommitActivity returns per-day commit counts for the heatmap.
func (a *App) CommitActivity(path string, weeks int) []DayCountView {
	days, err := git.CommitActivity(a.runner, path, weeks)
	if err != nil {
		return []DayCountView{}
	}
	out := make([]DayCountView, 0, len(days))
	for _, d := range days {
		out = append(out, DayCountView{Date: d.Date, Count: d.Count})
	}
	return out
}
