package orchestrator

import (
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/config"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/provider"
)

func TestBrainWithFallback(t *testing.T) {
	m, err := New(testConfig(t), WithContainers(newFake(), nil))
	if err != nil {
		t.Fatal(err)
	}
	k := &kitty.Kitty{Provider: "p"}
	// No fallback: the profile's own client.
	if c, _ := m.Brain(k); c.Describe() != "ollama/m" {
		t.Errorf("plain = %s", c.Describe())
	}
	// The page names q as p's fallback: the brain is backed by it, and the view says so.
	if err := m.SetProvider("p", config.Provider{Kind: "ollama", Model: "m", Fallback: "q"}); err != nil {
		t.Fatal(err)
	}
	c, err := m.Brain(k)
	if _, ok := c.(*provider.Fallback); err != nil || !ok || c.Describe() != "ollama/m (fallback ollama/n)" {
		t.Errorf("with fallback = %v %v", c, err)
	}
	if v := find(m.Settings().Providers, "p"); v == nil || v.Fallback != "q" {
		t.Errorf("view = %+v", v)
	}
	// A fallback that is not built (removed since) is skipped; an unknown one is refused.
	m.providers.Remove("q")
	if c, _ := m.Brain(k); c.Describe() != "ollama/m" {
		t.Errorf("missing fallback = %s", c.Describe())
	}
	if err := m.SetProvider("p", config.Provider{Kind: "ollama", Model: "m", Fallback: "zz"}); err == nil || !strings.Contains(err.Error(), "not a configured profile") {
		t.Errorf("unknown fallback: %v", err)
	}
	if _, err := m.Brain(&kitty.Kitty{Provider: "zz"}); err == nil {
		t.Error("unknown profile")
	}
}

func TestSettingsAutonomy(t *testing.T) {
	cfg := testConfig(t)
	m, err := New(cfg, WithContainers(newFake(), nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetAutonomy(config.Autonomy{BrainWait: "later"}); err == nil {
		t.Error("a bad wait is refused")
	}
	a := config.Autonomy{FollowUps: "5m 1h", BrainWait: "off"}
	if err := m.SetAutonomy(a); err != nil {
		t.Fatal(err)
	}
	if m.Autonomy() != a || m.Settings().Autonomy != a {
		t.Errorf("autonomy = %+v", m.Autonomy())
	}
	// It is kept across a restart, over the file.
	m2, err := New(cfg, WithContainers(newFake(), nil))
	if err != nil || m2.Autonomy() != a {
		t.Errorf("after a restart: %+v %v", m2.Autonomy(), err)
	}
}
