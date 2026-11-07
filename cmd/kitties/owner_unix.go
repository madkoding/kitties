//go:build !windows

package main

import (
	"os"
	"syscall"
)

// ownLike gives a file the owner of dir: root changing the service's files leaves them to the
// kitties user the service runs as.
func ownLike(dir, file string) {
	info, err := os.Stat(dir)
	if err != nil {
		return
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		_ = os.Chown(file, int(st.Uid), int(st.Gid))
	}
}
