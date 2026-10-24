//go:build windows

package selfupdate

import "errors"

func restart(string, []string, []string) error {
	return errors.New("restarting by itself is not supported on Windows")
}
