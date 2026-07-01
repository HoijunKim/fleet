package git

import (
	"strings"
	"testing"

	"github.com/hoijun/fleet/internal/repo"
)

// fakeRunner returns canned output keyed by the first git arg.
type fakeRunner struct {
	out map[string]string
	err map[string]error
}

func (f fakeRunner) Run(dir string, args ...string) (string, error) {
	key := args[0]
	if f.err != nil {
		if e, ok := f.err[key]; ok {
			return "", e
		}
	}
	return f.out[key], nil
}

func TestLoadFillsFields(t *testing.T) {
	f := fakeRunner{out: map[string]string{
		"status": "# branch.head main\n# branch.upstream origin/main\n# branch.ab +1 -0\n1 .M N... 1 1 1 a b file.go\n",
		"log":    "h1\x1fhoijun\x1f2026-07-01T10:00:00+09:00\x1fdid a thing",
		"remote": "git@github.com:hoijun/fleet.git\n",
		"grep":   "file.go:3\n",
	}}
	rp := repo.Repo{Name: "fleet", Path: "/x", IsGit: true}
	Load(f, &rp)

	if rp.Err != nil {
		t.Fatalf("unexpected err: %v", rp.Err)
	}
	if rp.Branch != "main" || !rp.Dirty || rp.ModifiedCount != 1 {
		t.Errorf("status not applied: %+v", rp)
	}
	if rp.Ahead != 1 || !rp.HasUpstream {
		t.Errorf("ahead/upstream not applied: %+v", rp)
	}
	if rp.Last.Author != "hoijun" || rp.Last.Message != "did a thing" {
		t.Errorf("commit not applied: %+v", rp.Last)
	}
	if rp.RemoteURL != "git@github.com:hoijun/fleet.git" {
		t.Errorf("remote=%q", rp.RemoteURL)
	}
	if rp.TodoCount != 3 {
		t.Errorf("todo=%d", rp.TodoCount)
	}
	if !rp.Loaded {
		t.Error("Loaded should be true")
	}
}

func TestLoadSetsErrOnStatusFailure(t *testing.T) {
	boom := &stubErr{msg: "not a repo"}
	f := fakeRunner{err: map[string]error{"status": boom}}
	rp := repo.Repo{IsGit: true}
	Load(f, &rp)
	if rp.Err == nil {
		t.Fatal("expected Err set")
	}
}

type stubErr struct{ msg string }

func (e *stubErr) Error() string { return e.msg }

func TestLoadToleratesMissingRemoteAndTodos(t *testing.T) {
	f := fakeRunner{
		out: map[string]string{
			"status": "# branch.head main\n",
			"log":    "h1\x1fa\x1f2026-07-01T10:00:00+09:00\x1fmsg",
		},
		err: map[string]error{
			"remote": &stubErr{msg: "no origin"},
			"grep":   &stubErr{msg: "no matches"}, // git grep exits 1 when nothing found
		},
	}
	rp := repo.Repo{IsGit: true}
	Load(f, &rp)
	if rp.Err != nil {
		t.Fatalf("remote/grep failures must not set Err: %v", rp.Err)
	}
	if rp.RemoteURL != "" || rp.TodoCount != 0 {
		t.Errorf("expected empty remote and 0 todos, got %q / %d", rp.RemoteURL, rp.TodoCount)
	}
	if !strings.HasPrefix(rp.Branch, "main") {
		t.Errorf("branch=%q", rp.Branch)
	}
}
