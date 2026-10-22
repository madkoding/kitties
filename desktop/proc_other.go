//go:build !windows

package main

import "os/exec"

// hideWindow does nothing outside Windows.
func hideWindow(*exec.Cmd) {}
