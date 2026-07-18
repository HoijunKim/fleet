// Package fileguard holds the one rule every on-disk store in fleet obeys: a
// file fleet could not parse is never overwritten. It is moved aside first, so
// the bytes the user cared about outlive fleet's failure to read them.
package fileguard

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Quarantine renames path out of the way and returns the new path, so the
// caller may safely write a fresh file at the original location. The suffix is
// a colon-free RFC3339 stamp, which is both sortable and a legal Windows
// filename. A missing file is not an error: there is nothing to preserve, and
// the caller's write is safe either way (the returned path is empty).
func Quarantine(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	stamp := strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339), ":", "")
	dest := path + ".corrupt-" + stamp
	// A second failure within the same second must not clobber the first
	// quarantined copy - that would be the very loss this package prevents.
	for i := 1; ; i++ {
		if _, err := os.Stat(dest); os.IsNotExist(err) {
			break
		}
		dest = fmt.Sprintf("%s.corrupt-%s-%d", path, stamp, i)
	}
	if err := os.Rename(path, dest); err != nil {
		return "", err
	}
	return dest, nil
}
