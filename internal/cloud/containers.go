package cloud

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/madkoding/kitties/internal/container"
)

// ErrDesktopChannel is returned for what needs the guest daemon (input on stdin, the live view,
// tunnels): commands and screenshots work through the provider's command API, the rest waits for
// the daemon. See docs/design/cloud-compute.md.
var ErrDesktopChannel = errors.New("cloud: the desktop channel of a cloud machine is not available yet")

// Backend is what the orchestrator asks of a place that runs desktops. It matches
// orchestrator.Containers, which the local container manager also satisfies.
type Backend interface {
	Up(ctx context.Context, s container.Spec) (string, error)
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Remove(ctx context.Context, name string) error
	Exec(ctx context.Context, name string, argv ...string) (string, error)
	Pull(ctx context.Context, image string) error
	ImageStale(ctx context.Context, name, image string) (bool, error)
	ExecInput(ctx context.Context, name, stdin string, argv ...string) (string, error)
	Screenshot(ctx context.Context, name string) ([]byte, error)
	Stream(ctx context.Context, name string, fps, quality int) (io.ReadCloser, error)
	Tunnel(ctx context.Context, name string, port int) (net.Conn, error)
	List(ctx context.Context) ([]container.Info, error)
}

// Containers is a Backend over a Provider: a container name is the machine's name.
type Containers struct {
	P    Provider
	Size string
	// zones remembers the time zone each machine was asked for (the spec's TZ, else the host's),
	// so a machine woken by Start gets it again.
	zones sync.Map
}

var _ Backend = (*Containers)(nil)

func (c *Containers) find(ctx context.Context, name string) (Machine, error) {
	all, err := c.P.List(ctx)
	if err != nil {
		return Machine{}, err
	}
	for _, m := range all {
		if m.Name == name {
			return m, nil
		}
	}
	return Machine{}, fmt.Errorf("%w: %s", ErrNotFound, name)
}

// Up returns the machine already made for the name (woken if asleep), or makes one. Looking the
// name up first covers a crash between creating and recording the machine: nothing is made twice.
func (c *Containers) Up(ctx context.Context, s container.Spec) (string, error) {
	m, err := c.find(ctx, s.Name)
	switch {
	case err == nil:
		if m.State == Asleep {
			if m, err = c.P.Wake(ctx, m.ID); err != nil {
				return "", err
			}
		}
		c.setZone(ctx, s.Name, m.ID, s.Zone())
		return m.ID, nil
	case !errors.Is(err, ErrNotFound):
		return "", err // an inventory that cannot be read is never taken for an empty one
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	m, err = Ensure(ctx, c.P, CreateRequest{Key: hex.EncodeToString(raw[:]), Name: s.Name, Size: c.Size})
	if err != nil {
		return "", err
	}
	c.setZone(ctx, s.Name, m.ID, s.Zone())
	return m.ID, nil
}

func (c *Containers) Start(ctx context.Context, name string) error {
	m, err := c.find(ctx, name)
	if err != nil {
		return err
	}
	if _, err = c.P.Wake(ctx, m.ID); err != nil {
		return err
	}
	zone := container.HostTimezone()
	if z, ok := c.zones.Load(name); ok {
		zone = z.(string)
	}
	c.setZone(ctx, name, m.ID, zone)
	return nil
}

// setZone puts the machine in the time zone, as the local desktops take the host's: the machine
// is a whole system, so its /etc/localtime (read by every program started after) rather than a
// TZ in its environment. A zone the machine has no file for (a POSIX one) is left out. Best
// effort: a machine that cannot be reached keeps its clock in UTC, and works.
func (c *Containers) setZone(ctx context.Context, name, id, zone string) {
	if zone == "" {
		return
	}
	c.zones.Store(name, zone)
	_, _ = c.P.Exec(ctx, id, "z="+quote(zone)+`; f="/usr/share/zoneinfo/$z"; [ -f "$f" ] || exit 0; `+
		`sudo -n ln -sf "$f" /etc/localtime && printf '%s\n' "$z" | sudo -n tee /etc/timezone >/dev/null`, execTimeout)
}

func (c *Containers) Stop(ctx context.Context, name string) error {
	m, err := c.find(ctx, name)
	if err != nil {
		return err
	}
	_, err = c.P.Sleep(ctx, m.ID)
	return err
}

// Remove deletes the machine; a machine that is already gone is not an error.
func (c *Containers) Remove(ctx context.Context, name string) error {
	m, err := c.find(ctx, name)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := c.P.Delete(ctx, m.ID); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return nil
}

func (c *Containers) List(ctx context.Context) ([]container.Info, error) {
	all, err := c.P.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]container.Info, 0, len(all))
	for _, m := range all {
		out = append(out, container.Info{ID: m.ID, Name: m.Name, Kitty: strings.TrimPrefix(m.Name, "kitty-"), Image: "cloud", Status: string(m.State), Running: m.State == Running})
	}
	return out, nil
}

// There is no image to pull or go stale: the machine is made from the provider's template.
func (c *Containers) Pull(context.Context, string) error { return nil }
func (c *Containers) ImageStale(context.Context, string, string) (bool, error) {
	return false, nil
}

// guestPrefix runs a command as the desktop's user on its display, as the local container does.
const guestPrefix = "sudo -n -H -u kitty env DISPLAY=:99 "

// execTimeout bounds one command on a machine.
const execTimeout = 2 * time.Minute

// quote makes one shell word of s.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// run executes a shell command on the running machine called name.
func (c *Containers) run(ctx context.Context, name, command string) (ExecResult, error) {
	m, err := c.find(ctx, name)
	if err != nil {
		return ExecResult{}, err
	}
	return c.P.Exec(ctx, m.ID, command, execTimeout)
}

// Exec runs argv in the kitty's work folder, as the desktop's user. Like the local container, it
// returns what the command wrote, and an error when it failed.
func (c *Containers) Exec(ctx context.Context, name string, argv ...string) (string, error) {
	if len(argv) == 0 {
		return "", errors.New("nothing to run")
	}
	words := make([]string, len(argv))
	for i, a := range argv {
		words[i] = quote(a)
	}
	res, err := c.run(ctx, name, "cd "+container.Workdir+" && "+guestPrefix+strings.Join(words, " "))
	if err != nil {
		return "", err
	}
	out := res.Stdout + res.Stderr
	switch {
	case res.TimedOut:
		return out, fmt.Errorf("exec in %s: timed out", name)
	case res.ExitCode != 0:
		return out, fmt.Errorf("exec in %s: exit status %d", name, res.ExitCode)
	}
	return out, nil
}

func (c *Containers) ExecInput(context.Context, string, string, ...string) (string, error) {
	return "", ErrDesktopChannel
}

// Screenshot runs the guest's kitty-shot helper (the same one the local image carries) and decodes
// its picture, which travels as base64 inside the command's text output.
func (c *Containers) Screenshot(ctx context.Context, name string) ([]byte, error) {
	res, err := c.run(ctx, name, guestPrefix+"kitty-shot | base64 -w0")
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 || res.TimedOut {
		return nil, fmt.Errorf("screenshot of %s: %s", name, strings.TrimSpace(res.Stderr))
	}
	if res.Truncated {
		return nil, fmt.Errorf("screenshot of %s: the picture is larger than the provider returns", name)
	}
	pic, err := base64.StdEncoding.DecodeString(strings.TrimSpace(res.Stdout))
	if err != nil || len(pic) == 0 {
		return nil, fmt.Errorf("screenshot of %s: the guest returned no picture", name)
	}
	return pic, nil
}
func (c *Containers) Stream(context.Context, string, int, int) (io.ReadCloser, error) {
	return nil, ErrDesktopChannel
}
func (c *Containers) Tunnel(context.Context, string, int) (net.Conn, error) {
	return nil, ErrDesktopChannel
}

// Router sends each call to the local or the cloud backend by the container's name. Either may be
// nil: its names then fail with the reason in Err instead of reaching nothing.
type Router struct {
	Local, Cloud Backend
	// IsCloud says whether the container with this name runs in the cloud.
	IsCloud func(name string) bool
	// LocalErr and CloudErr say why a backend is missing.
	LocalErr, CloudErr error
}

var _ Backend = (*Router)(nil)

func (r *Router) pick(name string) (Backend, error) {
	if r.IsCloud != nil && r.IsCloud(name) {
		if r.Cloud == nil {
			return nil, orDefault(r.CloudErr, "cloud: no cloud backend is configured")
		}
		return r.Cloud, nil
	}
	if r.Local == nil {
		return nil, orDefault(r.LocalErr, "no container runtime is available for local desktops")
	}
	return r.Local, nil
}

func orDefault(err error, text string) error {
	if err != nil {
		return err
	}
	return errors.New(text)
}

func (r *Router) Up(ctx context.Context, s container.Spec) (string, error) {
	b, err := r.pick(s.Name)
	if err != nil {
		return "", err
	}
	return b.Up(ctx, s)
}
func (r *Router) Start(ctx context.Context, name string) error {
	b, err := r.pick(name)
	if err != nil {
		return err
	}
	return b.Start(ctx, name)
}
func (r *Router) Stop(ctx context.Context, name string) error {
	b, err := r.pick(name)
	if err != nil {
		return err
	}
	return b.Stop(ctx, name)
}
func (r *Router) Remove(ctx context.Context, name string) error {
	b, err := r.pick(name)
	if err != nil {
		return err
	}
	return b.Remove(ctx, name)
}
func (r *Router) Exec(ctx context.Context, name string, argv ...string) (string, error) {
	b, err := r.pick(name)
	if err != nil {
		return "", err
	}
	return b.Exec(ctx, name, argv...)
}
func (r *Router) ExecInput(ctx context.Context, name, stdin string, argv ...string) (string, error) {
	b, err := r.pick(name)
	if err != nil {
		return "", err
	}
	return b.ExecInput(ctx, name, stdin, argv...)
}
func (r *Router) Screenshot(ctx context.Context, name string) ([]byte, error) {
	b, err := r.pick(name)
	if err != nil {
		return nil, err
	}
	return b.Screenshot(ctx, name)
}
func (r *Router) Stream(ctx context.Context, name string, fps, quality int) (io.ReadCloser, error) {
	b, err := r.pick(name)
	if err != nil {
		return nil, err
	}
	return b.Stream(ctx, name, fps, quality)
}
func (r *Router) Tunnel(ctx context.Context, name string, port int) (net.Conn, error) {
	b, err := r.pick(name)
	if err != nil {
		return nil, err
	}
	return b.Tunnel(ctx, name, port)
}

// Pull and ImageStale concern images, which only the local backend has.
func (r *Router) Pull(ctx context.Context, image string) error {
	if r.Local == nil {
		return nil
	}
	return r.Local.Pull(ctx, image)
}
func (r *Router) ImageStale(ctx context.Context, name, image string) (bool, error) {
	if r.IsCloud != nil && r.IsCloud(name) || r.Local == nil {
		return false, nil
	}
	return r.Local.ImageStale(ctx, name, image)
}

// LANStale concerns the local desktops; a machine in the cloud is left as it is.
func (r *Router) LANStale(ctx context.Context, name string, noLAN bool) (bool, error) {
	if r.IsCloud != nil && r.IsCloud(name) || r.Local == nil {
		return false, nil
	}
	l, ok := r.Local.(interface {
		LANStale(ctx context.Context, name string, noLAN bool) (bool, error)
	})
	if !ok {
		return false, nil
	}
	return l.LANStale(ctx, name, noLAN)
}

// ZoneStale concerns the local desktops, which take the host's time zone; a machine in the cloud
// is left as it is.
func (r *Router) ZoneStale(ctx context.Context, name, zone string) (bool, error) {
	if r.IsCloud != nil && r.IsCloud(name) || r.Local == nil {
		return false, nil
	}
	z, ok := r.Local.(interface {
		ZoneStale(ctx context.Context, name, zone string) (bool, error)
	})
	if !ok {
		return false, nil
	}
	return z.ZoneStale(ctx, name, zone)
}

// limiter is a local engine that can change a container's RAM and swap, and tell its own total.
type limiter interface {
	SetLimits(ctx context.Context, name string, memoryMB, swapMB int) error
	LimitsStale(ctx context.Context, name string, memoryMB, swapMB int) (bool, error)
	EngineMemoryMB(ctx context.Context) (int, error)
}

var errNoLimits = errors.New("the RAM and swap of a machine in the cloud are its provider's size")

// localLimiter is the local engine, for a desktop that is not in the cloud.
func (r *Router) localLimiter(name string) (limiter, error) {
	if name != "" && r.IsCloud != nil && r.IsCloud(name) {
		return nil, errNoLimits
	}
	l, ok := r.Local.(limiter)
	if !ok {
		return nil, errors.New("this engine cannot change a machine's memory")
	}
	return l, nil
}

// SetLimits changes the RAM and swap of a local desktop; a machine in the cloud has its size.
func (r *Router) SetLimits(ctx context.Context, name string, memoryMB, swapMB int) error {
	l, err := r.localLimiter(name)
	if err != nil {
		return err
	}
	return l.SetLimits(ctx, name, memoryMB, swapMB)
}

// LimitsStale concerns the local desktops only.
func (r *Router) LimitsStale(ctx context.Context, name string, memoryMB, swapMB int) (bool, error) {
	l, err := r.localLimiter(name)
	if err != nil {
		return false, nil
	}
	return l.LimitsStale(ctx, name, memoryMB, swapMB)
}

// EngineMemoryMB is the local engine's.
func (r *Router) EngineMemoryMB(ctx context.Context) (int, error) {
	l, err := r.localLimiter("")
	if err != nil {
		return 0, err
	}
	return l.EngineMemoryMB(ctx)
}

// List joins both backends. A backend that cannot answer is left out as long as the other can;
// when none can, the first error is returned.
func (r *Router) List(ctx context.Context) ([]container.Info, error) {
	var out []container.Info
	var first error
	answered := false
	for _, b := range []Backend{r.Local, r.Cloud} {
		if b == nil {
			continue
		}
		infos, err := b.List(ctx)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		answered = true
		out = append(out, infos...)
	}
	if !answered && first != nil {
		return nil, first
	}
	return out, nil
}

// homes are the local desktops' part: a machine in the cloud keeps its own disk, so it has
// nothing to move and no home of the host.
type homes interface {
	ImageHasHome(ctx context.Context, image string) (bool, error)
	HasHome(ctx context.Context, name string) (bool, error)
	ExecOut(ctx context.Context, name string, w io.Writer, argv ...string) error
	ExecIn(ctx context.Context, name string, r io.Reader, argv ...string) (string, error)
	Restart(ctx context.Context, name string) error
}

var errNoHomes = errors.New("this desktop's runtime does not keep homes on the host")

// localHomes is the local backend's homes, for a desktop that is not in the cloud.
func (r *Router) localHomes(name string) (homes, error) {
	if name != "" && r.IsCloud != nil && r.IsCloud(name) {
		return nil, errNoHomes
	}
	h, ok := r.Local.(homes)
	if !ok {
		return nil, errNoHomes
	}
	return h, nil
}

// ImageHasHome is the local engine's answer: the image is a local one.
func (r *Router) ImageHasHome(ctx context.Context, image string) (bool, error) {
	h, err := r.localHomes("")
	if err != nil {
		return false, nil
	}
	return h.ImageHasHome(ctx, image)
}

// HasHome says a machine in the cloud has its home already: there is nothing to move out of it.
func (r *Router) HasHome(ctx context.Context, name string) (bool, error) {
	h, err := r.localHomes(name)
	if err != nil {
		return true, nil
	}
	return h.HasHome(ctx, name)
}

func (r *Router) ExecOut(ctx context.Context, name string, w io.Writer, argv ...string) error {
	h, err := r.localHomes(name)
	if err != nil {
		return err
	}
	return h.ExecOut(ctx, name, w, argv...)
}

func (r *Router) ExecIn(ctx context.Context, name string, rd io.Reader, argv ...string) (string, error) {
	h, err := r.localHomes(name)
	if err != nil {
		return "", err
	}
	return h.ExecIn(ctx, name, rd, argv...)
}

func (r *Router) Restart(ctx context.Context, name string) error {
	h, err := r.localHomes(name)
	if err != nil {
		return err
	}
	return h.Restart(ctx, name)
}
