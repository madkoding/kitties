package orchestrator

import (
	"context"
	"testing"

	"github.com/madkoding/kitties/internal/config"
	"github.com/madkoding/kitties/internal/kitty"
)

func TestOneLead(t *testing.T) {
	ctx := context.Background()
	m, err := New(testConfig(t), WithContainers(newFake(), nil))
	if err != nil {
		t.Fatal(err)
	}
	if m.Lead() != nil {
		t.Fatal("no lead to begin with")
	}
	ana, _ := m.Create(ctx, Spec{Name: "ana", Lead: true})
	if l := m.Lead(); l == nil || l.ID != ana.ID {
		t.Fatalf("lead = %v", l)
	}
	// A second lead, made or edited, takes it from the first: there is one at most.
	bo, err := m.Create(ctx, Spec{Name: "bo", Lead: true})
	if err != nil {
		t.Fatal(err)
	}
	if l := m.Lead(); l == nil || l.ID != bo.ID {
		t.Fatalf("lead after bo = %v", l)
	}
	if a, _ := m.Get("ana"); a.Lead {
		t.Error("ana is no longer the lead")
	}
	yes, no := true, false
	if _, err := m.Edit("ana", Update{Lead: &yes}); err != nil {
		t.Fatal(err)
	}
	leads := 0
	for _, k := range m.List() {
		if k.Lead {
			leads++
		}
	}
	if leads != 1 || m.Lead().Name != "ana" {
		t.Errorf("leads = %d, lead %v", leads, m.Lead())
	}
	// It is kept: a manager reading the same state finds it.
	again, err := New(m.cfg, WithContainers(newFake(), nil))
	if err != nil {
		t.Fatal(err)
	}
	if l := again.Lead(); l == nil || l.Name != "ana" {
		t.Errorf("reloaded lead = %v", l)
	}
	if _, err := m.Edit("ana", Update{Lead: &no}); err != nil || m.Lead() != nil {
		t.Errorf("no lead = %v %v", m.Lead(), err)
	}
}

func TestLeadFromConfig(t *testing.T) {
	cfg := testConfig(t)
	cfg.Kitties = []config.Kitty{{Name: "ana"}, {Name: "bo", Lead: true}}
	m, err := New(cfg, WithContainers(newFake(), nil))
	if err != nil {
		t.Fatal(err)
	}
	_ = m.Bootstrap(context.Background())
	if l := m.Lead(); l == nil || l.Name != "bo" {
		t.Fatalf("lead = %v", l)
	}
	// One chosen later wins over the file on the next start.
	yes := true
	if _, err := m.Edit("ana", Update{Lead: &yes}); err != nil {
		t.Fatal(err)
	}
	_ = m.Bootstrap(context.Background())
	if l := m.Lead(); l == nil || l.Name != "ana" {
		t.Errorf("lead after a second bootstrap = %v", l)
	}
}

func TestSetTaskLimits(t *testing.T) {
	m, err := New(testConfig(t), WithContainers(newFake(), nil))
	if err != nil {
		t.Fatal(err)
	}
	k, _ := m.Create(context.Background(), Spec{Name: "ana", Limits: kitty.Limits{MaxTokens: 100, Retries: 2}})
	r, sec := 80, 600
	got, err := m.SetTaskLimits("ana", kitty.TaskLimits{MaxRounds: &r, MaxSeconds: &sec})
	if err != nil || got.Limits.MaxRounds != 80 || got.Limits.MaxSeconds != 600 || got.Limits.MaxTokens != 100 || got.Limits.Retries != 2 {
		t.Fatalf("set = %+v %v", got, err)
	}
	over := kitty.MaxRoundsCap + 1
	if _, err := m.SetTaskLimits("ana", kitty.TaskLimits{MaxRounds: &over}); err == nil {
		t.Error("past the cap")
	}
	if _, err := m.SetTaskLimits("nobody", kitty.TaskLimits{MaxRounds: &r}); err == nil {
		t.Error("unknown kitty")
	}
	if k, _ = m.Get(k.ID); k.Limits.MaxRounds != 80 {
		t.Errorf("kept = %d", k.Limits.MaxRounds)
	}
}
