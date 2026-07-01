package git

import (
	"testing"

	"github.com/hoijun/fleet/internal/repo"
)

type opFake struct {
	out  map[string]string
	last [][]string
}

func (f *opFake) Run(dir string, args ...string) (string, error) {
	f.last = append(f.last, args)
	key := args[0]
	if len(args) > 1 {
		key = args[0] + " " + args[1]
	}
	return f.out[key], nil
}

func TestBranches(t *testing.T) {
	f := &opFake{out: map[string]string{
		"branch --show-current": "main\n",
		"for-each-ref":          "main\ndev\nfeature/x\n",
	}}
	cur, all, err := Branches(f, "/x")
	if err != nil || cur != "main" {
		t.Fatalf("cur=%q err=%v", cur, err)
	}
	if len(all) != 3 || all[1] != "dev" {
		t.Errorf("all=%v", all)
	}
}

func TestBranchesRealGitShape(t *testing.T) {
	f := &opFake{out: map[string]string{
		"branch --show-current": "main\n",
		"for-each-ref": "a1 commit refs/heads/main\n" +
			"b2 commit refs/heads/feature/x\n" +
			"c3 commit refs/remotes/origin/main\n" +
			"d4 tag refs/tags/v1.0\n" +
			"e5 commit refs/stash\n",
	}}
	cur, all, err := Branches(f, "/x")
	if err != nil || cur != "main" {
		t.Fatalf("cur=%q err=%v", cur, err)
	}
	if len(all) != 2 || all[0] != "main" || all[1] != "feature/x" {
		t.Errorf("expected only local branches [main feature/x], got %v", all)
	}
}

func TestCommitAllStagesFirst(t *testing.T) {
	f := &opFake{out: map[string]string{}}
	if err := CommitAll(f, "/x", "msg"); err != nil {
		t.Fatal(err)
	}
	if len(f.last) != 2 || f.last[0][0] != "add" || f.last[1][0] != "commit" {
		t.Errorf("expected add then commit, got %v", f.last)
	}
}

func TestLogParsesMultiple(t *testing.T) {
	f := &opFake{out: map[string]string{
		"log -n": "h1\x1fa\x1f2026-07-01T10:00:00+09:00\x1fmsg1\nh2\x1fb\x1f2026-06-30T10:00:00+09:00\x1fmsg2",
	}}
	commits, err := Log(f, "/x", 2)
	if err != nil || len(commits) != 2 {
		t.Fatalf("commits=%v err=%v", commits, err)
	}
	if commits[0].Hash != "h1" || commits[1].Author != "b" {
		t.Errorf("bad commits: %+v", commits)
	}
	_ = repo.Commit{}
}

func TestStashList(t *testing.T) {
	f := &opFake{out: map[string]string{"stash list": "stash@{0}: WIP\nstash@{1}: more\n"}}
	l, err := StashList(f, "/x")
	if err != nil || len(l) != 2 {
		t.Fatalf("l=%v err=%v", l, err)
	}
}
