package agent

import "strings"

// Policy is the tool allow/deny lists handed to the claude CLI. Read is
// allow-listed so file reads run without a prompt (secret paths still blocked by
// the Read(**/.env)-style deny globs); secret reads and destructive shell
// commands are denied outright; mutating tools (Edit, Write, general Bash) AND
// the search tools (Grep, Glob) are deliberately absent from both lists so they
// fall through to the PreToolUse approval hook, where the classifier gates
// mutations and denies searches that target a secret-shaped path.
type Policy struct {
	Allowed    []string
	Disallowed []string
}

// DefaultPolicy returns fleet's slice-1 tool policy.
func DefaultPolicy() Policy {
	return Policy{
		Allowed: []string{
			"Read",
			"Bash(git status)", "Bash(git status:*)",
			"Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)",
		},
		Disallowed: []string{
			"Read(**/.env)", "Read(**/.env.*)", "Read(**/*secret*)",
			"Read(**/id_rsa)", "Read(**/id_ed25519)", "Read(**/*.pem)",
			"Read(**/*.key)", "Read(**/.ssh/**)",
			"Read(**/credentials)", "Read(**/credentials*)", "Read(**/.aws/**)",
			"Read(**/*token*)", "Read(**/*.p12)", "Read(**/*.pfx)",
			"Read(**/.netrc)", "Read(**/*.keystore)", "Read(**/*.ovpn)",
			"Bash(rm:*)", "Bash(sudo:*)", "Bash(curl:*)",
		},
	}
}

// Flags renders the policy as claude CLI flags: each non-empty list becomes a
// single comma-joined value (allow flag first, then deny).
func (p Policy) Flags() []string {
	var out []string
	if len(p.Allowed) > 0 {
		out = append(out, "--allowedTools", strings.Join(p.Allowed, ","))
	}
	if len(p.Disallowed) > 0 {
		out = append(out, "--disallowedTools", strings.Join(p.Disallowed, ","))
	}
	return out
}
