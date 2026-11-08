package selfupdate

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// THE KITTIES APP UPDATING ITSELF.
//
// The desktop app carries its own gateway (kitties-gateway, next to the app or inside
// Kitties.app). That gateway updates the whole app, not only itself, from the release's download
// for this system, checked against the same SHA256SUMS (and signature):
//
//   - Linux, kitties-desktop-linux-<arch>.zip: the app and the gateway are put in place of the
//     running ones (kept as <name>.prev).
//   - macOS, kitties-desktop-macos-<arch>.dmg: the disk image is opened and its Kitties.app put in
//     place of the running bundle (kept, hidden, as .Kitties.app.prev beside it).
//   - Windows, kitties-desktop-windows-<arch>-setup.exe: the installer runs silently; it stops the
//     app and the gateway, installs, and opens the app again (/RELAUNCH).
//
// Then the app is restarted (closed and opened again on the new version, which starts the new
// gateway): a detached helper does it, so it works from the app's window and from a browser alike.

// AppInstaller is the name the Windows installer is saved under before it runs.
const AppInstaller = "kitties-update-setup.exe"

// inApp says whether this is the gateway of the Kitties desktop app.
func (u *Updater) inApp() bool {
	return strings.HasPrefix(filepath.Base(u.Exe), "kitties-gateway") || strings.Contains(filepath.ToSlash(u.Exe), ".app/Contents/")
}

// appAsset is the release download of the desktop app for this system.
func (u *Updater) appAsset() string {
	switch u.OS {
	case "darwin":
		return "kitties-desktop-macos-" + u.Arch + ".dmg"
	case "windows":
		return "kitties-desktop-windows-" + u.Arch + "-setup.exe"
	default:
		return "kitties-desktop-" + u.OS + "-" + u.Arch + ".zip"
	}
}

// bundle is the macOS app bundle the gateway runs from ("" when it is not in one).
func (u *Updater) bundle() string {
	p := filepath.ToSlash(u.Exe)
	i := strings.Index(p, ".app/Contents/")
	if i < 0 {
		return ""
	}
	return filepath.FromSlash(p[:i+len(".app")])
}

// canApplyApp says whether the app this gateway belongs to can be updated from here.
func (u *Updater) canApplyApp() (bool, string) {
	dir := filepath.Dir(u.Exe)
	switch u.OS {
	case "windows":
		return true, ""
	case "darwin":
		b := u.bundle()
		if b == "" {
			return false, "this gateway is not inside Kitties.app: update the app by hand"
		}
		dir = filepath.Dir(b)
	}
	if !writable(dir) {
		return false, "the folder of the Kitties app cannot be written to: download the new version and install it by hand"
	}
	return true, ""
}

func writable(dir string) bool {
	probe, err := os.CreateTemp(dir, ".kitties-update-*")
	if err != nil {
		return false
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return true
}

// installApp puts the downloaded app in place of the running one.
func (u *Updater) installApp(ctx context.Context, data []byte, tag string) error {
	switch u.OS {
	case "windows":
		// The installer cannot run while the app holds its files: it is kept, and run when the app
		// is restarted.
		path := filepath.Join(os.TempDir(), AppInstaller)
		if err := os.WriteFile(path, data, 0o755); err != nil {
			return fmt.Errorf("saving the installer: %w", err)
		}
		u.installer = path
		return nil
	case "darwin":
		return u.installBundle(ctx, data, tag)
	default:
		return u.installZip(ctx, data, tag)
	}
}

// installZip puts the app and the gateway of the zip in place of the running ones.
func (u *Updater) installZip(ctx context.Context, data []byte, tag string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("the download is not a zip: %w", err)
	}
	dir := filepath.Dir(u.Exe)
	staged := map[string]string{}
	defer func() {
		for _, tmp := range staged {
			_ = os.Remove(tmp)
		}
	}()
	for _, f := range zr.File {
		name := filepath.Base(f.Name)
		if f.FileInfo().IsDir() || name != f.Name || (name != "kitties-desktop" && name != "kitties-gateway") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		tmp, err := os.CreateTemp(dir, ".kitties-new-*")
		if err != nil {
			rc.Close()
			return fmt.Errorf("writing the new version: %w", err)
		}
		_, err = io.Copy(tmp, io.LimitReader(rc, maxBinary))
		rc.Close()
		if cerr := tmp.Close(); err == nil {
			err = cerr
		}
		staged[name] = tmp.Name()
		if err != nil {
			return fmt.Errorf("writing the new version: %w", err)
		}
		if err := os.Chmod(tmp.Name(), 0o755); err != nil {
			return err
		}
	}
	if staged["kitties-desktop"] == "" || staged["kitties-gateway"] == "" {
		return fmt.Errorf("the download of %s does not hold kitties-desktop and kitties-gateway: it was not installed", tag)
	}
	if err := u.runsAs(ctx, staged["kitties-gateway"], tag); err != nil {
		return err
	}
	for _, name := range []string{"kitties-gateway", "kitties-desktop"} {
		cur := filepath.Join(dir, name)
		prev := cur + ".prev"
		_ = os.Remove(prev)
		if err := os.Rename(cur, prev); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("keeping the running version: %w", err)
		}
		if err := os.Rename(staged[name], cur); err != nil {
			_ = os.Rename(prev, cur)
			return fmt.Errorf("putting the new version in place: %w", err)
		}
		delete(staged, name)
	}
	return nil
}

// installBundle opens the disk image and puts its Kitties.app in place of the running bundle.
func (u *Updater) installBundle(ctx context.Context, data []byte, tag string) error {
	b := u.bundle()
	parent := filepath.Dir(b)
	work, err := os.MkdirTemp("", "kitties-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	dmg := filepath.Join(work, "kitties.dmg")
	if err := os.WriteFile(dmg, data, 0o600); err != nil {
		return err
	}
	mnt := filepath.Join(work, "mnt")
	if out, err := u.Run(ctx, "hdiutil", "attach", "-nobrowse", "-readonly", "-noautoopen", "-mountpoint", mnt, dmg); err != nil {
		return fmt.Errorf("opening the disk image: %v: %s", err, out)
	}
	defer func() { _, _ = u.Run(context.Background(), "hdiutil", "detach", "-force", mnt) }()
	apps, _ := filepath.Glob(filepath.Join(mnt, "*.app"))
	if len(apps) == 0 {
		return fmt.Errorf("the disk image of %s holds no app: it was not installed", tag)
	}
	next := filepath.Join(parent, "."+filepath.Base(b)+".new")
	_ = os.RemoveAll(next)
	if out, err := u.Run(ctx, "ditto", apps[0], next); err != nil {
		_ = os.RemoveAll(next)
		return fmt.Errorf("copying the new app: %v: %s", err, out)
	}
	if err := u.runsAs(ctx, filepath.Join(next, "Contents", "MacOS", "kitties-gateway"), tag); err != nil {
		_ = os.RemoveAll(next)
		return err
	}
	prev := filepath.Join(parent, "."+filepath.Base(b)+".prev")
	_ = os.RemoveAll(prev)
	if err := os.Rename(b, prev); err != nil {
		_ = os.RemoveAll(next)
		return fmt.Errorf("keeping the running app: %w", err)
	}
	if err := os.Rename(next, b); err != nil {
		_ = os.Rename(prev, b)
		return fmt.Errorf("putting the new app in place: %w", err)
	}
	return nil
}

// runsAs checks that a new gateway runs and says the version it should.
func (u *Updater) runsAs(ctx context.Context, exe, tag string) error {
	run, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := u.Run(run, exe, "version")
	if err != nil || !bytes.Contains(out, []byte(strings.TrimPrefix(tag, "v"))) {
		return fmt.Errorf("the new version did not run as %s (%v): it was not installed", tag, err)
	}
	return nil
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// relaunchApp closes the app and opens it again on the new version, from a helper that outlives
// both: the app (this gateway's parent) and this gateway stop, and the new app starts its gateway.
func (u *Updater) relaunchApp() error {
	if u.OS == "windows" {
		if u.installer == "" {
			return fmt.Errorf("no installer was downloaded")
		}
		// The installer stops the app and the gateway itself, installs, and opens the app again.
		return u.Detach(u.installer, []string{"/S", "/RELAUNCH"})
	}
	open := `cd "$DIR" && nohup ./kitties-desktop >/dev/null 2>&1 &`
	if u.OS == "darwin" {
		open = `open -n "$BUNDLE"`
	}
	script := `sleep 1
[ "$APP" -gt 1 ] && kill "$APP" 2>/dev/null
kill "$GW" 2>/dev/null
i=0; while kill -0 "$GW" 2>/dev/null && [ $i -lt 50 ]; do sleep 0.2; i=$((i+1)); done
kill -9 "$GW" 2>/dev/null
` + open + "\n"
	app := 0
	if u.appParent() {
		app = os.Getppid()
	}
	return u.Detach("/bin/sh", []string{"-c", script, "kitties-relaunch"},
		"APP="+strconv.Itoa(app), "GW="+strconv.Itoa(os.Getpid()), "DIR="+filepath.Dir(u.Exe), "BUNDLE="+u.bundle())
}

// appParent says whether this gateway was started by the app (its parent runs from the same place),
// and so whether the parent is the app to close.
func (u *Updater) appParent() bool {
	ppid := os.Getppid()
	if ppid <= 1 {
		return false
	}
	if u.OS == "linux" {
		exe, err := os.Readlink("/proc/" + strconv.Itoa(ppid) + "/exe")
		return err == nil && filepath.Dir(strings.TrimSuffix(exe, " (deleted)")) == filepath.Dir(u.Exe)
	}
	out, err := u.Run(context.Background(), "ps", "-o", "comm=", "-p", strconv.Itoa(ppid))
	return err == nil && u.bundle() != "" && strings.HasPrefix(strings.TrimSpace(string(out)), u.bundle())
}
