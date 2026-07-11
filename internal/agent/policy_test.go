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
	if !has(p.Disallowed, "Read(**/.env)") || !has(p.Disallowed, "Bash(rm:*)") {
		t.Errorf("secret/destructive must be denied: %+v", p.Disallowed)
	}
	if !has(p.Disallowed, "Read(**/*.key)") || !has(p.Disallowed, "Read(**/.ssh/**)") || !has(p.Disallowed, "Read(**/credentials*)") {
		t.Errorf("expanded secret coverage must be denied: %+v", p.Disallowed)
	}
	for _, g := range []string{
		"Read(**/*token*)", "Read(**/*.p12)", "Read(**/*.pfx)",
		"Read(**/.netrc)", "Read(**/*.keystore)", "Read(**/*.ovpn)",
	} {
		if !has(p.Disallowed, g) {
			t.Errorf("secret glob %s must be denied: %+v", g, p.Disallowed)
		}
	}
	// Mutators are gated by the hook, so they are in NEITHER list.
	for _, m := range []string{"Edit", "Write"} {
		if has(p.Allowed, m) || has(p.Disallowed, m) {
			t.Errorf("%s must be gated (absent from both lists)", m)
		}
	}
}

func TestPolicyPushIsGatedNotDenied(t *testing.T) {
	p := DefaultPolicy()
	for _, d := range p.Disallowed {
		if d == "Bash(git push:*)" {
			t.Fatal("git push must not be hard-denied; it is gated by the classifier")
		}
	}
	// rm/sudo/curl stay denied
	must := map[string]bool{"Bash(rm:*)": false, "Bash(sudo:*)": false, "Bash(curl:*)": false}
	for _, d := range p.Disallowed {
		if _, ok := must[d]; ok {
			must[d] = true
		}
	}
	for k, seen := range must {
		if !seen {
			t.Fatalf("expected %s still denied", k)
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
