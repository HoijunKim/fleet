// Package action builds the OS commands fleet runs on behalf of the user:
// opening an editor or terminal at a repo, and running arbitrary command lines.
// It imports no TUI code so it stays unit-testable.
package action

import (
	"os/exec"
	"runtime"

	"github.com/hoijun/fleet/internal/winhide"
)

// EditorCmd builds a command that opens path in the configured editor.
func EditorCmd(editor, path string) *exec.Cmd {
	return exec.Command(editor, path)
}

// TerminalCmd builds a command that opens a new terminal whose working
// directory is path. The terminal program's own conventions vary, so fleet just
// launches it with its working directory set to the repo.
func TerminalCmd(terminal, path string) *exec.Cmd {
	c := exec.Command(terminal)
	c.Dir = path
	return c
}

// RunInDir runs the shell command line in dir and returns combined output.
// The line is passed to the platform shell so pipes/args behave as the user
// expects.
func RunInDir(dir, line string) (string, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", line)
	} else {
		cmd = exec.Command("sh", "-c", line)
	}
	cmd.Dir = dir
	winhide.Apply(cmd)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
