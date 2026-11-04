package container

import (
	_ "embed"
	"os"
	"path/filepath"
)

// seccompProfile is Docker's default seccomp profile plus one rule: clone and unshare are allowed
// without CAP_SYS_ADMIN, so Chromium can build its sandbox from user namespaces (what the
// browser needs, instead of --no-sandbox). Everything else the default profile blocks stays
// blocked, and the container is neither privileged nor given any capability. Regenerate it with
// scripts/update-seccomp.sh.
//
//go:embed seccomp-kitty.json
var seccompProfile []byte

// writeSeccomp puts the profile in the user's cache dir (the runtime's client reads it from
// disk) and returns its path.
func writeSeccomp() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "kitties")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	p := filepath.Join(dir, "seccomp-kitty.json")
	if err := os.WriteFile(p, seccompProfile, 0o600); err != nil {
		return "", err
	}
	return p, nil
}
