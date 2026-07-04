// Package ai asks the local Claude Code CLI for a text completion. It shells to
// `claude --print` (headless), reusing the user's existing Claude auth so fleet
// needs no API key of its own. The single Runner seam keeps callers testable.
package ai

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/hoijun/fleet/internal/winhide"
)

// Runner turns a prompt into a completion. Tests substitute a fake.
type Runner interface {
	Ask(prompt string) (string, error)
}

// timeout bounds a single completion so a hung CLI never blocks the UI forever.
const timeout = 120 * time.Second

// ExecRunner runs the real `claude --print`, hiding the console window on
// Windows. The prompt is written to stdin (not argv) so long prompts - a git
// log plus task context - never hit the command-line length limit.
type ExecRunner struct{}

func (ExecRunner) Ask(prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "--print")
	winhide.Apply(cmd)
	cmd.Stdin = strings.NewReader(prompt)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("claude timed out after %s", timeout)
		}
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("claude: %s", msg)
	}
	return strings.TrimSpace(out.String()), nil
}

// Available reports whether the `claude` CLI is on PATH.
func Available() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}
