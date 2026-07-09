// Package syncengine runs fleet's offline-first PM sync: it derives stable
// document ids, tracks dirty documents against a local sync.json, and applies
// last-write-wins on pull. It is Wails-free and depends only on internal/store,
// internal/cloud, and internal/git.
package syncengine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/hoijun/fleet/internal/git"
	"github.com/hoijun/fleet/internal/store"
)

// State is the persisted sync bookkeeping (sync.json), keyed by doc_id.
type State struct {
	Cursor int64               `json:"cursor"`
	Docs   map[string]DocState `json:"docs"`
}

// DocState records what the engine last synced for one doc_id.
type DocState struct {
	LocalID   string `json:"localId"`
	Hash      string `json:"hash"`
	UpdatedAt string `json:"updatedAt"`
	Deleted   bool   `json:"deleted"`
}

// loadState reads sync.json; a missing file yields an empty (usable) State.
func loadState(path string) (State, error) {
	s := State{Docs: map[string]DocState{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return State{Docs: map[string]DocState{}}, err
	}
	if s.Docs == nil {
		s.Docs = map[string]DocState{}
	}
	return s, nil
}

// saveState writes sync.json atomically (temp file + rename).
func saveState(path string, s State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DocID derives the sync doc_id for a local record:
//   - manual project -> its opaque local id (already portable)
//   - code project with a remote -> "git:" + NormalizeRemote(remote)
//   - code project with no remote -> "local:" + shortHash(base(localID))
func DocID(localID string, rec store.Record, remote string) string {
	if rec.Manual {
		return localID
	}
	if remote != "" {
		return "git:" + git.NormalizeRemote(remote)
	}
	return "local:" + shortHash(filepath.Base(localID))
}

// payloadHash is the dirty-detection fingerprint of a doc payload.
func payloadHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// shortHash is a 12-hex-char digest, used for no-remote doc ids.
func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}

// newer reports whether RFC3339Nano time a is strictly after b. An empty b is
// treated as "older than anything"; an empty/unparsable a is not newer.
func newer(a, b string) bool {
	if b == "" {
		return a != ""
	}
	ta, ea := time.Parse(time.RFC3339Nano, a)
	if ea != nil {
		return false
	}
	tb, eb := time.Parse(time.RFC3339Nano, b)
	if eb != nil {
		return true
	}
	return ta.After(tb)
}

// NextBackoff returns the next capped exponential backoff delay: base when cur
// is below base, otherwise min(cur*2, max).
func NextBackoff(cur, base, max time.Duration) time.Duration {
	if cur < base {
		return base
	}
	n := cur * 2
	if n > max {
		return max
	}
	return n
}
