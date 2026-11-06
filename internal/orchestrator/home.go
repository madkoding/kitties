package orchestrator

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/madkoding/kitties/internal/container"
	"github.com/madkoding/kitties/internal/kitty"
)

// WHAT OUTLIVES A KITTY'S DESKTOP.
//
// Three folders of the host are mounted into each desktop, and none is ever deleted by an update,
// a stop, or the removal of the kitty:
//
//   - its workspace (<data>/work/<name> → ~/work): its files, browser profile, theme, and the
//     list of Debian packages it installed (installed again in a container made anew);
//   - its home (<data>/homes/<name> → /home/kitty): settings, ~/.local, /opt/kitty-apps, when the
//     image can seed one (container.HomeLabel);
//   - its Shared folder (<data>/shared/<name> → ~/Shared): what it made for its person, who sees
//     it in the page.
//
// A desktop made before homes lived on the host had its home inside the container: before that
// container is removed, its home is copied out as a tar (<data>/homes/.moving/<name>.tar), and
// put into the new desktop's home once it is up. The tar is deleted only when that worked.

// homeKeeper is a runtime that can give a desktop a home of the host.
type homeKeeper interface {
	ImageHasHome(ctx context.Context, image string) (bool, error)
	HasHome(ctx context.Context, name string) (bool, error)
	ExecOut(ctx context.Context, name string, w io.Writer, argv ...string) error
	ExecIn(ctx context.Context, name string, r io.Reader, argv ...string) (string, error)
	Restart(ctx context.Context, name string) error
}

// DefaultSharedQuotaMB is how much a kitty's Shared folder may hold when the configuration does
// not say (containers.shared_quota_mb; a negative value is no limit).
const DefaultSharedQuotaMB = 5120

// SharedQuotaMB is the most a kitty's Shared folder holds, in MB; 0 is no limit.
func (m *Manager) SharedQuotaMB() int {
	switch q := m.cfg.Containers.SharedQuotaMB; {
	case q < 0:
		return 0
	case q == 0:
		return DefaultSharedQuotaMB
	default:
		return q
	}
}

// SharedRoot is the host folder that holds every kitty's Shared folder, one per name.
func (m *Manager) SharedRoot() string { return filepath.Join(m.cfg.DataDirOr(), "shared") }

func (m *Manager) homeDir(k *kitty.Kitty) string {
	return filepath.Join(m.cfg.DataDirOr(), "homes", k.Name)
}

func (m *Manager) sharedDir(k *kitty.Kitty) string { return filepath.Join(m.SharedRoot(), k.Name) }

// movePath is where an older desktop's home waits to go into its new home.
func (m *Manager) movePath(k *kitty.Kitty) string {
	return filepath.Join(m.cfg.DataDirOr(), "homes", ".moving", k.Name+".tar")
}

// makeOpenDir creates a folder a container writes in: open to everyone, inside a root that is the
// gateway's alone (see makeWorkspace).
func makeOpenDir(dir string) error {
	root := filepath.Dir(dir)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o777) //nolint:gosec // the root keeps everyone else out
}

// homeKeeperFor is the runtime's homeKeeper, when it is one.
func (m *Manager) homeKeeperFor() (homeKeeper, bool) {
	hk, ok := m.ctr().(homeKeeper)
	return hk, ok
}

// desktopSpec is the container a kitty's desktop is made of, its folders created.
func (m *Manager) desktopSpec(ctx context.Context, k *kitty.Kitty) (container.Spec, error) {
	c := m.cfg.Containers
	env := make(map[string]string, len(c.Env)+1)
	for key, v := range c.Env {
		env[key] = v
	}
	env["KITTY_SHARED_QUOTA_MB"] = strconv.Itoa(m.SharedQuotaMB())
	spec := container.Spec{
		Name: k.Container, KittyName: k.Name, Image: c.Image, MemoryMB: c.MemoryMB, CPUs: c.CPUs,
		Network: c.Network, Workspace: k.Workspace, Shared: m.sharedDir(k), Env: env,
	}
	if err := makeOpenDir(spec.Shared); err != nil {
		return spec, fmt.Errorf("create the Shared folder: %w", err)
	}
	if hk, ok := m.homeKeeperFor(); ok {
		if yes, _ := hk.ImageHasHome(ctx, c.Image); yes {
			spec.Home = m.homeDir(k)
			if err := makeOpenDir(spec.Home); err != nil {
				return spec, fmt.Errorf("create the home: %w", err)
			}
		}
	}
	return spec, nil
}

// dropContainer removes a kitty's container so it can be made anew, keeping what it had: its
// home, when it is inside the container and the new one will have it on the host, and the
// sessions of its browser. A home that cannot be copied out keeps the container: nothing is lost.
func (m *Manager) dropContainer(ctx context.Context, k *kitty.Kitty, running bool) error {
	if err := m.keepOldHome(ctx, k, running); err != nil {
		return fmt.Errorf("the home of %s's desktop could not be kept, so its desktop was left as it is: %w", k.Name, err)
	}
	if running {
		m.saveSessions(ctx, k)
	}
	return m.ctr().Remove(ctx, k.Container)
}

// moveTimeout bounds copying a home out of, or into, a container.
var moveTimeout = 15 * time.Minute

// keepOldHome copies the home out of a container that has it inside, when the image the desktop
// is made anew from puts it on the host.
func (m *Manager) keepOldHome(ctx context.Context, k *kitty.Kitty, running bool) error {
	hk, ok := m.homeKeeperFor()
	if !ok {
		return nil
	}
	if yes, _ := hk.ImageHasHome(ctx, m.cfg.Containers.Image); !yes {
		return nil
	}
	has, err := hk.HasHome(ctx, k.Container)
	if err != nil {
		return err
	}
	path := m.movePath(k)
	if _, err := os.Stat(path); has || err == nil {
		return nil // already on the host, or already copied out and waiting
	}
	if !running {
		if err := m.ctr().Start(ctx, k.Container); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	part := path + ".part"
	f, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, moveTimeout)
	defer cancel()
	// The workspace and Shared are folders of the host already; the cache is not worth moving.
	err = hk.ExecOut(ctx, k.Container, f, "tar", "-C", container.HomeDir, "--exclude=./work", "--exclude=./Shared",
		"--exclude=./.cache", "-cf", "-", ".")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(part)
		return err
	}
	return os.Rename(part, path)
}

// finishMove puts a home copied out of an older desktop into the new one, and restarts it so its
// desktop reads it. What fails is tried again at the next start: the copy stays until it worked.
func (m *Manager) finishMove(ctx context.Context, k *kitty.Kitty, spec container.Spec) {
	if spec.Home == "" {
		return
	}
	hk, ok := m.homeKeeperFor()
	if !ok {
		return
	}
	path := m.movePath(k)
	f, err := os.Open(path)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, moveTimeout)
	defer cancel()
	_, err = hk.ExecIn(ctx, k.Container, f, "kitty-import", "--home")
	_ = f.Close()
	if err != nil {
		return
	}
	if err := hk.Restart(ctx, k.Container); err != nil {
		return
	}
	_ = os.Remove(path)
}

// Storage is what a kitty keeps on the host, by folder.
type Storage struct {
	// Home is true when its home lives on the host (it survives updates).
	Home          bool  `json:"home"`
	HomeBytes     int64 `json:"home_bytes"`
	WorkBytes     int64 `json:"work_bytes"`
	SharedBytes   int64 `json:"shared_bytes"`
	SharedQuotaMB int   `json:"shared_quota_mb"`
	// Moving is true while an older desktop's home waits to go into the new one.
	Moving bool `json:"moving,omitempty"`
}

// sizeCap bounds how many entries a size counts: a huge home is told as at least its size.
var sizeCap = 200000

// dirSize adds up the sizes of the files under dir, without following links.
func dirSize(dir string) int64 {
	var total int64
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if n++; n > sizeCap {
			return filepath.SkipAll
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// storage tells what a kitty keeps on the host.
func (m *Manager) storage(k *kitty.Kitty) Storage {
	s := Storage{SharedQuotaMB: m.SharedQuotaMB(), WorkBytes: dirSize(k.Workspace), SharedBytes: dirSize(m.sharedDir(k))}
	if st, err := os.Stat(m.homeDir(k)); err == nil && st.IsDir() {
		s.Home, s.HomeBytes = true, dirSize(m.homeDir(k))
	}
	if _, err := os.Stat(m.movePath(k)); err == nil {
		s.Moving = true
	}
	return s
}

// secret are the paths an export leaves out: keys and credentials stay sealed in the backup of
// the sessions (package keep), never in a file to download. By folder of the export.
var secret = map[string][]string{
	"home": {".ssh", ".gnupg", ".git-credentials", ".netrc", ".config/gh", ".docker", ".aws", ".kube",
		".config/gcloud", ".pki", ".local/share/keyrings", ".password-store", ".cache", "work", "Shared"},
	"work": {".browser"},
}

// Export writes a kitty's files as a .tar.gz: home/ (its settings and programs), work/ and
// shared/, without its keys, credentials and browser profile. Files the gateway cannot read
// are left out.
func (m *Manager) Export(ref string, w io.Writer) error {
	k, err := m.desktopKitty(ref)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	for _, part := range []struct{ name, dir string }{{"home", m.homeDir(k)}, {"work", k.Workspace}, {"shared", m.sharedDir(k)}} {
		if err := addTree(tw, part.name, part.dir, secret[part.name]); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// addTree writes dir into the tar under prefix, leaving out skip (paths relative to dir).
func addTree(tw *tar.Writer, prefix, dir string, skip []string) error {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil
	}
	left := map[string]bool{}
	for _, s := range skip {
		left[s] = true
	}
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable: left out
		}
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if left[rel] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		link := ""
		if d.Type()&fs.ModeSymlink != 0 {
			if link, err = os.Readlink(path); err != nil {
				return nil
			}
		} else if !d.IsDir() && !d.Type().IsRegular() {
			return nil // sockets, pipes, devices
		}
		h, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return nil
		}
		h.Name = prefix + "/" + rel
		h.Uid, h.Gid, h.Uname, h.Gname = 0, 0, "", ""
		if d.Type().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return nil // a file of the kitty's the gateway may not read
			}
			defer f.Close()
			if err := tw.WriteHeader(h); err != nil {
				return err
			}
			_, err = io.Copy(tw, io.LimitReader(f, h.Size))
			return err
		}
		return tw.WriteHeader(h)
	})
}

// errNotRunning is what an import says when the desktop is off.
var errNotRunning = errors.New("start the desktop first: the files go in through it")

// Import puts a kitty's files back from a .tar.gz that Export made (home/, work/, shared/). A file
// the kitty has that is newer than the copy stays. The desktop must be running.
func (m *Manager) Import(ctx context.Context, ref string, r io.Reader) error {
	k, err := m.desktopKitty(ref)
	if err != nil {
		return err
	}
	if _, running, err := m.containerState(ctx, k.Container); err != nil {
		return err
	} else if !running {
		return errNotRunning
	}
	hk, ok := m.homeKeeperFor()
	if !ok {
		return errors.New("this runtime cannot take files in")
	}
	ctx, cancel := context.WithTimeout(ctx, moveTimeout)
	defer cancel()
	if out, err := hk.ExecIn(ctx, k.Container, r, "kitty-import"); err != nil {
		if strings.Contains(err.Error(), "executable file not found") || strings.Contains(out, "not found") {
			return errors.New("this desktop's image cannot import yet: update the desktop")
		}
		return err
	}
	return nil
}
