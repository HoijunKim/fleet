package gh

import (
	"strings"
	"testing"
)

func TestOwnerRepo(t *testing.T) {
	cases := map[string][2]string{
		"git@github.com:hoijun/fleet.git":       {"hoijun", "fleet"},
		"https://github.com/hoijun/fleet.git":   {"hoijun", "fleet"},
		"https://github.com/hoijun/fleet":       {"hoijun", "fleet"},
		"ssh://git@github.com/hoijun/fleet.git": {"hoijun", "fleet"},
	}
	for remote, want := range cases {
		o, r, ok := OwnerRepo(remote)
		if !ok || o != want[0] || r != want[1] {
			t.Errorf("OwnerRepo(%q)=%q,%q,%v want %q,%q", remote, o, r, ok, want[0], want[1])
		}
	}
	// Non-GitHub hosts must be rejected so they are never queried against GitHub.
	for _, remote := range []string{
		"git@gitlab.com:x/y.git",
		"https://bitbucket.org/o/r.git",
		"ssh://git@gitlab.example.com/o/r.git",
	} {
		if _, _, ok := OwnerRepo(remote); ok {
			t.Errorf("non-GitHub remote %q must not parse as GitHub", remote)
		}
	}
	if _, _, ok := OwnerRepo(""); ok {
		t.Error("empty remote must not parse")
	}
	if _, _, ok := OwnerRepo("garbage"); ok {
		t.Error("garbage remote must not parse")
	}
}

type ghFake struct{ err error }

func (f ghFake) Run(args ...string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	j := strings.Join(args, " ")
	switch {
	case strings.Contains(j, "actions/runs"):
		return "success\n", nil
	case strings.Contains(j, "type:pr"):
		return "2\n", nil
	case strings.Contains(j, "type:issue"):
		return "5\n", nil
	}
	return "", nil
}

func TestFetchParses(t *testing.T) {
	info, err := Fetch(ghFake{}, "hoijun", "fleet")
	if err != nil {
		t.Fatal(err)
	}
	if info.CI != "success" || info.PRs != 2 || info.Issues != 5 || !info.Available {
		t.Errorf("info=%+v", info)
	}
}

func TestFetchErrorWhenGhUnavailable(t *testing.T) {
	_, err := Fetch(ghFake{err: &stubErr{"gh: not found"}}, "o", "r")
	if err == nil {
		t.Error("expected error when the gh CI call fails")
	}
}

// ciOnlyFake succeeds on the CI call but fails every PR/issue search, so Fetch
// must still report Available with the counts left at 0 (tolerated failure).
type ciOnlyFake struct{}

func (ciOnlyFake) Run(args ...string) (string, error) {
	if strings.Contains(strings.Join(args, " "), "actions/runs") {
		return "success\n", nil
	}
	return "", &stubErr{"search unavailable"}
}

func TestFetchToleratesPRIssueFailure(t *testing.T) {
	info, err := Fetch(ciOnlyFake{}, "o", "r")
	if err != nil {
		t.Fatalf("CI succeeded, Fetch must not error: %v", err)
	}
	if !info.Available || info.CI != "success" || info.PRs != 0 || info.Issues != 0 {
		t.Errorf("info=%+v want Available,CI=success,PRs=0,Issues=0", info)
	}
}

type stubErr struct{ msg string }

func (e *stubErr) Error() string { return e.msg }
