// Package agent drives the local `claude` CLI in headless agentic mode and
// gates its mutating tool calls through fleet's approval UI. It is Wails-free
// (stdlib + internal/store + internal/winhide only); app.go adapts it to Wails
// events/bindings.
//
// CLI capability spike (verified live against installed claude CLI v2.1.206,
// `claude --help` / `claude --version`, 2026-07-10):
// -p/--print, --output-format stream-json, --include-partial-messages,
// --verbose, --append-system-prompt, --allowedTools, --disallowedTools,
// --settings <file>, --resume are all documented in `claude --help` output
// as of v2.1.206. --max-turns <turns> is NOT listed in `claude --help` (it
// is registered with .hideHelp() in the CLI's own option parser, confirmed
// by inspecting the installed binary) but is a fully functional, currently
// used flag ("Maximum number of agentic turns in non-interactive mode.
// This will early exit the conversation after the specified number of
// turns"); the CLI's own internal background-agent driver invokes it as
// `--output-format stream-json --verbose --max-turns <n> --permission-mode
// dontAsk`, matching the combination this package depends on. --settings
// was present, so the temporary .claude/settings.json fallback described
// below is not needed against this CLI build but is kept documented for
// older installs. Below the v2.1 floor, callers degrade to single-shot.
// Fallback (only if --settings is ever absent on an older CLI): write a
// temporary .claude/settings.json in the repo cwd before the run and
// delete it after (non-invasive; see driver.go).
package agent

import (
	"strconv"
	"strings"
)

// minMajor, minMinor is the claude CLI floor for agentic mode: stream-json with
// PreToolUse JSON decisions requires v2.1+.
const (
	minMajor = 2
	minMinor = 1
)

// ParseVersion extracts the major and minor version from `claude --version`
// output such as "2.1.4 (Claude Code)" or "claude 2.3.0" or "v2.10.1". ok is
// false when no dotted numeric token is found.
func ParseVersion(out string) (major, minor int, ok bool) {
	for _, f := range strings.Fields(out) {
		f = strings.TrimPrefix(strings.TrimSpace(f), "v")
		if f == "" || f[0] < '0' || f[0] > '9' {
			continue
		}
		parts := strings.SplitN(f, ".", 3)
		if len(parts) < 2 {
			continue
		}
		maj, err1 := strconv.Atoi(parts[0])
		min, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			continue
		}
		return maj, min, true
	}
	return 0, 0, false
}

// MinVersionMet reports whether major.minor satisfies the agentic floor (v2.1).
func MinVersionMet(major, minor int) bool {
	if major != minMajor {
		return major > minMajor
	}
	return minor >= minMinor
}
