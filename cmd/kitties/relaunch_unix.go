//go:build !windows

package main

import (
	"os"
	"syscall"
)

// relaunch replaces this process with a fresh copy of itself, same process id (so systemd does not notice).
func relaunch() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}
