package orchestrator

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// zonedContainers is the fake runtime that also tells each container's time zone.
type zonedContainers struct {
	*fakeContainers
	zone    map[string]string
	zoneErr error
	asked   []string
}

func (z *zonedContainers) ZoneStale(_ context.Context, name, zone string) (bool, error) {
	z.asked = append(z.asked, zone)
	return z.zone[name] != zone, z.zoneErr
}

func zonedManager(t *testing.T) (*Manager, *zonedContainers, *memKeeper) {
	t.Helper()
	z := &zonedContainers{fakeContainers: newFake(), zone: map[string]string{}}
	m, err := New(testConfig(t), WithContainers(z, nil))
	if err != nil {
		t.Fatal(err)
	}
	keeper := &memKeeper{data: map[string][]byte{}}
	m.SetKeeper(keeper)
	return m, z, keeper
}

// A desktop made in another zone than the host's is made anew when it starts, its sessions kept.
func TestADesktopFollowsTheHostsTimeZone(t *testing.T) {
	ctx := context.Background()
	t.Setenv("TZ", "America/Santiago")
	m, z, keeper := zonedManager(t)
	k, _ := m.Create(ctx, Spec{Name: "mimi"})
	_ = m.Start(ctx, "mimi")
	z.zone[k.Container] = "America/Santiago"
	keeper.data[k.ID] = []byte("kept")
	_ = m.Stop(ctx, "mimi")

	// Same zone: started as it is.
	z.calls = nil
	if err := m.Start(ctx, "mimi"); err != nil {
		t.Fatal(err)
	}
	if c := strings.Join(z.calls, "|"); c != "list|start|exec" || z.asked[len(z.asked)-1] != "America/Santiago" {
		t.Errorf("same zone: calls %q asked %v", c, z.asked)
	}

	// The host moved while it was stopped: made anew, its sessions put back.
	_ = m.Stop(ctx, "mimi")
	t.Setenv("TZ", "Europe/Madrid")
	z.calls, z.inputs = nil, nil
	if err := m.Start(ctx, "mimi"); err != nil {
		t.Fatal(err)
	}
	if c := strings.Join(z.calls, "|"); !strings.Contains(c, "rm|up") || len(z.inputs) != 1 {
		t.Errorf("stopped, zone moved: calls %q inputs %v", c, z.inputs)
	}

	// The host moved while it runs (a gateway that starts again): its sessions are backed up first.
	z.zone[k.Container] = "America/Santiago"
	z.outputs = map[string]string{"kitty-backup": base64.StdEncoding.EncodeToString([]byte("fresh"))}
	z.calls = nil
	if err := m.Start(ctx, "mimi"); err != nil {
		t.Fatal(err)
	}
	if c := strings.Join(z.calls, "|"); !strings.Contains(c, "rm|up") || string(keeper.data[k.ID]) != "fresh" {
		t.Errorf("running, zone moved: calls %q kept %q", c, keeper.data[k.ID])
	}

	// Never while someone uses it: the person holds it, or the kitty works.
	z.zone[k.Container] = "America/Santiago"
	_, _ = m.Hold("mimi", true)
	z.calls = nil
	_ = m.Start(ctx, "mimi")
	_, _ = m.Hold("mimi", false)
	m.SetBusy(func(string) bool { return true })
	_ = m.Start(ctx, "mimi")
	if c := strings.Join(z.calls, "|"); strings.Contains(c, "rm") || strings.Contains(c, "up") {
		t.Errorf("a desktop in use was made anew: %q", c)
	}
	m.SetBusy(nil)

	// A look that fails is no move.
	z.zoneErr = errors.New("inspect failed")
	z.calls = nil
	_ = m.Start(ctx, "mimi")
	if c := strings.Join(z.calls, "|"); strings.Contains(c, "rm") {
		t.Errorf("a failed look made it anew: %q", c)
	}
}

// The zone asked for is the configuration's own TZ when it sets one.
func TestADesktopKeepsTheConfiguredTimeZone(t *testing.T) {
	ctx := context.Background()
	t.Setenv("TZ", "America/Santiago")
	m, z, _ := zonedManager(t)
	m.cfg.Containers.Env = map[string]string{"TZ": "Asia/Tokyo"}
	k, _ := m.Create(ctx, Spec{Name: "mimi"})
	_ = m.Start(ctx, "mimi")
	z.zone[k.Container] = "Asia/Tokyo"
	_ = m.Stop(ctx, "mimi")
	z.calls = nil
	_ = m.Start(ctx, "mimi")
	if c := strings.Join(z.calls, "|"); c != "list|start|exec" || z.asked[len(z.asked)-1] != "Asia/Tokyo" {
		t.Errorf("calls %q asked %v", c, z.asked)
	}
}

// A runtime that cannot tell a zone never makes a desktop anew for it.
func TestARuntimeWithoutZonesIsLeftAlone(t *testing.T) {
	ctx := context.Background()
	m, f, _ := sessionsManager(t)
	_, _ = m.Create(ctx, Spec{Name: "mimi"})
	_ = m.Start(ctx, "mimi")
	_ = m.Stop(ctx, "mimi")
	f.calls = nil
	_ = m.Start(ctx, "mimi")
	if c := strings.Join(f.calls, "|"); c != "list|start|exec" {
		t.Errorf("calls %q", c)
	}
}
