// Package repo defines the shared domain types describing a single repository
// as fleet knows it. Every producer (scan, git, meta) fills in part of a Repo.
package repo

import (
	"fmt"
	"time"
)

// Commit is the minimal description of a git commit shown in the UI.
type Commit struct {
	Hash    string
	Message string
	Author  string
	When    time.Time
}

// Repo is one directory fleet tracks. Fields are grouped by which producer
// fills them; zero values are valid ("not loaded yet").
type Repo struct {
	// Identity (set by scan).
	Name  string
	Path  string
	IsGit bool

	// Git status (set by git.Load).
	Branch        string
	Dirty         bool
	ModifiedCount int
	Ahead         int
	Behind        int
	HasUpstream   bool
	RemoteURL     string
	DirtyFiles    []string
	Last          Commit
	TodoCount     int

	// Meta (set by meta.Detect).
	Language  string
	SizeBytes int64
	HasReadme bool

	// Load state.
	Loaded bool  // git+meta finished for this repo
	Err    error // non-nil if loading this repo failed
}

// Marker is the compact status cell shown in the table.
func (r Repo) Marker() string {
	switch {
	case r.Err != nil:
		return "!"
	case !r.IsGit:
		return "-"
	case r.Dirty:
		return fmt.Sprintf("*%d", r.ModifiedCount)
	default:
		return "ok"
	}
}
