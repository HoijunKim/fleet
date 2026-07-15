package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/hoijun/fleet/internal/winhide"
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
	winhide.Apply(cmd)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		// Surface git's own diagnostic (e.g. "Permission denied (publickey)",
		// "Updates were rejected", "Your local changes would be overwritten")
		// instead of the bare "exit status N" from os/exec.
		if msg := strings.TrimSpace(errBuf.String()); msg != "" {
			return out.String(), fmt.Errorf("%s: %w", msg, err)
		}
	}
	return out.String(), err
}
