package orchestrator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/container"
	"github.com/madkoding/kitties/internal/kitty"
)

// limitFake is a runtime that can change limits in place (unless setErr) and tells whether a
// container's differ (stale).
type limitFake struct {
	*fakeContainers
	specs  []container.Spec
	set    []string
	setErr error
	stale  bool
	engine int
}

func (l *limitFake) Up(ctx context.Context, s container.Spec) (string, error) {
	l.specs = append(l.specs, s)
	return l.fakeContainers.Up(ctx, s)
}

func (l *limitFake) SetLimits(_ context.Context, name string, memoryMB, swapMB int) error {
	l.set = append(l.set, name)
	if l.setErr == nil {
		l.stale = false
	}
	return l.setErr
}

func (l *limitFake) LimitsStale(context.Context, string, int, int) (bool, error) { return l.stale, nil }

func (l *limitFake) EngineMemoryMB(context.Context) (int, error) {
	if l.engine == 0 {
		return 0, errors.New("cannot tell")
	}
	return l.engine, nil
}

func limitManager(t *testing.T) (*Manager, *limitFake, *kitty.Kitty) {
	t.Helper()
	f := &limitFake{fakeContainers: newFake(), engine: 8192}
	m, err := New(testConfig(t), WithContainers(f, nil))
	if err != nil {
		t.Fatal(err)
	}
	clk := &clock{t: time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)}
	m.now = clk.now
	if err := m.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	solo, err := m.Get("solo")
	if err != nil {
		t.Fatal(err)
	}
	return m, f, solo
}

func TestMemoryLimitsPerKittyAndDefaults(t *testing.T) {
	m, f, solo := limitManager(t)
	if len(f.specs) != 1 || f.specs[0].MemoryMB != 2048 || f.specs[0].SwapMB != kitty.DefaultSwapMB {
		t.Fatalf("made with the defaults: %+v", f.specs)
	}
	p, _ := m.Power(solo.ID)
	if p.MemoryMB != 2048 || p.SwapMB != 1024 || p.MemoryCustom || !p.MemoryLocal {
		t.Errorf("power = %+v", p)
	}
	mem, swap := 4096, 0
	k, err := m.Edit(solo.ID, Update{MemoryMB: &mem, SwapMB: &swap})
	if err != nil || *k.MemoryMB != 4096 || *k.SwapMB != 0 || k.MemoryPending || len(f.set) != 1 {
		t.Fatalf("edit = %+v %v %v", k, err, f.set)
	}
	if p, _ = m.Power(solo.ID); p.MemoryMB != 4096 || p.SwapMB != 0 || !p.MemoryCustom {
		t.Errorf("own limits = %+v", p)
	}
	// What the engine cannot change in place waits for the next start, which makes it anew.
	f.setErr, f.stale = errors.New("no swap accounting"), true
	mem = 3072
	if k, err = m.Edit(solo.ID, Update{MemoryMB: &mem, SwapMB: &swap}); err != nil || !k.MemoryPending {
		t.Fatalf("pending = %+v %v", k, err)
	}
	ctx := context.Background()
	if err := m.Start(ctx, solo.ID); err != nil {
		t.Fatal(err)
	}
	if k, _ = m.Get(solo.ID); !k.MemoryPending {
		t.Error("a running machine kept as it is still waits")
	}
	if err := m.PowerOff(ctx, solo.ID, kitty.OffOptions{}); err != nil {
		t.Fatal(err)
	}
	f.stale = true
	f.fakeContainers.calls = nil
	if err := m.Start(ctx, solo.ID); err != nil {
		t.Fatal(err)
	}
	f.stale = false
	if last := f.specs[len(f.specs)-1]; last.MemoryMB != 3072 || !strings.Contains(strings.Join(f.fakeContainers.calls, " "), "rm") {
		t.Errorf("made anew with the new limits: %+v %v", last, f.fakeContainers.calls)
	}
	if k, _ = m.Get(solo.ID); k.MemoryPending {
		t.Error("no longer pending")
	}
	yes := true
	if k, _ = m.Edit(solo.ID, Update{MemoryDefault: &yes}); k.MemoryMB != nil || k.SwapMB != nil {
		t.Errorf("back to the defaults: %+v", k)
	}
	// Bad values.
	big, neg := 9000, -1
	for _, u := range []Update{{MemoryMB: &mem}, {MemoryMB: &mem, SwapMB: &neg}, {MemoryMB: &big, SwapMB: &swap}} {
		if _, err := m.Edit(solo.ID, u); err == nil {
			t.Errorf("%+v accepted", u)
		}
	}
}

func TestMachineDefaults(t *testing.T) {
	m, f, solo := limitManager(t)
	if v := m.Machines(); v.MemoryMB != 2048 || v.SwapMB != 1024 || v.EngineMB != 8192 {
		t.Errorf("view = %+v", v)
	}
	ctx := context.Background()
	if err := m.SetMachineDefaults(ctx, MachineDefaults{MemoryMB: 3072, SwapMB: 512}); err != nil {
		t.Fatal(err)
	}
	if len(f.set) != 1 || f.set[0] != solo.Container {
		t.Errorf("the machines without their own get them: %v", f.set)
	}
	if p, _ := m.Power(solo.ID); p.MemoryMB != 3072 || p.SwapMB != 512 || p.MemoryCustom {
		t.Errorf("power = %+v", p)
	}
	for _, d := range []MachineDefaults{{MemoryMB: 100}, {MemoryMB: 16384}} {
		if err := m.SetMachineDefaults(ctx, d); err == nil {
			t.Errorf("%+v accepted", d)
		}
	}
	// The defaults are kept in the settings and come back.
	again, err := New(m.cfg, WithContainers(newFake(), nil))
	if err != nil {
		t.Fatal(err)
	}
	if d := again.MachineDefaults(); d.MemoryMB != 3072 || d.SwapMB != 512 {
		t.Errorf("reloaded = %+v", d)
	}
	if again.Machines().EngineMB != 0 {
		t.Error("a runtime that cannot tell its memory")
	}
	data, _ := os.ReadFile(filepath.Join(filepath.Dir(m.path), "settings.json"))
	if !strings.Contains(string(data), `"machines"`) {
		t.Errorf("settings = %s", data)
	}
}

func TestMemoryReading(t *testing.T) {
	m, f, solo := limitManager(t)
	f.outputs = map[string]string{"sh": "memory.max=2147483648\nmemory.current=1073741824\nMemTotal: 16384000 kB\nMemAvailable: 8192000 kB\n--- ps\n 102400 chromium\n"}
	ctx := context.Background()
	mem, err := m.Memory(ctx, solo.ID)
	if err != nil || mem.TotalMB != 2048 || mem.UsedMB != 1024 || len(mem.Top) != 1 {
		t.Fatalf("memory = %+v %v", mem, err)
	}
	execs := len(f.execs)
	if _, err := m.Memory(ctx, solo.ID); err != nil || len(f.execs) != execs {
		t.Error("a fresh reading is used again")
	}
	m.forgetMemory(solo.ID)
	f.outputs["sh"] = "nothing"
	if _, err := m.Memory(ctx, solo.ID); err == nil {
		t.Error("a machine that tells nothing")
	}
	f.fail = "exec"
	if _, err := m.Memory(ctx, solo.ID); err == nil {
		t.Error("a failed command")
	}
	f.fail = ""
	if _, err := m.Memory(ctx, "team-1"); !errors.Is(err, kitty.ErrNoMachine) {
		t.Errorf("no machine: %v", err)
	}
	if _, err := m.Memory(ctx, "ghost"); err == nil {
		t.Error("unknown kitty")
	}
	if err := m.PowerOff(ctx, solo.ID, kitty.OffOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Memory(ctx, solo.ID); err == nil {
		t.Error("a machine that is off")
	}
}
