package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/hoijun/fleet/internal/winhide"
)

// Options configures one agentic run of the claude CLI.
type Options struct {
	RepoDir      string
	Prompt       string
	SystemPrompt string
	Policy       Policy
	SettingsPath string
	HookURL      string
	ResumeID     string
	MaxTurns     int
}

// BuildArgs assembles the claude CLI argv for an agentic streaming run. Pure so
// it can be unit-tested without spawning anything. --verbose is required to
// enable stream-json output under -p on current CLI builds.
func BuildArgs(o Options) []string {
	args := []string{
		"-p", o.Prompt,
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--verbose",
	}
	if strings.TrimSpace(o.SystemPrompt) != "" {
		args = append(args, "--append-system-prompt", o.SystemPrompt)
	}
	args = append(args, o.Policy.Flags()...)
	if o.SettingsPath != "" {
		args = append(args, "--settings", o.SettingsPath)
	}
	if o.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(o.MaxTurns))
	}
	if o.ResumeID != "" {
		args = append(args, "--resume", o.ResumeID)
	}
	return args
}

// HookFlag is the sentinel argument that makes the fleet executable run as its
// own PreToolUse hook instead of launching the GUI. WriteHookSettings appends
// it to the fleet executable path, and the app's main() dispatches on it.
const HookFlag = "--agent-hook"

// WriteHookSettings writes a run-scoped claude settings file at path that
// registers fleet's PreToolUse hook for the mutating tools. The hook command is
// the current fleet executable (fleetExe, resolved via os.Executable) invoked
// with HookFlag, so there is no separate hook binary to ship - fleet.exe self-
// invokes as its own hook. The CLI is pointed at the settings with --settings
// so no project .claude/ is touched. The hook timeout is generous (human-scale)
// so a person has time to approve.
func WriteHookSettings(path, fleetExe string) error {
	cfg := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Edit|Write|Bash|Grep|Glob",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": hookCommand(fleetExe),
							"timeout": 900,
						},
					},
				},
			},
		},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// hookCommand builds the shell command string that invokes fleetExe as the
// PreToolUse hook. The executable path is wrapped in double quotes because on
// Windows it commonly contains spaces (e.g. under "Program Files"), and the
// claude CLI runs the command through a shell.
func hookCommand(fleetExe string) string {
	return `"` + fleetExe + `" ` + HookFlag
}

// Driver spawns the claude CLI and streams its events. Bin defaults to "claude"
// on PATH; tests point it at a stub. WaitDelay bounds Wait after a kill (a Node
// CLI can leave a grandchild holding the stdout pipe), defaulting to 10s.
type Driver struct {
	Bin       string
	WaitDelay time.Duration
}

// Run spawns claude for o and invokes onEvent for every parsed stream event, in
// stream order, until the process exits or ctx is cancelled. FLEET_HOOK_URL is
// injected into the child env so the PreToolUse hook can reach fleet.
func (d Driver) Run(ctx context.Context, o Options, onEvent func(Event)) error {
	bin := d.Bin
	if bin == "" {
		bin = "claude"
	}
	wait := d.WaitDelay
	if wait == 0 {
		wait = 10 * time.Second
	}
	cmd := exec.CommandContext(ctx, bin, BuildArgs(o)...)
	cmd.Dir = o.RepoDir
	winhide.Apply(cmd)
	cmd.WaitDelay = wait
	cmd.Env = append(os.Environ(), "FLEET_HOOK_URL="+o.HookURL)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)
	// tool_use inputs can carry large diffs; allow long lines.
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		for _, ev := range Parse(scanner.Bytes()) {
			onEvent(ev)
		}
	}
	err = cmd.Wait()
	if err != nil {
		if ctx.Err() == context.Canceled {
			return fmt.Errorf("cancelled")
		}
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("claude: %s", msg)
	}
	return nil
}
