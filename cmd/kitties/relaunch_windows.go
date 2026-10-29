//go:build windows

package main

import (
	"os"
	"os/exec"
)

// relaunch starts a fresh copy and ends this one: Windows cannot exec in place. The copy waits a
// moment for the port to be free (KITTIES_RELAUNCH).
func relaunch() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Env = append(os.Environ(), "KITTIES_RELAUNCH=1")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
