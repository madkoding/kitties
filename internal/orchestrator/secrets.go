package orchestrator

import (
	"os"
	"strings"

	"github.com/madkoding/kitties/internal/connector"
)

// SecretValues are the secrets of the models and the connections as they are now: the keys, the
// tokens, the secret fields and the signed-in accounts' tokens. The guardrails take them out of
// whatever reaches other people; they never leave the gateway.
func (m *Manager) SecretValues() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []string
	add := func(v string) {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	for _, p := range m.cfg.Providers {
		add(p.APIKey)
		if p.APIKeyEnv != "" {
			add(os.Getenv(p.APIKeyEnv))
		}
	}
	for _, c := range m.cfg.Connectors {
		add(c.Token)
		for _, v := range c.Headers {
			add(v)
		}
		secret := map[string]bool{}
		if k, ok := connector.LookupKind(c.Kind); ok {
			for _, f := range k.Fields {
				if f.Secret {
					secret[f.Name] = true
				}
			}
		}
		for name, v := range c.ConnectorSettings() {
			if secret[name] || secretName(name) {
				add(v)
			}
		}
	}
	return out
}

// secretName reports whether a setting's name says it holds a secret (an OAuth token, a client
// secret, a password), whatever its kind declares.
func secretName(name string) bool {
	n := strings.ToLower(name)
	if strings.HasSuffix(n, "_url") || strings.HasSuffix(n, "_uri") {
		return false
	}
	for _, w := range []string{"token", "secret", "password", "passwd", "api_key", "apikey", "private"} {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}
