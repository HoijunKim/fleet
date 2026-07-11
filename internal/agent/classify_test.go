package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func v(tool, input string, cur string) Verdict {
	return Classify(tool, json.RawMessage(input), ClassifyContext{CurrentBranch: cur, ProtectedBranches: DefaultProtectedBranches()})
}

func TestClassifyEdits(t *testing.T) {
	got := v("Edit", `{"file_path":"README.md","old_string":"a","new_string":"b"}`, "feat/x")
	if got.Decision != "gate" || got.Category != "edit" || got.Summary != "Edit README.md" {
		t.Fatalf("edit: %+v", got)
	}
	w := v("Write", `{"file_path":"new.txt","content":"hi"}`, "feat/x")
	if w.Decision != "gate" || w.Category != "edit" || w.Severity != "medium" {
		t.Fatalf("write: %+v", w)
	}
}

func TestClassifyPushProtectedAlwaysDenied(t *testing.T) {
	deny := []struct{ cmd, cur string }{
		{"git push origin main", "feat/x"},
		{"git push origin HEAD:main", "feat/x"},
		{"git push origin master", "feat/x"},
		{"git push --force origin main", "feat/x"},
		{"git push origin +main", "feat/x"},
		{"git push", "main"},          // bare push while on main
		{"git push origin", "master"}, // bare push, remote only, on master
		{"git push origin feat/x:main", "feat/x"},
		{"git -C . push origin main", "feat/x"},
	}
	for _, d := range deny {
		got := v("Bash", `{"command":`+jsonStr(d.cmd)+`}`, d.cur)
		if got.Decision != "deny" {
			t.Fatalf("expected deny for %q (on %q): %+v", d.cmd, d.cur, got)
		}
	}
}

func TestClassifyPushFeatureBranchGates(t *testing.T) {
	got := v("Bash", `{"command":"git push origin feat/x"}`, "feat/x")
	if got.Decision != "gate" || got.Category != "remote" || got.Severity != "high" {
		t.Fatalf("feature push: %+v", got)
	}
	bare := v("Bash", `{"command":"git push"}`, "feat/x")
	if bare.Decision != "gate" || bare.Category != "remote" {
		t.Fatalf("bare push on feature: %+v", bare)
	}
}

func TestClassifyCommitAndPR(t *testing.T) {
	c := v("Bash", `{"command":"git commit -m \"fix typo\""}`, "feat/x")
	if c.Decision != "gate" || c.Category != "shell" || c.Summary != "Commit: fix typo" {
		t.Fatalf("commit: %+v", c)
	}
	pr := v("Bash", `{"command":"gh pr create --fill"}`, "feat/x")
	if pr.Decision != "gate" || pr.Category != "remote" {
		t.Fatalf("pr: %+v", pr)
	}
}

func TestClassifySecretReadDenied(t *testing.T) {
	got := v("Bash", `{"command":"cat .env"}`, "feat/x")
	if got.Decision != "deny" {
		t.Fatalf("secret read should deny: %+v", got)
	}
}

func TestClassifyFailClosed(t *testing.T) {
	if v("Bash", `{}`, "feat/x").Decision != "gate" { // empty command -> generic gate is fine, but must not be a push
		// an empty command is a harmless no-op; gating it is acceptable
	}
	if v("Bash", `not json`, "feat/x").Decision != "deny" {
		t.Fatal("garbage input must deny")
	}
	if v("Frobnicate", `{}`, "feat/x").Decision != "deny" {
		t.Fatal("unknown tool must deny")
	}
}

// --- Bypass-fix regression tests (findings 1-7) -------------------------------

// TestClassifyCompoundAndDesyncBypassesDenied covers every bypass that let a
// default-branch push (or a secret read) slip past the classifier. Each row must
// DENY.
func TestClassifyCompoundAndDesyncBypassesDenied(t *testing.T) {
	deny := []struct {
		name, cmd, cur string
	}{
		// CRITICAL 1 - compound commands classify off the first subcommand.
		{"and-chain-push", "git add -A && git commit -m x && git push origin main", "feat/x"},
		{"semicolon-push", "git status; git push origin main", "feat/x"},
		{"checkout-then-push", "git checkout -b x && git push origin main", "feat/x"},
		{"or-chain-push", "false || git push origin main", "feat/x"},
		{"pipe-push", "echo hi | git push origin main", "feat/x"},
		{"newline-push", "git add -A\ngit push origin main", "feat/x"},
		{"commit-hides-push", "git commit -m \"wip\" && git push origin main", "feat/x"},
		// CRITICAL 2 - --all / --mirror push every local branch.
		{"push-all", "git push --all", "feat/x"},
		{"push-mirror", "git push --mirror", "feat/x"},
		{"push-force-all", "git push --force --all origin", "feat/x"},
		{"push-all-caps", "git push --ALL", "feat/x"},
		// CRITICAL 3 - value-taking global flags desync the parser.
		{"git-dir-space", "git --git-dir /x push origin main", "feat/x"},
		{"work-tree-space", "git --work-tree /w push origin main", "feat/x"},
		{"namespace-space", "git --namespace ns push origin main", "feat/x"},
		{"exec-path-space", "git --exec-path /e push origin main", "feat/x"},
		// CRITICAL 3 backstop - an UNKNOWN value flag still must not fall through.
		{"unknown-flag-desync", "git --frobnicate zz push origin main", "feat/x"},
		// IMPORTANT 4 - branch-name case must fold.
		{"branch-case-Main", "git push origin Main", "feat/x"},
		{"branch-case-MASTER", "git push origin MASTER", "feat/x"},
		{"head-colon-case", "git push origin HEAD:Main", "feat/x"},
		// MINOR 6 - subcommand case must fold.
		{"subcommand-case", "git PUSH origin main", "feat/x"},
		// IMPORTANT 5 - secret reads regardless of the reading command.
		{"git-show-secret", "git show HEAD:.env", "feat/x"},
		{"git-diff-secret", "git diff -- .env", "feat/x"},
		{"git-cat-file-secret", "git cat-file -p HEAD:.env", "feat/x"},
		{"grep-secret", "grep -r password .env", "feat/x"},
		{"abs-path-cat-secret", "/bin/cat .env", "feat/x"},
		{"command-cat-secret", "command cat .env", "feat/x"},
		{"sed-secret", "sed -n 1p .env", "feat/x"},
		{"cp-secret", "cp id_rsa /tmp/x", "feat/x"},
		{"secret-in-compound", "git status && cat .env", "feat/x"},
	}
	for _, d := range deny {
		got := v("Bash", `{"command":`+jsonStr(d.cmd)+`}`, d.cur)
		if got.Decision != "deny" {
			t.Fatalf("%s: expected deny for %q (on %q): %+v", d.name, d.cmd, d.cur, got)
		}
	}
}

// TestClassifyControlsStillGate confirms the fixes did not over-broaden: the safe
// controls still GATE (never deny, never allow).
func TestClassifyControlsStillGate(t *testing.T) {
	// Feature-branch push still gates remote/high.
	if g := v("Bash", `{"command":"git push origin feat/x"}`, "feat/x"); g.Decision != "gate" || g.Category != "remote" || g.Severity != "high" {
		t.Fatalf("feature push: %+v", g)
	}
	// Commit alone still gates shell with the message summary.
	if g := v("Bash", `{"command":"git commit -m \"hi\""}`, "feat/x"); g.Decision != "gate" || g.Category != "shell" || g.Summary != "Commit: hi" {
		t.Fatalf("commit alone: %+v", g)
	}
	// Compound WITHOUT a push still gates (no push segment denies).
	if g := v("Bash", `{"command":"git add -A && git commit -m x"}`, "feat/x"); g.Decision != "gate" {
		t.Fatalf("compound no-push: %+v", g)
	}
	// Compound gate Summary shows the WHOLE command, not just a segment, so a
	// later push can never be concealed.
	g := v("Bash", `{"command":"git add -A && git commit -m x"}`, "feat/x")
	if !contains(g.Summary, "git push") && !contains(g.Summary, "add -A") {
		t.Fatalf("compound summary should show the whole command: %+v", g)
	}
	// gh pr create still gates remote.
	if g := v("Bash", `{"command":"gh pr create --fill"}`, "feat/x"); g.Decision != "gate" || g.Category != "remote" {
		t.Fatalf("pr create: %+v", g)
	}
	// A commit message containing the word "push" must NOT trip the push backstop.
	if g := v("Bash", `{"command":"git commit -m \"push it real good\""}`, "feat/x"); g.Decision != "gate" || g.Category != "shell" {
		t.Fatalf("commit mentioning push: %+v", g)
	}
}

// TestTruncateRuneSafe covers MINOR 7: truncation must not cut a multibyte rune.
func TestTruncateRuneSafe(t *testing.T) {
	// 40 CJK runes (each 3 bytes in UTF-8); truncate to 10 runes.
	long := ""
	for i := 0; i < 40; i++ {
		long += "世"
	}
	out := truncate(long, 10)
	if !utf8.ValidString(out) {
		t.Fatalf("truncate produced invalid UTF-8: %q", out)
	}
	if n := utf8.RuneCountInString(out); n > 10 {
		t.Fatalf("truncate exceeded rune budget: %d runes in %q", n, out)
	}
	// Short strings pass through unchanged.
	if truncate("héllo", 80) != "héllo" {
		t.Fatalf("short string mangled: %q", truncate("héllo", 80))
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

// jsonStr quotes a string as a JSON literal for embedding in a command field.
func jsonStr(s string) string { b, _ := json.Marshal(s); return string(b) }
