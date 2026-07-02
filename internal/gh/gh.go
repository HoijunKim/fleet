// Package gh queries GitHub for a repo's CI / PR / issue status via the gh CLI.
package gh

import (
	"bytes"
	"os/exec"
	"strconv"
	"strings"

	"github.com/hoijun/fleet/internal/winhide"
)

// Runner runs a `gh` subcommand and returns stdout. The single seam through
// which this package touches gh; tests substitute a fake.
type Runner interface {
	Run(args ...string) (string, error)
}

// ExecRunner runs the real gh CLI, hiding the console window on Windows.
type ExecRunner struct{}

func (ExecRunner) Run(args ...string) (string, error) {
	cmd := exec.Command("gh", args...)
	winhide.Apply(cmd)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return out.String(), err
}

// OwnerRepo parses a GitHub remote URL into owner and repo. Only github.com
// remotes are accepted; non-GitHub hosts (gitlab, bitbucket, self-hosted, etc.)
// return ok=false so they are never queried against GitHub. Returns ok=false
// for empty/unparseable remotes.
func OwnerRepo(remote string) (owner, repo string, ok bool) {
	remote = strings.TrimSpace(remote)
	remote = strings.TrimSuffix(remote, ".git")
	var host string
	switch {
	case strings.HasPrefix(remote, "git@"):
		// git@host:owner/repo
		if i := strings.Index(remote, ":"); i >= 0 {
			host = remote[len("git@"):i]
			remote = remote[i+1:]
		} else {
			return "", "", false
		}
	case strings.HasPrefix(remote, "https://"), strings.HasPrefix(remote, "http://"):
		remote = remote[strings.Index(remote, "://")+3:]
		if i := strings.IndexByte(remote, '/'); i >= 0 {
			host = remote[:i]
			remote = remote[i+1:] // strip host
		} else {
			return "", "", false
		}
	case strings.HasPrefix(remote, "ssh://git@"):
		remote = strings.TrimPrefix(remote, "ssh://git@")
		if i := strings.IndexByte(remote, '/'); i >= 0 {
			host = remote[:i]
			remote = remote[i+1:]
		} else {
			return "", "", false
		}
	default:
		return "", "", false
	}
	// A host may carry an explicit port (github.com:22); strip it before compare.
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	if !strings.EqualFold(host, "github.com") {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(remote, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// Info is a repo's GitHub status.
type Info struct {
	CI        string
	PRs       int
	Issues    int
	Available bool
}

// Fetch queries gh for the latest CI conclusion/status and open PR/issue counts.
// A failure of the CI call (e.g. gh missing/unauthenticated) returns an error;
// PR/issue failures are tolerated (left 0).
func Fetch(r Runner, owner, repo string) (Info, error) {
	base := "repos/" + owner + "/" + repo
	ci, err := r.Run("api", base+"/actions/runs?per_page=1",
		"--jq", `.workflow_runs[0].conclusion // .workflow_runs[0].status // ""`)
	if err != nil {
		return Info{}, err
	}
	info := Info{CI: strings.TrimSpace(ci), Available: true}
	if out, err := r.Run("api", "-X", "GET", "search/issues",
		"-f", "q=repo:"+owner+"/"+repo+" type:pr state:open", "--jq", ".total_count"); err == nil {
		info.PRs = atoi(out)
	}
	if out, err := r.Run("api", "-X", "GET", "search/issues",
		"-f", "q=repo:"+owner+"/"+repo+" type:issue state:open", "--jq", ".total_count"); err == nil {
		info.Issues = atoi(out)
	}
	return info, nil
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
