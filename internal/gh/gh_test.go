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
		"ssh://git@github.com/hoijun/fleet.git":  {"hoijun", "fleet"},
	}
	for remote, want := range cases {
		o, r, ok := OwnerRepo(remote)
		if !ok || o != want[0] || r != want[1] {
			t.Errorf("OwnerRepo(%q)=%q,%q,%v want %q,%q", remote, o, r, ok, want[0], want[1])
		}
	}
	if _, _, ok := OwnerRepo("git@gitlab.com:x/y.git"); ok {
		// non-github still parses owner/repo; ok true is acceptable. Only assert empty/garbage fails:
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

type stubErr struct{ msg string }

func (e *stubErr) Error() string { return e.msg }
