package fileguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuarantineMovesTheBytesAside(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "projects.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	dest, err := Quarantine(p)
	if err != nil {
		t.Fatalf("Quarantine: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("original path must be free for the caller to write")
	}
	if data, _ := os.ReadFile(dest); string(data) != "{not json" {
		t.Errorf("quarantined bytes altered: %q", data)
	}
	if !strings.Contains(filepath.Base(dest), ".corrupt-") {
		t.Errorf("quarantined name should be recognizable, got %q", filepath.Base(dest))
	}
	if strings.Contains(filepath.Base(dest), ":") {
		t.Errorf("quarantined name must be a legal Windows filename, got %q", filepath.Base(dest))
	}
}

// Two failures in the same second must not have the second overwrite the copy
// the first one saved - that is exactly the loss this package exists to stop.
func TestQuarantineTwiceKeepsBothCopies(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "projects.json")

	for _, content := range []string{"first", "second"} {
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Quarantine(p); err != nil {
			t.Fatalf("Quarantine %s: %v", content, err)
		}
	}

	entries, _ := os.ReadDir(dir)
	seen := map[string]bool{}
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		seen[string(data)] = true
	}
	if !seen["first"] || !seen["second"] {
		t.Errorf("both quarantined copies must survive, found %v", seen)
	}
}

func TestQuarantineMissingFileIsNotAnError(t *testing.T) {
	dest, err := Quarantine(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("a missing file has nothing to preserve: %v", err)
	}
	if dest != "" {
		t.Errorf("expected no destination, got %q", dest)
	}
}
