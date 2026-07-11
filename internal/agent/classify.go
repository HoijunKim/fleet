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

func classifyBash(cmd string, ctx ClassifyContext) Verdict {
	c := strings.TrimSpace(cmd)
	if c == "" {
		return Verdict{Decision: "gate", Category: CatShell, Severity: SevLow, Summary: "Run a command"}
	}
	// Secret-read guard (best-effort): cat/less/head/... on a secret path.
	if readsSecret(c) {
		return deny("reading a secret file is blocked")
	}
	sub, args := gitSubcommand(c)
	switch {
	case sub == "push":
		return classifyPush(args, ctx)
	case sub == "commit":
		return Verdict{Decision: "gate", Category: CatShell, Severity: SevMedium, Summary: "Commit: " + commitMessage(c)}
	case isPRCreate(c):
		return Verdict{Decision: "gate", Category: CatRemote, Severity: SevMedium, Summary: "Open a pull request"}
	default:
		return Verdict{Decision: "gate", Category: CatShell, Severity: SevMedium, Summary: "Run: " + truncate(c, 80)}
	}
}

// classifyPush resolves the push destination(s) and denies any that hit a
// protected branch or that cannot be determined.
func classifyPush(args []string, ctx ClassifyContext) Verdict {
	protected := map[string]bool{}
	for _, b := range ctx.ProtectedBranches {
		protected[b] = true
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
		if protected[ctx.CurrentBranch] {
			return deny("push to the default branch is blocked")
		}
		return Verdict{Decision: "gate", Category: CatRemote, Severity: SevHigh, Summary: "Push branch " + ctx.CurrentBranch + " to " + remoteOf(refspecs)}
	}
	for _, r := range refs {
		dest := pushDest(r)
		if dest == "" {
			return deny("cannot determine push target")
		}
		if protected[dest] {
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

// gitSubcommand returns the git subcommand and its args if cmd is a git
// invocation, else ("", nil). Handles `git -C dir push …`.
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
		// skip global flags; -C and -c take a value
		if toks[i] == "-C" || toks[i] == "-c" {
			i++
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

var secretPathRe = regexp.MustCompile(`(?i)(\.env(\.\w+)?|id_rsa|id_ed25519|\.pem|\.key|\.p12|\.pfx|\.netrc|credentials|secret|\.ssh/)`)
var readCmdRe = regexp.MustCompile(`^\s*(cat|less|more|head|tail|xxd|base64|strings|od|nl)\b`)

func readsSecret(cmd string) bool {
	return readCmdRe.MatchString(cmd) && secretPathRe.MatchString(cmd)
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

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
