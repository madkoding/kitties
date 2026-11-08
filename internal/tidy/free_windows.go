//go:build windows

package tidy

import "golang.org/x/sys/windows"

// freeBytes is what is left on the disk of dir for this user, 0 when it cannot be told.
func freeBytes(dir string) int64 {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0
	}
	var free, total, all uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, &total, &all); err != nil {
		return 0
	}
	return int64(free) //nolint:gosec // a disk is far below 2^63 bytes
}
