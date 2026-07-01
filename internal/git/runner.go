package git

import (
	"bytes"
	"os/exec"
)

// Runner runs a git subcommand in dir and returns its stdout. It is the single
// seam through which fleet touches git; tests substitute a fake.
type Runner interface {
	Run(dir string, args ...string) (string, error)
}

// ExecRunner runs the real git binary via os/exec.
type ExecRunner struct{}

// Run executes `git <args...>` with working directory dir.
func (ExecRunner) Run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return out.String(), err
}
