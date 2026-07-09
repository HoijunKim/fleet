package git

import "testing"

func TestNormalizeRemote(t *testing.T) {
	cases := map[string]string{
		"git@github.com:Owner/Repo.git":               "github.com/owner/repo",
		"https://github.com/Owner/Repo.git":           "github.com/owner/repo",
		"https://github.com/Owner/Repo":               "github.com/owner/repo",
		"https://user:pass@github.com/Owner/Repo.git": "github.com/owner/repo",
		"ssh://git@github.com/Owner/Repo.git":         "github.com/owner/repo",
		"":                                            "",
	}
	for in, want := range cases {
		if got := NormalizeRemote(in); got != want {
			t.Errorf("NormalizeRemote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeRemote_TrailingSlashConvergence(t *testing.T) {
	const want = "github.com/user/repo"
	forms := []string{
		"https://github.com/user/repo",
		"https://github.com/user/repo/",
		"https://github.com/user/repo.git",
		"https://github.com/user/repo.git/",
		"https://github.com/user/repo/.git",
	}
	for _, in := range forms {
		if got := NormalizeRemote(in); got != want {
			t.Errorf("NormalizeRemote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeRemote_DistinctReposStayDistinct(t *testing.T) {
	a := NormalizeRemote("https://github.com/user/repo-a")
	b := NormalizeRemote("https://github.com/user/repo-b")
	if a == b {
		t.Errorf("NormalizeRemote collided distinct repos: %q == %q", a, b)
	}

	h1 := NormalizeRemote("https://host1.com/x/y")
	h2 := NormalizeRemote("https://host2.com/x/y")
	if h1 == h2 {
		t.Errorf("NormalizeRemote collided distinct hosts: %q == %q", h1, h2)
	}
}

func TestRemoteURL(t *testing.T) {
	f := &opFake{out: map[string]string{
		"remote get-url": "git@github.com:o/r.git\n",
	}}
	got, err := RemoteURL(f, "/x")
	if err != nil {
		t.Fatal(err)
	}
	if got != "git@github.com:o/r.git" {
		t.Errorf("RemoteURL = %q", got)
	}
}
