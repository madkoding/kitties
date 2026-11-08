//go:build linux || darwin || android || freebsd

package tidy

import "golang.org/x/sys/unix"

// freeBytes is what is left on the disk of dir for an unprivileged user, 0 when it cannot be told.
func freeBytes(dir string) int64 {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0
	}
	return int64(uint64(st.Bavail) * uint64(st.Bsize)) //nolint:gosec // a disk is far below 2^63 bytes
}
