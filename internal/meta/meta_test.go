package meta

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetectGoWithReadme(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module x\n")
	write(t, dir, "README.md", "hi")

	lang, size, readme := Detect(dir)
	if lang != "Go" {
		t.Errorf("lang=%q want Go", lang)
	}
	if size <= 0 {
		t.Errorf("size=%d want >0", size)
	}
	if !readme {
		t.Error("expected README detected")
	}
}

func TestDetectTypeScriptOverJavaScript(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", "{}")
	write(t, dir, "tsconfig.json", "{}")

	lang, _, readme := Detect(dir)
	if lang != "TypeScript" {
		t.Errorf("lang=%q want TypeScript", lang)
	}
	if readme {
		t.Error("no README expected")
	}
}

func TestDetectUnknown(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "notes.txt", "hi")
	lang, _, _ := Detect(dir)
	if lang != "" {
		t.Errorf("lang=%q want empty", lang)
	}
}
