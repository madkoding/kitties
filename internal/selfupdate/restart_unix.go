//go:build !windows

package selfupdate

import (
	"os"
	"os/exec"
	"syscall"
)

// restart replaces this process with the new binary, in place (the same process id, so a
// supervisor keeps following it). Sockets are closed on exec, so the new one binds them again.
func restart(exe string, args, env []string) error { return syscall.Exec(exe, args, env) }

// detach starts a program in a session of its own, so it outlives this process and its parent.
func detach(name string, args []string, env ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
