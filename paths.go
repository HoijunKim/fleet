package main

import (
	"os"
	"path/filepath"
)

func baseName(path string) string { return filepath.Base(path) }

func isGitDir(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && info.IsDir()
}
