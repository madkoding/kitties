//go:build windows

package container

import "golang.org/x/sys/windows"

func init() {
	hostUILanguages = func() []string {
		l, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
		if err != nil {
			return nil
		}
		return l
	}
}
