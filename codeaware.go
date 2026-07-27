package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hoijun/fleet/internal/git"
)

// Bindings that let the AI deep-dive read a repo like a person would: read a
// file, grep, list files. Read-only and confined to the repo, so "code-aware"
// works for any language (not just the Go/npm symbols the badge extracts).

const maxFileBytes = 200 * 1024 // refuse to read a huge/binary file
const maxFileChars = 24000      // truncate what we hand the model

// within reports whether p is root itself or lives under it.
func within(root, p string) bool {
	return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
}

// ReadRepoFile returns the text of a file inside repoPath. rel is repo-relative.
// It only reads git-TRACKED files - a gitignored/untracked file (e.g. .env,
// build output, node_modules) is refused even though it sits inside the repo,
// so the AI never sees secrets that are deliberately kept out of source. Paths
// escaping the repo (via .. or an in-repo symlink) are refused too. Errors come
// back as "error: ..." strings so the tool loop can feed them to the model.
func (a *App) ReadRepoFile(repoPath, rel string) string {
	rp, err := filepath.Abs(repoPath)
	if err != nil {
		return "error: bad repo path"
	}
	fp, err := filepath.Abs(filepath.Join(rp, rel))
	if err != nil {
		return "error: bad path"
	}
	// Lexical containment: cheap first gate against obvious ../ traversal.
	if !within(rp, fp) {
		return "error: path is outside the repo"
	}
	// Tracked-only gate: reading is restricted to files git tracks, which keeps
	// the AI out of gitignored secrets and junk that happen to live in the tree.
	// Tolerate a nil runner (unit tests) - containment + symlink checks still hold.
	if a.runner != nil {
		if _, err := a.runner.Run(rp, "ls-files", "--error-unmatch", "--", rel); err != nil {
			return "error: not a tracked file (use list to see tracked files)"
		}
	}
	// Resolve symlinks and re-check containment: a tracked symlink could point
	// outside the repo, and os.ReadFile would silently follow it. Resolve the
	// root too so a symlinked repo path (temp dirs, /var -> /private/var) still
	// compares equal.
	realRoot, err := filepath.EvalSymlinks(rp)
	if err != nil {
		return "error: bad repo path"
	}
	real, err := filepath.EvalSymlinks(fp)
	if err != nil {
		return "error: " + err.Error()
	}
	if !within(realRoot, real) {
		return "error: path is outside the repo"
	}
	info, err := os.Stat(real)
	if err != nil {
		return "error: " + err.Error()
	}
	if info.IsDir() {
		return "error: that is a directory (use list)"
	}
	if info.Size() > maxFileBytes {
		return "error: file too large to read"
	}
	data, err := os.ReadFile(real)
	if err != nil {
		return "error: " + err.Error()
	}
	s := string(data)
	if len(s) > maxFileChars {
		s = s[:maxFileChars] + "\n...(truncated)"
	}
	return s
}

// RepoGrep runs git grep in the repo and returns "file:line: text" lines (capped).
func (a *App) RepoGrep(path, query string) string {
	if strings.TrimSpace(query) == "" {
		return "(empty query)"
	}
	// The agent supplies regex patterns, so its grep stays regex-capable.
	hits, err := git.Grep(a.runner, path, query, git.GrepOpts{Regex: true})
	if err != nil {
		return "error: " + err.Error()
	}
	if len(hits) == 0 {
		return "(no matches)"
	}
	var b strings.Builder
	for i, h := range hits {
		if i >= 60 {
			b.WriteString("...(more matches)\n")
			break
		}
		b.WriteString(h.File + ":" + strconv.Itoa(h.Line) + ": " + h.Text + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// RepoFiles lists tracked files under an optional subdirectory (capped).
func (a *App) RepoFiles(path, sub string) string {
	args := []string{"ls-files"}
	if strings.TrimSpace(sub) != "" {
		args = append(args, "--", sub)
	}
	out, err := a.runner.Run(path, args...)
	if err != nil {
		return "error: " + err.Error()
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "(no tracked files)"
	}
	lines := strings.Split(out, "\n")
	if len(lines) > 200 {
		lines = append(lines[:200], "...(more files)")
	}
	return strings.Join(lines, "\n")
}
