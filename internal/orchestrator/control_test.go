package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/desktop"
)

func TestHoldAndInput(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	m, err := New(testConfig(t), WithContainers(f, nil))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	k, _ := m.Create(ctx, Spec{Name: "mimi"})
	no := false
	brain, _ := m.Create(ctx, Spec{Name: "brain", Desktop: &no})
	if m.Held(k.ID) {
		t.Error("held from the start")
	}
	if on, err := m.Hold("mimi", true); !on || err != nil || !m.Held(k.ID) {
		t.Errorf("hold = %v %v", on, err)
	}
	if on, err := m.Hold("mimi", false); on || err != nil || m.Held(k.ID) {
		t.Errorf("give back = %v %v", on, err)
	}
	if _, err := m.Hold("brain", true); err == nil || m.Held(brain.ID) {
		t.Error("a kitty without a desktop cannot be held")
	}
	if _, err := m.Hold("nobody", true); err == nil {
		t.Error("unknown kitty")
	}
	// The person's click runs, and takes the display over.
	if err := m.Input(ctx, "mimi", desktop.Action{Name: "click", Args: map[string]any{"x": 5.0, "y": 6.0}}); err != nil {
		t.Fatal(err)
	}
	if !m.Held(k.ID) || !strings.Contains(strings.Join(f.calls, " "), "exec") {
		t.Errorf("input = held %v, calls %v", m.Held(k.ID), f.calls)
	}
	if err := m.Input(ctx, "mimi", desktop.Action{Name: "run", Args: map[string]any{"argv": []any{"rm"}}}); err == nil {
		t.Error("the person's input runs no programs")
	}
	if err := m.Input(ctx, "nobody", desktop.Action{Name: "key", Args: map[string]any{"keys": "a"}}); err == nil {
		t.Error("unknown kitty input")
	}
	// A hold left alone ends by itself.
	now = now.Add(holdIdle + time.Second)
	if m.Held(k.ID) {
		t.Error("an idle hold ends")
	}
}
