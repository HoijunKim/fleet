package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hoijun/fleet/internal/config"
	"github.com/hoijun/fleet/internal/repo"
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
