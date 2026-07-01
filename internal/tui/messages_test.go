package tui

import (
	"testing"

	"github.com/hoijun/fleet/internal/git"
	"github.com/hoijun/fleet/internal/repo"
)

type okRunner struct{}

func (okRunner) Run(dir string, args ...string) (string, error) {
	switch args[0] {
	case "status":
		return "# branch.head main\n", nil
	case "log":
		return "h\x1fa\x1f2026-07-01T10:00:00+09:00\x1fmsg", nil
	default:
		return "", nil
	}
}

func TestLoadRepoCmdReturnsLoadedMsg(t *testing.T) {
	cmd := loadRepoCmd(okRunner{}, repo.Repo{Path: "/x", IsGit: true})
	msg := cmd()
	loaded, ok := msg.(repoLoadedMsg)
	if !ok {
		t.Fatalf("want repoLoadedMsg, got %T", msg)
	}
	if !loaded.Loaded || loaded.Branch != "main" {
		t.Errorf("repo not loaded: %+v", repo.Repo(loaded))
	}
}

func TestFetchCmdReturnsFetchDone(t *testing.T) {
	var _ git.Runner = okRunner{}
	cmd := fetchCmd(okRunner{}, "/x")
	msg := cmd()
	done, ok := msg.(fetchDoneMsg)
	if !ok {
		t.Fatalf("want fetchDoneMsg, got %T", msg)
	}
	if done.Path != "/x" || done.Err != nil {
		t.Errorf("done=%+v", done)
	}
}
