//go:build !linux && !darwin && !android && !freebsd && !windows

package tidy

func freeBytes(string) int64 { return 0 }
