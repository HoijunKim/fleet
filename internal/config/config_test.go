package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultHasSaneValues(t *testing.T) {
	d := Default()
	if d.ScanDepth < 1 {
		t.Errorf("ScanDepth=%d, want >=1", d.ScanDepth)
	}
	if d.Editor == "" || d.Terminal == "" {
		t.Errorf("editor/terminal must default to non-empty, got %q/%q", d.Editor, d.Terminal)
	}
	if !d.ShowNonGit {
		t.Error("ShowNonGit should default true")
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")

	want := Config{
		Roots:            []string{"C:/a", "D:/b"},
		ScanDepth:        3,
		Editor:           "code",
		Terminal:         "wt",
		AutoFetchMinutes: 5,
		ShowNonGit:       false,
	}
	if err := want.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := loadFrom(p)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if got.ScanDepth != 3 || got.Editor != "code" || got.AutoFetchMinutes != 5 || got.ShowNonGit != false {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if len(got.Roots) != 2 || got.Roots[0] != "C:/a" {
		t.Errorf("roots mismatch: %v", got.Roots)
	}
}

func TestLoadFromMissingWritesDefault(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")

	got, err := loadFrom(p)
	if err != nil {
		t.Fatalf("loadFrom missing: %v", err)
	}
	if got.Editor != Default().Editor {
		t.Errorf("expected defaults when file missing, got %+v", got)
	}
	// A default file should now exist and re-load identically.
	if _, err := loadFrom(p); err != nil {
		t.Fatalf("second loadFrom: %v", err)
	}
}

// config.toml holds the only copy of the AI and Notion keys - they are not in
// the keychain and not in an export - so a file fleet cannot decode must be
// preserved, not silently replaced by the defaults it falls back to.
func TestLoadFromCorruptQuarantinesTheFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	const original = "roots = [\"C:/a\"\nopenai_key = \"sk-secret\"\n" // unclosed array
	if err := os.WriteFile(p, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := loadFrom(p)
	if err == nil {
		t.Fatal("expected an error on a corrupt config")
	}
	if got.Editor != Default().Editor {
		t.Errorf("expected defaults on a corrupt config, got %+v", got)
	}

	entries, _ := os.ReadDir(dir)
	found := ""
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "config.toml.corrupt-") {
			found = filepath.Join(dir, e.Name())
		}
	}
	if found == "" {
		t.Fatal("corrupt config must be moved aside, not left to be overwritten")
	}
	data, _ := os.ReadFile(found)
	if string(data) != original {
		t.Errorf("quarantined bytes altered: %q", data)
	}
	// Saving the defaults back is now harmless: it cannot reach the real keys.
	if err := Default().Save(p); err != nil {
		t.Fatalf("Save after quarantine: %v", err)
	}
	if data, _ := os.ReadFile(found); string(data) != original {
		t.Error("a later Save must not touch the quarantined copy")
	}
}

// Save must be atomic: encoding straight into the live file would leave a
// truncated, unparseable config behind if the app died mid-write.
func TestSaveIsAtomic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := (Config{Editor: "code", ScanDepth: 1}).Save(p); err != nil {
		t.Fatal(err)
	}
	if err := (Config{Editor: "vim", ScanDepth: 2}).Save(p); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	if _, err := os.Stat(p + ".tmp"); !os.IsNotExist(err) {
		t.Error("Save must not leave its temp file behind")
	}
	got, err := loadFrom(p)
	if err != nil {
		t.Fatalf("loadFrom after atomic save: %v", err)
	}
	if got.Editor != "vim" {
		t.Errorf("second Save did not take effect: %+v", got)
	}
}

// A config we could not READ is a different failure from one we could not
// PARSE. Quarantine needs permission on the directory, not on the file, so it
// would happily rename a perfectly good config that a backup agent happened to
// be holding - leaving the user with working settings under a name the UI calls
// corrupt. Only a genuine parse failure may move the file.
func TestLoadFromUnreadableDoesNotQuarantine(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := os.Mkdir(p, 0o755); err != nil { // portable stand-in for an I/O failure
		t.Fatal(err)
	}

	if _, err := loadFrom(p); err == nil {
		t.Fatal("expected an error on an unreadable config")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".corrupt-") {
			t.Errorf("an unreadable config must be left alone, found %q", e.Name())
		}
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("the original path must still be there: %v", err)
	}
}
