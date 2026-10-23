package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/kitty"
)

// Docker Desktop opened after the gateway: it is found on the next round, and the kitties that
// wanted a body get one, the running ones started in it.
func TestRuntimeFoundLater(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	var found Containers
	tries := 0
	m, err := New(testConfig(t), WithContainers(nil, errors.New("docker does not answer")), WithRuntimeRetry(func(context.Context) (Containers, error) {
		tries++
		if found == nil {
			return nil, errors.New("docker does not answer")
		}
		return found, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	k, err := m.Create(ctx, Spec{Name: "mimi"})
	if err != nil || k.Desktop || !k.BodyLater || tries != 1 {
		t.Fatalf("created without docker = %+v %v (%d tries)", k, err, tries)
	}
	no := false
	plain, _ := m.Create(ctx, Spec{Name: "plain", Desktop: &no})
	if plain.BodyLater {
		t.Error("a kitty that wants no body waits for none")
	}
	if err := m.Start(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	var warned []string
	warn := func(format string, args ...any) { warned = append(warned, format) }
	m.Check(ctx, func(*kitty.Kitty, string) {}, warn)
	if has, why := m.HasContainers(); has || why == nil {
		t.Errorf("still nothing = %v %v", has, why)
	}
	found = f
	m.Check(ctx, func(*kitty.Kitty, string) {}, warn)
	if has, why := m.HasContainers(); !has || why != nil {
		t.Errorf("found = %v %v", has, why)
	}
	got, _ := m.Get("mimi")
	if !got.Desktop || got.BodyLater || got.Container == "" || got.Workspace == "" || !f.running[got.Container] {
		t.Errorf("mimi after = %+v, running %v", got, f.running)
	}
	if p, _ := m.Get("plain"); p.Desktop {
		t.Error("plain got a body")
	}
	if len(warned) != 1 || !strings.Contains(warned[0], "answers now") {
		t.Errorf("warned = %v", warned)
	}
	// Found once, it is not looked for again.
	before := tries
	if m.FindRuntime(ctx) || tries != before {
		t.Error("looked again")
	}
}
