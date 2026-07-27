package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hoijun/fleet/internal/cloud"
	"github.com/hoijun/fleet/internal/config"
	"github.com/hoijun/fleet/internal/edges"
	"github.com/hoijun/fleet/internal/git"
	"github.com/hoijun/fleet/internal/intel"
	"github.com/hoijun/fleet/internal/repo"
	"github.com/hoijun/fleet/internal/store"
	"github.com/hoijun/fleet/internal/syncengine"
)

type fakeRunner struct{ out map[string]string }

func (f fakeRunner) Run(dir string, args ...string) (string, error) {
	return f.out[args[0]], nil
}

func TestToViewMapsFields(t *testing.T) {
	r := repo.Repo{
		Name: "x", Path: "/x", IsGit: true, Branch: "main",
		Dirty: true, ModifiedCount: 2, Ahead: 1, Behind: 0, HasUpstream: true,
		RemoteURL: "git@h:/x.git", DirtyFiles: []string{"a.go"},
		Language: "Go", SizeBytes: 10, TodoCount: 3, Loaded: true,
	}
	r.Last = repo.Commit{Hash: "abcdef1", Message: "m", Author: "me"}
	v := toView(r)
	if v.Name != "x" || v.Branch != "main" || !v.Dirty || v.Modified != 2 {
		t.Errorf("bad view: %+v", v)
	}
	if v.Ahead != 1 || !v.HasUpstream || v.Remote != "git@h:/x.git" {
		t.Errorf("bad git view: %+v", v)
	}
	if v.LastHash != "abcdef1" || v.LastAuthor != "me" || v.Language != "Go" || v.Todo != 3 {
		t.Errorf("bad meta view: %+v", v)
	}
	if v.ErrMsg != "" || !v.Loaded {
		t.Errorf("bad state view: %+v", v)
	}
}

func TestToViewErrMsg(t *testing.T) {
	v := toView(repo.Repo{IsGit: true, Err: errStub{}})
	if v.ErrMsg == "" {
		t.Error("expected ErrMsg populated from Err")
	}
}

type errStub struct{}

func (errStub) Error() string { return "boom" }

func TestLoadRepoUsesRunner(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := &App{
		cfg: config.Default(),
		runner: fakeRunner{out: map[string]string{
			"status": "# branch.head main\n",
			"log":    "h\x1fme\x1f2026-07-01T10:00:00+09:00\x1fmsg",
		}},
	}
	v := a.LoadRepo(dir)
	if v.Branch != "main" || !v.Loaded {
		t.Errorf("LoadRepo did not load via runner: %+v", v)
	}
	if !v.IsGit {
		t.Errorf("expected IsGit true for a dir containing .git")
	}
}

func TestLoadRepoNonGitHasNoError(t *testing.T) {
	dir := t.TempDir() // no .git subdir
	a := &App{cfg: config.Default(), runner: fakeRunner{out: map[string]string{}}}
	v := a.LoadRepo(dir)
	if v.IsGit {
		t.Errorf("expected IsGit false for a non-git dir")
	}
	if v.ErrMsg != "" {
		t.Errorf("non-git dir must not produce an error, got %q", v.ErrMsg)
	}
	if !v.Loaded {
		t.Errorf("non-git dir should still be marked Loaded")
	}
}

func TestFetchReturnsEmptyOnSuccess(t *testing.T) {
	a := &App{runner: fakeRunner{out: map[string]string{}}}
	if msg := a.Fetch("/x"); msg != "" {
		t.Errorf("Fetch returned %q, want empty on success", msg)
	}
}

type errRunner struct{}

func (errRunner) Run(dir string, args ...string) (string, error) { return "", errStub{} }

func TestFetchAndPullReturnErrTextOnFailure(t *testing.T) {
	a := &App{runner: errRunner{}}
	if msg := a.Fetch("/x"); msg == "" {
		t.Error("Fetch should return error text on failure")
	}
	if msg := a.Pull("/x"); msg == "" {
		t.Error("Pull should return error text on failure")
	}
	if msg := a.Push("/x"); msg == "" {
		t.Error("Push should return error text on failure")
	}
}

func TestBranchNameValidation(t *testing.T) {
	a := &App{runner: fakeRunner{out: map[string]string{}}}
	if msg := a.CreateBranch("/x", ""); !strings.Contains(msg, "empty") {
		t.Errorf("empty branch name should be rejected, got %q", msg)
	}
	if msg := a.CreateBranch("/x", "-evil"); !strings.Contains(msg, "'-'") {
		t.Errorf("a '-'-prefixed name should be rejected, got %q", msg)
	}
	if msg := a.DeleteBranch("/x", "--force"); !strings.Contains(msg, "'-'") {
		t.Errorf("a '-'-prefixed delete should be rejected, got %q", msg)
	}
	if msg := a.CreateBranch("/x", "feature/ok"); msg != "" {
		t.Errorf("a normal name should pass, got %q", msg)
	}
}

func TestSyncedUncloned(t *testing.T) {
	a := newTestApp(t)
	// A locally-present code project is keyed by its path - NOT detached.
	if err := a.store.Update("C:/repos/here", func(r *store.Record) { r.Name = "local-here" }); err != nil {
		t.Fatal(err)
	}
	// A detached git: record: listed and cloneable, with its remote rebuilt.
	if err := a.store.Update("git:github.com/o/r", func(r *store.Record) {
		r.Name = "remote-one"
		r.Tasks = []store.Task{{ID: "t1", Title: "x"}}
	}); err != nil {
		t.Fatal(err)
	}
	// A detached local: record: listed but not cloneable (no known remote).
	if err := a.store.Update("local:abc123", func(r *store.Record) { r.Name = "no-remote" }); err != nil {
		t.Fatal(err)
	}
	a.AddProject("manual-one") // manual record - not detached

	got := a.SyncedUncloned()
	if len(got) != 2 {
		t.Fatalf("want 2 detached records, got %d: %+v", len(got), got)
	}
	by := map[string]UnclonedView{}
	for _, v := range got {
		by[v.ID] = v
	}
	if g := by["git:github.com/o/r"]; g.Remote != "https://github.com/o/r" || !g.CanClone || g.TaskCount != 1 {
		t.Errorf("git: record wrong: %+v", g)
	}
	if l := by["local:abc123"]; l.CanClone || l.Remote != "" {
		t.Errorf("local: record must not be cloneable: %+v", l)
	}

	// Once the repo is cloned into a scan root, it drops off the uncloned list.
	root := a.cfgSnapshot().Roots[0]
	if err := os.MkdirAll(filepath.Join(root, "r"), 0o755); err != nil { // matches cloneBase("https://github.com/o/r")
		t.Fatal(err)
	}
	after := a.SyncedUncloned()
	for _, v := range after {
		if v.ID == "git:github.com/o/r" {
			t.Error("a git: record whose repo is already cloned must not be listed")
		}
	}
	if len(after) != 1 { // only the local: record remains
		t.Errorf("want 1 remaining after clone, got %d: %+v", len(after), after)
	}
}

func TestCloneUnclonedGuards(t *testing.T) {
	a := newTestApp(t)
	// A local: record has no remote to clone.
	if msg := a.CloneUncloned("local:abc", ""); !strings.Contains(msg, "remote") {
		t.Errorf("local: clone should be refused, got %q", msg)
	}
	// An existing destination is never overwritten.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "r"), 0o755); err != nil {
		t.Fatal(err)
	}
	if msg := a.CloneUncloned("git:github.com/o/r", root); !strings.Contains(msg, "already exists") {
		t.Errorf("existing dest should be refused, got %q", msg)
	}
	// A pathological doc id whose repo segment is ".." must not resolve a dest
	// outside the root.
	if msg := a.CloneUncloned("git:github.com/o/..", root); !strings.Contains(msg, "folder name") {
		t.Errorf("a '..' repo segment should be refused, got %q", msg)
	}
}

func TestWriteExportProducesValidJSON(t *testing.T) {
	a := newTestApp(t)
	id := a.AddProject("exported")
	a.addTaskReturnID(t, id, "a task")

	dest := filepath.Join(t.TempDir(), "out.json")
	if err := a.writeExport(dest); err != nil {
		t.Fatalf("writeExport: %v", err)
	}
	raw, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	var body struct {
		Projects map[string]store.Record `json:"projects"`
		Intel    intel.Data              `json:"intel"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("export is not valid JSON: %v", err)
	}
	rec, ok := body.Projects[id]
	if !ok || rec.Name != "exported" || len(rec.Tasks) != 1 {
		t.Errorf("export missing the project/task: %+v", body.Projects)
	}
}

func TestDiffAllReturnsWorktreeDiff(t *testing.T) {
	// WorktreeDiff runs `git diff HEAD`; the fake keys canned output by argv[0].
	a := &App{runner: fakeRunner{out: map[string]string{"diff": "@@ -1 +1 @@\n-old\n+new\n"}}}
	got := a.DiffAll("/x")
	if !strings.Contains(got, "+new") || strings.Contains(got, "[error:") {
		t.Errorf("DiffAll returned %q, want the runner's diff with no error suffix", got)
	}
}

func TestRemoteToHTTPS(t *testing.T) {
	cases := map[string]string{
		"git@github.com:o/r.git":       "https://github.com/o/r",
		"https://github.com/o/r.git":   "https://github.com/o/r",
		"ssh://git@github.com/o/r.git": "https://github.com/o/r",
		"":                             "",
		"weird":                        "",
	}
	for in, want := range cases {
		if got := remoteToHTTPS(in); got != want {
			t.Errorf("remoteToHTTPS(%q)=%q want %q", in, got, want)
		}
	}
}

func TestSaveConfigPersistsAndUpdatesCache(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("APPDATA", tmp)         // config.Path() on Windows
	t.Setenv("XDG_CONFIG_HOME", tmp) // config.Path() elsewhere
	a := &App{cfg: config.Default()}
	c := config.Default()
	c.Editor = "myeditor"
	if msg := a.SaveConfig(c); msg != "" {
		t.Fatalf("SaveConfig returned error: %s", msg)
	}
	if a.cfg.Editor != "myeditor" {
		t.Errorf("in-memory cfg not updated: %q", a.cfg.Editor)
	}
	got, _, err := config.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.Editor != "myeditor" {
		t.Errorf("persisted editor=%q want myeditor", got.Editor)
	}
}

type fakeAI struct {
	out string
	err error
}

func (f fakeAI) Ask(_ context.Context, prompt string) (string, error) { return f.out, f.err }

func TestAskAINilRunner(t *testing.T) {
	a := newTestApp(t) // no aiRunner set
	if got := a.AskAI("hi"); got != "error: AI unavailable" {
		t.Errorf("nil runner: got %q", got)
	}
}

func TestAskAISuccess(t *testing.T) {
	a := newTestApp(t)
	a.aiRunner = fakeAI{out: "do the EMG labeling first"}
	if got := a.AskAI("what next?"); got != "do the EMG labeling first" {
		t.Errorf("got %q", got)
	}
}

func TestAskAIError(t *testing.T) {
	a := newTestApp(t)
	a.aiRunner = fakeAI{err: errStub{}}
	got := a.AskAI("x")
	if len(got) < 6 || got[:6] != "error:" {
		t.Errorf("error not surfaced with prefix: got %q", got)
	}
	if !strings.Contains(got, "boom") {
		t.Errorf("underlying error text dropped: got %q", got)
	}
}

func TestAskAIEmptyResponse(t *testing.T) {
	a := newTestApp(t)
	a.aiRunner = fakeAI{out: "   "}
	if got := a.AskAI("x"); got != "error: no response" {
		t.Errorf("empty response: got %q", got)
	}
}

func newTestApp(t *testing.T) *App {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	ed, err := edges.Open(filepath.Join(t.TempDir(), "edges.json"))
	if err != nil {
		t.Fatal(err)
	}
	is, err := intel.Open(filepath.Join(t.TempDir(), "intel.json"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Roots = []string{t.TempDir()} // hermetic: scan an empty temp dir, not the real ~/Projects
	// A real data dir so tests that touch dataDir-relative files (e.g.
	// sync-conflicts.jsonl) are isolated per test, not sharing the CWD.
	return &App{cfg: cfg, runner: fakeRunner{out: map[string]string{}}, store: st, intel: is, edges: ed, dataDir: t.TempDir()}
}

func TestAddAndListManualProject(t *testing.T) {
	a := newTestApp(t)
	id := a.AddProject("thesis")
	if id == "" {
		t.Fatal("AddProject returned empty id")
	}
	var found *ProjectView
	for i, p := range a.ListProjects() {
		if p.ID == id {
			found = &a.ListProjects()[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("manual project %q not in ListProjects", id)
	}
	if found.Name != "thesis" || found.Type != "manual" {
		t.Errorf("bad manual project: %+v", found)
	}
}

func TestTaskLifecycle(t *testing.T) {
	a := newTestApp(t)
	id := a.AddProject("p")
	tid := a.addTaskReturnID(t, id, "do X")
	// toggle
	if msg := a.ToggleTask(id, tid); msg != "" {
		t.Fatalf("ToggleTask: %s", msg)
	}
	p := a.projectByID(t, id)
	if len(p.Tasks) != 1 || !p.Tasks[0].Done {
		t.Errorf("task not toggled done: %+v", p.Tasks)
	}
	// delete
	if msg := a.DeleteTask(id, tid); msg != "" {
		t.Fatalf("DeleteTask: %s", msg)
	}
	if len(a.projectByID(t, id).Tasks) != 0 {
		t.Error("task not deleted")
	}
}

func TestUpdateProjectPersistsFields(t *testing.T) {
	a := newTestApp(t)
	id := a.AddProject("p")
	if msg := a.UpdateProject(id, "paused", 3, "2026-08-29", "defense prep"); msg != "" {
		t.Fatalf("UpdateProject: %s", msg)
	}
	p := a.projectByID(t, id)
	if p.Status != "paused" || p.Priority != 3 || p.Deadline != "2026-08-29" || p.Notes != "defense prep" {
		t.Errorf("update not applied: %+v", p)
	}
}

func TestGetProjectManual(t *testing.T) {
	a := newTestApp(t)
	id := a.AddProject("thesis")
	p := a.GetProject(id)
	if p.ID != id || p.Name != "thesis" || p.Type != "manual" {
		t.Errorf("GetProject manual wrong: %+v", p)
	}
}

func TestMutationsPersistViaUpdate(t *testing.T) {
	a := newTestApp(t)
	id := a.AddProject("p")
	if msg := a.AddTask(id, "do X", ""); msg != "" {
		t.Fatalf("AddTask: %s", msg)
	}
	tid := a.GetProject(id).Tasks[0].ID
	if msg := a.ToggleTask(id, tid); msg != "" {
		t.Fatalf("ToggleTask: %s", msg)
	}
	p := a.GetProject(id)
	if len(p.Tasks) != 1 || !p.Tasks[0].Done {
		t.Errorf("task not toggled via GetProject: %+v", p.Tasks)
	}
	if msg := a.UpdateProject(id, "paused", 3, "2026-08-29", "n"); msg != "" {
		t.Fatalf("UpdateProject: %s", msg)
	}
	p = a.GetProject(id)
	if p.Status != "paused" || p.Priority != 3 || p.Deadline != "2026-08-29" {
		t.Errorf("update not applied: %+v", p)
	}
}

func TestSetTagsRoundTrip(t *testing.T) {
	a := newTestApp(t)
	id := a.AddProject("p")
	if msg := a.SetTags(id, []string{"work", "urgent"}); msg != "" {
		t.Fatalf("SetTags: %s", msg)
	}
	tags := a.GetProject(id).Tags
	if len(tags) != 2 || tags[0] != "work" {
		t.Errorf("tags not persisted: %v", tags)
	}
}

func TestSetTagsNilCoerced(t *testing.T) {
	a := newTestApp(t)
	id := a.AddProject("p")
	if msg := a.SetTags(id, nil); msg != "" {
		t.Fatalf("SetTags nil: %s", msg)
	}
	if a.GetProject(id).Tags == nil {
		t.Error("Tags should never be nil in the view")
	}
}

func TestSetTaskStatus(t *testing.T) {
	a := newTestApp(t)
	id := a.AddProject("proj")
	a.AddTask(id, "task one", "")
	tv := a.GetProject(id).Tasks
	if len(tv) != 1 || tv[0].Status != "todo" {
		t.Fatalf("new task should be todo, got %+v", tv)
	}
	if msg := a.SetTaskStatus(id, tv[0].ID, "doing"); msg != "" {
		t.Fatalf("SetTaskStatus doing: %s", msg)
	}
	tv = a.GetProject(id).Tasks
	if tv[0].Status != "doing" || tv[0].Done {
		t.Errorf("want doing/undone, got %+v", tv[0])
	}
	if msg := a.SetTaskStatus(id, tv[0].ID, "done"); msg != "" {
		t.Fatalf("SetTaskStatus done: %s", msg)
	}
	if pv := a.GetProject(id); pv.Tasks[0].Status != "done" || !pv.Tasks[0].Done || pv.DoneCount != 1 || pv.TaskCount != 1 {
		t.Errorf("want done+mirror+counts, got %+v", pv)
	}
	if msg := a.SetTaskStatus(id, tv[0].ID, "bogus"); msg == "" {
		t.Error("invalid status must be rejected")
	}
}

func TestReorderTasks(t *testing.T) {
	a := newTestApp(t)
	id := a.AddProject("proj2")
	a.AddTask(id, "A", "")
	a.AddTask(id, "B", "")
	a.AddTask(id, "C", "")
	ts := a.GetProject(id).Tasks
	ids := []string{ts[2].ID, ts[0].ID, ts[1].ID} // C, A, B
	if msg := a.ReorderTasks(id, ids); msg != "" {
		t.Fatalf("ReorderTasks: %s", msg)
	}
	got := a.GetProject(id).Tasks
	if got[0].Title != "C" || got[1].Title != "A" || got[2].Title != "B" {
		t.Errorf("bad order: %v", []string{got[0].Title, got[1].Title, got[2].Title})
	}
	// omitting an id keeps it (appended), never drops
	if msg := a.ReorderTasks(id, []string{got[2].ID}); msg != "" {
		t.Fatal(msg)
	}
	if len(a.GetProject(id).Tasks) != 3 {
		t.Error("reorder must never drop a task")
	}
	// a duplicate id must not duplicate the task (no store corruption)
	cur := a.GetProject(id).Tasks
	if msg := a.ReorderTasks(id, []string{cur[0].ID, cur[0].ID, cur[1].ID, cur[2].ID}); msg != "" {
		t.Fatal(msg)
	}
	after := a.GetProject(id).Tasks
	if len(after) != 3 {
		t.Errorf("duplicate id must not duplicate a task, got %d tasks", len(after))
	}
	uniq := map[string]bool{}
	for _, tk := range after {
		if uniq[tk.ID] {
			t.Errorf("duplicate task id %s persisted", tk.ID)
		}
		uniq[tk.ID] = true
	}
}

// TestGeneratedIDsUnique guards against the coarse-clock collision: rapid
// successive AddTask/AddProject calls once shared a time.Now().UnixNano() and so
// got the same id, which the id-keyed mutators then corrupted. Every generated id
// must be distinct regardless of clock resolution.
func TestGeneratedIDsUnique(t *testing.T) {
	a := newTestApp(t)
	id := a.AddProject("p")
	seen := map[string]bool{id: true}
	for i := 0; i < 200; i++ {
		a.AddTask(id, "t", "")
		pid := a.AddProject("q")
		if seen[pid] {
			t.Fatalf("duplicate project id %q at iter %d", pid, i)
		}
		seen[pid] = true
	}
	for _, tk := range a.GetProject(id).Tasks {
		if seen[tk.ID] {
			t.Fatalf("duplicate task id %q", tk.ID)
		}
		seen[tk.ID] = true
	}
	if len(a.GetProject(id).Tasks) != 200 {
		t.Errorf("want 200 tasks, got %d (ids collided or dropped)", len(a.GetProject(id).Tasks))
	}
}

// helpers
func (a *App) addTaskReturnID(t *testing.T, projectID, title string) string {
	t.Helper()
	if msg := a.AddTask(projectID, title, ""); msg != "" {
		t.Fatalf("AddTask: %s", msg)
	}
	tasks := a.projectByID(t, projectID).Tasks
	if len(tasks) == 0 {
		t.Fatal("no task after AddTask")
	}
	return tasks[len(tasks)-1].ID
}

func (a *App) projectByID(t *testing.T, id string) ProjectView {
	t.Helper()
	for _, p := range a.ListProjects() {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("project %q not found", id)
	return ProjectView{}
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSearchAllEmptyQuery(t *testing.T) {
	a := newTestApp(t)
	if got := a.SearchAll("   ", false); got == nil || len(got) != 0 {
		t.Errorf("blank query must return empty non-nil, got %v", got)
	}
}

func TestSearchAllAssembles(t *testing.T) {
	// A real git repo in a temp root so scan.Discover finds it; the fake runner
	// returns canned grep output for the "grep" subcommand.
	root := t.TempDir()
	repo := filepath.Join(root, "myrepo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Roots = []string{root}
	a := &App{cfg: cfg, runner: fakeRunner{out: map[string]string{"grep": "main.go:1:package main\n"}}, store: newTestStore(t)}
	hits := a.SearchAll("package", false)
	if len(hits) != 1 {
		t.Fatalf("hits=%v", hits)
	}
	if hits[0].Repo != "myrepo" || hits[0].File != "main.go" || hits[0].Line != 1 {
		t.Errorf("hit=%+v", hits[0])
	}
}

func TestSearchFilesEmptyQuery(t *testing.T) {
	a := newTestApp(t)
	if got := a.SearchFiles("   "); got == nil || len(got) != 0 {
		t.Errorf("blank query must return empty non-nil, got %v", got)
	}
}

func TestSearchFilesAssembles(t *testing.T) {
	// A real git repo in a temp root so scan.Discover finds it; the fake runner
	// returns canned ls-files output for the "ls-files" subcommand.
	root := t.TempDir()
	repo := filepath.Join(root, "myrepo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Roots = []string{root}
	a := &App{cfg: cfg, runner: fakeRunner{out: map[string]string{"ls-files": "src/main.go\nREADME.md\nconfig.yaml\n"}}, store: newTestStore(t)}
	hits := a.SearchFiles("main")
	if len(hits) != 1 {
		t.Fatalf("hits=%v", hits)
	}
	if hits[0].Repo != "myrepo" || hits[0].File != "src/main.go" {
		t.Errorf("hit=%+v", hits[0])
	}
	// Test case-insensitive matching: uppercase query should match lowercase file
	hitsUpper := a.SearchFiles("MAIN")
	if len(hitsUpper) != 1 {
		t.Fatalf("case-insensitive query should match, got hits=%v", hitsUpper)
	}
	if hitsUpper[0].File != "src/main.go" {
		t.Errorf("case-insensitive hit file=%q, want src/main.go", hitsUpper[0].File)
	}
}

func TestSearchFilesFuzzyRanksAndFilters(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "r")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Roots = []string{root}
	// "cbsv" is a subsequence of CommitBox.svelte (tight, boundary-rich) and a
	// looser subsequence of a helper; it is NOT a subsequence of notes.md.
	a := &App{cfg: cfg, runner: fakeRunner{out: map[string]string{
		"ls-files": "internal/cabinets_view.go\nfrontend/src/lib/CommitBox.svelte\ndocs/notes.md\n",
	}}, store: newTestStore(t)}

	hits := a.SearchFiles("cbsv")
	if len(hits) == 0 {
		t.Fatal("expected fuzzy matches")
	}
	if hits[0].File != "frontend/src/lib/CommitBox.svelte" {
		t.Errorf("best match should rank first, got %q (all: %+v)", hits[0].File, hits)
	}
	for _, h := range hits {
		if h.File == "docs/notes.md" {
			t.Error("notes.md is not a subsequence of cbsv and must be excluded")
		}
	}
}

type ghFakeApp struct{}

func (ghFakeApp) Run(args ...string) (string, error) {
	j := strings.Join(args, " ")
	switch {
	case strings.Contains(j, "actions/runs"):
		return "failure\n", nil
	case strings.Contains(j, "type:pr"):
		return "1\n", nil
	case strings.Contains(j, "type:issue"):
		return "3\n", nil
	}
	return "", nil
}

func TestGitHubInfoParses(t *testing.T) {
	a := newTestApp(t)
	a.ghRunner = ghFakeApp{}
	v := a.GitHubInfo("git@github.com:hoijun/fleet.git")
	if !v.Available || v.CI != "failure" || v.PRs != 1 || v.Issues != 3 {
		t.Errorf("view=%+v", v)
	}
}

func TestGitHubInfoNoRemote(t *testing.T) {
	a := newTestApp(t)
	a.ghRunner = ghFakeApp{}
	if v := a.GitHubInfo(""); v.Available {
		t.Error("empty remote must be Available=false")
	}
}

type ghCountFake struct{ calls *int }

func (f ghCountFake) Run(args ...string) (string, error) {
	*f.calls++
	j := strings.Join(args, " ")
	switch {
	case strings.Contains(j, "actions/runs"):
		return "success\n", nil
	case strings.Contains(j, "type:pr"):
		return "1\n", nil
	case strings.Contains(j, "type:issue"):
		return "2\n", nil
	}
	return "", nil
}

func TestGitHubInfoCachesSuccess(t *testing.T) {
	calls := 0
	a := newTestApp(t)
	a.ghRunner = ghCountFake{calls: &calls}
	a.GitHubInfo("git@github.com:o/r.git")
	first := calls
	if first == 0 {
		t.Fatal("first call should have invoked the runner")
	}
	a.GitHubInfo("git@github.com:o/r.git") // must hit cache
	if calls != first {
		t.Errorf("second call should hit cache; runner calls went %d -> %d", first, calls)
	}
}

type ghErrCountFake struct{ calls *int }

func (f ghErrCountFake) Run(args ...string) (string, error) {
	*f.calls++
	return "", ghTestErr{}
}

type ghTestErr struct{}

func (ghTestErr) Error() string { return "gh unavailable" }

func TestGitHubInfoDoesNotCacheFailure(t *testing.T) {
	calls := 0
	a := newTestApp(t)
	a.ghRunner = ghErrCountFake{calls: &calls}
	a.GitHubInfo("git@github.com:o/r.git")
	first := calls
	a.GitHubInfo("git@github.com:o/r.git") // must retry (failure not cached)
	if calls <= first {
		t.Errorf("failed fetch must not be cached; second call should retry (calls %d -> %d)", first, calls)
	}
}

// dirRemoteRunner is a git.Runner that resolves "remote get-url origin" per
// directory. fakeRunner (above) is dir-agnostic - it answers purely off
// args[0], the same for every repo - so it cannot model two repos discovered
// under one App/runner with different remotes, which GitHubSignals's test
// needs. This is the minimal extension that closes that one gap; every other
// git subcommand still returns empty output, same as fakeRunner's zero value.
type dirRemoteRunner struct{ remotes map[string]string }

func (r dirRemoteRunner) Run(dir string, args ...string) (string, error) {
	if len(args) > 0 && args[0] == "remote" {
		if url, ok := r.remotes[dir]; ok {
			return url, nil
		}
		return "", errStub{} // no origin configured for this repo
	}
	return "", nil
}

func TestGitHubSignals(t *testing.T) {
	// Discover finds three temp git repos under one root: one with a
	// github.com remote, one with no remote, one with a non-GitHub remote.
	root := t.TempDir()
	ghRepo := filepath.Join(root, "ghrepo")
	noRemoteRepo := filepath.Join(root, "norepo")
	gitlabRepo := filepath.Join(root, "gitlabrepo")
	for _, dir := range []string{ghRepo, noRemoteRepo, gitlabRepo} {
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cfg := config.Default()
	cfg.Roots = []string{root}
	a := &App{
		cfg: cfg,
		runner: dirRemoteRunner{remotes: map[string]string{
			ghRepo:     "git@github.com:hoijun/fleet.git",
			gitlabRepo: "git@gitlab.com:hoijun/other.git",
			// noRemoteRepo intentionally absent -> RemoteURL errors (no origin)
		}},
		ghRunner: ghFakeApp{}, // canned CI=failure, PRs=1, Issues=3
		store:    newTestStore(t),
	}

	got := a.GitHubSignals()
	if len(got) != 1 {
		t.Fatalf("only the github-remote repo should be included: %+v", got)
	}
	s := got[0]
	if s.RepoPath != ghRepo || s.Name != "ghrepo" {
		t.Errorf("wrong repo signaled: %+v", s)
	}
	if s.CI == "" || s.PRs < 0 {
		t.Fatalf("signal not populated: %+v", s)
	}
	if s.CI != "failure" || s.PRs != 1 || s.Issues != 3 {
		t.Errorf("signal values don't match the faked gh output: %+v", s)
	}
}

func TestGitHubSignalsGHUnavailable(t *testing.T) {
	root := t.TempDir()
	ghRepo := filepath.Join(root, "ghrepo")
	if err := os.MkdirAll(filepath.Join(ghRepo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Roots = []string{root}
	calls := 0
	a := &App{
		cfg:      cfg,
		runner:   dirRemoteRunner{remotes: map[string]string{ghRepo: "git@github.com:hoijun/fleet.git"}},
		ghRunner: ghErrCountFake{calls: &calls}, // gh CLI unavailable/erroring
		store:    newTestStore(t),
	}
	got := a.GitHubSignals()
	if got == nil {
		t.Fatal("GitHubSignals must return a non-nil empty slice when gh is unavailable, not nil")
	}
	if len(got) != 0 {
		t.Fatalf("gh-unavailable env should yield no signals: %+v", got)
	}
}

func TestEdgeBindingsRoundTrip(t *testing.T) {
	a := newTestApp(t)
	if msg := a.AddEdge("/a", "/b", "http", "n"); msg != "" {
		t.Fatalf("AddEdge: %s", msg)
	}
	list := a.ListEdges()
	if len(list) != 1 || list[0].From != "/a" || list[0].Kind != "http" {
		t.Fatalf("list=%+v", list)
	}
	if msg := a.AddEdge("/a", "/a", "http", ""); msg == "" {
		t.Error("self-edge must be rejected")
	}
	if msg := a.RemoveEdge(list[0].ID); msg != "" {
		t.Fatalf("RemoveEdge: %s", msg)
	}
	if len(a.ListEdges()) != 0 {
		t.Error("edge not removed")
	}
}

func TestListEdgesNonNil(t *testing.T) {
	a := newTestApp(t)
	if a.ListEdges() == nil {
		t.Error("ListEdges must be non-nil")
	}
}

func TestEdgeBindingsNilStoreDoesNotPanic(t *testing.T) {
	a := &App{cfg: config.Default(), runner: fakeRunner{out: map[string]string{}}}
	if got := a.ListEdges(); got == nil || len(got) != 0 {
		t.Errorf("ListEdges with nil store must be non-nil empty, got %v", got)
	}
	if msg := a.AddEdge("/a", "/b", "http", ""); msg == "" {
		t.Error("AddEdge with nil store must return an error message")
	}
	if msg := a.RemoveEdge("x"); msg == "" {
		t.Error("RemoveEdge with nil store must return an error message")
	}
}

func TestRepoGraphFiltersDanglingManualEdge(t *testing.T) {
	// newTestApp's config root is an empty temp dir, so RepoGraph discovers
	// zero repos/nodes. A manual edge between two non-existent node ids must
	// therefore be filtered out of the merged graph.
	a := newTestApp(t)
	if msg := a.AddEdge("/x", "/y", "http", ""); msg != "" {
		t.Fatalf("AddEdge: %s", msg)
	}
	g := a.RepoGraph()
	for _, e := range g.Edges {
		if e.Manual {
			t.Errorf("dangling manual edge must be filtered, got %+v", e)
		}
	}
}

func TestRepoSymbolsCaches(t *testing.T) {
	dir := t.TempDir()
	goFile := filepath.Join(dir, "foo.go")
	if err := os.WriteFile(goFile, []byte("package foo\n\nfunc Exported() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := newTestApp(t)
	v1 := a.RepoSymbols(dir)
	if !containsString(v1.GoExported, "Exported") {
		t.Fatalf("v1.GoExported = %v, want it to contain Exported", v1.GoExported)
	}

	// Overwrite the source so a fresh extract would no longer find Exported.
	// (Overwrite rather than os.Remove to avoid flaky Windows file-lock removes.)
	if err := os.WriteFile(goFile, []byte("package foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	v2 := a.RepoSymbols(dir)
	if !containsString(v2.GoExported, "Exported") {
		t.Fatalf("v2.GoExported = %v, want it to still contain Exported (served from cache)", v2.GoExported)
	}
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestAgentConsentMarker(t *testing.T) {
	dir := t.TempDir()
	a := &App{dataDir: dir}
	if a.AgentConsent() {
		t.Fatal("consent must be false before it is given")
	}
	if msg := a.GiveAgentConsent(); msg != "" {
		t.Fatalf("GiveAgentConsent error: %s", msg)
	}
	if !a.AgentConsent() {
		t.Error("consent must be true after GiveAgentConsent")
	}
	if a.consentPath() != filepath.Join(dir, "agent_consent") {
		t.Errorf("consentPath = %q", a.consentPath())
	}
}

// TestAgentAskRefusesWithoutConsent guards the consent gate: AgentAsk must
// refuse (before spawning anything or touching the store) when the one-time
// agentic consent has not been given.
func TestAgentAskRefusesWithoutConsent(t *testing.T) {
	dir := t.TempDir() // no agent_consent marker written
	a := &App{dataDir: dir}
	got := a.AgentAsk(dir, "why is CI red?")
	if !strings.Contains(got, "consent") {
		t.Errorf("AgentAsk without consent = %q, want a consent error", got)
	}
}

// TestAgentAskFleetNoRoots guards the fleet run's root gate: with no project
// root configured there is nowhere to run the agent, so AgentAskFleet must
// refuse before ever touching the store or runAgent's consent/available gates.
func TestAgentAskFleetNoRoots(t *testing.T) {
	a := &App{} // zero-value config: cfg.Roots is empty
	got := a.AgentAskFleet("hi")
	if !strings.Contains(got, "root") {
		t.Fatalf("AgentAskFleet with no roots = %q, want an error mentioning root", got)
	}
}

func TestAgenda(t *testing.T) {
	a := newTestApp(t)
	p := a.AddProject("proj")
	a.UpdateProject(p, "active", 0, "2026-08-01", "")
	a.AddTask(p, "due task", "2026-07-10")
	a.AddTask(p, "no-due doing", "")
	a.AddTask(p, "done task", "2026-07-05")
	for _, tk := range a.GetProject(p).Tasks {
		switch tk.Title {
		case "no-due doing":
			a.SetTaskStatus(p, tk.ID, "doing")
		case "done task":
			a.SetTaskStatus(p, tk.ID, "done")
		}
	}

	items := a.Agenda()
	if items == nil {
		t.Fatal("Agenda must be non-nil")
	}

	for _, it := range items {
		if it.Title == "done task" {
			t.Error("done task must be excluded")
		}
	}

	var haveDeadline, haveDueTask, haveDoingTask bool
	for _, it := range items {
		if it.Kind == "deadline" && it.ProjectID == p {
			haveDeadline = true
		}
		if it.Kind == "task" && it.Title == "due task" {
			haveDueTask = true
		}
		if it.Kind == "task" && it.Title == "no-due doing" {
			haveDoingTask = true
		}
	}
	if !haveDeadline {
		t.Errorf("expected a deadline item, got %+v", items)
	}
	if !haveDueTask {
		t.Errorf("expected the due task item, got %+v", items)
	}
	if !haveDoingTask {
		t.Errorf("expected the no-due doing task item, got %+v", items)
	}

	if last := items[len(items)-1]; last.Due != "" {
		t.Errorf("empty-due item should sort last, got %+v", last)
	}
}

func TestDetectEditors(t *testing.T) {
	installed := map[string]bool{"code": true, "nvim": true}
	look := func(cmd string) (string, error) {
		if installed[cmd] {
			return "/usr/bin/" + cmd, nil
		}
		return "", errors.New("not found")
	}
	got := detectEditors(look)
	// full known list returned; installed ones flagged + sorted first
	if len(got) < 5 {
		t.Fatalf("expected the full known list, got %d", len(got))
	}
	if !got[0].Installed || !got[1].Installed {
		t.Fatalf("installed editors should sort first: %+v", got[:2])
	}
	var code EditorOption
	for _, e := range got {
		if e.Command == "code" {
			code = e
		}
	}
	if code.Name != "VS Code" || !code.Installed {
		t.Fatalf("code: %+v", code)
	}
}

type dirGrepFake struct{ byDir map[string]string }

func (f dirGrepFake) Run(dir string, args ...string) (string, error) {
	if len(args) > 0 && args[0] == "grep" {
		return f.byDir[dir], nil
	}
	return "", nil
}

func TestSearchAllRoundRobinFairness(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"aaa", "zzz"} {
		if err := os.MkdirAll(filepath.Join(root, name, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.Roots = []string{root}
	a := &App{cfg: cfg, runner: dirGrepFake{byDir: map[string]string{
		filepath.Join(root, "aaa"): "f1:1:x\nf2:2:x\n",
		filepath.Join(root, "zzz"): "g1:1:x\n",
	}}, store: newTestStore(t)}
	hits := a.SearchAll("x", false)
	repos := map[string]int{}
	for _, h := range hits {
		repos[h.Repo]++
	}
	if repos["aaa"] != 2 || repos["zzz"] != 1 {
		t.Fatalf("both repos must be represented, got %v", repos)
	}
	// Round-robin interleaves: aaa, zzz, aaa (a later repo is not starved).
	if len(hits) != 3 || hits[0].Repo != "aaa" || hits[1].Repo != "zzz" || hits[2].Repo != "aaa" {
		t.Fatalf("expected interleaved aaa/zzz/aaa, got %+v", hits)
	}
}

func TestEditTask(t *testing.T) {
	a := newTestApp(t)
	id := a.AddProject("p")
	tid := a.addTaskReturnID(t, id, "old title")
	a.ToggleTask(id, tid) // mark done; edit must not reset it
	if msg := a.EditTask(id, tid, "new title", "2026-08-01"); msg != "" {
		t.Fatalf("EditTask: %s", msg)
	}
	p := a.projectByID(t, id)
	if p.Tasks[0].Title != "new title" || p.Tasks[0].Due != "2026-08-01" {
		t.Errorf("task not edited: %+v", p.Tasks[0])
	}
	if !p.Tasks[0].Done {
		t.Error("EditTask must not change the done state")
	}
	if msg := a.EditTask(id, tid, "  ", ""); msg == "" {
		t.Error("empty title must be rejected")
	}
	if a.projectByID(t, id).Tasks[0].Title != "new title" {
		t.Error("a rejected edit must not mutate the task")
	}
	// A surrounding-whitespace title is stored trimmed.
	if msg := a.EditTask(id, tid, "  padded  ", ""); msg != "" {
		t.Fatalf("EditTask: %s", msg)
	}
	if got := a.projectByID(t, id).Tasks[0].Title; got != "padded" {
		t.Errorf("title should be stored trimmed, got %q", got)
	}
	// An unknown taskID is a no-op success and touches no existing task.
	if msg := a.EditTask(id, "no-such-task", "ghost", "2026-01-01"); msg != "" {
		t.Errorf("editing a missing task should be a no-op success, got %q", msg)
	}
	if got := a.projectByID(t, id).Tasks[0].Title; got != "padded" {
		t.Errorf("a missing-task edit must not mutate other tasks, got %q", got)
	}
}

func TestRenameProject(t *testing.T) {
	a := newTestApp(t)
	id := a.AddProject("old name")
	if msg := a.RenameProject(id, "new name"); msg != "" {
		t.Fatalf("RenameProject: %s", msg)
	}
	if got := a.projectByID(t, id).Name; got != "new name" {
		t.Errorf("not renamed: %q", got)
	}
	if msg := a.RenameProject(id, "  "); msg == "" {
		t.Error("empty name must be rejected")
	}
	// A code project's name comes from its folder; RenameProject must leave a
	// non-manual record untouched (and not error).
	if err := a.store.Update("/some/code/path", func(r *store.Record) { r.Name = "folder" }); err != nil {
		t.Fatal(err)
	}
	if msg := a.RenameProject("/some/code/path", "hijacked"); msg != "" {
		t.Fatalf("RenameProject on a code project should be a no-op success: %s", msg)
	}
	if got, _ := a.store.Get("/some/code/path"); got.Name != "folder" {
		t.Errorf("RenameProject must not rename a non-manual project, got %q", got.Name)
	}
}

// StartupHealth is what stands between a user and an app that silently looks
// like it forgot everything. It must name the affected file and mark the
// project store as frozen, since that is the one whose writes are refused.
func TestStartupHealthReportsFailedLoads(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "projects.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, storeErr := store.Open(p)
	if storeErr == nil {
		t.Fatal("precondition: the store should have failed to load")
	}
	// A real engine: DiscardCorruptStore resets it, and the App is never built
	// without one.
	eng := syncengine.New(cloud.New("http://127.0.0.1:0"), filepath.Join(dir, "sync.json"),
		syncengine.NewProject(st, func(string) string { return "" }, st.Degraded))
	a := &App{cfg: config.Default(), store: st, dataDir: dir, storeLoadErr: storeErr, engine: eng,
		syncTrigger: make(chan struct{}, 1)}

	issues := a.StartupHealth()
	if len(issues) != 1 {
		t.Fatalf("expected one issue, got %+v", issues)
	}
	if issues[0].Scope != "projects" || !issues[0].Frozen {
		t.Errorf("projects failure must be reported as frozen, got %+v", issues[0])
	}
	if issues[0].Error == "" {
		t.Error("the issue must carry the underlying error")
	}

	// A healthy app reports nothing at all.
	if got := newTestApp(t).StartupHealth(); len(got) != 0 {
		t.Errorf("a healthy app must report no issues, got %+v", got)
	}

	// The opt-in reset clears the condition and re-enables writes.
	if msg := a.DiscardCorruptStore(); msg != "" {
		t.Fatalf("DiscardCorruptStore: %s", msg)
	}
	if got := a.StartupHealth(); len(got) != 0 {
		t.Errorf("expected no issues after the reset, got %+v", got)
	}
	if id := a.AddProject("after reset"); id == "" {
		t.Error("writes must work after the reset")
	}
}

// ConflictBackups is the only way a user can see what sync destroyed. It must
// survive the file being absent, and the truncated last line a crash leaves.
func TestConflictBackupsListsNewestFirstAndTolerates(t *testing.T) {
	dir := t.TempDir()
	a := &App{cfg: config.Default(), dataDir: dir}

	if got := a.ConflictBackups(); len(got) != 0 {
		t.Errorf("a missing file must yield an empty list, got %+v", got)
	}

	lines := `{"at":"2026-07-01T10:00:00Z","localId":"m-1","name":"older","payload":{}}
{"at":"2026-07-02T10:00:00Z","localId":"m-2","name":"newer","payload":{}}
{"at":"2026-07-03T10:00:00Z","localId":"C:/repos/app","payload":{}}
{"at":"2026-07-04T10:00:00Z","localId":"m-4","na`
	if err := os.WriteFile(filepath.Join(dir, "sync-conflicts.jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}

	got := a.ConflictBackups()
	if len(got) != 3 {
		t.Fatalf("expected the 3 complete entries, got %+v", got)
	}
	if got[0].Name != "app" {
		t.Errorf("newest must come first, and a nameless record falls back to its path, got %+v", got[0])
	}
	if got[2].Name != "older" {
		t.Errorf("oldest must come last, got %+v", got[2])
	}
}

// End-to-end over the real NewApp wiring: a corrupt projects.json in the data
// directory must produce a degraded store, a reported health issue, refused
// writes, and a sync engine that will not push from it. This is the exact chain
// that would otherwise turn one bad local file into multi-device data loss.
func TestNewAppSurfacesACorruptStoreEndToEnd(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("config.Path uses APPDATA on windows; XDG_CONFIG_HOME elsewhere")
	}
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	// Point sync at a local no-op server: the degraded source is now skipped
	// (not aborted early), so SyncOnce proceeds to a harmless pull instead of
	// failing on a DNS lookup for the real backend.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"docs": []any{}, "cursor": 0})
	}))
	defer srv.Close()
	t.Setenv("FLEET_API_URL", srv.URL)
	fleetDir := filepath.Join(dir, "fleet")
	if err := os.MkdirAll(fleetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const original = "{not json"
	if err := os.WriteFile(filepath.Join(fleetDir, "projects.json"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	a := NewApp()

	issues := a.StartupHealth()
	if len(issues) != 1 || issues[0].Scope != "projects" {
		t.Fatalf("expected a projects issue, got %+v", issues)
	}
	if a.store.Degraded() == nil {
		t.Error("the store should be degraded")
	}
	if err := a.store.Put("x", store.Record{Manual: true, Name: "x"}); err == nil {
		t.Error("writes must be refused while degraded")
	}
	if err := a.engine.SyncOnce("tok"); err != nil {
		t.Errorf("a degraded store must skip, not error, got %v", err)
	}
	if skipped := a.engine.SkippedDegraded(); len(skipped) != 1 || skipped[0] != "project" {
		t.Errorf("the engine must report the skipped degraded project, got %v", skipped)
	}

	// The bytes survived, under a name the banner can point the user at.
	entries, _ := os.ReadDir(fleetDir)
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "projects.json.corrupt-") {
			data, _ := os.ReadFile(filepath.Join(fleetDir, e.Name()))
			if string(data) != original {
				t.Errorf("quarantined bytes altered: %q", data)
			}
			found = true
		}
	}
	if !found {
		t.Error("the corrupt file must be quarantined, not left to be overwritten")
	}
}

// "Start fresh" means "this device no longer holds the truth", NOT "delete my
// projects". Without resetting the sync engine, sync.json still lists every
// synced doc, none appear in the now-empty store, and the next cycle pushes a
// tombstone for each - destroying the intact server copy and, on their next
// pull, every other device. That would invert the entire point of this tier:
// the unreadable file preserved, the good cloud copy destroyed.
func TestDiscardCorruptStoreDoesNotTombstoneTheCloud(t *testing.T) {
	var pushed []cloud.Doc
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body struct {
				Docs []cloud.Doc `json:"docs"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			pushed = append(pushed, body.Docs...)
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}, "cursor": 0})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"docs": []any{}, "cursor": 0})
	}))
	defer srv.Close()

	dir := t.TempDir()
	p := filepath.Join(dir, "projects.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, storeErr := store.Open(p)
	if storeErr == nil {
		t.Fatal("precondition: the store should have failed to load")
	}
	// sync.json remembers two projects that synced fine before the corruption.
	statePath := filepath.Join(dir, "sync.json")
	state := `{"cursor":7,"docs":{"m-1":{"localId":"m-1","hash":"h1","updatedAt":"2026-07-01T00:00:00Z"},` +
		`"m-2":{"localId":"m-2","hash":"h2","updatedAt":"2026-07-01T00:00:00Z"}}}`
	if err := os.WriteFile(statePath, []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	eng := syncengine.New(cloud.New(srv.URL), statePath,
		syncengine.NewProject(st, func(string) string { return "" }, st.Degraded))
	a := &App{cfg: config.Default(), store: st, dataDir: dir, storeLoadErr: storeErr, engine: eng,
		syncTrigger: make(chan struct{}, 1)}

	if msg := a.DiscardCorruptStore(); msg != "" {
		t.Fatalf("DiscardCorruptStore: %s", msg)
	}
	if err := a.engine.SyncOnce("tok"); err != nil {
		t.Fatalf("sync after the reset should work: %v", err)
	}

	for _, d := range pushed {
		if d.Deleted {
			t.Errorf("discarding a local file must never delete %q on the server", d.DocID)
		}
	}
}

func TestFocusNilContextIsNoop(t *testing.T) {
	// OnSecondInstanceLaunch can fire before startup has assigned a.ctx.
	// focus must return instead of calling into a runtime with no window.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("focus panicked with a nil context: %v", r)
		}
	}()
	a := &App{}
	a.focus()
}

func TestBuildVersionOnZeroApp(t *testing.T) {
	// The Settings modal asks for this before anything else is loaded, so it
	// must answer from package state alone - no config, no store, no ctx.
	if got := (&App{}).BuildVersion(); got == "" {
		t.Error("BuildVersion() = \"\", want a printable build string")
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := (git.ExecRunner{}).Run(dir, args...); err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
}

func TestExportIncludesIntel(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	is, err := intel.Open(filepath.Join(t.TempDir(), "intel.json"))
	if err != nil {
		t.Fatal(err)
	}
	is.SetBrief(intel.Brief{Text: "exported brief"})
	a := &App{store: st, intel: is}

	dest := filepath.Join(t.TempDir(), "out.json")
	if err := a.writeExport(dest); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Projects map[string]json.RawMessage `json:"projects"`
		Intel    intel.Data                 `json:"intel"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("export is not the {projects, intel} shape: %v", err)
	}
	if body.Intel.Brief.Text != "exported brief" {
		t.Errorf("intel brief missing from export: %+v", body.Intel)
	}
}

func TestIntelBindingsRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	gitRun(t, dir, "-c", "init.defaultBranch=master", "init")
	gitRun(t, dir, "remote", "add", "origin", "git@github.com:Owner/Repo.git")

	is, err := intel.Open(filepath.Join(t.TempDir(), "intel.json"))
	if err != nil {
		t.Fatal(err)
	}
	a := &App{runner: git.ExecRunner{}, intel: is}

	if msg := a.SaveChat(dir, []intel.Turn{{Role: "user", Text: "hi"}}); msg != "" {
		t.Fatalf("SaveChat: %s", msg)
	}
	// Stored under the git: identity, reachable by the same path.
	if got := a.GetChat(dir); len(got) != 1 || got[0].Text != "hi" {
		t.Errorf("GetChat = %+v, want one 'hi' turn", got)
	}
	if _, ok := is.Snapshot().Chats["git:github.com/owner/repo"]; !ok {
		t.Error("chat was not stored under the normalized git identity")
	}

	if msg := a.SaveBrief("today", "2026-07-24T00:00:00Z", "ko"); msg != "" {
		t.Fatalf("SaveBrief: %s", msg)
	}
	if b := a.GetBrief(); b.Text != "today" || b.Lang != "ko" {
		t.Errorf("GetBrief = %+v", b)
	}
}

// A conflicted merge round-trips through the bindings exactly as the UI drives
// it: list the conflict, resolve it, finish. This is the integration seam the
// per-function git tests do not cover.
func TestConflictBindingsRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	gitRun(t, dir, "-c", "init.defaultBranch=master", "init")
	gitRun(t, dir, "config", "user.email", "t@t")
	gitRun(t, dir, "config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "base")
	gitRun(t, dir, "checkout", "-b", "other")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("other\n"), 0o644)
	gitRun(t, dir, "commit", "-am", "other")
	gitRun(t, dir, "checkout", "master")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("mine\n"), 0o644)
	gitRun(t, dir, "commit", "-am", "mine")
	if _, err := (git.ExecRunner{}).Run(dir, "merge", "other"); err == nil {
		t.Fatal("the merge should have conflicted")
	}

	a := &App{runner: git.ExecRunner{}}

	cs := a.Conflicts(dir)
	if len(cs) != 1 || cs[0].Path != "f.txt" {
		t.Fatalf("Conflicts = %+v, want one entry for f.txt", cs)
	}
	if cs[0].Mode != "merge" || cs[0].MineLabel != "Keep mine" {
		t.Errorf("view carries the wrong mode/labels: %+v", cs[0])
	}

	if msg := a.ResolveConflict(dir, "f.txt", "mine"); msg != "" {
		t.Fatalf("ResolveConflict: %s", msg)
	}
	if msg := a.ContinueOperation(dir); msg != "" {
		t.Fatalf("ContinueOperation: %s", msg)
	}
	if op := git.OperationInProgress(dir); op != "" {
		t.Errorf("merge not finished, still %q", op)
	}
}

func writeBackupLine(t *testing.T, dir, localID, when, name string, rec store.Record) {
	t.Helper()
	payload, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(struct {
		At      string          `json:"at"`
		LocalID string          `json:"localId"`
		Name    string          `json:"name"`
		Payload json.RawMessage `json:"payload"`
	}{At: when, LocalID: localID, Name: name, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "sync-conflicts.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreBackupReStampsSoItWinsLWW(t *testing.T) {
	a := newTestApp(t)
	when := "2026-07-01T00:00:00Z"
	writeBackupLine(t, a.dataDir, "m-1", when, "my project",
		store.Record{Manual: true, Name: "my project", Notes: "the lost note", UpdatedAt: "2026-07-01T00:00:00Z"})

	if msg := a.RestoreBackup("m-1", when); msg != "" {
		t.Fatalf("RestoreBackup: %s", msg)
	}
	rec, ok := a.store.Get("m-1")
	if !ok || rec.Notes != "the lost note" {
		t.Fatalf("restored record missing or wrong: %+v ok=%v", rec, ok)
	}
	if rec.UpdatedAt <= "2026-07-01T00:00:00Z" {
		t.Errorf("UpdatedAt was not re-stamped to now: %q", rec.UpdatedAt)
	}
}

func TestRestoreBackupRecreatesADeletedRecord(t *testing.T) {
	a := newTestApp(t)
	when := "2026-07-02T00:00:00Z"
	writeBackupLine(t, a.dataDir, "m-gone", when, "deleted one",
		store.Record{Manual: true, Name: "deleted one", UpdatedAt: when})
	if _, ok := a.store.Get("m-gone"); ok {
		t.Fatal("precondition: m-gone should not be in the store")
	}
	if msg := a.RestoreBackup("m-gone", when); msg != "" {
		t.Fatalf("RestoreBackup: %s", msg)
	}
	if rec, ok := a.store.Get("m-gone"); !ok || rec.Name != "deleted one" {
		t.Errorf("a deleted record should be re-created by restore: %+v ok=%v", rec, ok)
	}
}

func TestRestoreBackupNoMatchErrorsAndWritesNothing(t *testing.T) {
	a := newTestApp(t)
	writeBackupLine(t, a.dataDir, "m-1", "2026-07-01T00:00:00Z", "x", store.Record{Manual: true, Name: "x"})
	if msg := a.RestoreBackup("m-1", "1999-01-01T00:00:00Z"); msg == "" {
		t.Error("a when that matches no line must return an error")
	}
	if _, ok := a.store.Get("m-1"); ok {
		t.Error("a non-matching restore must not write anything")
	}
}

func TestRestoreBackupPicksTheRightLineAmongMany(t *testing.T) {
	a := newTestApp(t)
	writeBackupLine(t, a.dataDir, "m-1", "2026-07-01T00:00:00Z", "v1",
		store.Record{Manual: true, Name: "m-1", Notes: "first", UpdatedAt: "2026-07-01T00:00:00Z"})
	writeBackupLine(t, a.dataDir, "m-1", "2026-07-03T00:00:00Z", "v2",
		store.Record{Manual: true, Name: "m-1", Notes: "second", UpdatedAt: "2026-07-03T00:00:00Z"})
	if msg := a.RestoreBackup("m-1", "2026-07-01T00:00:00Z"); msg != "" {
		t.Fatalf("RestoreBackup: %s", msg)
	}
	if rec, _ := a.store.Get("m-1"); rec.Notes != "first" {
		t.Errorf("restored the wrong line: got Notes=%q, want first", rec.Notes)
	}
}

func TestRestoredRecordIsRePushed(t *testing.T) {
	var pushed []cloud.Doc
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body struct {
				Docs []cloud.Doc `json:"docs"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			pushed = append(pushed, body.Docs...)
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}, "cursor": 0})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"docs": []any{}, "cursor": 0})
	}))
	defer srv.Close()

	dir := t.TempDir()
	st, _ := store.Open(filepath.Join(dir, "projects.json"))
	eng := syncengine.New(cloud.New(srv.URL), filepath.Join(dir, "sync.json"),
		syncengine.NewProject(st, func(string) string { return "" }, nil))
	a := &App{store: st, dataDir: dir, engine: eng, syncTrigger: make(chan struct{}, 1)}

	when := "2026-07-01T00:00:00Z"
	writeBackupLine(t, dir, "m-1", when, "p",
		store.Record{Manual: true, Name: "p", Notes: "recovered", UpdatedAt: when})
	if msg := a.RestoreBackup("m-1", when); msg != "" {
		t.Fatalf("RestoreBackup: %s", msg)
	}
	if err := a.engine.SyncOnce("tok"); err != nil {
		t.Fatalf("sync: %v", err)
	}
	found := false
	for _, d := range pushed {
		if d.DocID == "m-1" && !d.Deleted {
			found = true
		}
	}
	if !found {
		t.Error("a restored record must be re-pushed on the next sync")
	}
}

func writeExportFile(t *testing.T, path string, projects map[string]store.Record, in intel.Data) {
	t.Helper()
	body := struct {
		Projects map[string]store.Record `json:"projects"`
		Intel    intel.Data              `json:"intel"`
	}{projects, in}
	data, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestImportCommitUpsertsWithoutDeleting(t *testing.T) {
	a := newTestApp(t)
	_ = a.store.Update("m-local", func(r *store.Record) { r.Manual = true; r.Name = "local only" })
	_ = a.store.Update("m-1", func(r *store.Record) { r.Manual = true; r.Name = "old" })

	path := filepath.Join(t.TempDir(), "export.json")
	writeExportFile(t, path, map[string]store.Record{
		"m-1": {Manual: true, Name: "new", Notes: "imported", UpdatedAt: "2020-01-01T00:00:00Z"},
		"m-2": {Manual: true, Name: "added", UpdatedAt: "2020-01-01T00:00:00Z"},
	}, intel.Data{})

	if err := a.importCommit(path); err != nil {
		t.Fatalf("importCommit: %v", err)
	}
	if rec, _ := a.store.Get("m-1"); rec.Name != "new" || rec.Notes != "imported" {
		t.Errorf("m-1 not overwritten: %+v", rec)
	}
	if rec, _ := a.store.Get("m-1"); rec.UpdatedAt <= "2020-01-01T00:00:00Z" {
		t.Errorf("m-1 not re-stamped: %q", rec.UpdatedAt)
	}
	if _, ok := a.store.Get("m-2"); !ok {
		t.Error("m-2 was not added")
	}
	if rec, ok := a.store.Get("m-local"); !ok || rec.Name != "local only" {
		t.Error("import must not delete a local-only record")
	}
}

func TestImportCommitBringsChatsAndBrief(t *testing.T) {
	a := newTestApp(t)
	path := filepath.Join(t.TempDir(), "export.json")
	writeExportFile(t, path, map[string]store.Record{}, intel.Data{
		Brief: intel.Brief{Text: "imported brief"},
		Chats: map[string]intel.Chat{"git:x": {Turns: []intel.Turn{{Role: "user", Text: "hi"}}}},
	})
	if err := a.importCommit(path); err != nil {
		t.Fatalf("importCommit: %v", err)
	}
	if b := a.intel.Brief(); b.Text != "imported brief" {
		t.Errorf("brief not imported: %+v", b)
	}
	if turns := a.intel.Chat("git:x"); len(turns) != 1 || turns[0].Text != "hi" {
		t.Errorf("chat not imported: %+v", turns)
	}
}

func TestImportCommitEmptyBriefDoesNotWipeLocal(t *testing.T) {
	a := newTestApp(t)
	_ = a.intel.SetBrief(intel.Brief{Text: "my local brief"})
	path := filepath.Join(t.TempDir(), "export.json")
	writeExportFile(t, path, map[string]store.Record{}, intel.Data{})
	if err := a.importCommit(path); err != nil {
		t.Fatalf("importCommit: %v", err)
	}
	if b := a.intel.Brief(); b.Text != "my local brief" {
		t.Errorf("an empty imported brief must not wipe the local one: %+v", b)
	}
}

func TestImportCommitRefusesWhenStoreDegraded(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "projects.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, _ := store.Open(p)
	is, _ := intel.Open(filepath.Join(dir, "intel.json"))
	a := &App{store: st, intel: is, dataDir: dir, syncTrigger: make(chan struct{}, 1)}

	path := filepath.Join(dir, "export.json")
	writeExportFile(t, path, map[string]store.Record{"m-1": {Manual: true, Name: "x"}}, intel.Data{})
	if err := a.importCommit(path); err == nil {
		t.Error("importCommit must refuse when the store is degraded")
	}
}

func TestImportSummaryCountsAndMalformed(t *testing.T) {
	a := newTestApp(t)
	_ = a.store.Update("m-1", func(r *store.Record) { r.Manual = true; r.Name = "existing" })

	path := filepath.Join(t.TempDir(), "export.json")
	writeExportFile(t, path, map[string]store.Record{
		"m-1": {Manual: true, Name: "a"},
		"m-2": {Manual: true, Name: "b"},
	}, intel.Data{
		Brief: intel.Brief{Text: "b"},
		Chats: map[string]intel.Chat{"git:x": {Turns: []intel.Turn{{Role: "user", Text: "q"}}}},
	})
	s := a.importSummary(path)
	if s.Error != "" {
		t.Fatalf("unexpected error: %s", s.Error)
	}
	if s.Projects != 2 || s.ProjectsOverwrite != 1 {
		t.Errorf("project counts wrong: %+v", s)
	}
	if s.Chats != 1 || !s.Brief {
		t.Errorf("intel counts wrong: %+v", s)
	}

	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s := a.importSummary(bad); s.Error == "" {
		t.Error("a malformed file must report an error")
	}
}

func TestImportedRecordIsRePushed(t *testing.T) {
	var pushed []cloud.Doc
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body struct {
				Docs []cloud.Doc `json:"docs"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			pushed = append(pushed, body.Docs...)
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}, "cursor": 0})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"docs": []any{}, "cursor": 0})
	}))
	defer srv.Close()

	dir := t.TempDir()
	st, _ := store.Open(filepath.Join(dir, "projects.json"))
	is, _ := intel.Open(filepath.Join(dir, "intel.json"))
	eng := syncengine.New(cloud.New(srv.URL), filepath.Join(dir, "sync.json"),
		syncengine.NewProject(st, func(string) string { return "" }, nil))
	a := &App{store: st, intel: is, dataDir: dir, engine: eng, syncTrigger: make(chan struct{}, 1)}

	path := filepath.Join(dir, "export.json")
	writeExportFile(t, path, map[string]store.Record{
		"m-1": {Manual: true, Name: "p", UpdatedAt: "2020-01-01T00:00:00Z"},
	}, intel.Data{})
	if err := a.importCommit(path); err != nil {
		t.Fatalf("importCommit: %v", err)
	}
	if err := a.engine.SyncOnce("tok"); err != nil {
		t.Fatalf("sync: %v", err)
	}
	found := false
	for _, d := range pushed {
		if d.DocID == "m-1" && !d.Deleted {
			found = true
		}
	}
	if !found {
		t.Error("an imported record must be re-pushed on the next sync")
	}
}

func TestImportCommitBindingReturnsErrMsg(t *testing.T) {
	a := newTestApp(t)
	if msg := a.ImportCommit(filepath.Join(t.TempDir(), "nope.json")); msg == "" {
		t.Error("ImportCommit on a missing file must return an error string")
	}
	path := filepath.Join(t.TempDir(), "ok.json")
	writeExportFile(t, path, map[string]store.Record{"m-1": {Manual: true, Name: "x"}}, intel.Data{})
	if msg := a.ImportCommit(path); msg != "" {
		t.Errorf("a valid import must return \"\", got %q", msg)
	}
}

func TestDeleteBranchForceGuardsEmptyName(t *testing.T) {
	a := newTestApp(t)
	if msg := a.DeleteBranchForce("/repo", "  "); msg == "" {
		t.Error("an empty branch name must be refused without shelling out")
	}
	if msg := a.DeleteBranchForce("/repo", "-D"); msg == "" {
		t.Error("a name starting with '-' must be refused")
	}
}

func TestSearchAllIgnoreCaseThreadsThrough(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "r")
	gitRun(t, root, "-c", "init.defaultBranch=master", "init", "r")
	gitRun(t, repo, "config", "user.email", "t@t")
	gitRun(t, repo, "config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("A line with TODO here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-m", "init")

	cfg := config.Default()
	cfg.Roots = []string{root}
	cfg.ScanDepth = 2
	a := &App{cfg: cfg, runner: git.ExecRunner{}}

	// Case-insensitive finds the uppercase TODO from a lowercase query.
	if hits := a.SearchAll("todo", true); len(hits) == 0 {
		t.Error("ignoreCase search should match TODO from 'todo'")
	}
	// Case-sensitive does not.
	if hits := a.SearchAll("todo", false); len(hits) != 0 {
		t.Errorf("case-sensitive search should not match TODO from 'todo', got %+v", hits)
	}
}

func TestCherryPickBindingCleanAndBadHash(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	gitRun(t, dir, "-c", "init.defaultBranch=master", "init")
	gitRun(t, dir, "config", "gc.auto", "0")
	gitRun(t, dir, "config", "user.email", "t@t")
	gitRun(t, dir, "config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "base")
	gitRun(t, dir, "checkout", "-b", "side")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "side")
	hash := ""
	if out, err := (git.ExecRunner{}).Run(dir, "rev-parse", "HEAD"); err == nil {
		hash = strings.TrimSpace(out)
	}
	gitRun(t, dir, "checkout", "master")

	a := &App{runner: git.ExecRunner{}}
	if msg := a.CherryPick(dir, hash); msg != "" {
		t.Errorf("clean cherry-pick binding should return \"\", got %q", msg)
	}
	if msg := a.CherryPick(dir, "nonexistent-hash"); msg == "" {
		t.Error("a bad hash must return an error string")
	}
}

func TestRestoreReflogRefusesDirtyAndSucceedsClean(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	gitRun(t, dir, "-c", "init.defaultBranch=master", "init")
	gitRun(t, dir, "config", "gc.auto", "0")
	gitRun(t, dir, "config", "user.email", "t@t")
	gitRun(t, dir, "config", "user.name", "T")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "one")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "two")
	gitRun(t, dir, "reset", "--hard", "HEAD~1") // drop "two"

	a := &App{runner: git.ExecRunner{}}

	// Dirty tree: restore is refused and the uncommitted file survives.
	os.WriteFile(filepath.Join(dir, "wip.txt"), []byte("wip\n"), 0o644)
	if msg := a.RestoreReflog(dir, "HEAD@{1}"); msg == "" {
		t.Error("RestoreReflog must refuse on a dirty tree")
	}
	if _, err := os.Stat(filepath.Join(dir, "wip.txt")); err != nil {
		t.Error("a refused restore must not touch the working tree")
	}

	// Clean tree: restore succeeds and brings back the dropped commit's file.
	os.Remove(filepath.Join(dir, "wip.txt"))
	if msg := a.RestoreReflog(dir, "HEAD@{1}"); msg != "" {
		t.Fatalf("clean RestoreReflog: %s", msg)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err != nil {
		t.Errorf("restore should bring back the dropped commit's file: %v", err)
	}
}

func TestReflogBindingLists(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	gitRun(t, dir, "-c", "init.defaultBranch=master", "init")
	gitRun(t, dir, "config", "gc.auto", "0")
	gitRun(t, dir, "config", "user.email", "t@t")
	gitRun(t, dir, "config", "user.name", "T")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "one")

	a := &App{runner: git.ExecRunner{}}
	entries := a.Reflog(dir, 10)
	if len(entries) == 0 || entries[0].Ref != "HEAD@{0}" {
		t.Errorf("Reflog binding = %+v, want a HEAD@{0} entry", entries)
	}
}
