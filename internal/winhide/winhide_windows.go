//go:build windows

package winhide

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// Apply prevents the child process from opening/flashing a console window.
func Apply(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
