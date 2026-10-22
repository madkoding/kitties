package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/config"
	"github.com/madkoding/kitties/internal/kitty"
)

func TestEditBrainAndConnections(t *testing.T) {
	m, err := New(testConfig(t), WithContainers(newFake(), nil))
	if err != nil {
		t.Fatal(err)
	}
	k, err := m.Create(context.Background(), Spec{Name: "mimi", Desktop: new(bool)})
	if err != nil {
		t.Fatal(err)
	}
	q, conns := "q", []string{"hook", "sl"}
	got, err := m.Edit(k.ID, Update{Provider: &q, Connectors: &conns})
	if err != nil || got.Provider != "q" || strings.Join(got.Connectors, ",") != "hook,sl" {
		t.Fatalf("edit = %+v %v", got, err)
	}
	none := []string{}
	if got, _ = m.Edit(k.ID, Update{Connectors: &none}); len(got.Connectors) != 0 {
		t.Errorf("connections cleared = %v", got.Connectors)
	}
	bad := "nope"
	if _, err := m.Edit(k.ID, Update{Provider: &bad}); err == nil || !strings.Contains(err.Error(), `there is no model called "nope"`) {
		t.Errorf("unknown model: %v", err)
	}
	badC := []string{"nope"}
	if _, err := m.Edit(k.ID, Update{Connectors: &badC}); err == nil || !strings.Contains(err.Error(), `there is no connection called "nope"`) {
		t.Errorf("unknown connection: %v", err)
	}
	if _, err := m.Create(context.Background(), Spec{Name: "x", Connectors: []string{"nope"}}); err == nil || !strings.Contains(err.Error(), "no connection called") {
		t.Errorf("create with an unknown connection: %v", err)
	}
}

func TestNoModelSaysWhatToDo(t *testing.T) {
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	m, err := New(cfg, WithContainers(newFake(), nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(context.Background(), Spec{Name: "mimi"}); !errors.Is(err, ErrNoModel) || !strings.Contains(err.Error(), "add one in Settings") {
		t.Errorf("no model: %v", err)
	}
	// A model that cannot be built is named with its reason.
	t.Setenv("OPENAI_API_KEY", "")
	if err := m.SetProvider("brain", config.Provider{Kind: "openai", Model: "gpt-5.5"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(context.Background(), Spec{Name: "mimi", Provider: "brain"}); err == nil || !strings.Contains(err.Error(), `the model "brain" cannot be used yet`) || !strings.Contains(err.Error(), "API key") {
		t.Errorf("unusable model: %v", err)
	}
}

func TestRemovingAModelMovesItsKitties(t *testing.T) {
	m, err := New(testConfig(t), WithContainers(newFake(), nil))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	k, err := m.Create(ctx, Spec{Name: "mimi", Provider: "q"})
	if err != nil {
		t.Fatal(err)
	}
	// Its model goes: it moves to the default one.
	if err := m.RemoveProvider("q"); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.Get(k.ID); got.Provider != "p" {
		t.Errorf("mimi thinks with %q, want the default p", got.Provider)
	}
	// The last model in use stays: the kitties would have no brain.
	if err := m.RemoveProvider("p"); err == nil || !strings.Contains(err.Error(), "still think with") || !strings.Contains(err.Error(), "mimi") {
		t.Errorf("last model: %v", err)
	}
	if _, err := m.Get(k.ID); err != nil {
		t.Fatal(err)
	}
	// Without kitties on it, the last model can go.
	for _, x := range m.List() {
		if err := m.Remove(ctx, x.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.RemoveProvider("p"); err != nil {
		t.Errorf("unused last model: %v", err)
	}
}

func TestSettingsSayWhoUsesWhat(t *testing.T) {
	m, err := New(testConfig(t), WithContainers(newFake(), nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(context.Background(), Spec{Name: "mimi", Provider: "q", Connectors: []string{"hook"}}); err != nil {
		t.Fatal(err)
	}
	v := m.Settings()
	if q := find(v.Providers, "q"); q == nil || strings.Join(q.UsedBy, ",") != "mimi" {
		t.Errorf("q used by %+v", q)
	}
	if h := findC(v.Connectors, "hook"); h == nil || strings.Join(h.UsedBy, ",") != "mimi" {
		t.Errorf("hook used by %+v", h)
	}
	if sl := findC(v.Connectors, "sl"); sl == nil || len(sl.UsedBy) != 0 {
		t.Errorf("sl used by %+v", sl)
	}
}

func TestConnectionsAllNoneOrSome(t *testing.T) {
	m, err := New(testConfig(t), WithContainers(newFake(), nil))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	all, _ := m.Create(ctx, Spec{Name: "all"})
	some, _ := m.Create(ctx, Spec{Name: "some", Connectors: []string{"hook"}})
	none, err := m.Create(ctx, Spec{Name: "none", Connectors: []string{kitty.NoConnectors}})
	if err != nil {
		t.Fatal(err)
	}
	v := m.Settings()
	if h := findC(v.Connectors, "hook"); strings.Join(h.UsedBy, ",") != "all,some" {
		t.Errorf("hook used by %v", h.UsedBy)
	}
	if sl := findC(v.Connectors, "sl"); strings.Join(sl.UsedBy, ",") != "all" {
		t.Errorf("sl used by %v", sl.UsedBy)
	}
	// Removing the only connection of a kitty leaves it with none, not with all.
	if err := m.RemoveConnector("hook"); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.Get(some.ID); strings.Join(got.Connectors, ",") != kitty.NoConnectors {
		t.Errorf("some = %v", got.Connectors)
	}
	if got, _ := m.Get(all.ID); len(got.Connectors) != 0 {
		t.Errorf("all = %v", got.Connectors)
	}
	if got, _ := m.Get(none.ID); strings.Join(got.Connectors, ",") != kitty.NoConnectors {
		t.Errorf("none = %v", got.Connectors)
	}
	// "None" alone is a valid list, mixed with names it is not a connection.
	mixed := []string{kitty.NoConnectors, "sl"}
	if _, err := m.Edit(all.ID, Update{Connectors: &mixed}); err == nil {
		t.Error("a mixed list is refused")
	}
}
