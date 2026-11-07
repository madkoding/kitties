package orchestrator

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/madkoding/kitties/internal/kitty"
)

// THE MEMORY OF THE MACHINE.
//
// Each kitty's machine has so much RAM and swap: its own, chosen in its settings, or the
// defaults for every kitty (Settings › Machines, else containers.memory_mb and swap_mb). A change
// reaches a machine that exists at once when the engine can (docker update); otherwise it waits
// until the machine is turned on again, when it is made anew with the new limits. A machine in
// the cloud has its provider's size instead.
//
// What a machine uses now is read inside it (kitty.MemoryScript), without counting as a use, and
// kept a few seconds so a kitty asking at every round does not run a command each time.

// MachineDefaults are the RAM and swap of every kitty's machine that has none of its own.
type MachineDefaults struct {
	MemoryMB int `json:"memory_mb"`
	SwapMB   int `json:"swap_mb"`
}

// MachinesView is the defaults as the settings page shows them, with what the engine has.
type MachinesView struct {
	MachineDefaults
	// EngineMB is the memory the engine can give all its containers (0 when it cannot tell).
	EngineMB int `json:"engine_mb,omitempty"`
}

// limiter is a runtime that can change a container's limits and tell whether they differ.
type limiter interface {
	SetLimits(ctx context.Context, name string, memoryMB, swapMB int) error
	LimitsStale(ctx context.Context, name string, memoryMB, swapMB int) (bool, error)
}

// engineSizer is a runtime that can tell the memory it has.
type engineSizer interface {
	EngineMemoryMB(ctx context.Context) (int, error)
}

// memoryCache keeps the last reading of each machine's memory for a few seconds.
type memoryCache struct {
	mu   sync.Mutex
	read map[string]memoryReading
	// engine is the engine's memory once read, 0 before.
	engine int
}

type memoryReading struct {
	at  time.Time
	mem kitty.Memory
}

// memoryFresh is how long a reading of a machine's memory is used again.
var memoryFresh = 10 * time.Second

// MemoryLimits are the RAM and swap of a kitty's machine, in MB, and whether they are its own.
func (m *Manager) MemoryLimits(k *kitty.Kitty) (memoryMB, swapMB int, custom bool) {
	d := m.MachineDefaults()
	memoryMB, swapMB = d.MemoryMB, d.SwapMB
	if k.MemoryMB != nil {
		memoryMB, custom = *k.MemoryMB, true
	}
	if k.SwapMB != nil {
		swapMB, custom = *k.SwapMB, true
	}
	return memoryMB, swapMB, custom
}

// MachineDefaults are the RAM and swap of the machines without values of their own.
func (m *Manager) MachineDefaults() MachineDefaults {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return MachineDefaults{MemoryMB: m.cfg.Containers.MemoryMB, SwapMB: m.cfg.Containers.SwapMB}
}

// Machines is the defaults with the engine's memory, for the settings page.
func (m *Manager) Machines() MachinesView {
	return MachinesView{MachineDefaults: m.MachineDefaults(), EngineMB: m.engineMB()}
}

// engineMB is the engine's memory, read once (it changes only when the engine is set up again).
func (m *Manager) engineMB() int {
	m.mem.mu.Lock()
	defer m.mem.mu.Unlock()
	if m.mem.engine > 0 {
		return m.mem.engine
	}
	s, ok := m.ctr().(engineSizer)
	if !ok {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if n, err := s.EngineMemoryMB(ctx); err == nil {
		m.mem.engine = n
	}
	return m.mem.engine
}

// fitsEngine refuses more RAM than the engine has.
func (m *Manager) fitsEngine(memoryMB int) error {
	if engine := m.engineMB(); engine > 0 && memoryMB > engine {
		return errors.New("memory_mb is more than the " + kitty.MBText(engine) + " the container engine has: give the engine more memory first (Docker Desktop › Settings › Resources)")
	}
	return nil
}

// SetMachineDefaults changes the RAM and swap of every machine without values of its own. The
// machines that exist get them at once when the engine can, or when they are turned on again.
func (m *Manager) SetMachineDefaults(ctx context.Context, d MachineDefaults) error {
	if err := kitty.ValidMemory(d.MemoryMB, d.SwapMB); err != nil {
		return err
	}
	if err := m.fitsEngine(d.MemoryMB); err != nil {
		return err
	}
	m.mu.Lock()
	m.cfg.Containers.MemoryMB, m.cfg.Containers.SwapMB = d.MemoryMB, d.SwapMB
	m.overlay.Machines = &d
	err := m.saveSettings()
	m.mu.Unlock()
	if err != nil {
		return err
	}
	for _, k := range m.List() {
		if k.Desktop && k.MemoryMB == nil && k.SwapMB == nil {
			m.applyLimits(ctx, k)
		}
	}
	return nil
}

// applyLimits gives a kitty's machine its limits now, when it exists and the engine can; when it
// cannot, the machine is marked to get them the next time it is turned on.
func (m *Manager) applyLimits(ctx context.Context, k *kitty.Kitty) {
	if !k.Desktop || k.Container == "" || m.cfg.IsCloudContainer(k.Container) {
		return
	}
	l, ok := m.ctr().(limiter)
	if !ok {
		return
	}
	memoryMB, swapMB, _ := m.MemoryLimits(k)
	pending := false
	if exists, _, err := m.containerState(ctx, k.Container); err == nil && exists {
		pending = memoryMB <= 0 || l.SetLimits(ctx, k.Container, memoryMB, swapMB) != nil
	}
	m.forgetMemory(k.ID)
	_ = m.setMemoryPending(k.ID, pending)
}

func (m *Manager) setMemoryPending(id string, pending bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.kitties[id]
	if !ok {
		return ErrNotFound
	}
	if k.MemoryPending == pending {
		return nil
	}
	k.MemoryPending = pending
	return m.saveLocked()
}

// limitsMoved tells whether a kitty's container has other limits than it should: it is then
// made anew when it starts. A runtime that cannot tell, or a failed look, is taken for no change.
func (m *Manager) limitsMoved(ctx context.Context, k *kitty.Kitty) bool {
	l, ok := m.ctr().(limiter)
	if !ok {
		return false
	}
	memoryMB, swapMB, _ := m.MemoryLimits(k)
	moved, err := l.LimitsStale(ctx, k.Container, memoryMB, swapMB)
	return err == nil && moved
}

// Memory is what a kitty's machine has and uses now. It is read inside the machine, which must be
// on, and does not count as a use of it.
func (m *Manager) Memory(ctx context.Context, ref string) (kitty.Memory, error) {
	k, err := m.Get(ref)
	if err != nil {
		return kitty.Memory{}, err
	}
	if !k.Desktop {
		return kitty.Memory{}, kitty.ErrNoMachine
	}
	if k.Status != kitty.StatusRunning {
		return kitty.Memory{}, errors.New(k.Name + "'s machine is not on")
	}
	m.mem.mu.Lock()
	if r, ok := m.mem.read[k.ID]; ok && m.now().Sub(r.at) < memoryFresh {
		m.mem.mu.Unlock()
		return r.mem, nil
	}
	m.mem.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := m.ctr().Exec(ctx, k.Container, kitty.MemoryArgv...)
	if err != nil {
		return kitty.Memory{}, err
	}
	mem, ok := kitty.ParseMemory(out)
	if !ok {
		return kitty.Memory{}, errors.New("the machine did not tell its memory")
	}
	m.mem.mu.Lock()
	defer m.mem.mu.Unlock()
	if m.mem.read == nil {
		m.mem.read = map[string]memoryReading{}
	}
	m.mem.read[k.ID] = memoryReading{at: m.now(), mem: mem}
	return mem, nil
}

// forgetMemory drops the last reading of a machine, after something changed it.
func (m *Manager) forgetMemory(id string) {
	m.mem.mu.Lock()
	defer m.mem.mu.Unlock()
	delete(m.mem.read, id)
}
