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

func TestClassifyGrepGlob(t *testing.T) {
	// Secret-path targets deny; content patterns and normal scopes allow.
	cases := []struct {
		name, tool, input, want string
	}{
		{"grep secret path", "Grep", `{"pattern":"x","path":"repo/.env"}`, "deny"},
		{"grep secret glob", "Grep", `{"pattern":"x","glob":"**/*.key"}`, "deny"},
		{"grep secret path id_rsa", "Grep", `{"pattern":"x","path":".ssh/id_rsa"}`, "deny"},
		{"grep content pattern secret", "Grep", `{"pattern":"secret"}`, "allow"},
		{"grep content pattern password", "Grep", `{"pattern":"password"}`, "allow"},
		{"grep normal path", "Grep", `{"pattern":"TODO","path":"internal/agent"}`, "allow"},
		{"grep normal glob", "Grep", `{"pattern":"func","glob":"**/*.go"}`, "allow"},
		{"grep unparseable", "Grep", `{"pattern":123}`, "deny"},
		{"glob secret pattern", "Glob", `{"pattern":"**/id_rsa"}`, "deny"},
		{"glob secret pattern key", "Glob", `{"pattern":"**/*.pem"}`, "deny"},
		{"glob secret path bare .ssh", "Glob", `{"pattern":"**/*.go","path":".ssh"}`, "deny"},
		{"glob secret path .aws dir", "Glob", `{"pattern":"*","path":"home/.aws"}`, "deny"},
		{"grep token glob", "Grep", `{"pattern":"x","glob":"**/*token*"}`, "deny"},
		{"glob normal", "Glob", `{"pattern":"**/*.go"}`, "allow"},
		{"glob normal src path", "Glob", `{"pattern":"**/*.go","path":"internal/agent"}`, "allow"},
		{"glob unparseable", "Glob", `{"pattern":123}`, "deny"},
	}
	for _, c := range cases {
		got := v(c.tool, c.input, "feat/x")
		if got.Decision != c.want {
			t.Fatalf("%s: got %q want %q (%+v)", c.name, got.Decision, c.want, got)
		}
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
	// An empty Bash command is a harmless no-op: it must gate (not deny) and
	// must specifically NOT be classified as a push/remote action.
	empty := v("Bash", `{}`, "feat/x")
	if empty.Decision != "gate" {
		t.Fatalf("empty command should gate as a harmless no-op: %+v", empty)
	}
	if empty.Category == CatRemote {
		t.Fatalf("empty command must not classify as a push: %+v", empty)
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
		// PASS-2 CRITICAL 1 - a single `&` backgrounds the command and conceals a
		// trailing push; splitting on `&` exposes it to the per-segment backstop.
		{"single-amp-commit-push", "git commit -m x & git push origin main", "feat/x"},
		{"single-amp-status-push", "git status & git push origin main", "feat/x"},
		// PASS-2 CRITICAL 2 - the Windows native binary `git.exe` must normalize.
		{"git-exe-push", "git.exe push origin main", "feat/x"},
		{"git-EXE-push", "git.EXE push origin main", "feat/x"},
		// PASS-2 IMPORTANT 3 - command-substitution / subshell glue the binary to
		// punctuation so gitSubcommand cannot see it; the backstop still denies.
		{"cmd-subst-push", "$(git push origin main)", "feat/x"},
		{"backtick-push", "`git push origin main`", "feat/x"},
		{"subshell-push", "(git push origin main)", "feat/x"},
		// PASS-2 IMPORTANT 4 - a line continuation splits one push across lines.
		{"line-continuation-push", "git push \\\norigin main", "feat/x"},
		// PASS-2 MINOR 6 - the attached `=` forms of --all / --mirror.
		{"push-all-eq", "git push --all=", "feat/x"},
		{"push-mirror-eq-origin", "git push --mirror=origin", "feat/x"},
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
	// PASS-2: a quoted commit message "push the fix" keeps its quotes, so the bare
	// `push` backstop must not match it - it still gates as a shell commit.
	if g := v("Bash", `{"command":"git commit -m \"push the fix\""}`, "feat/x"); g.Decision != "gate" || g.Category != "shell" {
		t.Fatalf("commit 'push the fix': %+v", g)
	}
	// PASS-2: an unrelated binary named `gitx` (not git) with a push arg stays a
	// generic shell gate - the backstop only fires when a git binary is present.
	if g := v("Bash", `{"command":"gitx push origin main"}`, "feat/x"); g.Decision != "gate" {
		t.Fatalf("gitx push: %+v", g)
	}
	// PASS-2: a plain echo gates (no push token, no secret).
	if g := v("Bash", `{"command":"echo hello"}`, "feat/x"); g.Decision != "gate" {
		t.Fatalf("echo hello: %+v", g)
	}
}

// --- Pass-3 bypass-fix regression tests -------------------------------------

// TestClassifyHeadAtPushResolvesToCurrentBranch covers CRITICAL 1: `HEAD`/`@`
// as a push destination refer to the CURRENT branch, not a literal branch
// named "HEAD"/"@". When the current branch is protected, these must deny
// exactly like a bare `git push` on that branch already does.
func TestClassifyHeadAtPushResolvesToCurrentBranch(t *testing.T) {
	deny := []struct{ name, cmd, cur string }{
		{"head-on-main", "git push origin HEAD", "main"},
		{"at-on-master", "git push origin @", "master"},
		{"head-tracking-on-main", "git push -u origin HEAD", "main"},
	}
	for _, d := range deny {
		got := v("Bash", `{"command":`+jsonStr(d.cmd)+`}`, d.cur)
		if got.Decision != "deny" {
			t.Fatalf("%s: expected deny for %q (on %q): %+v", d.name, d.cmd, d.cur, got)
		}
	}
	// Control: HEAD push while on a feature branch is a normal remote push and
	// still gates (it does NOT deny - resolving HEAD must not over-broaden).
	g := v("Bash", `{"command":"git push origin HEAD"}`, "feat/x")
	if g.Decision != "gate" || g.Category != "remote" {
		t.Fatalf("HEAD push on feature branch should gate remote: %+v", g)
	}
}

// TestClassifyPathQualifiedGitBinary covers IMPORTANT 2: a path-qualified or
// platform-shim git binary token must still be recognized as git so an
// unconditional push to the default branch denies instead of falling through
// to a generic gate.
func TestClassifyPathQualifiedGitBinary(t *testing.T) {
	deny := []struct{ name, cmd string }{
		{"abs-path-git", "/usr/bin/git push origin main"},
		{"relative-git", "./git push origin main"},
		{"git-cmd-shim", "git.cmd push origin main"},
	}
	for _, d := range deny {
		got := v("Bash", `{"command":`+jsonStr(d.cmd)+`}`, "feat/x")
		if got.Decision != "deny" {
			t.Fatalf("%s: expected deny for %q: %+v", d.name, d.cmd, got)
		}
	}
	// Controls: an unrelated binary is still not git, and a feature-branch
	// push is still a normal gate - the fix must not over-broaden.
	if g := v("Bash", `{"command":"gitx push origin main"}`, "feat/x"); g.Decision != "gate" {
		t.Fatalf("gitx push should stay a generic gate: %+v", g)
	}
	if g := v("Bash", `{"command":"git push origin feat/x"}`, "feat/x"); g.Decision != "gate" || g.Category != "remote" {
		t.Fatalf("feature-branch push should gate remote: %+v", g)
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

// --- Final-review fix: secret-read guard must not read commit/PR MESSAGES ---

// TestClassifyCommitMessageMentioningSecretGates covers the over-denial found
// in final review: classifyBash used to run readsSecret on the WHOLE command
// before classifying the git subcommand, so a commit message that merely
// MENTIONS a secret keyword ("secrets", ".env", "credentials", ...) was
// wrongly auto-denied with "reading a secret file is blocked" - silently
// breaking commit -> push -> PR. A commit message is text the agent wrote,
// not a file read, and must gate like any other commit.
func TestClassifyCommitMessageMentioningSecretGates(t *testing.T) {
	gate := []struct{ name, cmd string }{
		{"mentions-secrets", `git commit -m "mask secrets before sending diffs to AI"`},
		{"mentions-dotenv", `git commit -m "harden .env parsing"`},
	}
	for _, g := range gate {
		got := v("Bash", `{"command":`+jsonStr(g.cmd)+`}`, "feat/x")
		if got.Decision != "gate" {
			t.Fatalf("%s: expected gate (not deny) for %q: %+v", g.name, g.cmd, got)
		}
		if got.Category != CatShell {
			t.Fatalf("%s: expected shell category for %q: %+v", g.name, g.cmd, got)
		}
	}
}

// TestClassifySecretReadStillDeniesAfterCommitMessageFix is the control set:
// the commit/PR-message carve-out must NOT weaken any actual secret read, a
// default-branch push, or a compound where a later segment is a real read.
func TestClassifySecretReadStillDeniesAfterCommitMessageFix(t *testing.T) {
	deny := []struct{ name, cmd string }{
		{"cat-dotenv", "cat .env"},
		{"git-show-dotenv", "git show HEAD:.env"},
		{"grep-password-dotenv", "grep -r password .env"},
		{"compound-commit-then-cat-secret", `git commit -m x && cat .env`},
		{"push-default-branch", "git push origin main"},
	}
	for _, d := range deny {
		got := v("Bash", `{"command":`+jsonStr(d.cmd)+`}`, "feat/x")
		if got.Decision != "deny" {
			t.Fatalf("%s: expected deny for %q: %+v", d.name, d.cmd, got)
		}
	}
	// Feature-branch push and remote gates must still be unaffected.
	if g := v("Bash", `{"command":"git push origin feat/x"}`, "feat/x"); g.Decision != "gate" || g.Category != CatRemote {
		t.Fatalf("feature push: %+v", g)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

// jsonStr quotes a string as a JSON literal for embedding in a command field.
func jsonStr(s string) string { b, _ := json.Marshal(s); return string(b) }
