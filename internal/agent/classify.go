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
	Decision string // "gate" | "deny"
	Reason   string
	Category Category
	Severity Severity
	Summary  string
}

// DefaultProtectedBranches are never pushed to by the agent.
func DefaultProtectedBranches() []string { return []string{"main", "master"} }

func deny(reason string) Verdict { return Verdict{Decision: "deny", Reason: reason} }

// Classify decides how a gated tool call is handled. It NEVER returns allow;
// callers treat "gate" as "ask the user" and "deny" as "block". Any parse
// failure or ambiguity is deny (fail-closed).
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
	default:
		return deny("tool not permitted")
	}
}

// shellSepRe splits a command line on shell separators. The `||` alternative is
// listed before `|` so a logical-OR is consumed whole, not as two empty pipes.
var shellSepRe = regexp.MustCompile(`&&|\|\||;|\||\n|\r`)

// splitSegments breaks a command line into the sub-commands the shell would run.
// Splitting is deliberately quote-unaware: over-splitting can only produce more
// segments to inspect (fail-closed), never hide one. Returns a single-element
// slice when no separator is present.
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
	// Secret-read guard runs on the WHOLE command (unsplit), so a secret path in
	// any position is caught regardless of the reading command's name.
	if readsSecret(c) {
		return deny("reading a secret file is blocked")
	}
	// Compound commands (`a && b`, `a; b`, `a | b`, newline, ...) are classified
	// per segment; the verdict is deny if ANY segment denies, else a single gate
	// whose Summary shows the whole command (never a per-segment summary that
	// could conceal a later push). splitSegments only splits when a separator is
	// present, so classifySegment does not recurse.
	segs := splitSegments(c)
	if len(segs) > 1 {
		for _, seg := range segs {
			if v := classifySegment(seg, ctx); v.Decision == "deny" {
				return v
			}
		}
		return Verdict{Decision: "gate", Category: CatShell, Severity: SevMedium, Summary: "Run: " + truncate(c, 80)}
	}
	return classifySegment(c, ctx)
}

// classifySegment classifies a single command with no shell separators.
func classifySegment(seg string, ctx ClassifyContext) Verdict {
	sub, args := gitSubcommand(seg)
	sub = strings.ToLower(sub) // subcommand match is case-insensitive
	switch {
	case sub == "push":
		return classifyPush(args, ctx)
	case sub == "commit":
		return Verdict{Decision: "gate", Category: CatShell, Severity: SevMedium, Summary: "Commit: " + commitMessage(seg)}
	case sub != "" && hasPushToken(seg):
		// Fail-closed backstop: a git command carries a `push` token but the
		// parser did not classify it as a push (e.g. an unknown value-taking
		// global flag desynced the token stream). Never fall through to a
		// generic gate that could let the push run unseen.
		return deny("push present but not parsed as a push; blocked")
	case isPRCreate(seg):
		return Verdict{Decision: "gate", Category: CatRemote, Severity: SevMedium, Summary: "Open a pull request"}
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

// classifyPush resolves the push destination(s) and denies any that hit a
// protected branch or that cannot be determined.
func classifyPush(args []string, ctx ClassifyContext) Verdict {
	// --all / --mirror push (or mirror) every local branch, including the
	// default branch, so they can never resolve to a safe bare-branch gate.
	for _, a := range args {
		switch strings.ToLower(a) {
		case "--all", "--mirror":
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

// gitSubcommand returns the git subcommand and its args if cmd is a git
// invocation, else ("", nil). Handles global flags including the value-taking
// ones (`git --git-dir /x push …`) so the value token is not mistaken for the
// subcommand.
func gitSubcommand(cmd string) (string, []string) {
	toks := strings.Fields(cmd)
	i := 0
	for i < len(toks) && toks[i] != "git" {
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
