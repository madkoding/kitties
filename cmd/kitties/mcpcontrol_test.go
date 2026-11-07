package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/config"
)

func newMCPControl(t *testing.T, mutate func(*config.Config)) (*mcpControl, string) {
	t.Helper()
	old := lanIP
	t.Cleanup(func() { lanIP = old })
	lanIP = func() string { return "192.168.1.9" }
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	if mutate != nil {
		mutate(&cfg)
	}
	return mcpControlOf(cfg), cfg.DataDir
}

func TestMCPControlShowsTheCommandWithoutTheToken(t *testing.T) {
	m, dir := newMCPControl(t, func(c *config.Config) { c.Gateway.Listen = "0.0.0.0:7488" })
	if err := os.WriteFile(filepath.Join(dir, "mcp.token"), []byte("abcdefghijklmnop-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info := m.MCP()
	if !info.Enabled || !info.CanChange || info.TokenManaged || len(info.Connect) != 2 {
		t.Fatalf("info = %+v", info)
	}
	// Exposed to the network: the network address first, for Claude Code on another computer, with
	// a placeholder the page fills in; this machine's address reads the token file in place.
	lan, local := info.Connect[0], info.Connect[1]
	if lan.Where != "lan" || lan.URL != "http://"+lanIP()+":7488/mcp" || !lan.WithToken || info.URL != lan.URL || info.Command != lan.Command ||
		lan.Command != `claude mcp add --transport http --scope user kitties `+lan.URL+` --header "Authorization: Bearer <MCP_TOKEN>"` {
		t.Errorf("lan = %+v", lan)
	}
	if local.Where != "local" || local.URL != "http://127.0.0.1:7488/mcp" || local.WithToken ||
		!strings.Contains(local.Command, "$(cat "+filepath.Join(dir, "mcp.token")+")") {
		t.Errorf("local = %+v", local)
	}
	for _, c := range info.Connect {
		if strings.Contains(c.Command, "secret") {
			t.Errorf("the token is in %q", c.Command)
		}
	}
	if tok, err := m.Token(); err != nil || tok != "abcdefghijklmnop-secret" {
		t.Errorf("token = %q %v", tok, err)
	}
}

func TestMCPControlURLs(t *testing.T) {
	lan := "192.168.1.9"
	for listen, want := range map[string][]string{
		"127.0.0.1:9100": {"http://127.0.0.1:9100/mcp"},
		"[::]:9100":      {"http://" + lan + ":9100/mcp", "http://127.0.0.1:9100/mcp"},
		":9100":          {"http://" + lan + ":9100/mcp", "http://127.0.0.1:9100/mcp"},
		"192.168.1.5:80": {"http://192.168.1.5:80/mcp", "http://127.0.0.1:80/mcp"},
	} {
		m, _ := newMCPControl(t, func(c *config.Config) { c.Gateway.Listen = listen })
		var got []string
		for _, c := range m.MCP().Connect {
			got = append(got, c.URL)
		}
		if strings.Join(got, " ") != strings.Join(want, " ") || m.MCP().URL != want[0] {
			t.Errorf("listen %s: urls %v, want %v", listen, got, want)
		}
	}
	m, _ := newMCPControl(t, func(c *config.Config) { c.Gateway.PublicURL = "https://k.example/" })
	if i := m.MCP(); i.URL != "https://k.example/mcp" || i.Connect[0].Where != "public" || !i.Connect[0].WithToken {
		t.Errorf("public url = %+v", i)
	}
	// TLS on the gateway itself: https everywhere.
	m, _ = newMCPControl(t, func(c *config.Config) { c.Gateway.TLSCert = "c.pem" })
	if got := m.MCP().URL; got != "https://127.0.0.1:7488/mcp" {
		t.Errorf("tls url = %s", got)
	}
	// In a container the token file is not where Claude Code runs: every command takes the token.
	t.Setenv("KITTIES_IN_DOCKER", "1")
	m, _ = newMCPControl(t, nil)
	if i := m.MCP(); !i.InContainer || !i.Connect[0].WithToken || strings.Contains(i.Command, "$(cat") {
		t.Errorf("container = %+v", i)
	}
}

func TestMCPControlSavesTheSwitches(t *testing.T) {
	m, dir := newMCPControl(t, nil)
	if err := m.SetMCP(config.MCPSettings{OAuth: true}); err == nil {
		t.Error("oauth without the connector was accepted")
	}
	want := config.MCPSettings{Enabled: true, AllowExec: true, OAuth: true}
	if err := m.SetMCP(want); err != nil {
		t.Fatal(err)
	}
	if got, ok := config.SavedMCPSettings(dir); !ok || got != want {
		t.Errorf("saved = %+v %v", got, ok)
	}
}

func TestMCPControlTokens(t *testing.T) {
	m, dir := newMCPControl(t, func(c *config.Config) { c.Gateway.MCP.Enabled = false })
	if _, err := m.Token(); err == nil {
		t.Error("a token without a file")
	}
	if err := m.RotateToken(); err == nil {
		t.Error("rotated while the connector is off")
	}
	m, dir = newMCPControl(t, func(c *config.Config) { c.Gateway.MCP.Enabled = true })
	if err := os.WriteFile(filepath.Join(dir, "mcp.token"), []byte("old-token-0123456789\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.RotateToken(); err != nil {
		t.Fatal(err)
	}
	if tok, err := m.Token(); err != nil || tok == "old-token-0123456789" || len(tok) < 16 {
		t.Errorf("rotated token = %q %v", tok, err)
	}
	// a token from the configuration is shown but not replaced here
	m, _ = newMCPControl(t, func(c *config.Config) { c.Gateway.MCP.Enabled = true; c.Gateway.MCP.Token = "from-the-config-0123" })
	if !m.MCP().TokenManaged {
		t.Error("a configured token is not marked as managed")
	}
	if tok, _ := m.Token(); tok != "from-the-config-0123" {
		t.Errorf("token = %q", tok)
	}
	if err := m.RotateToken(); err == nil {
		t.Error("replaced a token set in the configuration")
	}
}
