//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps the gateway from opening a console window of its own.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
