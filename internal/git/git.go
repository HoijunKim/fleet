package git

import (
	"strings"

	"github.com/hoijun/fleet/internal/repo"
)

// Load fills rp's git fields using r. A failure of the core status command is
// fatal for this repo (sets rp.Err); failures of the optional remote/todo
// lookups are tolerated and leave their fields at zero. rp.Loaded is set true
// on success.
func Load(r Runner, rp *repo.Repo) {
	statusOut, err := r.Run(rp.Path, "status", "--porcelain=v2", "--branch")
	if err != nil {
		rp.Err = err
		return
	}
	st := parseStatus(statusOut)
	rp.Branch = st.Branch
	rp.Dirty = st.Dirty
	rp.ModifiedCount = st.Modified
	rp.Ahead = st.Ahead
	rp.Behind = st.Behind
	rp.HasUpstream = st.HasUpstream
	rp.DirtyFiles = st.Files

	if logOut, err := r.Run(rp.Path, "log", "-1", "--format=%H%x1f%an%x1f%cI%x1f%s"); err == nil {
		rp.Last = parseLastCommit(logOut)
	}
	if remoteOut, err := r.Run(rp.Path, "remote", "get-url", "origin"); err == nil {
		rp.RemoteURL = strings.TrimSpace(remoteOut)
	}
	if grepOut, err := r.Run(rp.Path, "grep", "-cE", "TODO|FIXME"); err == nil {
		rp.TodoCount = parseTodoCount(grepOut)
	}
	rp.Loaded = true
}

// Fetch runs `git fetch` in dir.
func Fetch(r Runner, dir string) error {
	_, err := r.Run(dir, "fetch")
	return err
}

// Pull runs `git pull --ff-only` in dir.
func Pull(r Runner, dir string) error {
	_, err := r.Run(dir, "pull", "--ff-only")
	return err
}
