package agent

import (
	"encoding/json"
	"regexp"
	"strings"
)

type Category string
type Severity string

const (
	CatEdit   Category = "edit"
	CatShell  Category = "shell"
	CatRemote Category = "remote"

	SevLow    Severity = "low"
	SevMedium Severity = "medium"
	SevHigh   Severity = "high"
)

// ClassifyContext carries the repo state the classifier needs. CurrentBranch is
// resolved live by the caller (the branch can change mid-run via checkout).
type ClassifyContext struct {
	CurrentBranch     string
	ProtectedBranches []string
}

// Verdict is the classifier's decision for one gated tool call.
type Verdict struct {
	Decision string // "allow" | "gate" | "deny"
	Reason   string
	Category Category
	Severity Severity
	Summary  string
}

// DefaultProtectedBranches are never pushed to by the agent.
func DefaultProtectedBranches() []string { return []string{"main", "master"} }

func deny(reason string) Verdict  { return Verdict{Decision: "deny", Reason: reason} }
func allow(reason string) Verdict { return Verdict{Decision: "allow", Reason: reason} }

// Classify decides how a gated tool call is handled. It returns "allow" ONLY
// for a safe read-scope Grep/Glob (no secret-shaped path/glob target); callers
// treat "allow" as "run without asking", "gate" as "ask the user", and "deny"
// as "block". Any parse failure or ambiguity is deny (fail-closed).
func Classify(toolName string, toolInput json.RawMessage, ctx ClassifyContext) Verdict {
	switch toolName {
	case "Edit":
		var p struct {
			FilePath string `json:"file_path"`
		}
		if json.Unmarshal(toolInput, &p) != nil {
			return deny("unreadable edit")
		}
		return Verdict{Decision: "gate", Category: CatEdit, Severity: SevLow, Summary: "Edit " + baseOr(p.FilePath, "a file")}
	case "Write":
		var p struct {
			FilePath string `json:"file_path"`
		}
		if json.Unmarshal(toolInput, &p) != nil {
			return deny("unreadable write")
		}
		return Verdict{Decision: "gate", Category: CatEdit, Severity: SevMedium, Summary: "Create " + baseOr(p.FilePath, "a file")}
	case "Bash":
		var p struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(toolInput, &p) != nil {
			return deny("unreadable command")
		}
		return classifyBash(p.Command, ctx)
	case "Grep":
		// pattern is a CONTENT regex, not a path - never checked against
		// secretPathRe (a legit `Grep(pattern="secret")` must run). Only path/glob
		// scope which files are searched, so only they can target a secret file.
		// pattern is decoded so a malformed pattern denies (fail-closed).
		var p struct {
			Pattern string `json:"pattern"`
			Path    string `json:"path"`
			Glob    string `json:"glob"`
		}
		if json.Unmarshal(toolInput, &p) != nil {
			return deny("unreadable grep")
		}
		if secretScopeRe.MatchString(p.Path) || secretScopeRe.MatchString(p.Glob) {
			return deny("grep of a secret path is blocked")
		}
		return allow("search")
	case "Glob":
		// pattern IS a path glob here; both it and an optional path scope can
		// target a secret file.
		var p struct {
			Pattern string `json:"pattern"`
			Path    string `json:"path"`
		}
		if json.Unmarshal(toolInput, &p) != nil {
			return deny("unreadable glob")
		}
		if secretScopeRe.MatchString(p.Pattern) || secretScopeRe.MatchString(p.Path) {
			return deny("glob of a secret path is blocked")
		}
		return allow("search")
	default:
		return deny("tool not permitted")
	}
}

// shellSepRe splits a command line on shell separators. Two-character operators
// are listed before their one-character prefixes (`&&` before `&`, `||` before
// `|`) so each is consumed whole, not as two empty separators. A single `&`
// backgrounds the command to its left, so `git commit -m x & git push …` runs
// BOTH; splitting on it exposes the trailing push to the per-segment backstop.
var shellSepRe = regexp.MustCompile(`&&|\|\||;|\||&|\n|\r`)

// splitSegments breaks a command line into the sub-commands the shell would run.
// Splitting is deliberately quote-unaware: it is a best-effort over-approximation
// that errs toward MORE segments. A quoted separator can spuriously split a token
// (e.g. a commit message) — at worst yielding an extra segment that gates, or a
// refspec with a stray quote that then gates rather than resolving to a protected
// branch. It never merges segments to drop a `push` token; any push riding a
// separator lands on its own segment where classifySegment's backstop inspects it.
// Returns a single-element slice when no separator is present.
func splitSegments(c string) []string {
	if !shellSepRe.MatchString(c) {
		return []string{c}
	}
	var segs []string
	for _, p := range shellSepRe.Split(c, -1) {
		if p = strings.TrimSpace(p); p != "" {
			segs = append(segs, p)
		}
	}
	if len(segs) == 0 {
		return []string{c}
	}
	return segs
}

func classifyBash(cmd string, ctx ClassifyContext) Verdict {
	c := strings.TrimSpace(cmd)
	if c == "" {
		return Verdict{Decision: "gate", Category: CatShell, Severity: SevLow, Summary: "Run a command"}
	}
	// A backslash-newline is a shell line continuation: the next line is part of
	// the SAME command. Collapse it to a space FIRST so a refspec continued onto
	// the next line (`git push \<newline>origin main`) is not orphaned onto its
	// own pseudo-segment by the newline separator.
	c = lineContinuationRe.ReplaceAllString(c, " ")
	// The secret-read guard is applied PER SEGMENT inside classifySegment (after
	// the git subcommand is known), not here on the whole command: a commit/push/
	// PR message is text, not a file read, and must never trip it. See
	// classifySegment for the guard itself.
	// Compound commands (`a && b`, `a & b`, `a; b`, `a | b`, newline, ...) are
	// classified per segment: deny if ANY segment denies, else a single gate whose
	// Summary shows the WHOLE command. Because splitting is quote-unaware the
	// per-segment summary is not the safety mechanism — each segment is run
	// independently through classifySegment's bare-`push`-token backstop, so a push
	// hidden behind a separator (or an odd binary form) is denied on its own
	// segment. splitSegments only splits when a separator is present, so
	// classifySegment does not recurse.
	segs := splitSegments(c)
	if len(segs) > 1 {
		for _, seg := range segs {
			if v := classifySegment(seg, ctx); v.Decision == "deny" {
				return v
			}
		}
		return Verdict{Decision: "gate", Category: CatShell, Severity: SevMedium, Summary: "Run: " + truncate(c, 80)}
	}
	return classifySegment(segs[0], ctx)
}

// classifySegment classifies a single command with no shell separators. The
// git subcommand (if any) is identified FIRST so commit/push/PR-create can be
// gated on their own terms; the secret-read guard only reaches commands that
// are none of those, so a commit/push/PR message that merely MENTIONS a
// secret keyword (e.g. `git commit -m "harden .env parsing"`) is never
// treated as a secret read. An actual secret read - `cat .env`, `grep
// password .env`, `git show HEAD:.env`, etc. - still denies via readsSecret.
func classifySegment(seg string, ctx ClassifyContext) Verdict {
	sub, args := gitSubcommand(seg)
	sub = strings.ToLower(sub) // subcommand match is case-insensitive
	// A cleanly-parsed git push is the ONLY safe push form; classifyPush decides
	// gate (feature branch) vs deny (protected / unresolvable). It never reads
	// the secret guard - a refspec is not a secret.
	if sub == "push" {
		return classifyPush(args, ctx)
	}
	// Fail-closed push backstop. Any segment that carries a bare `push` token
	// alongside a git binary but did NOT parse as a clean push above is denied.
	// This fires BEFORE the commit case (so a segment that also parses as
	// `commit` cannot conceal a trailing push) and is independent of whether
	// gitSubcommand detected git: it catches command-substitution and subshell
	// forms whose binary token is glued to punctuation (`$(git push …)`,
	// backtick-git, `(git push …)`) and an unknown value-taking global flag that
	// desyncs the token stream. A quoted commit message keeps its quotes, so
	// `-m "push the fix"` is not a bare `push` token and still gates as a commit.
	// `gitx`/`mygit` are other programs (no git binary token) and stay generic.
	if hasPushToken(seg) && hasGitBinaryToken(seg) {
		return deny("push present but not a clean safe push; blocked")
	}
	switch {
	case sub == "commit":
		// A commit MESSAGE is text the agent wrote, not a file read - it must
		// never be run through the secret-read guard, or a message that simply
		// mentions "secret"/".env"/"credentials" (e.g. describing a hardening
		// fix) would be wrongly denied.
		return Verdict{Decision: "gate", Category: CatShell, Severity: SevMedium, Summary: "Commit: " + commitMessage(seg)}
	case isPRCreate(seg):
		// Same reasoning as commit: a PR title/body is text, not a file read.
		return Verdict{Decision: "gate", Category: CatRemote, Severity: SevMedium, Summary: "Open a pull request"}
	case readsSecret(seg):
		// Everything else that touches a secret-shaped path is a real read:
		// cat/less/more/head/tail/xxd/base64/strings/od/nl/sed/awk/dd/cp/grep …
		// on a secret path, or git show/diff/cat-file targeting one.
		return deny("reading a secret file is blocked")
	default:
		return Verdict{Decision: "gate", Category: CatShell, Severity: SevMedium, Summary: "Run: " + truncate(seg, 80)}
	}
}

// hasPushToken reports whether any bare token equals `push`. Quoted tokens (e.g.
// a commit message "push it") keep their quotes and so do not match.
func hasPushToken(cmd string) bool {
	for _, t := range strings.Fields(cmd) {
		if strings.ToLower(t) == "push" {
			return true
		}
	}
	return false
}

// hasGitBinaryToken reports whether any token denotes the git binary, tolerating
// a `.exe`/`.EXE` suffix and leading shell-substitution/subshell punctuation that
// glues it to `$(`, a backtick, or `(`. It deliberately does NOT match `gitx` or
// `mygit`, which are other programs, so those stay generic gates.
func hasGitBinaryToken(cmd string) bool {
	for _, t := range strings.Fields(cmd) {
		if gitBinaryLoose(t) {
			return true
		}
	}
	return false
}

// classifyPush resolves the push destination(s) and denies any that hit a
// protected branch or that cannot be determined.
func classifyPush(args []string, ctx ClassifyContext) Verdict {
	// --all / --mirror push (or mirror) every local branch, including the
	// default branch, so they can never resolve to a safe bare-branch gate. Match
	// the attached `=` forms (`--all=`, `--mirror=origin`) by prefix too.
	for _, a := range args {
		la := strings.ToLower(a)
		if la == "--all" || la == "--mirror" ||
			strings.HasPrefix(la, "--all=") || strings.HasPrefix(la, "--mirror=") {
			return deny("push --all/--mirror can hit the default branch")
		}
	}
	protected := map[string]bool{}
	for _, b := range ctx.ProtectedBranches {
		protected[strings.ToLower(b)] = true
	}
	var refspecs []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue // flags (incl. --force)
		}
		refspecs = append(refspecs, a)
	}
	// refspecs[0] is the remote (if present); the rest are refs.
	var refs []string
	if len(refspecs) >= 2 {
		refs = refspecs[1:]
	}
	// Bare push (no refspec) targets the current branch.
	if len(refs) == 0 {
		if ctx.CurrentBranch == "" {
			return deny("cannot determine push target")
		}
		if protected[strings.ToLower(ctx.CurrentBranch)] {
			return deny("push to the default branch is blocked")
		}
		return Verdict{Decision: "gate", Category: CatRemote, Severity: SevHigh, Summary: "Push branch " + ctx.CurrentBranch + " to " + remoteOf(refspecs)}
	}
	for _, r := range refs {
		dest := pushDest(r)
		if dest == "" {
			return deny("cannot determine push target")
		}
		// `HEAD` and `@` are git shorthand for "the branch currently checked
		// out" - `git push origin HEAD` pushes to a remote branch named after
		// the CURRENT branch, not a branch literally named "HEAD". Resolve it
		// before the protected-branch check so `git push origin HEAD` while on
		// main/master denies like the bare `git push` does. Unresolvable ->
		// deny (fail-closed).
		if strings.EqualFold(dest, "HEAD") || dest == "@" {
			if ctx.CurrentBranch == "" {
				return deny("cannot determine push target")
			}
			dest = ctx.CurrentBranch
		}
		if protected[strings.ToLower(dest)] {
			return deny("push to the default branch is blocked")
		}
	}
	return Verdict{Decision: "gate", Category: CatRemote, Severity: SevHigh, Summary: "Push " + strings.Join(refs, ", ") + " to " + remoteOf(refspecs)}
}

// pushDest returns the destination branch name of a refspec: the right side of
// a colon, else the whole ref, with a leading '+' (force) stripped and any
// "HEAD:" / "refs/heads/" prefixes normalized to the branch name.
func pushDest(ref string) string {
	ref = strings.TrimPrefix(ref, "+")
	if i := strings.LastIndex(ref, ":"); i >= 0 {
		ref = ref[i+1:]
	}
	ref = strings.TrimPrefix(ref, "refs/heads/")
	return ref
}

func remoteOf(refspecs []string) string {
	if len(refspecs) >= 1 {
		return refspecs[0]
	}
	return "origin"
}

// consumesValue reports whether a git global flag takes a separate value token
// (the space form, e.g. `--git-dir /x`). The attached form (`--git-dir=/x`) is a
// single flag token and needs no special handling.
func consumesValue(flag string) bool {
	switch flag {
	case "-C", "-c", "--git-dir", "--work-tree", "--namespace", "--exec-path":
		return true
	}
	return false
}

// lineContinuationRe matches a backslash-newline shell line continuation
// (`\<LF>` or `\<CRLF>`), which joins the next line onto the current command.
var lineContinuationRe = regexp.MustCompile(`\\\r?\n`)

// gitBinary reports whether tok is the git executable. It first strips
// surrounding quotes and any path prefix down to the BASENAME (everything
// through the last `/` or `\`) so a path-qualified invocation
// (`/usr/bin/git`, `./git`, `C:\...\git.exe`, a quoted path) is recognized,
// then tolerates a `.exe`/`.EXE`/`.cmd`/`.CMD` suffix (`git.exe`, `git.cmd`).
// `gitx`/`mygit` are other programs and are NOT git.
func gitBinary(tok string) bool {
	tok = strings.Trim(tok, `"'`)
	if i := strings.LastIndexAny(tok, `/\`); i >= 0 {
		tok = tok[i+1:]
	}
	for _, ext := range []string{".exe", ".cmd"} {
		if len(tok) >= len(ext) && strings.EqualFold(tok[len(tok)-len(ext):], ext) {
			tok = tok[:len(tok)-len(ext)]
			break
		}
	}
	return tok == "git"
}

// gitBinaryLoose additionally tolerates leading shell-substitution/subshell
// punctuation that glues the binary to `$(`, a backtick, or `(` (`$(git`,
// backtick-git, `(git`). Used only by the push backstop, which denies outright
// rather than resolving a branch from a punctuation-mangled invocation.
func gitBinaryLoose(tok string) bool {
	return gitBinary(strings.TrimLeft(tok, "$(`"))
}

// gitSubcommand returns the git subcommand and its args if cmd is a git
// invocation, else ("", nil). Handles global flags including the value-taking
// ones (`git --git-dir /x push …`) so the value token is not mistaken for the
// subcommand.
func gitSubcommand(cmd string) (string, []string) {
	toks := strings.Fields(cmd)
	i := 0
	for i < len(toks) && !gitBinary(toks[i]) {
		i++ // tolerate a leading `env FOO=bar git …`
	}
	if i >= len(toks) {
		return "", nil
	}
	i++ // past "git"
	for i < len(toks) && strings.HasPrefix(toks[i], "-") {
		if consumesValue(toks[i]) {
			i++ // skip this flag's value token
		}
		i++
	}
	if i >= len(toks) {
		return "", nil
	}
	return toks[i], toks[i+1:]
}

func isPRCreate(cmd string) bool {
	return regexp.MustCompile(`\bgh\s+pr\s+create\b`).MatchString(cmd) ||
		regexp.MustCompile(`\bgit\s+request-pull\b`).MatchString(cmd)
}

// secretPathRe matches a token/substring naming a secret file or path.
var secretPathRe = regexp.MustCompile(`(?i)(\.env(\.\w+)?|id_rsa|id_ed25519|\.pem|\.key|\.p12|\.pfx|\.netrc|credentials|secret|\.ssh/)`)

// secretScopeRe matches a Grep/Glob path or glob that targets a secret-shaped
// file OR a secret directory. It mirrors the Read(**/...) deny globs in
// policy.go (so search cannot reach what Read cannot), and - unlike secretArgRe,
// which scans a whole Bash command for a file arg - it also matches a bare
// secret DIRECTORY (`.ssh`, `.aws`) that a Glob/Grep scope could enumerate even
// with no trailing file. Over-matching (a source path merely CONTAINING
// "secret"/"token") errs toward deny (fail-closed); the agent can still search
// with an unscoped pattern.
var secretScopeRe = regexp.MustCompile(`(?i)(\.env(\.\w+)?|id_rsa|id_ed25519|\.pem|\.key|\.p12|\.pfx|\.netrc|\.keystore|\.ovpn|credentials|secret|token|\.ssh(?:/|$)|\.aws(?:/|$))`)

// secretArgRe matches a secret path at an argument boundary (start-of-string, a
// path separator, whitespace, or a shell operator), so ANY reading command hits
// it — cat, /bin/cat, `command cat`, sed, awk, dd, cp, `grep -r … .env`, … —
// rather than a fixed command allowlist.
var secretArgRe = regexp.MustCompile(`(?i)(^|/|\s|&&|;|\|)[^\s/]*(\.env(\.\w+)?|id_rsa|id_ed25519|\.pem|\.key|\.p12|\.pfx|\.netrc|credentials|secret|\.ssh/)`)

// gitShowSecretRe matches git plumbing that can print a file's contents from a
// revision (`git show HEAD:.env`, `git cat-file -p HEAD:.env`, `git diff … .env`)
// where the secret path may be attached to a rev by ':' (not an arg boundary).
var gitShowSecretRe = regexp.MustCompile(`(?i)\bgit\s+(show|diff|cat-file)\b`)

func readsSecret(cmd string) bool {
	if secretArgRe.MatchString(cmd) {
		return true
	}
	if gitShowSecretRe.MatchString(cmd) && secretPathRe.MatchString(cmd) {
		return true
	}
	return false
}

var commitMsgRe = regexp.MustCompile(`-m\s+("([^"]*)"|'([^']*)'|(\S+))`)

func commitMessage(cmd string) string {
	m := commitMsgRe.FindStringSubmatch(cmd)
	if m == nil {
		return "(no message)"
	}
	for _, g := range m[2:] {
		if g != "" {
			return truncate(g, 80)
		}
	}
	return "(no message)"
}

func baseOr(path, fallback string) string {
	if path == "" {
		return fallback
	}
	path = strings.ReplaceAll(path, "\\", "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// truncate shortens s to at most n runes (rune-aware so a multibyte rune near
// the limit is never cut mid-encoding), appending "..." when it trims.
func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 3 {
		return string(r[:n])
	}
	return string(r[:n-3]) + "..."
}
