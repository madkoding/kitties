package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/madkoding/kitties/internal/config"
	"github.com/madkoding/kitties/internal/connector"
	"github.com/madkoding/kitties/internal/oauth"
	"github.com/madkoding/kitties/internal/provider"
)

// Settings are what the interface can change at run time: the AI profiles and the connectors.
// They are kept in <data_dir>/settings.json on top of the configuration file, and win over it
// entry by entry, so a key typed in the page is never written back into the YAML.
type Settings struct {
	Providers       map[string]config.Provider  `json:"providers"`
	Connectors      map[string]config.Connector `json:"connectors"`
	DefaultProvider string                      `json:"default_provider,omitempty"`
	// Removed names entries of the file the person deleted from the page.
	Removed struct {
		Providers  []string `json:"providers,omitempty"`
		Connectors []string `json:"connectors,omitempty"`
	} `json:"removed"`
}

func (m *Manager) settingsPath() string {
	return filepath.Join(filepath.Dir(m.path), "settings.json")
}

// loadSettings reads the overlay and merges it into the configuration.
func (m *Manager) loadSettings() error {
	data, err := os.ReadFile(m.settingsPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read the settings: %w", err)
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("the settings file is damaged (%s): %w", m.settingsPath(), err)
	}
	m.overlay = s
	if m.cfg.Providers == nil {
		m.cfg.Providers = map[string]config.Provider{}
	}
	if m.cfg.Connectors == nil {
		m.cfg.Connectors = map[string]config.Connector{}
	}
	for _, n := range s.Removed.Providers {
		delete(m.cfg.Providers, n)
	}
	for _, n := range s.Removed.Connectors {
		delete(m.cfg.Connectors, n)
	}
	for n, p := range s.Providers {
		m.cfg.Providers[n] = p
	}
	for n, c := range s.Connectors {
		m.cfg.Connectors[n] = c
	}
	if s.DefaultProvider != "" {
		m.cfg.DefaultProvider = s.DefaultProvider
	}
	return nil
}

func (m *Manager) saveSettings() error {
	data, err := json.MarshalIndent(m.overlay, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.settingsPath()), 0o700); err != nil {
		return fmt.Errorf("create the data folder: %w", err)
	}
	tmp := m.settingsPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("save the settings: %w", err)
	}
	return os.Rename(tmp, m.settingsPath())
}

func without(list []string, name string) []string {
	out := list[:0]
	for _, x := range list {
		if x != name {
			out = append(out, x)
		}
	}
	return out
}

// ProviderView is one AI profile as the page sees it: the key is never sent, only whether one is set.
type ProviderView struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Model       string   `json:"model"`
	BaseURL     string   `json:"base_url,omitempty"`
	APIKeySet   bool     `json:"api_key_set"`
	APIKeyEnv   string   `json:"api_key_env,omitempty"`
	MaxTokens   int      `json:"max_tokens,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	Default     bool     `json:"default"`
	Describe    string   `json:"describe"`
	// FromFile is true when the profile comes from the YAML (an edit shadows it, a delete hides it).
	FromFile bool `json:"from_file"`
	// Error says why the profile could not be built (a missing key), empty when it works.
	Error string `json:"error,omitempty"`
}

// ConnectorView is one connector as the page sees it: its settings with every secret field
// replaced by whether one is set.
type ConnectorView struct {
	Name        string            `json:"name"`
	Kind        string            `json:"kind"`
	Category    string            `json:"category"`
	Label       string            `json:"label"`
	Description string            `json:"description,omitempty"`
	URL         string            `json:"url,omitempty"`
	TokenSet    bool              `json:"token_set"`
	ChatID      string            `json:"chat_id,omitempty"`
	Command     string            `json:"command,omitempty"`
	Settings    map[string]string `json:"settings"`
	SecretsSet  []string          `json:"secrets_set"`
	FromFile    bool              `json:"from_file"`
	// OAuth is the identity service of the kind, and SignedIn/Account say whether a sign-in is there.
	OAuth    string `json:"oauth,omitempty"`
	SignedIn bool   `json:"signed_in,omitempty"`
	Account  string `json:"account,omitempty"`
}

// connectorView masks the secrets of one connector.
func connectorView(name string, c config.Connector, fromFile bool) ConnectorView {
	kind, _ := connector.LookupKind(c.Kind)
	all := c.ConnectorSettings()
	shown := map[string]string{}
	var secrets []string
	secret := map[string]bool{}
	for _, f := range kind.Fields {
		if f.Secret {
			secret[f.Name] = true
		}
	}
	for k, v := range all {
		if secret[k] || strings.HasPrefix(k, "oauth_") {
			if v != "" && secret[k] {
				secrets = append(secrets, k)
			}
			continue
		}
		shown[k] = v
	}
	sort.Strings(secrets)
	creds := oauth.FromSettings(all)
	return ConnectorView{Name: name, Kind: c.Kind, Category: kind.Category, Label: kind.Label, Description: c.Description, URL: shown["url"],
		TokenSet: c.Token != "" || all["token"] != "", ChatID: c.ChatID, Command: c.Command, Settings: shown, SecretsSet: secrets, FromFile: fromFile,
		OAuth: kind.OAuth, SignedIn: kind.OAuth != "" && creds.SignedIn(), Account: creds.Account}
}

// SettingsView is the settings page.
type SettingsView struct {
	Providers       []ProviderView   `json:"providers"`
	Connectors      []ConnectorView  `json:"connectors"`
	DefaultProvider string           `json:"default_provider"`
	Kinds           []provider.Kind  `json:"kinds"`
	ConnectorKinds  []string         `json:"connector_kinds"`
	Catalog         []connector.Kind `json:"connector_catalog"`
	Categories      []string         `json:"connector_categories"`
	MotitaURL       string           `json:"motita_url,omitempty"`
}

// Settings describes the profiles and connectors, secrets left out.
func (m *Manager) Settings() SettingsView {
	m.mu.RLock()
	defer m.mu.RUnlock()
	view := SettingsView{DefaultProvider: m.cfg.DefaultProvider, ConnectorKinds: config.ConnectorKinds, Catalog: connector.Kinds(),
		Categories: connector.Categories, MotitaURL: m.cfg.Motita.URL}
	for _, k := range provider.Kinds() {
		kind, _ := provider.Lookup(k)
		view.Kinds = append(view.Kinds, kind)
	}
	names := make([]string, 0, len(m.cfg.Providers))
	for n := range m.cfg.Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p := m.cfg.Providers[n]
		_, fromOverlay := m.overlay.Providers[n]
		v := ProviderView{Name: n, Kind: p.Kind, Model: p.Model, BaseURL: p.BaseURL, APIKeySet: p.Options().APIKey != "",
			APIKeyEnv: p.APIKeyEnv, MaxTokens: p.MaxTokens, Temperature: p.Temperature, Default: n == m.cfg.DefaultProvider, FromFile: !fromOverlay}
		if c, err := m.providers.Get(n); err == nil {
			v.Describe = c.Describe()
		} else {
			v.Error = m.buildErrors[n]
			if v.Error == "" {
				v.Error = "not available"
			}
		}
		view.Providers = append(view.Providers, v)
	}
	names = names[:0]
	for n := range m.cfg.Connectors {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		_, fromOverlay := m.overlay.Connectors[n]
		view.Connectors = append(view.Connectors, connectorView(n, m.cfg.Connectors[n], !fromOverlay))
	}
	if view.Providers == nil {
		view.Providers = []ProviderView{}
	}
	if view.Connectors == nil {
		view.Connectors = []ConnectorView{}
	}
	return view
}

// SetProvider adds or replaces a profile, builds its client and saves the overlay. An empty key
// keeps the one the profile had.
func (m *Manager) SetProvider(name string, p config.Provider) error {
	if err := config.ValidateProvider(name, p); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if p.APIKey == "" {
		if old, ok := m.cfg.Providers[name]; ok {
			p.APIKey = old.APIKey
		}
	}
	if m.overlay.Providers == nil {
		m.overlay.Providers = map[string]config.Provider{}
	}
	if m.cfg.Providers == nil {
		m.cfg.Providers = map[string]config.Provider{}
	}
	m.overlay.Providers[name] = p
	m.overlay.Removed.Providers = without(m.overlay.Removed.Providers, name)
	m.cfg.Providers[name] = p
	if m.cfg.DefaultProvider == "" {
		m.cfg.DefaultProvider, m.overlay.DefaultProvider = name, name
	}
	if err := m.providers.Build(name, p.Options()); err != nil {
		m.providers.Remove(name)
		m.buildErrors[name] = err.Error()
	} else {
		delete(m.buildErrors, name)
	}
	return m.saveSettings()
}

// RemoveProvider forgets a profile. A kitty that used it keeps the name and fails to answer
// until the profile comes back.
func (m *Manager) RemoveProvider(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.cfg.Providers[name]; !ok {
		return fmt.Errorf("%w: provider %q", ErrNotFound, name)
	}
	delete(m.cfg.Providers, name)
	delete(m.overlay.Providers, name)
	delete(m.buildErrors, name)
	m.providers.Remove(name)
	if !contains(m.overlay.Removed.Providers, name) {
		m.overlay.Removed.Providers = append(m.overlay.Removed.Providers, name)
	}
	if m.cfg.DefaultProvider == name {
		m.cfg.DefaultProvider = ""
		for _, n := range m.providers.Names() {
			m.cfg.DefaultProvider = n
			break
		}
		m.overlay.DefaultProvider = m.cfg.DefaultProvider
	}
	return m.saveSettings()
}

// SetDefaultProvider picks the profile a kitty uses when it names none.
func (m *Manager) SetDefaultProvider(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.cfg.Providers[name]; !ok {
		return fmt.Errorf("%w: provider %q", ErrNotFound, name)
	}
	m.cfg.DefaultProvider, m.overlay.DefaultProvider = name, name
	return m.saveSettings()
}

// TestProvider asks the profile for one short answer, to check a key and a model.
func (m *Manager) TestProvider(ctx context.Context, name string) (string, error) {
	c, err := m.providers.Get(name)
	if err != nil {
		m.mu.RLock()
		reason := m.buildErrors[name]
		m.mu.RUnlock()
		if reason != "" {
			return "", errors.New(reason)
		}
		return "", err
	}
	reply, err := c.Complete(ctx, []provider.Message{
		{Role: "system", Content: "Answer in at most five words."},
		{Role: "user", Content: "Say hello and your model name."},
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(reply.Content), nil
}

// SetConnector adds or replaces a connector and saves the overlay. An empty token keeps the one it had.
func (m *Manager) SetConnector(name string, c config.Connector) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.cfg.Connectors[name]; ok && old.Kind == c.Kind {
		// An empty secret keeps the one the connector had.
		if c.Token == "" {
			c.Token = old.Token
		}
		kind, _ := connector.LookupKind(c.Kind)
		oldS := old.ConnectorSettings()
		keep := func(key string) {
			if strings.TrimSpace(c.Settings[key]) == "" && oldS[key] != "" {
				if c.Settings == nil {
					c.Settings = map[string]string{}
				}
				c.Settings[key] = oldS[key]
			}
		}
		for _, f := range kind.Fields {
			if f.Secret {
				keep(f.Name)
			}
		}
		// A sign-in survives an edit of the other fields.
		for _, key := range []string{oauth.KeyAccess, oauth.KeyRefresh, oauth.KeyExpiry, oauth.KeyAccount} {
			keep(key)
		}
	}
	if err := config.ValidateConnector(name, c, m.cfg.Motita.URL); err != nil {
		return err
	}
	if m.overlay.Connectors == nil {
		m.overlay.Connectors = map[string]config.Connector{}
	}
	if m.cfg.Connectors == nil {
		m.cfg.Connectors = map[string]config.Connector{}
	}
	m.overlay.Connectors[name] = c
	m.overlay.Removed.Connectors = without(m.overlay.Removed.Connectors, name)
	m.cfg.Connectors[name] = c
	m.connectors.Add(name, m.buildConnector(name, c))
	return m.saveSettings()
}

// RemoveConnector forgets a connector.
func (m *Manager) RemoveConnector(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.cfg.Connectors[name]; !ok {
		return fmt.Errorf("%w: connector %q", ErrNotFound, name)
	}
	delete(m.cfg.Connectors, name)
	delete(m.overlay.Connectors, name)
	m.connectors.Remove(name)
	if !contains(m.overlay.Removed.Connectors, name) {
		m.overlay.Removed.Connectors = append(m.overlay.Removed.Connectors, name)
	}
	return m.saveSettings()
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
