//go:build windows

package main

// ownLike does nothing on Windows, which has no service of scripts/install.sh.
func ownLike(dir, file string) {}
