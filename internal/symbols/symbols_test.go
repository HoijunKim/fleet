package symbols

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func contains(items []string, want string) bool {
	for _, it := range items {
		if it == want {
			return true
		}
	}
	return false
}

func TestExtract(t *testing.T) {
	dir := t.TempDir()

	write(t, dir, "main/main.go", "package main\n\nfunc Run() {}\n\nfunc unexported() {}\n")
	write(t, dir, "lib/lib.go", "package lib\n\nfunc Exported() {}\n\ntype Widget struct{}\n\nfunc (Widget) M() {}\n")
	write(t, dir, "lib/lib_test.go", "package lib\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n")
	write(t, dir, "vendor/x/v.go", "package x\n\nfunc Vendored() {}\n")
	write(t, dir, "package.json", `{"name":"pkg","scripts":{"build":"x","dev":"y"},"bin":{"pkg":"cli.js"}}`)

	got := Extract(dir)

	if !contains(got.GoMainPkgs, "main") {
		t.Errorf("GoMainPkgs=%v want to contain %q", got.GoMainPkgs, "main")
	}

	for _, want := range []string{"Run", "Exported", "Widget"} {
		if !contains(got.GoExported, want) {
			t.Errorf("GoExported=%v want to contain %q", got.GoExported, want)
		}
	}
	for _, notWant := range []string{"unexported", "M", "Vendored", "TestX"} {
		if contains(got.GoExported, notWant) {
			t.Errorf("GoExported=%v should NOT contain %q", got.GoExported, notWant)
		}
	}

	wantScripts := []string{"build", "dev"}
	if !reflect.DeepEqual(got.NpmScripts, wantScripts) {
		t.Errorf("NpmScripts=%v want %v", got.NpmScripts, wantScripts)
	}

	if !contains(got.NpmBin, "pkg") {
		t.Errorf("NpmBin=%v want to contain %q", got.NpmBin, "pkg")
	}

	if got.GoMainPkgs == nil {
		t.Error("GoMainPkgs is nil, want non-nil")
	}
	if got.GoExported == nil {
		t.Error("GoExported is nil, want non-nil")
	}
	if got.NpmScripts == nil {
		t.Error("NpmScripts is nil, want non-nil")
	}
	if got.NpmBin == nil {
		t.Error("NpmBin is nil, want non-nil")
	}

	if got.Truncated {
		t.Error("Truncated should be false for a small tree")
	}
}

func TestExtractTruncatesAt400Files(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "many")

	for i := 0; i < 401; i++ {
		write(t, sub, fmt.Sprintf("f%d.go", i), fmt.Sprintf("package p\n\nfunc F%d() {}\n", i))
	}

	got := Extract(dir)
	if !got.Truncated {
		t.Error("Truncated should be true when more than 400 .go files are parsed")
	}
}

func TestExtractEmptyDir(t *testing.T) {
	dir := t.TempDir()
	got := Extract(dir)

	if got.GoMainPkgs == nil || len(got.GoMainPkgs) != 0 {
		t.Errorf("GoMainPkgs=%v want empty non-nil slice", got.GoMainPkgs)
	}
	if got.GoExported == nil || len(got.GoExported) != 0 {
		t.Errorf("GoExported=%v want empty non-nil slice", got.GoExported)
	}
	if got.NpmScripts == nil || len(got.NpmScripts) != 0 {
		t.Errorf("NpmScripts=%v want empty non-nil slice", got.NpmScripts)
	}
	if got.NpmBin == nil || len(got.NpmBin) != 0 {
		t.Errorf("NpmBin=%v want empty non-nil slice", got.NpmBin)
	}
	if got.Truncated {
		t.Error("Truncated should be false for empty dir")
	}
}
