package orchestrator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/config"
	"github.com/madkoding/kitties/internal/provider"
)

func TestSettingsProviders(t *testing.T) {
	cfg := testConfig(t)
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	view := m.Settings()
	if len(view.Providers) != 2 || !view.Providers[0].FromFile || !view.Providers[0].Default || len(view.Kinds) == 0 || len(view.ConnectorKinds) < 20 || len(view.Catalog) != len(view.ConnectorKinds) || view.MotitaURL != "http://m" {
		t.Errorf("view = %+v", view)
	}
	// Add a profile from the page: it needs a key, so it is kept with its reason.
	t.Setenv("OPENAI_API_KEY", "")
	if err := m.SetProvider("fast", config.Provider{Kind: "openai", Model: "gpt-5.5"}); err != nil {
		t.Fatal(err)
	}
	v := find(m.Settings().Providers, "fast")
	if v == nil || v.FromFile || v.APIKeySet || !strings.Contains(v.Error, "API key") {
		t.Errorf("keyless profile = %+v", v)
	}
	if _, err := m.TestProvider(context.Background(), "fast"); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Errorf("TestProvider reports the build error: %v", err)
	}
	// With a key it builds; an edit with an empty key keeps it.
	if err := m.SetProvider("fast", config.Provider{Kind: "openai", Model: "gpt-5.5", APIKey: "sk-1"}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetProvider("fast", config.Provider{Kind: "openai", Model: "gpt-5.5-mini"}); err != nil {
		t.Fatal(err)
	}
	v = find(m.Settings().Providers, "fast")
	if v == nil || !v.APIKeySet || v.Error != "" || v.Model != "gpt-5.5-mini" || v.Describe != "openai/gpt-5.5-mini" {
		t.Errorf("edited profile = %+v", v)
	}
	// Validation.
	if err := m.SetProvider("Bad", config.Provider{Kind: "openai"}); err == nil {
		t.Error("bad name")
	}
	if err := m.SetProvider("x", config.Provider{Kind: "nope"}); err == nil {
		t.Error("bad kind")
	}
	// Default.
	if err := m.SetDefaultProvider("fast"); err != nil || m.Config().DefaultProvider != "fast" {
		t.Error("SetDefaultProvider")
	}
	if err := m.SetDefaultProvider("zz"); !errors.Is(err, ErrNotFound) {
		t.Error("unknown default")
	}
	// The overlay survives a restart and wins over the file.
	again, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if v := find(again.Settings().Providers, "fast"); v == nil || v.Model != "gpt-5.5-mini" || again.Config().DefaultProvider != "fast" {
		t.Errorf("after restart = %+v", v)
	}
	// Shadowing and removing a file profile.
	if err := again.SetProvider("p", config.Provider{Kind: "ollama", Model: "other"}); err != nil {
		t.Fatal(err)
	}
	if v := find(again.Settings().Providers, "p"); v.FromFile || v.Model != "other" {
		t.Errorf("shadowed = %+v", v)
	}
	if err := again.RemoveProvider("fast"); err != nil || again.Config().DefaultProvider == "fast" {
		t.Errorf("RemoveProvider: %v default=%s", err, again.Config().DefaultProvider)
	}
	if err := again.RemoveProvider("q"); err != nil {
		t.Error(err)
	}
	if err := again.RemoveProvider("q"); !errors.Is(err, ErrNotFound) {
		t.Error("remove twice")
	}
	third, _ := New(cfg)
	names := []string{}
	for _, p := range third.Settings().Providers {
		names = append(names, p.Name)
	}
	if strings.Join(names, ",") != "p" {
		t.Errorf("removed file profiles stay removed: %v", names)
	}
	if err := third.SetProvider("q", config.Provider{Kind: "ollama", Model: "back"}); err != nil {
		t.Fatal(err)
	}
	if len(third.Settings().Providers) != 2 || len(third.overlay.Removed.Providers) != 1 {
		t.Error("a removed name comes back when set again")
	}
	// The test call reaches the client.
	reg := provider.NewRegistry()
	reg.Add("fake", provider.Func(func(context.Context, []provider.Message) (provider.Reply, error) {
		return provider.Reply{Content: " hi "}, nil
	}), provider.Options{})
	reg.Add("broken", provider.Func(func(context.Context, []provider.Message) (provider.Reply, error) {
		return provider.Reply{}, errors.New("down")
	}), provider.Options{})
	faked, _ := New(cfg, WithProviders(reg))
	if out, err := faked.TestProvider(context.Background(), "fake"); err != nil || out != "hi" {
		t.Errorf("TestProvider = %q %v", out, err)
	}
	if _, err := faked.TestProvider(context.Background(), "broken"); err == nil {
		t.Error("a failing provider")
	}
	if _, err := faked.TestProvider(context.Background(), "zz"); err == nil {
		t.Error("unknown provider")
	}
}

func TestSettingsConnectors(t *testing.T) {
	cfg := testConfig(t)
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetConnector("team", config.Connector{Kind: "slack", URL: "https://hooks/x", Description: "team channel"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Connectors().Get("team"); !ok {
		t.Error("the connector is live")
	}
	if err := m.SetConnector("tg2", config.Connector{Kind: "telegram", ChatID: "1"}); err == nil {
		t.Error("telegram needs a token")
	}
	if err := m.SetConnector("gh", config.Connector{Kind: "github", Settings: map[string]string{"token": "t1", "repo": "a/b"}}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetConnector("gh", config.Connector{Kind: "github", Settings: map[string]string{"repo": "a/c"}}); err != nil {
		t.Errorf("an empty secret setting keeps the saved one: %v", err)
	}
	if v := findC(m.Settings().Connectors, "gh"); v == nil || v.Settings["repo"] != "a/c" || v.Settings["token"] != "" || len(v.SecretsSet) != 1 || !v.TokenSet || v.Category != "git" {
		t.Errorf("github view = %+v", v)
	}
	if err := m.SetConnector("gh", config.Connector{Kind: "gitlab", URL: "https://g"}); err == nil {
		t.Error("a change of kind does not carry the old secret")
	}
	if err := m.SetConnector("tg", config.Connector{Kind: "telegram", ChatID: "9"}); err != nil {
		t.Errorf("an empty token keeps the file's: %v", err)
	}
	if v := findC(m.Settings().Connectors, "tg"); v == nil || !v.TokenSet || v.ChatID != "9" || v.FromFile {
		t.Errorf("edited connector = %+v", v)
	}
	if err := m.SetConnector("Bad", config.Connector{Kind: "shell", Command: "x"}); err == nil {
		t.Error("bad name")
	}
	if err := m.RemoveConnector("hook"); err != nil {
		t.Error(err)
	}
	if err := m.RemoveConnector("hook"); !errors.Is(err, ErrNotFound) {
		t.Error("remove twice")
	}
	again, _ := New(cfg)
	if v := findC(again.Settings().Connectors, "hook"); v != nil {
		t.Error("removed connectors stay removed")
	}
	if v := findC(again.Settings().Connectors, "team"); v == nil || v.Description != "team channel" {
		t.Error("added connectors survive a restart")
	}
	if _, ok := again.Connectors().Get("team"); !ok {
		t.Error("and are live")
	}
	empty := config.Config{DataDir: t.TempDir()}
	e, _ := New(empty)
	if err := e.SetConnector("w", config.Connector{Kind: "webhook", URL: "http://x"}); err != nil {
		t.Error(err)
	}
	if err := e.SetProvider("l", config.Provider{Kind: "ollama", Model: "m"}); err != nil || e.Config().DefaultProvider != "l" {
		t.Error("the first profile becomes the default")
	}
}

func TestSettingsFileFailures(t *testing.T) {
	cfg := testConfig(t)
	_ = os.MkdirAll(cfg.DataDir, 0o700)
	_ = os.WriteFile(filepath.Join(cfg.DataDir, "settings.json"), []byte("{"), 0o600)
	if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Errorf("damaged settings: %v", err)
	}
	_ = os.Remove(filepath.Join(cfg.DataDir, "settings.json"))
	_ = os.Mkdir(filepath.Join(cfg.DataDir, "settings.json"), 0o700)
	if _, err := New(cfg); err == nil {
		t.Error("unreadable settings")
	}
	_ = os.Remove(filepath.Join(cfg.DataDir, "settings.json"))
	m, _ := New(cfg)
	_ = os.WriteFile(filepath.Join(cfg.DataDir, "kitties.json"), []byte("[]"), 0o600)
	m.path = filepath.Join(cfg.DataDir, "kitties.json", "x", "kitties.json")
	if err := m.SetProvider("l", config.Provider{Kind: "ollama", Model: "m"}); err == nil {
		t.Error("a save failure is reported")
	}
	if len(without([]string{"a", "b"}, "a")) != 1 || !contains([]string{"a"}, "a") {
		t.Error("helpers")
	}
}

func find(list []ProviderView, name string) *ProviderView {
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}

func findC(list []ConnectorView, name string) *ConnectorView {
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}
