package orchestrator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/config"
	"github.com/madkoding/kitties/internal/oauth"
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
	if err := m.SetConnector("gh", config.Connector{Kind: "discord"}); err == nil {
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

func TestOAuthStore(t *testing.T) {
	cfg := testConfig(t)
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if m.OAuth() == nil {
		t.Fatal("the manager has an OAuth manager")
	}
	if err := m.SetConnector("gw", config.Connector{Kind: "google-workspace", Settings: map[string]string{"client_id": "cid", "client_secret": "sec"}}); err != nil {
		t.Fatal(err)
	}
	app, err := m.OAuthApp("gw")
	if err != nil || app.Provider != "google" || app.Kind != "google-workspace" || app.ClientID != "cid" || app.ClientSecret != "sec" {
		t.Errorf("OAuthApp = %+v %v", app, err)
	}
	if _, err := m.OAuthApp("zz"); err == nil {
		t.Error("unknown connector")
	}
	if _, err := m.OAuthApp("dc"); err == nil {
		t.Error("discord does not sign in")
	}
	if app, err := m.OAuthApp("sl"); err != nil || app.Provider != "slack" {
		t.Errorf("slack signs in too: %+v %v", app, err)
	}
	// What a sign-in learns goes into the settings, next to what was there.
	if err := m.SaveOAuthSettings("gw", map[string]string{"page_id": "P1"}); err != nil {
		t.Fatal(err)
	}
	if app, _ := m.OAuthApp("gw"); app.Settings["page_id"] != "P1" || app.ClientID != "cid" {
		t.Errorf("learned = %+v", app)
	}
	if err := m.SaveOAuthSettings("zz", nil); err == nil {
		t.Error("an unknown connector learns nothing")
	}
	if names := m.OAuthConnectors(); strings.Join(names, ",") != "gw,sl" {
		t.Errorf("OAuthConnectors = %v", names)
	}
	// The slack connector has a webhook of its own: no sign-in needed; the google one has none.
	views := map[string]ConnectorView{}
	for _, v := range m.Settings().Connectors {
		views[v.Name] = v
	}
	if views["sl"].NeedsSignIn || !views["gw"].NeedsSignIn || views["gw"].SignedIn {
		t.Errorf("needs_sign_in: sl=%+v gw=%+v", views["sl"], views["gw"])
	}
	// The refresh loop runs until its context ends, and asks nothing when nothing is due.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.KeepTokensFresh(ctx, func(string, ...any) { t.Error("nothing to warn about") })
		close(done)
	}()
	cancel()
	<-done
	if c, err := m.OAuthCredentials("gw"); err != nil || c.SignedIn() {
		t.Errorf("not signed in yet: %+v %v", c, err)
	}
	if _, err := m.OAuthCredentials("zz"); err == nil {
		t.Error("unknown connector credentials")
	}
	creds := oauth.Credentials{AccessToken: "at", RefreshToken: "rt", Expiry: time.Now().Add(time.Hour), Account: "me@x"}
	if err := m.SaveOAuthCredentials("gw", creds); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveOAuthCredentials("zz", creds); err == nil {
		t.Error("save for unknown connector")
	}
	v := findC(m.Settings().Connectors, "gw")
	if v == nil || v.OAuth != "google" || !v.SignedIn || v.Account != "me@x" || v.Settings["oauth_access_token"] != "" || v.Settings["client_id"] != "cid" {
		t.Errorf("view = %+v", v)
	}
	// An edit of the other fields keeps the sign-in; the token source reads it back.
	if err := m.SetConnector("gw", config.Connector{Kind: "google-workspace", Description: "mail", Settings: map[string]string{"client_id": "cid2"}}); err != nil {
		t.Fatal(err)
	}
	if c, _ := m.OAuthCredentials("gw"); !c.SignedIn() || c.Account != "me@x" {
		t.Errorf("sign-in survives an edit: %+v", c)
	}
	if app, _ := m.OAuthApp("gw"); app.ClientSecret != "sec" {
		t.Error("the secret survives an edit")
	}
	if tok, err := m.OAuth().Token(context.Background(), "gw"); err != nil || tok != "at" {
		t.Errorf("Token = %q %v", tok, err)
	}
	again, _ := New(cfg)
	if c, _ := again.OAuthCredentials("gw"); !c.SignedIn() {
		t.Error("the sign-in survives a restart")
	}
	conn, ok := again.Connectors().Get("gw")
	if !ok {
		t.Fatal("connector built")
	}
	if _, err := conn.Call(context.Background(), map[string]any{"to": "a@x", "text": "hi"}); err == nil || strings.Contains(err.Error(), "not signed in") {
		t.Errorf("the token source is wired: %v", err)
	}
}

func TestSettingsAccounts(t *testing.T) {
	m, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetProvider("plan", config.Provider{Kind: "codex", Auth: "login", APIKey: "sk-not-kept"}); err != nil {
		t.Fatal(err)
	}
	v := find(m.Settings().Providers, "plan")
	if v == nil || v.Auth != "login" || v.Error != "" || v.Describe != "codex/gpt-5.5-codex" {
		t.Errorf("signed-in profile = %+v", v)
	}
	if ready := strings.Join(m.Settings().OAuthReady, ","); !strings.Contains(ready, "home-assistant") || !strings.Contains(ready, "mastodon") {
		t.Errorf("ready = %s", ready)
	}
	if len(m.Settings().Accounts) == 0 || m.Accounts() == nil {
		t.Error("the page lists the accounts")
	}
	if _, err := m.TestProvider(context.Background(), "plan"); err == nil || !strings.Contains(err.Error(), "sign in from Settings") {
		t.Errorf("a profile without its account = %v", err)
	}
}
