//go:build !windows

// Package winhide hides the console window a child process would otherwise
// flash when spawned from a GUI application. It is a no-op off Windows.
package winhide

import "os/exec"

// Apply is a no-op on non-Windows platforms.
func Apply(cmd *exec.Cmd) {}
