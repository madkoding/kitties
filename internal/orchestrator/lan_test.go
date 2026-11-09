package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/container"
	"github.com/madkoding/kitties/internal/kitty"
)

// lanContainers is the fake runtime that remembers whether each container is off the local network.
type lanContainers struct {
	*fakeContainers
	off map[string]bool
}

func (l *lanContainers) Up(ctx context.Context, s container.Spec) (string, error) {
	l.off[s.Name] = s.NoLAN
	return l.fakeContainers.Up(ctx, s)
}

func (l *lanContainers) LANStale(_ context.Context, name string, noLAN bool) (bool, error) {
	return l.off[name] != noLAN, nil
}

// The WhatsApp kitty's machine is made off the local network, and made anew when that changes;
// the lead's never is.
func TestOffLANMachines(t *testing.T) {
	ctx := context.Background()
	l := &lanContainers{fakeContainers: newFake(), off: map[string]bool{}}
	m, err := New(testConfig(t), WithContainers(l, nil))
	if err != nil {
		t.Fatal(err)
	}
	k, _ := m.Create(ctx, Spec{Name: "mimi"})
	off := true
	m.SetOffLAN(func(id string) bool { return off && id == k.ID })
	if err := m.Start(ctx, "mimi"); err != nil {
		t.Fatal(err)
	}
	if !l.off[k.Container] {
		t.Fatal("the WhatsApp kitty's machine is on the local network")
	}
	// An image that cannot close it says so.
	if got, _ := m.Get("mimi"); got.LocalNetwork != kitty.LANFailed {
		t.Fatalf("local network = %q, want failed", got.LocalNetwork)
	}
	l.outputs = map[string]string{"cat": "closed\n"}
	_ = m.Stop(ctx, "mimi")
	if err := m.Start(ctx, "mimi"); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.Get("mimi"); got.LocalNetwork != kitty.LANClosed {
		t.Fatalf("local network = %q, want closed", got.LocalNetwork)
	}
	_ = m.Stop(ctx, "mimi")
	off = false
	l.calls = nil
	if err := m.Start(ctx, "mimi"); err != nil {
		t.Fatal(err)
	}
	if c := strings.Join(l.calls, "|"); !strings.Contains(c, "rm|up") || l.off[k.Container] {
		t.Fatalf("the setting changed but the machine was not made anew: %q", c)
	}
	if got, _ := m.Get("mimi"); got.LocalNetwork != "" {
		t.Fatalf("local network = %q once it may reach it", got.LocalNetwork)
	}
	k.Lead = true
	off = true
	if m.noLAN(k) {
		t.Fatal("the lead's machine is never off the local network")
	}
}
