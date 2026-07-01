// Package meta derives non-git metadata about a repository directory.
package meta

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// marker maps a filename that uniquely identifies a project's language.
// Order matters only via the special TypeScript check below.
var markers = []struct {
	file string
	lang string
}{
	{"go.mod", "Go"},
	{"Cargo.toml", "Rust"},
	{"pyproject.toml", "Python"},
	{"requirements.txt", "Python"},
	{"setup.py", "Python"},
	{"pom.xml", "Java"},
	{"build.gradle", "Java"},
	{"Gemfile", "Ruby"},
	{"composer.json", "PHP"},
	{"package.json", "JavaScript"},
}

// skipDirs are not descended into when summing size (they are large and derived).
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true,
	"target": true, "dist": true, "build": true,
}

// Detect returns the primary language (or ""), the directory size in bytes
// (excluding derived/heavy directories), and whether a README exists.
func Detect(path string) (string, int64, bool) {
	lang := detectLang(path)
	size := dirSize(path)
	readme := hasReadme(path)
	return lang, size, readme
}

func detectLang(path string) string {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(path, name))
		return err == nil
	}
	for _, m := range markers {
		if exists(m.file) {
			if m.lang == "JavaScript" && exists("tsconfig.json") {
				return "TypeScript"
			}
			return m.lang
		}
	}
	return ""
}

func hasReadme(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(strings.ToLower(e.Name()), "readme") {
			return true
		}
	}
	return false
}

func dirSize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != path && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
