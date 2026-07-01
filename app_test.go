package main

import (
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
	a := &App{
		cfg: config.Default(),
		runner: fakeRunner{out: map[string]string{
			"status": "# branch.head main\n",
			"log":    "h\x1fme\x1f2026-07-01T10:00:00+09:00\x1fmsg",
		}},
	}
	v := a.LoadRepo("/some/path")
	if v.Branch != "main" || !v.Loaded {
		t.Errorf("LoadRepo did not load via runner: %+v", v)
	}
}

func TestFetchReturnsEmptyOnSuccess(t *testing.T) {
	a := &App{runner: fakeRunner{out: map[string]string{}}}
	if msg := a.Fetch("/x"); msg != "" {
		t.Errorf("Fetch returned %q, want empty on success", msg)
	}
}
