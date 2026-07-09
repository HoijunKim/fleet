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
