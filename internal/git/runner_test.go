package git

import (
	"os/exec"
	"strings"
	"testing"
)

// TestExecRunnerSurfacesGitStderr verifies a git failure carries git's own
// diagnostic, not just the bare "exit status N".
func TestExecRunnerSurfacesGitStderr(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir() // an empty dir is not a git repo
	_, err := ExecRunner{}.Run(dir, "status")
	if err == nil {
		t.Fatal("expected an error running git status outside a repo")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("error must carry git's real diagnostic, got: %v", err)
	}
}
