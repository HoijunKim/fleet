package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoijun/fleet/internal/config"
	"github.com/hoijun/fleet/internal/edges"
	"github.com/hoijun/fleet/internal/repo"
	"github.com/hoijun/fleet/internal/store"
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
	cfg := config.Default()
	cfg.Roots = []string{t.TempDir()} // hermetic: scan an empty temp dir, not the real ~/Projects
	return &App{cfg: cfg, runner: fakeRunner{out: map[string]string{}}, store: st, edges: ed}
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
	if got := a.SearchAll("   "); got == nil || len(got) != 0 {
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
	hits := a.SearchAll("package")
	if len(hits) != 1 {
		t.Fatalf("hits=%v", hits)
	}
	if hits[0].Repo != "myrepo" || hits[0].File != "main.go" || hits[0].Line != 1 {
		t.Errorf("hit=%+v", hits[0])
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
