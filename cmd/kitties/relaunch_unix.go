//go:build !windows

package main

import (
	"os"
	"syscall"
)

// execSelf is syscall.Exec; tests replace it.
var execSelf = syscall.Exec

// relaunch replaces this process with a fresh copy of itself, same process id (so systemd does not notice).
func relaunch() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return execSelf(exe, os.Args, os.Environ())
}
