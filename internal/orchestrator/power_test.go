package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/kitty"
)

// clock is a manager's time, moved by the test.
type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

// powerManager is a manager with one running kitty with a desktop (solo) and a clock of its own.
func powerManager(t *testing.T) (*Manager, *fakeContainers, *clock, *kitty.Kitty) {
	t.Helper()
	f := newFake()
	m, err := New(testConfig(t), WithContainers(f, nil))
	if err != nil {
		t.Fatal(err)
	}
	clk := &clock{t: time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)}
	m.now = clk.now
	if err := m.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	solo, err := m.Get("solo")
	if err != nil || !solo.Desktop || solo.Status != kitty.StatusRunning {
		t.Fatalf("solo = %+v %v", solo, err)
	}
	return m, f, clk, solo
}

func check(m *Manager) (warned []string) {
	m.Check(context.Background(), func(*kitty.Kitty, string) {}, func(format string, a ...any) { warned = append(warned, fmt.Sprintf(format, a...)) })
	return warned
}

func TestPowerStateAndDefault(t *testing.T) {
	m, _, _, solo := powerManager(t)
	p, err := m.Power(solo.ID)
	if err != nil || p.State != kitty.PowerOn || p.AutoOffMinutes != 15 || p.AutoOffCustom || p.InUse {
		t.Fatalf("power = %+v %v", p, err)
	}
	if p.OffInSeconds == nil || *p.OffInSeconds != 15*60 {
		t.Errorf("off in = %v", p.OffInSeconds)
	}
	if _, err := m.Power("team-1"); !errors.Is(err, kitty.ErrNoMachine) {
		t.Errorf("a kitty without a desktop has no machine: %v", err)
	}
	if _, err := m.Power("zz"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown = %v", err)
	}
}

// A machine nobody uses turns itself off after the idle time (15 minutes by default), keeps its
// sessions, and says why it is off; the watchdog does not bring it back.
func TestMachineTurnsOffWhenIdle(t *testing.T) {
	m, f, clk, solo := powerManager(t)
	var heard time.Duration
	m.SetOnPowerOff(func(k *kitty.Kitty, idle time.Duration) { heard = idle })
	check(m) // counts from here
	clk.advance(14 * time.Minute)
	if w := check(m); len(w) != 0 || !f.running["kitty-solo"] {
		t.Fatalf("turned off early: %v", w)
	}
	clk.advance(2 * time.Minute)
	warned := check(m)
	if f.running["kitty-solo"] || len(warned) != 1 || !strings.Contains(warned[0], "turned off") || heard < 15*time.Minute {
		t.Fatalf("running = %v warned = %v heard = %v", f.running["kitty-solo"], warned, heard)
	}
	p, _ := m.Power(solo.ID)
	if p.State != kitty.PowerOff || p.OffReason != kitty.OffIdle {
		t.Errorf("power = %+v", p)
	}
	check(m)
	if f.running["kitty-solo"] {
		t.Error("the watchdog brought an idle machine back")
	}
	// Turning it on again clears the reason and restarts the count.
	if _, err := m.PowerOn(solo.ID); err != nil {
		t.Fatal(err)
	}
	k := waitStatus(t, m, solo.ID, kitty.StatusRunning)
	if k.OffReason != "" || !f.running["kitty-solo"] {
		t.Errorf("kitty = %+v", k)
	}
	clk.advance(10 * time.Minute)
	check(m)
	if !f.running["kitty-solo"] {
		t.Error("the count did not restart when it was turned on")
	}
}

// Anything in use keeps it on, and the time without use counts from the end of the use.
func TestMachineInUseIsNotTurnedOff(t *testing.T) {
	m, f, clk, solo := powerManager(t)
	check(m)

	// A task of the kitty.
	busy := true
	m.SetBusy(func(string) bool { return busy })
	clk.advance(time.Hour)
	check(m)
	if !f.running["kitty-solo"] {
		t.Fatal("turned off in the middle of a task")
	}
	if p, _ := m.Power(solo.ID); !p.InUse || p.InUseBy != kitty.UseTask || p.OffInSeconds != nil {
		t.Errorf("power = %+v", p)
	}
	busy = false
	clk.advance(14 * time.Minute)
	check(m)
	if !f.running["kitty-solo"] {
		t.Fatal("the idle count did not start when the task ended")
	}

	// The person watching (a live view open) and controlling.
	stream, err := m.Stream(context.Background(), solo.ID, 5, 60)
	if err != nil {
		t.Fatal(err)
	}
	clk.advance(time.Hour)
	check(m)
	if !f.running["kitty-solo"] {
		t.Fatal("turned off while the person watches")
	}
	if p, _ := m.Power(solo.ID); p.InUseBy != kitty.UseWatch {
		t.Errorf("power = %+v", p)
	}
	_ = stream.Close()
	_ = stream.Close() // closing twice counts once
	if _, streams := m.use.running(solo.ID); streams != 0 {
		t.Errorf("streams = %d", streams)
	}
	if _, err := m.Hold(solo.ID, true); err != nil {
		t.Fatal(err)
	}
	if p, _ := m.Power(solo.ID); p.InUseBy != kitty.UseControl {
		t.Errorf("power = %+v", p)
	}
	clk.advance(4 * time.Minute)
	check(m)
	if !f.running["kitty-solo"] {
		t.Fatal("turned off while the person controls")
	}
	_, _ = m.Hold(solo.ID, false)

	// A command in flight.
	done := m.use.enter(solo.ID, m.now)
	clk.advance(time.Hour)
	check(m)
	if !f.running["kitty-solo"] {
		t.Fatal("turned off in the middle of a command")
	}
	done()

	// And an action on the display restarts the count.
	clk.advance(10 * time.Minute)
	if _, err := m.Screenshot(context.Background(), solo.ID); err != nil {
		t.Fatal(err)
	}
	clk.advance(10 * time.Minute)
	check(m)
	if !f.running["kitty-solo"] {
		t.Fatal("an action did not count as use")
	}
	clk.advance(6 * time.Minute)
	check(m)
	if f.running["kitty-solo"] {
		t.Error("never turned off once idle")
	}
}

func TestAutoOffSettings(t *testing.T) {
	m, f, clk, solo := powerManager(t)
	check(m)
	// Never.
	never := 0
	k, err := m.Edit(solo.ID, Update{AutoOff: &never})
	if err != nil || k.AutoOff == nil || *k.AutoOff != 0 {
		t.Fatalf("edit = %+v %v", k, err)
	}
	clk.advance(30 * 24 * time.Hour)
	check(m)
	if !f.running["kitty-solo"] {
		t.Fatal("a machine set to never turned off")
	}
	p, _ := m.Power(solo.ID)
	if p.AutoOffMinutes != 0 || !p.AutoOffCustom || p.OffInSeconds != nil {
		t.Errorf("power = %+v", p)
	}
	// A shorter time.
	five := 5
	if _, err := m.Edit(solo.ID, Update{AutoOff: &five}); err != nil {
		t.Fatal(err)
	}
	clk.advance(6 * time.Minute)
	check(m)
	if f.running["kitty-solo"] {
		t.Error("five minutes did not turn it off")
	}
	// It survives a restart, and the default comes back.
	m2, _ := New(m.cfg, WithContainers(f, nil))
	if k, _ := m2.Get("solo"); k.AutoOff == nil || *k.AutoOff != 5 {
		t.Errorf("after a restart = %+v", k)
	}
	yes := true
	k, err = m.Edit(solo.ID, Update{AutoOffDefault: &yes})
	if err != nil || k.AutoOff != nil || m.AutoOffMinutes(k) != 15 {
		t.Errorf("default = %+v %v", k, err)
	}
	for _, bad := range []int{-1, kitty.MaxAutoOffMinutes + 1} {
		if _, err := m.Edit(solo.ID, Update{AutoOff: &bad}); err == nil || !strings.Contains(err.Error(), "auto_off_minutes") {
			t.Errorf("%d = %v", bad, err)
		}
	}
}

func TestPowerOffAndOn(t *testing.T) {
	ctx := context.Background()
	m, f, _, solo := powerManager(t)
	busy := true
	m.SetBusy(func(string) bool { return busy })

	// In use: refused, unless forced; the kitty itself may turn its own machine off mid-task.
	var inUse *kitty.InUseError
	if err := m.PowerOff(ctx, solo.ID, kitty.OffOptions{}); !errors.As(err, &inUse) || inUse.By != kitty.UseTask || !f.running["kitty-solo"] {
		t.Fatalf("PowerOff in use = %v", err)
	}
	if !strings.Contains(inUse.Error(), "solo") {
		t.Errorf("error = %q", inUse)
	}
	_, _ = m.Hold(solo.ID, true)
	if err := m.PowerOff(ctx, solo.ID, kitty.OffOptions{Self: true, Reason: kitty.OffKitty}); !errors.As(err, &inUse) || inUse.By != kitty.UseControl {
		t.Fatalf("the kitty turned off a machine the person controls: %v", err)
	}
	_, _ = m.Hold(solo.ID, false)
	if err := m.PowerOff(ctx, solo.ID, kitty.OffOptions{Self: true, Reason: kitty.OffKitty}); err != nil {
		t.Fatal(err)
	}
	if k, _ := m.Get(solo.ID); k.Status != kitty.StatusStopped || k.OffReason != kitty.OffKitty {
		t.Errorf("kitty = %+v", k)
	}

	// On again, and forced off.
	if _, err := m.PowerOn(solo.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, solo.ID, kitty.StatusRunning)
	if err := m.PowerOff(ctx, solo.ID, kitty.OffOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	if p, _ := m.Power(solo.ID); p.State != kitty.PowerOff || p.OffReason != kitty.OffManual {
		t.Errorf("power = %+v", p)
	}
	if err := m.PowerOff(ctx, "team-1", kitty.OffOptions{}); !errors.Is(err, kitty.ErrNoMachine) {
		t.Errorf("no desktop = %v", err)
	}
	if _, err := m.PowerOn("team-1"); !errors.Is(err, kitty.ErrNoMachine) {
		t.Errorf("no desktop = %v", err)
	}
}

func TestPowerOnWait(t *testing.T) {
	ctx := context.Background()
	m, f, _, solo := powerManager(t)
	powerPoll = time.Millisecond
	defer func() { powerPoll = 500 * time.Millisecond }()
	_ = m.Stop(ctx, solo.ID)
	if err := m.PowerOnWait(ctx, solo.ID, 3*time.Second); err != nil || !f.running["kitty-solo"] {
		t.Fatalf("PowerOnWait = %v", err)
	}
	// Already on: nothing to wait for.
	if err := m.PowerOnWait(ctx, solo.ID, time.Second); err != nil {
		t.Fatal(err)
	}
	// It cannot start: the error says so.
	_ = m.Stop(ctx, solo.ID)
	f.fail = "start"
	err := m.PowerOnWait(ctx, solo.ID, 3*time.Second)
	if err == nil || !strings.Contains(err.Error(), "could not start") {
		t.Errorf("failing start = %v", err)
	}
}

// A live view that is closed twice counts once, and an Exec in flight is a command.
func TestUseTrackerCounts(t *testing.T) {
	m, _, _, solo := powerManager(t)
	var u useTracker
	end := u.enter(solo.ID, m.now)
	if execs, _ := u.running(solo.ID); execs != 1 {
		t.Errorf("execs = %d", execs)
	}
	end()
	if execs, _ := u.running(solo.ID); execs != 0 {
		t.Errorf("execs = %d", execs)
	}
	rc := watchedStream{ReadCloser: io.NopCloser(strings.NewReader("")), done: u.watch(solo.ID, m.now)}
	if _, s := u.running(solo.ID); s != 1 {
		t.Errorf("streams = %d", s)
	}
	_ = rc.Close()
	if _, s := u.running(solo.ID); s != 0 {
		t.Errorf("streams = %d", s)
	}
}

func TestComputeFollowsTheConfiguration(t *testing.T) {
	m, _, _, solo := powerManager(t)
	if got, err := m.Compute(solo.ID); err != nil || got != "local" {
		t.Fatalf("compute = %q %v", got, err)
	}
	m.cfg.Containers.Runtime = "cloud"
	if got, _ := m.Compute(solo.ID); got != "cloud" {
		t.Errorf("a cloud deployment: %q", got)
	}
	if _, err := m.Compute("team-1"); !errors.Is(err, kitty.ErrNoMachine) {
		t.Errorf("no desktop: %v", err)
	}
	if _, err := m.Compute("zz"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
}
