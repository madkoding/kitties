package gateway

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/orchestrator"
)

// memoryFleet is a power fleet that tells its machines' memory and sizes them.
type memoryFleet struct {
	*powerFleet
	memErr   error
	defaults orchestrator.MachineDefaults
}

func (m *memoryFleet) Memory(context.Context, string) (kitty.Memory, error) {
	return kitty.Memory{TotalMB: 2048, UsedMB: 512, AvailableMB: 1536}, m.memErr
}

func (m *memoryFleet) Machines() orchestrator.MachinesView {
	return orchestrator.MachinesView{MachineDefaults: m.defaults, EngineMB: 8192}
}

func (m *memoryFleet) SetMachineDefaults(_ context.Context, d orchestrator.MachineDefaults) error {
	if d.MemoryMB < kitty.MinMemoryMB {
		return errors.New("memory_mb is too small")
	}
	m.defaults = d
	return nil
}

func TestMachinesAndMemory(t *testing.T) {
	f := &fakeFleet{kitties: map[string]*kitty.Kitty{"a1": {ID: "a1", Name: "ana", Desktop: true, Status: kitty.StatusRunning}}}
	chats := &fakeChats{sessions: map[string]*chat.Session{}}
	plain := httptest.NewServer(New(f, chats, "tok", "test"))
	defer plain.Close()
	if code, _, _ := call(t, plain, "GET", "/v1/settings/machines", "tok", nil); code != 404 {
		t.Errorf("without machines = %d", code)
	}
	if code, _, _ := call(t, plain, "PUT", "/v1/settings/machines", "tok", `{}`); code != 404 {
		t.Errorf("without machines = %d", code)
	}
	m := &memoryFleet{powerFleet: &powerFleet{fakeFleet: f, power: kitty.Power{State: kitty.PowerOn}},
		defaults: orchestrator.MachineDefaults{MemoryMB: 2048, SwapMB: 1024}}
	srv := httptest.NewServer(New(m, chats, "tok", "test"))
	defer srv.Close()
	code, got, _ := call(t, srv, "GET", "/v1/kitties/ana/power", "tok", nil)
	if mem, _ := got["memory"].(map[string]any); code != 200 || mem["available_mb"] != float64(1536) {
		t.Errorf("power with memory = %d %v", code, got)
	}
	m.memErr = errors.New("cannot tell")
	if _, got, _ = call(t, srv, "GET", "/v1/kitties/ana/power", "tok", nil); got["memory"] != nil {
		t.Errorf("unknown memory is left out: %v", got)
	}
	if code, got, _ = call(t, srv, "GET", "/v1/settings/machines", "tok", nil); code != 200 || got["memory_mb"] != float64(2048) || got["engine_mb"] != float64(8192) {
		t.Errorf("machines = %d %v", code, got)
	}
	if code, got, _ = call(t, srv, "PUT", "/v1/settings/machines", "tok", `{"memory_mb": 4096, "swap_mb": 0}`); code != 200 || got["memory_mb"] != float64(4096) || got["swap_mb"] != float64(0) {
		t.Errorf("set = %d %v", code, got)
	}
	if code, _, body := call(t, srv, "PUT", "/v1/settings/machines", "tok", `{"memory_mb": 1}`); code != 400 || !strings.Contains(string(body), "too small") {
		t.Errorf("too small = %d %s", code, body)
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/settings/machines", "tok", `nope`); code != 400 {
		t.Errorf("bad body = %d", code)
	}
}

type memComputeFleet struct{ *memoryFleet }

func (memComputeFleet) Compute(string) (string, error) { return "local", nil }

func TestKittyComputeTellsMemory(t *testing.T) {
	f := &fakeFleet{kitties: map[string]*kitty.Kitty{"a1": {ID: "a1", Name: "ana", Desktop: true, Status: kitty.StatusRunning}}}
	m := &memoryFleet{powerFleet: &powerFleet{fakeFleet: f, power: kitty.Power{State: kitty.PowerOn, MemoryMB: 2048}}}
	s := New(memComputeFleet{m}, &fakeChats{sessions: map[string]*chat.Session{}}, "gw", "test").WithMCP(mcpToken, false)
	text, _, _ := tool(t, s, "kitty_compute", map[string]any{"ref": "ana"})
	if !strings.Contains(text, `"available_mb": 1536`) || !strings.Contains(text, `"memory_mb": 2048`) {
		t.Errorf("kitty_compute = %q", text)
	}
}
