package intel

import (
	"crypto/sha1"
	"encoding/hex"
	"path/filepath"

	"github.com/hoijun/fleet/internal/git"
)

// FleetID is the identity of the fleet-wide chat: not a repo, a fixed key.
const FleetID = "__fleet__"

// ChatID derives a stable chat identity from a repo path, using the same
// convention as project doc-ids (syncengine.DocID), so a later sync tier can
// carry chats across devices without re-keying:
//
//   - the fleet-wide chat        -> "__fleet__"
//   - a repo with a git remote   -> "git:" + normalized remote (machine-stable)
//   - a repo with no remote       -> "local:" + short path hash (machine-local)
//
// The frontend passes a path (or FleetID) and never derives an identity itself.
func ChatID(runner git.Runner, path string) string {
	if path == FleetID {
		return FleetID
	}
	if remote, err := git.RemoteURL(runner, path); err == nil && remote != "" {
		return "git:" + git.NormalizeRemote(remote)
	}
	return "local:" + shortHash(filepath.Base(path))
}

func shortHash(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}
