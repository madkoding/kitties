//go:build windows

package selfupdate

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func restart(string, []string, []string) error {
	return errors.New("restarting by itself is not supported on Windows")
}

// detach starts a program apart from this one (no console, a process group of its own), so it
// outlives this process and its parent.
func detach(name string, args []string, env ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x00000200} // DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
