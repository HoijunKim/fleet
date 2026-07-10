package agent

import (
	"strings"
	"testing"
)

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestDefaultPolicyLists(t *testing.T) {
	p := DefaultPolicy()
	if !has(p.Allowed, "Read") || !has(p.Allowed, "Grep") || !has(p.Allowed, "Glob") {
		t.Errorf("read-only tools must be allowed: %+v", p.Allowed)
	}
	if !has(p.Disallowed, "Read(**/.env)") || !has(p.Disallowed, "Bash(rm:*)") || !has(p.Disallowed, "Bash(git push:*)") {
		t.Errorf("secret/destructive must be denied: %+v", p.Disallowed)
	}
	// Mutators are gated by the hook, so they are in NEITHER list.
	for _, m := range []string{"Edit", "Write"} {
		if has(p.Allowed, m) || has(p.Disallowed, m) {
			t.Errorf("%s must be gated (absent from both lists)", m)
		}
	}
}

func TestPolicyFlags(t *testing.T) {
	p := Policy{Allowed: []string{"Read", "Grep"}, Disallowed: []string{"Bash(rm:*)"}}
	got := strings.Join(p.Flags(), " ")
	want := "--allowedTools Read,Grep --disallowedTools Bash(rm:*)"
	if got != want {
		t.Errorf("Flags() = %q want %q", got, want)
	}
	if len(Policy{}.Flags()) != 0 {
		t.Error("empty policy must produce no flags")
	}
}
