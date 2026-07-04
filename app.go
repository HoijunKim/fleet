package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hoijun/fleet/internal/action"
	"github.com/hoijun/fleet/internal/ai"
	"github.com/hoijun/fleet/internal/config"
	"github.com/hoijun/fleet/internal/deps"
	"github.com/hoijun/fleet/internal/edges"
	"github.com/hoijun/fleet/internal/gh"
	"github.com/hoijun/fleet/internal/git"
	"github.com/hoijun/fleet/internal/meta"
	"github.com/hoijun/fleet/internal/notion"
	"github.com/hoijun/fleet/internal/repo"
	"github.com/hoijun/fleet/internal/scan"
	"github.com/hoijun/fleet/internal/store"
	"github.com/hoijun/fleet/internal/symbols"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails binding layer exposed to the front end.
type App struct {
	ctx      context.Context
	mu       sync.RWMutex
	cfg      config.Config
	runner   git.Runner
	store    *store.Store
	ghRunner gh.Runner
	ghCache  map[string]GitHubView
	ghMu     sync.RWMutex
	edges    *edges.Store
	symCache map[string]SymbolsView
	symMu    sync.RWMutex
	aiRunner ai.Runner
}

// NewApp builds the App with the real git runner and loaded config.
func NewApp() *App {
	cfg, cfgPath, _ := config.Load()
	storePath := filepath.Join(filepath.Dir(cfgPath), "projects.json")
	st, _ := store.Open(storePath) // empty store on error; UI still works
	edgesPath := filepath.Join(filepath.Dir(cfgPath), "edges.json")
	ed, _ := edges.Open(edgesPath) // empty store on error; UI still works
	return &App{cfg: cfg, runner: git.ExecRunner{}, store: st, ghRunner: gh.ExecRunner{}, ghCache: map[string]GitHubView{}, edges: ed, symCache: map[string]SymbolsView{}, aiRunner: ai.New(cfg.AIProvider, cfg.AIModel, cfg.OpenAIKey, cfg.GeminiKey)}
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
	a.aiRunner = ai.New(c.AIProvider, c.AIModel, c.OpenAIKey, c.GeminiKey)
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
	ID     string `json:"id"`
	Title  string `json:"title"`
	Done   bool   `json:"done"`
	Status string `json:"status"`
	Due    string `json:"due"`
}

// ProjectView is the JS-facing unified project (project-management fields only;
// live git status is merged in by the front end via LoadRepo for code projects).
type ProjectView struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Type      string     `json:"type"` // "code" | "manual"
	RepoPath  string     `json:"repoPath"`
	Status    string     `json:"status"`
	Priority  int        `json:"priority"`
	Deadline  string     `json:"deadline"`
	Notes     string     `json:"notes"`
	Tags      []string   `json:"tags"`
	Tasks     []TaskView `json:"tasks"`
	DoneCount int        `json:"doneCount"`
	TaskCount int        `json:"taskCount"`
}

// DayCountView is the JS-facing per-day commit count.
type DayCountView struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

func toTaskViews(ts []store.Task) []TaskView {
	out := make([]TaskView, 0, len(ts))
	for _, t := range ts {
		out = append(out, TaskView{ID: t.ID, Title: t.Title, Done: t.Done, Status: t.Status, Due: t.Due})
	}
	return out
}

func recordToView(id, name, typ, repoPath string, r store.Record) ProjectView {
	tags := r.Tags
	if tags == nil {
		tags = []string{}
	}
	doneCount := 0
	for _, t := range r.Tasks {
		if t.Status == "done" {
			doneCount++
		}
	}
	return ProjectView{
		ID: id, Name: name, Type: typ, RepoPath: repoPath,
		Status: r.Status, Priority: r.Priority, Deadline: r.Deadline,
		Notes: r.Notes, Tags: tags, Tasks: toTaskViews(r.Tasks),
		DoneCount: doneCount, TaskCount: len(r.Tasks),
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

// GetProject returns the current view for one project id (targeted refresh after
// a mutation, avoiding a full rescan). Code projects (id == repo path) carry only
// their project-management fields here; the front end keeps the live git fields.
func (a *App) GetProject(id string) ProjectView {
	rec, _ := a.store.Get(id)
	if rec.Manual {
		return recordToView(id, rec.Name, "manual", "", rec)
	}
	return recordToView(id, filepath.Base(id), "code", id, rec)
}

// AddProject creates a manual project and returns its id, or "" on failure.
func (a *App) AddProject(name string) string {
	id := "m-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := a.store.Update(id, func(r *store.Record) {
		r.Manual = true
		r.Name = name
		r.Status = "active"
	}); err != nil {
		return ""
	}
	return id
}

// UpdateProject sets a project's status/priority/deadline/notes.
func (a *App) UpdateProject(id, status string, priority int, deadline, notes string) string {
	return errMsg(a.store.Update(id, func(r *store.Record) {
		r.Status = status
		r.Priority = priority
		r.Deadline = deadline
		r.Notes = notes
	}))
}

// DeleteProject removes a project's stored data (manual project disappears; a
// code project loses its project-management data but is still discovered).
func (a *App) DeleteProject(id string) string { return errMsg(a.store.Delete(id)) }

// AddTask appends a task to a project.
func (a *App) AddTask(projectID, title, due string) string {
	tid := "t-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	return errMsg(a.store.Update(projectID, func(r *store.Record) {
		r.Tasks = append(r.Tasks, store.Task{ID: tid, Title: title, Due: due, Status: "todo"})
	}))
}

// ToggleTask flips a task's done state, keeping Status in sync with Done.
func (a *App) ToggleTask(projectID, taskID string) string {
	return errMsg(a.store.Update(projectID, func(r *store.Record) {
		for i := range r.Tasks {
			if r.Tasks[i].ID == taskID {
				r.Tasks[i].Done = !r.Tasks[i].Done
				if r.Tasks[i].Done {
					r.Tasks[i].Status = "done"
				} else {
					r.Tasks[i].Status = "todo"
				}
			}
		}
	}))
}

// validTaskStatuses are the only statuses SetTaskStatus accepts.
var validTaskStatuses = map[string]bool{"todo": true, "doing": true, "done": true}

// SetTaskStatus sets a task's status (todo/doing/done), mirroring Done from
// it. An unrecognized status is rejected with no mutation. A missing taskID
// is a no-op success, mirroring ToggleTask's silent miss for consistency.
func (a *App) SetTaskStatus(projectID, taskID, status string) string {
	if !validTaskStatuses[status] {
		return "invalid status: " + status
	}
	return errMsg(a.store.Update(projectID, func(r *store.Record) {
		for i := range r.Tasks {
			if r.Tasks[i].ID == taskID {
				r.Tasks[i].Status = status
				r.Tasks[i].Done = status == "done"
			}
		}
	}))
}

// ReorderTasks rebuilds a project's task order to match orderedIDs. Ids in
// orderedIDs with no matching task are ignored; any current task whose id is
// NOT in orderedIDs is appended at the end in its original order, so a task
// is never dropped.
func (a *App) ReorderTasks(projectID string, orderedIDs []string) string {
	return errMsg(a.store.Update(projectID, func(r *store.Record) {
		byID := make(map[string]store.Task, len(r.Tasks))
		for _, t := range r.Tasks {
			byID[t.ID] = t
		}
		seen := make(map[string]bool, len(orderedIDs))
		reordered := make([]store.Task, 0, len(r.Tasks))
		for _, id := range orderedIDs {
			if seen[id] {
				continue // a duplicate id must not duplicate the task
			}
			if t, ok := byID[id]; ok {
				reordered = append(reordered, t)
				seen[id] = true
			}
		}
		for _, t := range r.Tasks {
			if !seen[t.ID] {
				reordered = append(reordered, t)
			}
		}
		r.Tasks = reordered
	}))
}

// DeleteTask removes a task from a project.
func (a *App) DeleteTask(projectID, taskID string) string {
	return errMsg(a.store.Update(projectID, func(r *store.Record) {
		kept := r.Tasks[:0]
		for _, t := range r.Tasks {
			if t.ID != taskID {
				kept = append(kept, t)
			}
		}
		r.Tasks = kept
	}))
}

// SetTags sets a project's tags.
func (a *App) SetTags(id string, tags []string) string {
	if tags == nil {
		tags = []string{}
	}
	return errMsg(a.store.Update(id, func(r *store.Record) { r.Tags = tags }))
}

// AgendaItem is one fleet-wide agenda entry: a project deadline or an
// incomplete due/doing task.
type AgendaItem struct {
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
	Kind        string `json:"kind"`   // "deadline" | "task"
	TaskID      string `json:"taskId"` // task id for a task item; "" for a deadline
	Title       string `json:"title"`
	Due         string `json:"due"` // may be ""
	Status      string `json:"status"`
}

// Agenda flattens every project's deadline and every incomplete due/doing
// task into a single fleet-wide list, sorted by due date ascending (items
// with no due date sort last). Store-only: no scan, no git.
func (a *App) Agenda() []AgendaItem {
	snap := a.store.Snapshot()
	out := []AgendaItem{}
	for id, r := range snap {
		name := r.Name
		if name == "" {
			name = filepath.Base(id)
		}
		if r.Deadline != "" && r.Status != "done" {
			out = append(out, AgendaItem{
				ProjectID: id, ProjectName: name, Kind: "deadline",
				Title: name, Due: r.Deadline, Status: r.Status,
			})
		}
		for _, t := range r.Tasks {
			if t.Status == "done" {
				continue
			}
			if t.Due == "" && t.Status != "doing" {
				continue
			}
			out = append(out, AgendaItem{
				ProjectID: id, ProjectName: name, Kind: "task", TaskID: t.ID,
				Title: t.Title, Due: t.Due, Status: t.Status,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		di, dj := out[i].Due, out[j].Due
		if di == "" {
			return false // empty due never sorts before anything
		}
		if dj == "" {
			return true // non-empty due always sorts before an empty one
		}
		return di < dj
	})
	return out
}

// OpenURL opens an arbitrary URL (e.g. a Notion page) in the default browser.
func (a *App) OpenURL(url string) string {
	if strings.TrimSpace(url) == "" {
		return "no url"
	}
	wruntime.BrowserOpenURL(a.ctx, url)
	return ""
}

// AIAvailable reports whether the configured provider can run (Claude CLI on
// PATH, or an API key set), so the UI can degrade gracefully.
func (a *App) AIAvailable() bool {
	c := a.cfgSnapshot()
	return ai.Available(c.AIProvider, c.OpenAIKey, c.GeminiKey)
}

// AICheck reports whether the given (unsaved) provider + keys would be usable,
// so the Settings UI can validate the edit in front of the user, not just the
// saved config.
func (a *App) AICheck(provider, openAIKey, geminiKey string) bool {
	return ai.Available(provider, openAIKey, geminiKey)
}

// AskAI runs a prompt through the configured AI provider and returns the
// completion. On any failure it returns a string prefixed with "error:" (never
// an empty string that the UI might render as a blank answer).
func (a *App) AskAI(prompt string) string {
	a.mu.RLock()
	runner := a.aiRunner
	a.mu.RUnlock()
	if runner == nil {
		return "error: AI unavailable"
	}
	out, err := runner.Ask(prompt)
	if err != nil {
		return "error: " + err.Error()
	}
	if strings.TrimSpace(out) == "" {
		return "error: no response"
	}
	return out
}

// NotionTaskView is a JS-facing Notion task.
type NotionTaskView struct {
	Title  string `json:"title"`
	Due    string `json:"due"`
	Status string `json:"status"`
	Done   bool   `json:"done"`
	URL    string `json:"url"`
}

// NotionAvailable reports whether a Notion token and database id are configured.
func (a *App) NotionAvailable() bool {
	c := a.cfgSnapshot()
	return notion.Available(c.NotionToken, c.NotionTasksDB)
}

// NotionResult carries the pulled tasks plus any error text, so the UI can tell
// "empty board" apart from "auth/network failed".
type NotionResult struct {
	Tasks []NotionTaskView `json:"tasks"`
	Error string           `json:"error"`
}

// NotionTasks pulls tasks from the configured Notion database. Tasks is always
// non-nil; Error is set (and Tasks empty) when the API call fails.
func (a *App) NotionTasks() NotionResult {
	res := NotionResult{Tasks: []NotionTaskView{}}
	c := a.cfgSnapshot()
	if !notion.Available(c.NotionToken, c.NotionTasksDB) {
		return res
	}
	tasks, err := notion.New(c.NotionToken).Tasks(c.NotionTasksDB)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	for _, t := range tasks {
		res.Tasks = append(res.Tasks, NotionTaskView{Title: t.Title, Due: t.Due, Status: t.Status, Done: t.Done, URL: t.URL})
	}
	return res
}

// GraphNode/GraphEdge/GraphView are the JS-facing dependency graph.
type GraphNode struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Tags  []string `json:"tags"`
	IsGit bool     `json:"isGit"`
}
type GraphEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Manual bool   `json:"manual"`
	Kind   string `json:"kind"`
}
type GraphView struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// RepoGraph builds the dependency graph over discovered git repos (nodes) and
// their go.mod/package.json cross-dependencies (edges). Tags come from the store.
func (a *App) RepoGraph() GraphView {
	cfg := a.cfgSnapshot()
	repos := scan.Discover(cfg.Roots, cfg.ScanDepth, false) // git repos only
	refs := make([]deps.RepoRef, 0, len(repos))
	for _, r := range repos {
		refs = append(refs, deps.RepoRef{ID: r.Path, Path: r.Path, Name: r.Name})
	}
	g := deps.BuildGraph(refs)
	snap := a.store.Snapshot()
	nodes := make([]GraphNode, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		tags := snap[n.ID].Tags
		if tags == nil {
			tags = []string{}
		}
		nodes = append(nodes, GraphNode{ID: n.ID, Name: n.Name, Tags: tags, IsGit: true})
	}
	graphEdges := make([]GraphEdge, 0, len(g.Edges))
	for _, e := range g.Edges {
		graphEdges = append(graphEdges, GraphEdge{From: e.From, To: e.To, Manual: false, Kind: ""})
	}
	if a.edges != nil {
		nodeIDs := make(map[string]bool, len(nodes))
		for _, n := range nodes {
			nodeIDs[n.ID] = true
		}
		for _, me := range a.edges.List() {
			if nodeIDs[me.From] && nodeIDs[me.To] {
				graphEdges = append(graphEdges, GraphEdge{From: me.From, To: me.To, Manual: true, Kind: me.Kind})
			}
		}
	}
	return GraphView{Nodes: nodes, Edges: graphEdges}
}

// SearchHit is one cross-repo search result.
type SearchHit struct {
	Repo     string `json:"repo"`
	RepoPath string `json:"repoPath"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Text     string `json:"text"`
}

// SearchAll runs git grep across all discovered git repos and returns the hits
// (capped to keep the UI responsive). A blank query returns no hits.
func (a *App) SearchAll(query string) []SearchHit {
	out := []SearchHit{}
	if strings.TrimSpace(query) == "" {
		return out
	}
	cfg := a.cfgSnapshot()
	for _, r := range scan.Discover(cfg.Roots, cfg.ScanDepth, false) {
		hits, _ := git.Grep(a.runner, r.Path, query)
		for _, h := range hits {
			out = append(out, SearchHit{Repo: r.Name, RepoPath: r.Path, File: h.File, Line: h.Line, Text: h.Text})
			if len(out) >= 500 {
				return out
			}
		}
	}
	return out
}

// OpenEditorAt opens a specific file (repo-relative) in the configured editor.
func (a *App) OpenEditorAt(repoPath, file string) string {
	return errMsg(action.EditorCmd(a.cfgSnapshot().Editor, filepath.Join(repoPath, file)).Start())
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

// GitHubView is a repo's GitHub status for the front end.
type GitHubView struct {
	CI        string `json:"ci"`
	PRs       int    `json:"prs"`
	Issues    int    `json:"issues"`
	Available bool   `json:"available"`
}

// GitHubInfo returns a repo's GitHub status (cached per owner/repo). Returns
// Available=false when the remote is not a parseable GitHub URL or gh fails.
func (a *App) GitHubInfo(remote string) GitHubView {
	owner, repo, ok := gh.OwnerRepo(remote)
	if !ok {
		return GitHubView{Available: false}
	}
	key := owner + "/" + repo
	a.ghMu.RLock()
	if v, hit := a.ghCache[key]; hit {
		a.ghMu.RUnlock()
		return v
	}
	a.ghMu.RUnlock()

	info, err := gh.Fetch(a.ghRunner, owner, repo)
	if err != nil {
		return GitHubView{Available: false} // do not cache failures - retry next time
	}
	v := GitHubView{CI: info.CI, PRs: info.PRs, Issues: info.Issues, Available: info.Available}
	a.ghMu.Lock()
	if a.ghCache == nil {
		a.ghCache = map[string]GitHubView{}
	}
	a.ghCache[key] = v
	a.ghMu.Unlock()
	return v
}

// SymbolsView is a repo's symbol summary for the front end.
type SymbolsView struct {
	GoMainPkgs []string `json:"goMainPkgs"`
	GoExported []string `json:"goExported"`
	NpmScripts []string `json:"npmScripts"`
	NpmBin     []string `json:"npmBin"`
	Truncated  bool     `json:"truncated"`
}

// RepoSymbols returns a repo's symbol summary (cached per path). The first
// call for a given path extracts and caches; subsequent calls for the same
// path are served from the cache without touching disk again.
func (a *App) RepoSymbols(path string) SymbolsView {
	a.symMu.RLock()
	if v, hit := a.symCache[path]; hit {
		a.symMu.RUnlock()
		return v
	}
	a.symMu.RUnlock()

	set := symbols.Extract(path)
	v := SymbolsView{
		GoMainPkgs: set.GoMainPkgs,
		GoExported: set.GoExported,
		NpmScripts: set.NpmScripts,
		NpmBin:     set.NpmBin,
		Truncated:  set.Truncated,
	}
	a.symMu.Lock()
	if a.symCache == nil {
		a.symCache = map[string]SymbolsView{}
	}
	a.symCache[path] = v
	a.symMu.Unlock()
	return v
}

// EdgeView is the JS-facing manual repo graph edge.
type EdgeView struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
	Note string `json:"note"`
}

// ListEdges returns all manual edges (non-nil, even when the edge store
// failed to open).
func (a *App) ListEdges() []EdgeView {
	out := []EdgeView{}
	if a.edges == nil {
		return out
	}
	for _, e := range a.edges.List() {
		out = append(out, EdgeView{ID: e.ID, From: e.From, To: e.To, Kind: e.Kind, Note: e.Note})
	}
	return out
}

// AddEdge creates a manual edge between two repos and returns "" on success,
// or the error text on failure (including when the edge store is unavailable).
func (a *App) AddEdge(from, to, kind, note string) string {
	if a.edges == nil {
		return "edges unavailable"
	}
	_, err := a.edges.Add(from, to, kind, note)
	return errMsg(err)
}

// RemoveEdge deletes a manual edge by id.
func (a *App) RemoveEdge(id string) string {
	if a.edges == nil {
		return "edges unavailable"
	}
	return errMsg(a.edges.Remove(id))
}
