package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/container"
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
	_ = m.Stop(ctx, "mimi")
	off = false
	l.calls = nil
	if err := m.Start(ctx, "mimi"); err != nil {
		t.Fatal(err)
	}
	if c := strings.Join(l.calls, "|"); !strings.Contains(c, "rm|up") || l.off[k.Container] {
		t.Fatalf("the setting changed but the machine was not made anew: %q", c)
	}
	k.Lead = true
	off = true
	if m.noLAN(k) {
		t.Fatal("the lead's machine is never off the local network")
	}
}
