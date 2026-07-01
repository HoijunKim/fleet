package main

import (
	"os"
	"path/filepath"
	"strings"
)

func baseName(path string) string { return filepath.Base(path) }

func isGitDir(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && info.IsDir()
}

// remoteToHTTPS converts a git remote URL (git@host:owner/repo.git or
// https://host/owner/repo.git or ssh://...) into a browsable https URL. Returns
// "" if it cannot.
func remoteToHTTPS(remote string) string {
	remote = strings.TrimSpace(remote)
	remote = strings.TrimSuffix(remote, ".git")
	switch {
	case strings.HasPrefix(remote, "https://"):
		return remote
	case strings.HasPrefix(remote, "http://"):
		return remote
	case strings.HasPrefix(remote, "git@"):
		// git@github.com:owner/repo -> https://github.com/owner/repo
		rest := strings.TrimPrefix(remote, "git@")
		rest = strings.Replace(rest, ":", "/", 1)
		return "https://" + rest
	case strings.HasPrefix(remote, "ssh://git@"):
		rest := strings.TrimPrefix(remote, "ssh://git@")
		return "https://" + rest
	default:
		return ""
	}
}
