package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hoijun/fleet/internal/action"
	"github.com/hoijun/fleet/internal/agent"
	"github.com/hoijun/fleet/internal/ai"
	"github.com/hoijun/fleet/internal/cloud"
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
	"github.com/hoijun/fleet/internal/syncengine"
	"github.com/hoijun/fleet/internal/winhide"
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
	ghCache  map[string]ghEntry
	ghMu     sync.RWMutex
	edges    *edges.Store
	symCache map[string]symEntry
	symMu    sync.RWMutex
	aiRunner ai.Runner
	aiMu     sync.Mutex
	aiCancel context.CancelFunc
	aiGen    int

	// agentic deep-dive (drives the claude CLI + PreToolUse approval gate)
	dataDir      string
	agentCoord   *agent.Coordinator
	agentSrv     *agent.ApprovalServer
	agentMu      sync.Mutex
	agentCancel  context.CancelFunc
	agentSession map[string]string

	// cloud sync + auth
	cloudClient *cloud.Client
	creds       cloud.CredStore
	engine      *syncengine.Engine
	authMu      sync.Mutex
	user        cloud.User
	signedIn    bool
	session     *cloud.Session
	syncMu      sync.Mutex
	syncView    SyncStateView
	syncTrigger chan struct{}
	// syncRunMu single-flights the actual SyncOnce call: the background loop
	// and a UI-triggered SyncNow can both call runSync concurrently, and
	// running two syncs at once on the same Session/Engine risks a double
	// Refresh (one spuriously failing) and concurrent store/state mutation.
	// It is deliberately separate from syncMu (which only guards syncView)
	// since it must stay held across the whole network round trip.
	syncRunMu sync.Mutex
}

// Cached GitHub/symbol lookups expire so a session doesn't show stale CI or
// symbols indefinitely.
type ghEntry struct {
	v  GitHubView
	at time.Time
}
type symEntry struct {
	v  SymbolsView
	at time.Time
}

const (
	ghTTL  = 3 * time.Minute
	symTTL = 5 * time.Minute
)

// NewApp builds the App with the real git runner, loaded config, and the cloud
// sync stack (API client, keychain-backed credential store, sync engine).
func NewApp() *App {
	cfg, cfgPath, _ := config.Load()
	dir := filepath.Dir(cfgPath)
	storePath := filepath.Join(dir, "projects.json")
	st, _ := store.Open(storePath) // empty store on error; UI still works
	edgesPath := filepath.Join(dir, "edges.json")
	ed, _ := edges.Open(edgesPath) // empty store on error; UI still works

	cl := cloud.New(apiURL())
	creds := cloud.KeyringStore{Service: "fleet", User: "refresh"}
	syncPath := filepath.Join(dir, "sync.json")
	eng := syncengine.New(st, cl, syncPath, func(path string) string {
		u, _ := git.RemoteURL(git.ExecRunner{}, path)
		return u
	})

	return &App{
		cfg: cfg, runner: git.ExecRunner{}, store: st,
		ghRunner: gh.ExecRunner{}, ghCache: map[string]ghEntry{}, edges: ed,
		symCache:     map[string]symEntry{},
		aiRunner:     ai.New(cfg.AIProvider, cfg.AIModel, cfg.OpenAIKey, cfg.GeminiKey),
		dataDir:      dir,
		agentCoord:   agent.NewCoordinator(),
		agentSession: map[string]string{},
		cloudClient:  cl, creds: creds, engine: eng,
		syncTrigger: make(chan struct{}, 1),
		syncView:    SyncStateView{State: "signedout"},
	}
}

// cfgSnapshot returns a copy of the current config, safe to call from any
// goroutine (each Wails-bound method call runs on its own goroutine).
func (a *App) cfgSnapshot() config.Config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.startSync(ctx)
}

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

// EditorOption is a known editor and whether its command is on PATH.
type EditorOption struct {
	Name      string `json:"name"`
	Command   string `json:"command"`
	Installed bool   `json:"installed"`
}

// knownEditors is the curated name->command list shown in the picker (display
// order when equally installed).
var knownEditors = []EditorOption{
	{Name: "VS Code", Command: "code"},
	{Name: "Cursor", Command: "cursor"},
	{Name: "Windsurf", Command: "windsurf"},
	{Name: "Sublime Text", Command: "subl"},
	{Name: "Zed", Command: "zed"},
	{Name: "Neovim", Command: "nvim"},
	{Name: "Vim", Command: "vim"},
	{Name: "IntelliJ IDEA", Command: "idea"},
	{Name: "WebStorm", Command: "webstorm"},
	{Name: "Emacs", Command: "emacs"},
	{Name: "Notepad++", Command: "notepad++"},
}

// DetectEditors returns the known editors, marking the ones on PATH installed
// and sorting installed-first (stable within each group).
func (a *App) DetectEditors() []EditorOption { return detectEditors(exec.LookPath) }

func detectEditors(lookPath func(string) (string, error)) []EditorOption {
	var installed, missing []EditorOption
	for _, e := range knownEditors {
		e := e
		if _, err := lookPath(e.Command); err == nil {
			e.Installed = true
			installed = append(installed, e)
		} else {
			missing = append(missing, e)
		}
	}
	return append(installed, missing...)
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

// MergeUpstream and RebaseUpstream integrate a diverged upstream into the
// current branch. On a conflict they abort and return a human message rather
// than stranding the working tree mid-operation (see git.integrateUpstream).
func (a *App) MergeUpstream(path string) string  { return errMsg(git.MergeUpstream(a.runner, path)) }
func (a *App) RebaseUpstream(path string) string { return errMsg(git.RebaseUpstream(a.runner, path)) }

// RepoDiff returns the repo's uncommitted working changes (capped), for the
// AI deep-dive prompt.
func (a *App) RepoDiff(path string) string { return git.Diff(a.runner, path) }

// StagedDiff returns the staged changes (capped), for drafting a commit message.
func (a *App) StagedDiff(path string) string { return git.StagedDiff(a.runner, path) }

func (a *App) DiffFile(path, file string) string {
	out, err := git.DiffFile(a.runner, path, file)
	if err != nil {
		return out + "\n[error: " + err.Error() + "]"
	}
	return out
}

// DiffAll returns the whole working-tree diff (all changed files) for the
// combined "view all changes" modal. Uncapped, mirroring DiffFile.
func (a *App) DiffAll(path string) string {
	out, err := git.WorktreeDiff(a.runner, path)
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

// StashApply restores stash entry i without removing it; StashDrop deletes it.
func (a *App) StashApply(path string, i int) string { return errMsg(git.StashApply(a.runner, path, i)) }
func (a *App) StashDrop(path string, i int) string  { return errMsg(git.StashDrop(a.runner, path, i)) }

// CreateBranch creates a branch and switches to it; DeleteBranch safe-deletes one.
func (a *App) CreateBranch(path, name string) string {
	if e := validBranchName(name); e != "" {
		return e
	}
	return errMsg(git.CreateBranch(a.runner, path, strings.TrimSpace(name)))
}
func (a *App) DeleteBranch(path, name string) string {
	if e := validBranchName(name); e != "" {
		return e
	}
	return errMsg(git.DeleteBranch(a.runner, path, strings.TrimSpace(name)))
}

// validBranchName rejects an empty name or one that git would parse as a flag
// (a leading "-"), returning an error string or "" when the name is acceptable.
func validBranchName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "error: branch name cannot be empty"
	}
	if strings.HasPrefix(name, "-") {
		return "error: branch name cannot start with '-'"
	}
	return ""
}

// StatusFilesView is a changed file's staged/unstaged state for the staging UI.
type StatusFilesView struct {
	Path     string `json:"path"`
	Staged   bool   `json:"staged"`
	Unstaged bool   `json:"unstaged"`
	Conflict bool   `json:"conflict"`
}

// StatusFiles returns per-file staged/unstaged state for the commit-staging UI.
func (a *App) StatusFiles(path string) []StatusFilesView {
	fs, _ := git.StatusFiles(a.runner, path)
	out := make([]StatusFilesView, 0, len(fs))
	for _, f := range fs {
		out = append(out, StatusFilesView{Path: f.Path, Staged: f.Staged, Unstaged: f.Unstaged, Conflict: f.Conflict})
	}
	return out
}

// StageFile/UnstageFile move a single path in/out of the index. CommitStaged
// commits only the index; CommitAmend rewrites the last commit; LastCommitMessage
// returns HEAD's message for prefilling an amend.
func (a *App) StageFile(path, file string) string   { return errMsg(git.StageFile(a.runner, path, file)) }
func (a *App) UnstageFile(path, file string) string { return errMsg(git.UnstageFile(a.runner, path, file)) }
func (a *App) CommitStaged(path, msg string) string { return errMsg(git.CommitStaged(a.runner, path, msg)) }
func (a *App) CommitAmend(path, msg string) string  { return errMsg(git.CommitAmend(a.runner, path, msg)) }
func (a *App) LastCommitMessage(path string) string {
	msg, _ := git.LastCommitMessage(a.runner, path)
	return msg
}

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

// idSeq makes generated ids unique even when time.Now().UnixNano() repeats,
// which it does on Windows' coarse clock for calls within the same tick. Without
// it two tasks (or projects) added in the same tick share an id, and the id-keyed
// mutators (SetTaskStatus, ToggleTask, DeleteTask, ReorderTasks) then corrupt the
// list. The time prefix keeps ids roughly ordered and distinct across restarts;
// the counter guarantees uniqueness within a process run.
var idSeq atomic.Uint64

func nextID(prefix string) string {
	return prefix + strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.FormatUint(idSeq.Add(1), 36)
}

// AddProject creates a manual project and returns its id, or "" on failure.
func (a *App) AddProject(name string) string {
	id := nextID("m-")
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

// UnclonedView is a project that synced from another device but has no local
// repo on this machine (a "detached" record, keyed by its portable git:/local:
// doc id rather than a filesystem path).
type UnclonedView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Remote    string `json:"remote"` // https clone URL, or "" when unknown
	TaskCount int    `json:"taskCount"`
	CanClone  bool   `json:"canClone"`
}

// SyncedUncloned lists detached records: sync docs for code projects whose repo
// is not present locally. A cloned repo is keyed by its filesystem path, so a
// record whose store id still carries the portable git:/local: prefix is one
// that arrived from another device and was never reconciled to a local clone.
func (a *App) SyncedUncloned() []UnclonedView {
	roots := a.cfgSnapshot().Roots
	out := []UnclonedView{}
	for id, rec := range a.store.Snapshot() {
		if !strings.HasPrefix(id, "git:") && !strings.HasPrefix(id, "local:") {
			continue
		}
		remote, canClone := unclonedRemote(id)
		// "Uncloned" means not on disk. Once its repo has been cloned into a scan
		// root (where CloneUncloned puts it), drop it from the list even before
		// the sync engine reconciles the detached record.
		if canClone && clonedInRoots(roots, remote) {
			continue
		}
		out = append(out, UnclonedView{
			ID: id, Name: rec.Name, Remote: remote,
			TaskCount: len(rec.Tasks), CanClone: canClone,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// clonedInRoots reports whether a folder for remote's repo already exists in one
// of the scan roots - the same destination CloneUncloned would use.
func clonedInRoots(roots []string, remote string) bool {
	base := cloneBase(remote)
	if base == "" {
		return false
	}
	for _, root := range roots {
		if fi, err := os.Stat(filepath.Join(root, base)); err == nil && fi.IsDir() {
			return true
		}
	}
	return false
}

// unclonedRemote rebuilds an https clone URL from a git: doc id
// (git:host/owner/repo -> https://host/owner/repo), or returns ("", false) for a
// local: id, which was synced without any known remote.
func unclonedRemote(id string) (string, bool) {
	if rest := strings.TrimPrefix(id, "git:"); rest != id {
		return "https://" + rest, true
	}
	return "", false
}

// cloneBase is the destination directory name for a clone: the repo segment of
// the remote URL, without any trailing ".git". It returns "" for a degenerate
// segment (empty, ".", "..", or one containing a path separator) so a
// pathological doc id can never resolve dest to a scan root's parent.
func cloneBase(remote string) string {
	remote = strings.TrimSuffix(remote, "/")
	if i := strings.LastIndex(remote, "/"); i >= 0 {
		remote = remote[i+1:]
	}
	remote = strings.TrimSuffix(remote, ".git")
	if remote == "" || remote == "." || remote == ".." ||
		strings.ContainsAny(remote, `/\`) {
		return ""
	}
	return remote
}

// CloneUncloned clones a detached git: record's repo into destRoot (or the first
// configured Root when destRoot is empty). It refuses a record with no known
// remote and never overwrites an existing directory. On success a later scan
// discovers the clone and the sync engine reconciles the detached record to it.
func (a *App) CloneUncloned(id, destRoot string) string {
	remote, ok := unclonedRemote(id)
	if !ok {
		return "error: this project has no known remote to clone"
	}
	if strings.TrimSpace(destRoot) == "" {
		roots := a.cfgSnapshot().Roots
		if len(roots) == 0 {
			return "error: no scan root configured to clone into"
		}
		destRoot = roots[0]
	}
	base := cloneBase(remote)
	if base == "" {
		return "error: could not derive a folder name from " + remote
	}
	dest := filepath.Join(destRoot, base)
	if _, err := os.Stat(dest); err == nil {
		return "error: " + dest + " already exists"
	}
	return errMsg(git.Clone(a.runner, remote, dest))
}

// ExportData writes the full local store (every project and its tasks) to a
// user-chosen JSON file. Returns "" on success, "cancelled" if the save dialog
// is dismissed, or an error string. Local only - no network.
func (a *App) ExportData() string {
	dest, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
		Title:           "Export fleet data",
		DefaultFilename: "fleet-export-" + time.Now().Format("2006-01-02") + ".json",
		Filters:         []wruntime.FileFilter{{DisplayName: "JSON", Pattern: "*.json"}},
	})
	if err != nil {
		return "error: " + err.Error()
	}
	if strings.TrimSpace(dest) == "" {
		return "cancelled"
	}
	if err := a.writeExport(dest); err != nil {
		return "error: " + err.Error()
	}
	return ""
}

// writeExport serializes the whole store to dest as pretty JSON. Split from the
// dialog so the export payload is testable without a native file picker.
func (a *App) writeExport(dest string) error {
	data, err := json.MarshalIndent(a.store.Snapshot(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dest, data, 0o644)
}

// AddTask appends a task to a project.
func (a *App) AddTask(projectID, title, due string) string {
	tid := nextID("t-")
	return errMsg(a.store.Update(projectID, func(r *store.Record) {
		r.Tasks = append(r.Tasks, store.Task{ID: tid, Title: title, Due: due, Status: "todo"})
	}))
}

// EditTask updates a task's title and due date, leaving its status/done state
// unchanged. An empty title is rejected; a missing taskID is a no-op success.
func (a *App) EditTask(projectID, taskID, title, due string) string {
	if strings.TrimSpace(title) == "" {
		return "error: task title cannot be empty"
	}
	return errMsg(a.store.Update(projectID, func(r *store.Record) {
		for i := range r.Tasks {
			if r.Tasks[i].ID == taskID {
				r.Tasks[i].Title = strings.TrimSpace(title)
				r.Tasks[i].Due = due
			}
		}
	}))
}

// RenameProject renames a MANUAL project. Code projects take their name from
// the scanned folder, so a rename on one is a no-op (the UI only offers it for
// manual projects). An empty name is rejected.
func (a *App) RenameProject(id, name string) string {
	if strings.TrimSpace(name) == "" {
		return "error: name cannot be empty"
	}
	return errMsg(a.store.Update(id, func(r *store.Record) {
		if r.Manual {
			r.Name = strings.TrimSpace(name)
		}
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

// GitHubURL returns the repo's github.com web URL for a remote, or "" when the
// remote is not a GitHub repo. The UI appends /actions, /pulls, /issues.
func (a *App) GitHubURL(remote string) string {
	owner, repo, ok := gh.OwnerRepo(remote)
	if !ok {
		return ""
	}
	return "https://github.com/" + owner + "/" + repo
}

// OpenURL opens an arbitrary URL (e.g. a Notion page) in the default browser.
func (a *App) OpenURL(url string) string {
	if strings.TrimSpace(url) == "" {
		return "no url"
	}
	wruntime.BrowserOpenURL(a.ctx, url)
	return ""
}

// CancelAI aborts the in-flight AI request (kills the claude subprocess or the
// HTTP call), so the UI's Cancel button actually stops work.
func (a *App) CancelAI() {
	a.aiMu.Lock()
	c := a.aiCancel
	a.aiMu.Unlock()
	if c != nil {
		c()
	}
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
	// Register a cancel so CancelAI() can actually kill the subprocess / abort
	// the HTTP call, not just hide the spinner.
	ctx, cancel := context.WithCancel(context.Background())
	a.aiMu.Lock()
	if a.aiCancel != nil {
		a.aiCancel() // supersede an earlier in-flight request
	}
	a.aiCancel = cancel
	a.aiGen++
	myGen := a.aiGen
	a.aiMu.Unlock()
	defer func() {
		a.aiMu.Lock()
		if a.aiGen == myGen {
			a.aiCancel = nil // only clear if a newer request didn't replace us
		}
		a.aiMu.Unlock()
		cancel()
	}()

	out, err := runner.Ask(ctx, prompt)
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
	ID           string `json:"id"`
	Title        string `json:"title"`
	Due          string `json:"due"`
	Status       string `json:"status"`
	Done         bool   `json:"done"`
	URL          string `json:"url"`
	CheckboxProp string `json:"checkboxProp"`
}

// NotionComplete checks off a checkbox-based Notion task (writes back).
func (a *App) NotionComplete(pageID, checkboxProp string) string {
	c := a.cfgSnapshot()
	if strings.TrimSpace(c.NotionToken) == "" {
		return "error: Notion not configured"
	}
	return errMsg(notion.New(c.NotionToken).Complete(pageID, checkboxProp))
}

// NotionAvailable reports whether a Notion token and database id are configured.
func (a *App) NotionAvailable() bool {
	c := a.cfgSnapshot()
	return notion.Available(c.NotionToken, c.NotionTasksDB)
}

// NotionDBView is a Notion database for the settings picker.
type NotionDBView struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// NotionDBList is the picker result (databases + any error). Token is passed in
// so the settings UI can list databases for an unsaved token.
type NotionDBList struct {
	DBs   []NotionDBView `json:"dbs"`
	Error string         `json:"error"`
}

// NotionDatabases lists the databases the given token's integration can see, so
// the user picks one instead of pasting a raw id.
func (a *App) NotionDatabases(token string) NotionDBList {
	res := NotionDBList{DBs: []NotionDBView{}}
	dbs, err := notion.New(token).Databases()
	if err != nil {
		res.Error = err.Error()
		return res
	}
	for _, d := range dbs {
		res.DBs = append(res.DBs, NotionDBView{ID: d.ID, Title: d.Title})
	}
	return res
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
		res.Tasks = append(res.Tasks, NotionTaskView{ID: t.ID, Title: t.Title, Due: t.Due, Status: t.Status, Done: t.Done, URL: t.URL, CheckboxProp: t.CheckboxProp})
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
	repos := scan.Discover(cfg.Roots, cfg.ScanDepth, false)
	// Gather up to searchPerRepo hits per repo, then round-robin merge up to the
	// global cap, so an alphabetically-early repo cannot starve later repos of
	// the result budget (every matching repo gets representation).
	perRepo := make([][]SearchHit, len(repos))
	for i, r := range repos {
		hits, _ := git.Grep(a.runner, r.Path, query)
		for _, h := range hits {
			perRepo[i] = append(perRepo[i], SearchHit{Repo: r.Name, RepoPath: r.Path, File: h.File, Line: h.Line, Text: h.Text})
			if len(perRepo[i]) >= searchPerRepoCap {
				break
			}
		}
	}
	for round := 0; len(out) < searchGlobalCap; round++ {
		added := false
		for i := range perRepo {
			if round < len(perRepo[i]) {
				out = append(out, perRepo[i][round])
				added = true
				if len(out) >= searchGlobalCap {
					break
				}
			}
		}
		if !added {
			break
		}
	}
	return out
}

const (
	searchPerRepoCap = 60  // max hits taken from a single repo before round-robin
	searchGlobalCap  = 500 // total hits returned to the UI
)

// FileHit is one cross-repo file-name search result.
type FileHit struct {
	Repo     string `json:"repo"`
	RepoPath string `json:"repoPath"`
	File     string `json:"file"`
}

// SearchFiles finds tracked files across all discovered repos whose repo-
// relative path contains query (case-insensitive), capped for a responsive UI.
func (a *App) SearchFiles(query string) []FileHit {
	out := []FileHit{}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return out
	}
	cfg := a.cfgSnapshot()
	repos := scan.Discover(cfg.Roots, cfg.ScanDepth, false)
	// Same round-robin fairness as SearchAll so early repos don't starve later
	// ones of the file-hit budget.
	perRepo := make([][]FileHit, len(repos))
	for i, r := range repos {
		files, _ := git.ListFiles(a.runner, r.Path)
		for _, f := range files {
			if strings.Contains(strings.ToLower(f), q) {
				perRepo[i] = append(perRepo[i], FileHit{Repo: r.Name, RepoPath: r.Path, File: f})
				if len(perRepo[i]) >= searchPerRepoCap {
					break
				}
			}
		}
	}
	for round := 0; len(out) < searchGlobalCap; round++ {
		added := false
		for i := range perRepo {
			if round < len(perRepo[i]) {
				out = append(out, perRepo[i][round])
				added = true
				if len(out) >= searchGlobalCap {
					break
				}
			}
		}
		if !added {
			break
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
	return a.githubInfoForRemote(remote)
}

// githubInfoForRemote returns a repo's GitHub status (cached per owner/repo).
// Returns Available=false when the remote is not a parseable GitHub URL or gh
// fails. Shared by GitHubInfo and GitHubSignals so both hit the same cache.
func (a *App) githubInfoForRemote(remote string) GitHubView {
	owner, repo, ok := gh.OwnerRepo(remote)
	if !ok {
		return GitHubView{Available: false}
	}
	key := owner + "/" + repo
	a.ghMu.RLock()
	e, hit := a.ghCache[key]
	a.ghMu.RUnlock()
	if hit && time.Since(e.at) < ghTTL {
		return e.v
	}

	info, err := gh.Fetch(a.ghRunner, owner, repo)
	if err != nil {
		return GitHubView{Available: false} // do not cache failures - retry next time
	}
	v := GitHubView{CI: info.CI, PRs: info.PRs, Issues: info.Issues, Available: info.Available}
	a.ghMu.Lock()
	if a.ghCache == nil {
		a.ghCache = map[string]ghEntry{}
	}
	a.ghCache[key] = ghEntry{v: v, at: time.Now()}
	a.ghMu.Unlock()
	return v
}

// RepoGHSignal is one repo's GitHub status for the brief.
type RepoGHSignal struct {
	RepoPath string `json:"repoPath"`
	Name     string `json:"name"`
	CI       string `json:"ci"`
	PRs      int    `json:"prs"`
	Issues   int    `json:"issues"`
}

// GitHubSignals bulk-fetches GitHub status for every discovered repo that has a
// GitHub remote, bounded-parallel and cache-backed (shares the badge cache).
// Repos with no/non-GitHub remote or an unavailable result are omitted; a
// gh-less environment returns an empty slice, never an error.
func (a *App) GitHubSignals() []RepoGHSignal {
	out := []RepoGHSignal{}
	cfg := a.cfgSnapshot()
	repos := scan.Discover(cfg.Roots, cfg.ScanDepth, false)

	type job struct {
		path, name, remote string
	}
	var jobs []job
	for _, r := range repos {
		remote, err := git.RemoteURL(a.runner, r.Path)
		if err != nil || strings.TrimSpace(remote) == "" {
			continue
		}
		if _, _, ok := gh.OwnerRepo(remote); !ok {
			continue // not a GitHub remote
		}
		jobs = append(jobs, job{path: r.Path, name: r.Name, remote: remote})
	}

	results := make([]RepoGHSignal, len(jobs))
	found := make([]bool, len(jobs))
	sem := make(chan struct{}, 6) // bounded worker pool
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func(i int, j job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			v := a.githubInfoForRemote(j.remote)
			if !v.Available {
				return
			}
			results[i] = RepoGHSignal{RepoPath: j.path, Name: j.name, CI: v.CI, PRs: v.PRs, Issues: v.Issues}
			found[i] = true
		}(i, j)
	}
	wg.Wait()
	for i := range jobs {
		if found[i] {
			out = append(out, results[i])
		}
	}
	return out
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
	e, hit := a.symCache[path]
	a.symMu.RUnlock()
	if hit && time.Since(e.at) < symTTL {
		return e.v
	}

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
		a.symCache = map[string]symEntry{}
	}
	a.symCache[path] = symEntry{v: v, at: time.Now()}
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

// AgentAvailable reports whether the agentic deep-dive can run: the provider
// must be Claude (Claude Code), the claude CLI must be on PATH, and it must meet
// the v2.1 floor (stream-json + PreToolUse JSON decisions). Below the floor the
// UI degrades to the single-shot deep-dive.
func (a *App) AgentAvailable() bool {
	c := a.cfgSnapshot()
	if c.AIProvider != "" && c.AIProvider != "claude" {
		return false
	}
	path, err := exec.LookPath("claude")
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	winhide.Apply(cmd)
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	maj, min, ok := agent.ParseVersion(string(out))
	return ok && agent.MinVersionMet(maj, min)
}

// consentPath is the marker file recording one-time agentic consent.
func (a *App) consentPath() string { return filepath.Join(a.dataDir, "agent_consent") }

// AgentConsent reports whether the one-time agentic consent was given.
func (a *App) AgentConsent() bool {
	_, err := os.Stat(a.consentPath())
	return err == nil
}

// GiveAgentConsent records the one-time consent. Returns "" on success.
func (a *App) GiveAgentConsent() string {
	if err := os.MkdirAll(a.dataDir, 0o755); err != nil {
		return err.Error()
	}
	return errMsg(os.WriteFile(a.consentPath(), []byte("1"), 0o644))
}

// fleetExecutable resolves the path of the running fleet executable, which
// WriteHookSettings registers (with agent.HookFlag) as the PreToolUse hook
// command. fleet.exe self-invokes as its own hook, so no separate hook binary
// is shipped. Falls back to os.Args[0] if os.Executable fails.
func fleetExecutable() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	if len(os.Args) > 0 {
		return os.Args[0]
	}
	return "fleet"
}

// AgentAsk starts an agentic deep-dive on projectID's repo for question. It
// spawns the claude CLI, streams events to the front end (agent:text/activity/
// done/error), and gates mutating tool calls through agent:action. Returns ""
// on a successful start, or an "error: ..." string.
func (a *App) AgentAsk(projectID, question string) string {
	// Checked here too (ahead of the store read below), not just inside
	// runAgent: TestAgentAskRefusesWithoutConsent requires AgentAsk to refuse
	// before touching the store at all. runAgent re-checks it (and
	// AgentAvailable) as the single source of truth enforced for every
	// caller, including AgentAskFleet.
	if !a.AgentConsent() {
		return "error: agentic deep-dive requires consent"
	}
	repoDir := projectID // a code project's id is its repo path
	rec, _ := a.store.Get(projectID)
	name := rec.Name
	if name == "" {
		name = filepath.Base(projectID)
	}
	return a.runAgent(repoDir, agent.BuildSystemPrompt(name, rec), projectID, question)
}

// runAgent is the shared agentic-run pipeline behind AgentAsk and
// AgentAskFleet: consent/availability gates, tmp hook settings, the
// single-run mutex (agentMu/agentCancel/agentSrv), the approval server +
// classify closure, and the run goroutine (events + cleanup). repoDir is
// where the claude CLI runs, systemPrompt frames its role, and sessionKey
// keys the resume-session cache (a.agentSession) - callers that want an
// independent conversation history pass distinct keys. Returns "" on a
// successful start, or an "error: ..." string.
func (a *App) runAgent(repoDir, systemPrompt, sessionKey, question string) string {
	if !a.AgentConsent() {
		return "error: agentic deep-dive requires consent"
	}
	if !a.AgentAvailable() {
		return "error: agentic deep-dive requires the Claude (Claude Code) provider"
	}

	tmpDir, err := os.MkdirTemp("", "fleet-agent-")
	if err != nil {
		return "error: " + err.Error()
	}
	settings := filepath.Join(tmpDir, "settings.json")
	if err := agent.WriteHookSettings(settings, fleetExecutable()); err != nil {
		os.RemoveAll(tmpDir)
		return "error: " + err.Error()
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.agentMu.Lock()
	if a.agentCancel != nil {
		a.agentCancel() // supersede any earlier run
	}
	a.agentCancel = cancel
	if a.agentSrv != nil {
		a.agentSrv.Stop(nil)
	}
	srv := agent.NewApprovalServer(ctx, a.agentCoord, 10*time.Minute,
		func(req agent.ActionRequest) {
			wruntime.EventsEmit(a.ctx, "agent:action", req)
		},
		func(tool string, input json.RawMessage, cwd string) agent.Verdict {
			// The branch can change mid-run via checkout, so it is resolved
			// live from the tool call's cwd rather than cached at run start.
			// Only Bash classification can reach a push decision (Edit/Write
			// never consult CurrentBranch), so the branch - a git subprocess
			// spawn - is resolved only for Bash; other tools skip it.
			var branch string
			if tool == "Bash" {
				if cwd == "" {
					cwd = repoDir
				}
				branch = git.CurrentBranch(cwd)
			}
			return agent.Classify(tool, input, agent.ClassifyContext{
				CurrentBranch:     branch,
				ProtectedBranches: agent.DefaultProtectedBranches(),
			})
		})
	if err := srv.Start(); err != nil {
		a.agentMu.Unlock()
		cancel()
		os.RemoveAll(tmpDir)
		return "error: " + err.Error()
	}
	a.agentSrv = srv
	resume := a.agentSession[sessionKey]
	a.agentMu.Unlock()

	opts := agent.Options{
		RepoDir:      repoDir,
		Prompt:       question,
		SystemPrompt: systemPrompt,
		Policy:       agent.DefaultPolicy(),
		SettingsPath: settings,
		HookURL:      srv.URL(),
		ResumeID:     resume,
		MaxTurns:     24,
	}
	go func() {
		// Stop the loopback approval server when the run ends so it doesn't
		// leak on normal completion (not just on the next AgentAsk/CancelAgent).
		// Registered first so it runs last (after cancel unblocks any pending
		// Await), letting Shutdown drain cleanly.
		defer srv.Stop(nil)
		defer os.RemoveAll(tmpDir)
		defer cancel()
		sawResult := false
		err := agent.Driver{}.Run(ctx, opts, func(ev agent.Event) {
			switch ev.Kind {
			case agent.KindInit:
				if ev.SessionID != "" {
					a.agentMu.Lock()
					a.agentSession[sessionKey] = ev.SessionID
					a.agentMu.Unlock()
				}
			case agent.KindText:
				// Stream only the partial text_delta chunks; the CLI repeats the
				// same text as a complete assistant block when partial messages
				// are on, so emitting both would double the answer.
				if ev.Partial {
					wruntime.EventsEmit(a.ctx, "agent:text", ev.Text)
				}
			case agent.KindTool:
				wruntime.EventsEmit(a.ctx, "agent:activity", map[string]any{
					"tool": ev.ToolName, "input": string(ev.ToolInput),
				})
			case agent.KindResult:
				sawResult = true
				wruntime.EventsEmit(a.ctx, "agent:done", map[string]any{
					"result": ev.Result, "costUsd": ev.CostUSD,
					"inputTokens": ev.InputTokens, "outputTokens": ev.OutputTokens,
				})
			}
		})
		if err != nil {
			wruntime.EventsEmit(a.ctx, "agent:error", err.Error())
			return
		}
		if !sawResult {
			// The CLI exited cleanly but never emitted a final result line: still
			// emit a terminal event so the overlay does not hang on "working...".
			wruntime.EventsEmit(a.ctx, "agent:done", map[string]any{"result": ""})
		}
	}()
	return ""
}

// AgentAskFleet starts a fleet-wide agentic deep-dive: the agent runs at the
// first configured project root and can read/grep across every repo under
// it, with each mutating action approved by the user just like AgentAsk.
// Returns "" on a successful start, or an "error: ..." string.
func (a *App) AgentAskFleet(question string) string {
	cfg := a.cfgSnapshot()
	if len(cfg.Roots) == 0 {
		return "error: no project root configured"
	}
	root := cfg.Roots[0]

	var fps []agent.FleetProject
	for id, rec := range a.store.Snapshot() { // id is the record's key: a repo path for code projects
		open := 0
		for _, t := range rec.Tasks {
			if t.Status != "done" {
				open++
			}
		}
		name := rec.Name
		if name == "" {
			name = filepath.Base(id)
		}
		fps = append(fps, agent.FleetProject{Name: name, Status: rec.Status, Deadline: rec.Deadline, OpenTasks: open})
	}
	return a.runAgent(root, agent.BuildFleetSystemPrompt(fps), "__fleet__", question)
}

// ApproveAction resolves the outstanding gated tool call id with the user's
// decision, unblocking the waiting fleet-hook request.
func (a *App) ApproveAction(id string, approved bool) {
	reason := "approved in fleet"
	if !approved {
		reason = "rejected in fleet"
	}
	a.agentCoord.Decide(id, approved, reason)
}

// CancelAgent kills the in-flight agentic run (context cancel + WaitDelay) and
// unblocks any pending approval as a deny.
func (a *App) CancelAgent() {
	a.agentMu.Lock()
	c := a.agentCancel
	a.agentMu.Unlock()
	if c != nil {
		c()
	}
}
