package agent

import (
	"encoding/json"
	"path"
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
		if scopeTargetsSecret(p.Path) || scopeTargetsSecret(p.Glob) {
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
		if scopeTargetsSecret(p.Pattern) || scopeTargetsSecret(p.Path) {
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

// secretScopeRe matches the LITERAL secret-shaped substrings in a Grep/Glob
// path or glob (a name like `.env`/`id_rsa`/`credentials` typed verbatim, or a
// secret directory `.ssh`/`.aws` with a `/` or `\` separator). It is the
// first-line literal check inside scopeTargetsSecret; the glob-aware check there
// handles WILDCARD forms (`*.k*`, `id_*`, `*.{key,pem}`) that resolve to a
// secret file without the literal token ever appearing. Over-matching (a source
// path merely CONTAINING "secret"/"token") errs toward deny (fail-closed); the
// agent can still search with an unscoped pattern.
var secretScopeRe = regexp.MustCompile(`(?i)(\.env(\.\w+)?|id_rsa|id_ed25519|\.pem|\.key|\.p12|\.pfx|\.netrc|\.keystore|\.ovpn|credentials|secret|token|\.ssh(?:[/\\]|$)|\.aws(?:[/\\]|$))`)

// secretExts are file extensions whose contents are secret (mirrors the
// Read(**/*.pem) family in policy.go). Matched STEM-INDEPENDENTLY: a glob like
// `production.ke*` or `privkey.p*` targets a key regardless of stem, so a
// fixed-canonical-filename check (which only caught `server.key`) is not enough
// - the extension pattern is matched on its own via path.Match.
var secretExts = []string{"pem", "key", "p12", "pfx", "keystore", "ovpn"}

// secretNames are representative secret file names (exact + standard variants).
// A filename glob (or, when it carries a literal stem, its extension-stripped
// stem) is path.Match-ed against these, so wildcard forms (`id_*`, `.e*.local`,
// `cred*`, `*sec*`) are caught even though the literal token never appears. Bare
// `secret`/`token`/`credentials` are tested only through hasLiteral-guarded
// segments/stems, so a pure-wildcard scope (`*`, `*.json` -> stem `*`) does not
// explode into an allow-everything deny.
var secretNames = []string{
	"id_rsa", "id_ed25519", "id_dsa", ".netrc", "credentials", "secret", "token",
	".env", ".env.local", ".env.production", ".env.development", ".env.test",
	".env.staging", ".env.prod", ".env.dev",
}

// secretDirNames are secret directories a Grep/Glob scope must not enumerate.
var secretDirNames = []string{".ssh", ".aws"}

// scopeTargetsSecret reports whether a Grep/Glob path or glob argument targets a
// secret-shaped file or directory. A glob is a PATTERN that can resolve to a
// secret without containing the token literally, so a substring scan alone is
// bypassable. It (1) runs the literal secretScopeRe scan, then (2) normalizes
// `\`->`/`, expands brace alternations (failing CLOSED if that truncates),
// splits into path segments, and - for any segment carrying a literal character
// - matches secret DIR names and, for the filename segment, a secret EXTENSION
// (stem-independent) or a secret NAME, all via path.Match. Pure-wildcard
// segments (`*`, `**`) are inherently unclassifiable enumeration and are not
// denied (a bare listing yields names, not contents; Read still gates the
// files). Malformed patterns are hits (fail-closed).
func scopeTargetsSecret(s string) bool {
	if s == "" {
		return false
	}
	if secretScopeRe.MatchString(s) {
		return true
	}
	exps, truncated := expandBraces(strings.ReplaceAll(s, `\`, "/"))
	if truncated {
		return true // an over-large brace glob is evasion-shaped; fail closed
	}
	for _, g := range exps {
		segs := strings.Split(g, "/")
		for i, seg := range segs {
			if seg == "" || seg == "**" || seg == "." || seg == ".." || !hasLiteral(seg) {
				continue
			}
			for _, dir := range secretDirNames {
				if matchGlob(seg, dir) {
					return true
				}
			}
			if i == len(segs)-1 && lastSegHitsSecret(seg) {
				return true
			}
		}
	}
	return false
}

// lastSegHitsSecret reports whether a filename glob targets a secret file by
// extension (stem-independent) or by name. The caller guarantees seg has a
// literal character.
func lastSegHitsSecret(seg string) bool {
	if dot := strings.LastIndexByte(seg, '.'); dot >= 0 {
		ext := seg[dot+1:]
		for _, se := range secretExts {
			if matchGlob(ext, se) {
				return true
			}
		}
	}
	stem := seg
	if dot := strings.LastIndexByte(seg, '.'); dot > 0 { // keep a leading-dot dotfile whole
		stem = seg[:dot]
	}
	stemLit := hasLiteral(stem)
	for _, name := range secretNames {
		if matchGlob(seg, name) || (stemLit && matchGlob(stem, name)) {
			return true
		}
	}
	return false
}

// hasLiteral reports whether s contains a non-wildcard character. A pure
// wildcard (`*`, `**`, `*?`) matches every candidate unconditionally, so it must
// not be tested against bare secret names (that would deny every `*.<ext>` glob)
// - such segments are treated as unclassifiable enumeration and allowed.
func hasLiteral(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool {
		return r != '*' && r != '?'
	}) >= 0
}

// matchGlob reports whether pattern matches name via path.Match; a malformed
// pattern returns true (fail-closed: an unparseable scope is treated as a hit).
func matchGlob(pattern, name string) bool {
	ok, err := path.Match(pattern, name)
	if err != nil {
		return true
	}
	return ok
}

// expandBraces expands shell brace alternations (`{a,b}`, nested) to a fixed
// point so path.Match - which has no brace support - can test each concrete
// alternative (`*.{key,pem}` -> `*.key`, `*.pem`). Returns (expansions,
// truncated); truncated is true if it exceeds the cap, and the caller treats
// truncation as a secret hit (fail-closed) so a brace-bomb that buries a secret
// alternative past the cap cannot fail open. Unbalanced `{` is dropped literally
// to make progress.
func expandBraces(s string) ([]string, bool) {
	out := []string{s}
	for {
		idx := -1
		for i, g := range out {
			if strings.IndexByte(g, '{') >= 0 {
				idx = i
				break
			}
		}
		if idx < 0 {
			return out, false
		}
		g := out[idx]
		open := strings.IndexByte(g, '{')
		closeIdx := matchingBrace(g, open)
		if closeIdx < 0 { // unbalanced: drop the '{' and continue
			out[idx] = g[:open] + g[open+1:]
			continue
		}
		pre, body, post := g[:open], g[open+1:closeIdx], g[closeIdx+1:]
		alts := splitTopComma(body)
		expanded := make([]string, 0, len(alts))
		for _, a := range alts {
			expanded = append(expanded, pre+a+post)
		}
		out = append(out[:idx:idx], append(expanded, out[idx+1:]...)...)
		if len(out) > 256 {
			return out, true
		}
	}
}

// matchingBrace returns the index of the '}' closing the '{' at open (respecting
// nesting), or -1 if unbalanced.
func matchingBrace(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// splitTopComma splits body on commas at brace-depth 0 so a nested group's
// commas stay with it.
func splitTopComma(body string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, body[start:i])
				start = i + 1
			}
		}
	}
	return append(out, body[start:])
}

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
