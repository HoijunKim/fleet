package git

import "strings"

// NormalizeRemote reduces a git remote URL to a stable, machine-independent
// identity: scheme and credentials removed, host+path lowercased, trailing
// "/" and ".git" stripped (in both orders, so "repo/", "repo.git",
// "repo.git/", and "repo/.git" all converge). It is the basis of a code
// project's sync doc_id.
//
// The whole path is lowercased so the same GitHub repo converges regardless
// of owner/repo casing (GitHub is case-insensitive). Known limitation: on
// case-SENSITIVE hosts (self-hosted GitLab/Gitea/Bitbucket Server), two repos
// that differ only by path case would collide to one doc_id; acceptable for
// the GitHub-centric v0.
func NormalizeRemote(remote string) string {
	remote = strings.TrimSpace(remote)
	remote = strings.TrimSuffix(remote, "/")
	remote = strings.TrimSuffix(remote, ".git")
	remote = strings.TrimSuffix(remote, "/")
	switch {
	case strings.HasPrefix(remote, "git@"):
		rest := strings.TrimPrefix(remote, "git@")
		rest = strings.Replace(rest, ":", "/", 1) // host:owner/repo -> host/owner/repo
		remote = rest
	case strings.HasPrefix(remote, "ssh://"):
		rest := strings.TrimPrefix(remote, "ssh://")
		if i := strings.Index(rest, "@"); i >= 0 {
			rest = rest[i+1:] // strip user@ credentials
		}
		remote = rest
	case strings.HasPrefix(remote, "https://"), strings.HasPrefix(remote, "http://"):
		rest := remote[strings.Index(remote, "://")+3:]
		if i := strings.Index(rest, "@"); i >= 0 {
			rest = rest[i+1:] // strip user:pass@ credentials
		}
		remote = rest
	}
	return strings.ToLower(remote)
}

// RemoteURL returns the raw origin remote URL for a repo, or an error when the
// repo has no origin (or is not a git repo). Uses the standard Runner seam.
func RemoteURL(r Runner, dir string) (string, error) {
	out, err := r.Run(dir, "remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
