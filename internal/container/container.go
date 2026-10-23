// Package container runs each kitty's isolated environment: one container per kitty, built from
// the kitty-desktop image (a headless X display on Xvfb with a browser and the usual desktop
// tools), with its own workspace volume. A person looks at the display through a screenshot
// (Screenshot) instead of a VNC port, so nothing is published on the host.
//
// The orchestrator never writes a command line: it fills a Spec and this package builds the argv
// for docker or podman. No shell is involved.
package container

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// Label marks every container this package starts, so a sweep finds them and nothing else.
	Label = "kitties.bot"
	// NameLabel carries the kitty's name.
	NameLabel = "kitties.name"

	// Workdir is where the kitty's workspace is mounted inside the container.
	Workdir = "/home/kitty/work"
	// Display is the Xvfb display the image starts.
	Display = ":99"
)

var (
	imageRef = regexp.MustCompile(`^[a-z0-9][a-zA-Z0-9._/:@-]{0,254}$`)
	nameRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	envKey   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// Seams: what the machine does, as variables so a test says what the machine has.
var (
	lookPath = exec.LookPath
	runCmd   = func(ctx context.Context, bin string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, bin, args...).CombinedOutput()
	}
)

// Runtime is the container engine found on this machine.
type Runtime struct {
	Bin string // docker or podman
}

// ErrNoRuntime is returned when neither docker nor podman answers.
var ErrNoRuntime = errors.New("no container runtime: install Docker or Podman, or set containers.runtime")

// A Mac app opened from the Finder gets a bare PATH (/usr/bin:/bin:/usr/sbin:/sbin), without
// the folders Docker Desktop, Homebrew, OrbStack, Rancher Desktop or Podman put their programs
// in: they are added, so docker is found and finds its own helpers (docker-credential-desktop).
var (
	goos      = runtime.GOOS
	homeDir   = os.UserHomeDir
	setPath   = func(v string) { _ = os.Setenv("PATH", v) }
	getPath   = func() string { return os.Getenv("PATH") }
	isDir     = func(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }
	pathOnce  sync.Once
	macPlaces = []string{"/usr/local/bin", "/opt/homebrew/bin", "~/.docker/bin", "/Applications/Docker.app/Contents/Resources/bin",
		"~/.orbstack/bin", "~/.rd/bin", "/opt/podman/bin", "/opt/local/bin"}
)

// widenPath adds the folders container engines live in that PATH lacks.
func widenPath() {
	if goos != "darwin" {
		return
	}
	home, _ := homeDir()
	path := getPath()
	have := map[string]bool{}
	for _, dir := range filepath.SplitList(path) {
		have[dir] = true
	}
	for _, dir := range macPlaces {
		if strings.HasPrefix(dir, "~/") {
			if home == "" {
				continue
			}
			dir = filepath.Join(home, dir[2:])
		}
		if !have[dir] && isDir(dir) {
			path += string(filepath.ListSeparator) + dir
			have[dir] = true
		}
	}
	setPath(strings.TrimPrefix(path, string(filepath.ListSeparator)))
}

// Detect finds the runtime. want is docker, podman or auto.
func Detect(ctx context.Context, want string) (Runtime, error) {
	pathOnce.Do(widenPath)
	candidates := []string{"docker", "podman"}
	if want != "" && want != "auto" {
		candidates = []string{want}
	}
	var reasons []string
	for _, bin := range candidates {
		path, err := lookPath(bin)
		if err != nil {
			reasons = append(reasons, bin+" is not installed")
			continue
		}
		if out, err := runCmd(ctx, path, "info", "--format", "{{.ID}}"); err != nil {
			reasons = append(reasons, fmt.Sprintf("%s does not answer: %s", bin, strings.TrimSpace(string(out))))
			continue
		}
		return Runtime{Bin: path}, nil
	}
	return Runtime{}, fmt.Errorf("%w (%s)", ErrNoRuntime, strings.Join(reasons, "; "))
}

// Spec is one container to run.
type Spec struct {
	Name      string
	KittyName string
	Image     string
	MemoryMB  int
	CPUs      float64
	Network   string
	// Workspace is the host folder mounted at Workdir.
	Workspace string
	Env       map[string]string
}

// Validate refuses a spec that would build a wrong or dangerous command line.
func (s Spec) Validate() error {
	if !nameRe.MatchString(s.Name) {
		return fmt.Errorf("container name %q: lowercase letters, digits and dashes", s.Name)
	}
	if !imageRef.MatchString(s.Image) {
		return fmt.Errorf("image %q is not a valid reference", s.Image)
	}
	if s.MemoryMB < 0 || s.CPUs < 0 {
		return errors.New("memory and cpus cannot be negative")
	}
	if s.Network != "" && !nameRe.MatchString(s.Network) {
		return fmt.Errorf("network %q is not a valid name", s.Network)
	}
	for k := range s.Env {
		if !envKey.MatchString(k) {
			return fmt.Errorf("environment variable %q is not a valid name", k)
		}
	}
	return nil
}

// Args builds the `run` argv for the spec.
func (s Spec) Args() []string {
	args := []string{"run", "-d", "--name", s.Name, "--label", Label + "=1", "--label", NameLabel + "=" + s.KittyName,
		"--restart", "unless-stopped", "--shm-size", "512m"}
	if s.MemoryMB > 0 {
		args = append(args, "--memory", strconv.Itoa(s.MemoryMB)+"m")
	}
	if s.CPUs > 0 {
		args = append(args, "--cpus", strconv.FormatFloat(s.CPUs, 'f', -1, 64))
	}
	if s.Network != "" {
		args = append(args, "--network", s.Network)
	}
	if s.Workspace != "" {
		args = append(args, "-v", s.Workspace+":"+Workdir)
	}
	keys := make([]string, 0, len(s.Env))
	for k := range s.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-e", k+"="+s.Env[k])
	}
	args = append(args, "-e", "KITTY_NAME="+s.KittyName, "-e", "DISPLAY="+Display)
	return append(args, s.Image)
}

// Manager starts and stops containers with one runtime.
type Manager struct {
	rt Runtime
}

// New creates a manager for rt.
func New(rt Runtime) *Manager { return &Manager{rt: rt} }

// Runtime names the binary in use.
func (m *Manager) Runtime() string { return m.rt.Bin }

// Up starts the container and returns its id.
func (m *Manager) Up(ctx context.Context, s Spec) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	out, err := runCmd(ctx, m.rt.Bin, s.Args()...)
	if err != nil {
		return "", fmt.Errorf("start %s: %s", s.Name, tail(string(out), 6))
	}
	return strings.TrimSpace(string(out)), nil
}

// Stop stops the container, keeping it for a later Start.
func (m *Manager) Stop(ctx context.Context, name string) error {
	return m.simple(ctx, "stop", "-t", "10", name)
}

// Start starts a stopped container again.
func (m *Manager) Start(ctx context.Context, name string) error {
	return m.simple(ctx, "start", name)
}

// Remove removes the container, running or not.
func (m *Manager) Remove(ctx context.Context, name string) error {
	return m.simple(ctx, "rm", "-f", name)
}

func (m *Manager) simple(ctx context.Context, args ...string) error {
	if !nameRe.MatchString(args[len(args)-1]) {
		return fmt.Errorf("container name %q is not valid", args[len(args)-1])
	}
	out, err := runCmd(ctx, m.rt.Bin, args...)
	if err != nil {
		return fmt.Errorf("%s %s: %s", args[0], args[len(args)-1], tail(string(out), 6))
	}
	return nil
}

// Exec runs a command inside the container and returns what it printed.
func (m *Manager) Exec(ctx context.Context, name string, argv ...string) (string, error) {
	if !nameRe.MatchString(name) {
		return "", fmt.Errorf("container name %q is not valid", name)
	}
	if len(argv) == 0 {
		return "", errors.New("nothing to run")
	}
	args := append([]string{"exec", "-w", Workdir, name}, argv...)
	out, err := runCmd(ctx, m.rt.Bin, args...)
	if err != nil {
		return string(out), fmt.Errorf("exec in %s: %w", name, err)
	}
	return string(out), nil
}

// Screenshot captures the kitty's display as PNG bytes, with the image's own `kitty-shot` tool.
func (m *Manager) Screenshot(ctx context.Context, name string) ([]byte, error) {
	if !nameRe.MatchString(name) {
		return nil, fmt.Errorf("container name %q is not valid", name)
	}
	out, err := runCmd(ctx, m.rt.Bin, "exec", name, "kitty-shot")
	if err != nil {
		return nil, fmt.Errorf("screenshot of %s: %s", name, tail(string(out), 6))
	}
	return out, nil
}

// Info is one container of ours.
type Info struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kitty   string `json:"kitty"`
	Image   string `json:"image"`
	Status  string `json:"status"`
	Running bool   `json:"running"`
}

// List returns every container carrying Label, running or not.
func (m *Manager) List(ctx context.Context) ([]Info, error) {
	out, err := runCmd(ctx, m.rt.Bin, "ps", "-a", "--filter", "label="+Label, "--format",
		"{{.ID}}|{{.Names}}|{{.Image}}|{{.Status}}|{{.Label \""+NameLabel+"\"}}")
	if err != nil {
		return nil, fmt.Errorf("list the containers: %s", tail(string(out), 6))
	}
	var infos []Info
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Split(line, "|")
		if len(parts) < 4 {
			continue
		}
		info := Info{ID: parts[0], Name: parts[1], Image: parts[2], Status: parts[3],
			Running: strings.HasPrefix(parts[3], "Up")}
		if len(parts) > 4 {
			info.Kitty = parts[4]
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// Status reports whether the container exists and runs.
func (m *Manager) Status(ctx context.Context, name string) (exists, running bool, err error) {
	infos, err := m.List(ctx)
	if err != nil {
		return false, false, err
	}
	for _, i := range infos {
		if i.Name == name {
			return true, i.Running, nil
		}
	}
	return false, false, nil
}

// WaitReady polls until the container reports running, or the timeout passes.
func (m *Manager) WaitReady(ctx context.Context, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		exists, running, err := m.Status(ctx, name)
		if err != nil {
			return err
		}
		if exists && running {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s did not come up in %s", name, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollEvery):
		}
	}
}

var pollEvery = 500 * time.Millisecond

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
