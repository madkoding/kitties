package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/madkoding/kitties/internal/config"
	"github.com/madkoding/kitties/internal/kitty"
)

// THE POWER OF THE MACHINE.
//
// A kitty's machine (its container) is turned on and off by the person, by the kitty and, when
// nobody uses it, by itself. "In use" is: the kitty is in the middle of a task, the person has
// taken the display over or is watching it live, or a command is running in it. The time without
// use counts from the last of those: an action of the kitty on the display, a look or a click of
// the person, the end of a task. A machine never turns itself off in the middle of any of them.

// useTracker remembers when each machine was last used and what is using it now.
type useTracker struct {
	mu      sync.Mutex
	last    map[string]time.Time
	execs   map[string]int
	streams map[string]int
}

func (u *useTracker) init() {
	if u.last == nil {
		u.last, u.execs, u.streams = map[string]time.Time{}, map[string]int{}, map[string]int{}
	}
}

func (u *useTracker) touch(id string, at time.Time) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.init()
	u.last[id] = at
}

// enter marks a command running; the returned function marks it over (and counts as a use).
func (u *useTracker) enter(id string, now func() time.Time) func() {
	u.mu.Lock()
	u.init()
	u.execs[id]++
	u.last[id] = now()
	u.mu.Unlock()
	return func() { u.leave(u.execs, id, now) }
}

// watch marks a live view open; the returned function marks it closed.
func (u *useTracker) watch(id string, now func() time.Time) func() {
	u.mu.Lock()
	u.init()
	u.streams[id]++
	u.last[id] = now()
	u.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { u.leave(u.streams, id, now) }) }
}

func (u *useTracker) leave(counts map[string]int, id string, now func() time.Time) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if counts[id] > 0 {
		counts[id]--
	}
	u.last[id] = now()
}

func (u *useTracker) lastUse(id string) (time.Time, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	t, ok := u.last[id]
	return t, ok
}

func (u *useTracker) running(id string) (execs, streams int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.execs[id], u.streams[id]
}

func (u *useTracker) forget(id string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.last, id)
}

// watchedStream is a live view that tells the manager when it is closed.
type watchedStream struct {
	io.ReadCloser
	done func()
}

func (w watchedStream) Close() error {
	err := w.ReadCloser.Close()
	w.done()
	return err
}

// AutoOffMinutes is the idle time of a kitty's machine: its own, or the gateway's default. 0 is
// never.
func (m *Manager) AutoOffMinutes(k *kitty.Kitty) int {
	if k.AutoOff != nil {
		return *k.AutoOff
	}
	return m.cfg.Containers.AutoOffMinutes
}

// inUse says what keeps a machine on right now, or "" when nothing does. self leaves out the
// kitty's own task (the kitty turning off its own machine).
func (m *Manager) inUse(id string, self bool) string {
	if m.Held(id) {
		return kitty.UseControl
	}
	if _, streams := m.use.running(id); streams > 0 {
		return kitty.UseWatch
	}
	if !self && m.busy(id) {
		return kitty.UseTask
	}
	if execs, _ := m.use.running(id); execs > 0 {
		return kitty.UseCommand
	}
	return ""
}

// Power is what is known about a kitty's machine now.
func (m *Manager) Power(ref string) (kitty.Power, error) {
	k, err := m.Get(ref)
	if err != nil {
		return kitty.Power{}, err
	}
	if !k.Desktop {
		return kitty.Power{}, kitty.ErrNoMachine
	}
	p := kitty.Power{AutoOffMinutes: m.AutoOffMinutes(k), AutoOffCustom: k.AutoOff != nil}
	switch k.Status {
	case kitty.StatusRunning:
		p.State = kitty.PowerOn
	case kitty.StatusStarting:
		p.State = kitty.PowerStarting
	case kitty.StatusError:
		p.State, p.Error = kitty.PowerError, k.Error
	default:
		p.State, p.OffReason = kitty.PowerOff, k.OffReason
	}
	if p.State != kitty.PowerOn {
		return p, nil
	}
	if by := m.inUse(k.ID, false); by != "" {
		p.InUse, p.InUseBy = true, by
		return p, nil
	}
	if last, ok := m.use.lastUse(k.ID); ok {
		idle := m.now().Sub(last)
		p.IdleSeconds = int(idle.Seconds())
		if p.AutoOffMinutes > 0 {
			left := max(int((time.Duration(p.AutoOffMinutes)*time.Minute - idle).Seconds()), 0)
			p.OffInSeconds = &left
		}
	}
	return p, nil
}

// PowerOn turns a kitty's machine on without waiting for it: it is starting until its desktop is
// up. A machine already on or starting is left as it is.
func (m *Manager) PowerOn(ref string) (*kitty.Kitty, error) {
	k, err := m.Get(ref)
	if err != nil {
		return nil, err
	}
	if !k.Desktop {
		return nil, kitty.ErrNoMachine
	}
	if k.Status == kitty.StatusRunning || k.Status == kitty.StatusStarting {
		m.use.touch(k.ID, m.now())
		return k, nil
	}
	m.use.touch(k.ID, m.now())
	return m.StartInBackground(k.ID)
}

// powerPoll is how often PowerOnWait looks at a machine that is starting.
var powerPoll = 500 * time.Millisecond

// PowerOnWait turns the machine on and waits until it is up, for at most wait: a kitty about to
// use its display. It says so when the machine is still starting after that (the first start
// downloads an image) or could not start.
func (m *Manager) PowerOnWait(ctx context.Context, ref string, wait time.Duration) error {
	k, err := m.PowerOn(ref)
	if err != nil {
		return err
	}
	deadline := time.After(wait)
	for k.Status != kitty.StatusRunning {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("%s's machine is still starting after %s (the first start downloads its image): try again in a minute", k.Name, wait.Round(time.Second))
		case <-time.After(powerPoll):
		}
		if k, err = m.Get(k.ID); err != nil {
			return err
		}
		if k.Status == kitty.StatusError {
			return fmt.Errorf("%s's machine could not start: %s", k.Name, k.Error)
		}
		if k.Status == kitty.StatusStopped {
			return fmt.Errorf("%s's machine was turned off while it started", k.Name)
		}
	}
	m.use.touch(k.ID, m.now())
	return nil
}

// PowerOff turns a kitty's machine off, keeping its sessions. A machine that something is using
// is not turned off (see kitty.OffOptions).
func (m *Manager) PowerOff(ctx context.Context, ref string, o kitty.OffOptions) error {
	k, err := m.Get(ref)
	if err != nil {
		return err
	}
	if !k.Desktop {
		return kitty.ErrNoMachine
	}
	if !o.Force {
		if by := m.inUse(k.ID, o.Self); by != "" {
			return &kitty.InUseError{Kitty: k.Name, By: by}
		}
	}
	if o.Reason == "" {
		o.Reason = kitty.OffManual
	}
	if err := m.Stop(ctx, k.ID); err != nil {
		return err
	}
	return m.setOffReason(k.ID, o.Reason)
}

func (m *Manager) setOffReason(id, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.kitties[id]
	if !ok {
		return ErrNotFound
	}
	k.OffReason = reason
	return m.saveLocked()
}

// SetOnPowerOff sets what hears about each machine that turned itself off for being idle.
func (m *Manager) SetOnPowerOff(fn func(k *kitty.Kitty, idle time.Duration)) { m.onPowerOff = fn }

// idleCheck turns a running machine off when it has not been used for its kitty's idle time.
// The first look at a machine only starts the count, so a gateway that just came up does not turn
// off a machine that was in use before it.
func (m *Manager) idleCheck(ctx context.Context, k *kitty.Kitty, warn func(format string, args ...any)) {
	limit := time.Duration(m.AutoOffMinutes(k)) * time.Minute
	if limit <= 0 {
		return
	}
	last, seen := m.use.lastUse(k.ID)
	if !seen {
		m.use.touch(k.ID, m.now())
		return
	}
	if m.inUse(k.ID, false) != "" {
		// What is in use now keeps the count at zero: it starts when the use ends.
		m.use.touch(k.ID, m.now())
		return
	}
	idle := m.now().Sub(last)
	if idle < limit {
		return
	}
	if err := m.PowerOff(ctx, k.ID, kitty.OffOptions{Reason: kitty.OffIdle}); err != nil {
		var busy *kitty.InUseError
		if !errors.As(err, &busy) {
			warn("watchdog: %s: could not turn off after %s idle: %v", k.Name, idle.Round(time.Second), err)
		}
		return
	}
	warn("watchdog: %s's machine turned off after %s without use", k.Name, idle.Round(time.Minute))
	if m.onPowerOff != nil {
		m.onPowerOff(k, idle)
	}
}

// watchedConn is a live view's connection that tells the manager when it is closed.
type watchedConn struct {
	net.Conn
	done func()
	once *sync.Once
}

func (w watchedConn) Close() error {
	err := w.Conn.Close()
	w.once.Do(w.done)
	return err
}

// Compute is where a kitty's desktop runs: "local" (a container on this machine) or "cloud". It
// follows the configuration (the kitty's own compute, else the deployment's); a kitty without a
// desktop has none.
func (m *Manager) Compute(ref string) (string, error) {
	k, err := m.Get(ref)
	if err != nil {
		return "", err
	}
	if !k.Desktop {
		return "", kitty.ErrNoMachine
	}
	if m.cfg.IsCloudContainer(kitty.ContainerName(k.Name)) {
		return config.ComputeCloud, nil
	}
	return config.ComputeLocal, nil
}
