package repo

import "testing"

func TestMarker(t *testing.T) {
	cases := []struct {
		name string
		r    Repo
		want string
	}{
		{"non-git", Repo{IsGit: false}, "-"},
		{"clean", Repo{IsGit: true, Dirty: false}, "ok"},
		{"dirty", Repo{IsGit: true, Dirty: true, ModifiedCount: 3}, "*3"},
		{"errored", Repo{IsGit: true, Err: errStub}, "!"},
	}
	for _, c := range cases {
		if got := c.r.Marker(); got != c.want {
			t.Errorf("%s: Marker()=%q want %q", c.name, got, c.want)
		}
	}
}

var errStub = &stubErr{}

type stubErr struct{}

func (*stubErr) Error() string { return "boom" }
