//go:build !windows

package selfupdate

import "syscall"

// restart replaces this process with the new binary, in place (the same process id, so a
// supervisor keeps following it). Sockets are closed on exec, so the new one binds them again.
func restart(exe string, args, env []string) error { return syscall.Exec(exe, args, env) }
