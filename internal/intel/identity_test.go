package intel

import (
	"os/exec"
	"testing"

	"github.com/hoijun/fleet/internal/git"
)

func gitOK(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := (git.ExecRunner{}).Run(dir, args...); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}

func TestChatIDFleetPassesThrough(t *testing.T) {
	if got := ChatID(git.ExecRunner{}, FleetID); got != FleetID {
		t.Errorf("ChatID(__fleet__) = %q, want %q", got, FleetID)
	}
}

func TestChatIDUsesGitRemoteWhenPresent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	gitOK(t, dir, "-c", "init.defaultBranch=master", "init")
	gitOK(t, dir, "remote", "add", "origin", "git@github.com:Owner/Repo.git")
	got := ChatID(git.ExecRunner{}, dir)
	if got != "git:github.com/owner/repo" {
		t.Errorf("ChatID = %q, want git:github.com/owner/repo", got)
	}
}

func TestChatIDFallsBackToLocalWithoutRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	gitOK(t, dir, "-c", "init.defaultBranch=master", "init")
	got := ChatID(git.ExecRunner{}, dir)
	if len(got) < 6 || got[:6] != "local:" {
		t.Errorf("ChatID = %q, want a local: id for a repo with no remote", got)
	}
	// Stable: the same path yields the same id.
	if again := ChatID(git.ExecRunner{}, dir); again != got {
		t.Errorf("ChatID not stable: %q != %q", got, again)
	}
}
