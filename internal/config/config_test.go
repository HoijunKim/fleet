package config

import (
	"path/filepath"
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
