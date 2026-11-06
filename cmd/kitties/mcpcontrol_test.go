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
	if info.URL != "http://127.0.0.1:7488/mcp" || !info.CanChange || info.TokenManaged {
		t.Fatalf("info = %+v", info)
	}
	if !strings.Contains(info.Command, "claude mcp add --transport http kitties http://127.0.0.1:7488/mcp") ||
		!strings.Contains(info.Command, "$(cat "+filepath.Join(dir, "mcp.token")+")") || strings.Contains(info.Command, "secret") {
		t.Errorf("command = %q", info.Command)
	}
	if tok, err := m.Token(); err != nil || tok != "abcdefghijklmnop-secret" {
		t.Errorf("token = %q %v", tok, err)
	}
}

func TestMCPControlURLs(t *testing.T) {
	for listen, want := range map[string]string{
		"127.0.0.1:9100": "http://127.0.0.1:9100/mcp",
		"[::]:9100":      "http://127.0.0.1:9100/mcp",
		":9100":          "http://127.0.0.1:9100/mcp",
		"192.168.1.5:80": "http://192.168.1.5:80/mcp",
	} {
		m, _ := newMCPControl(t, func(c *config.Config) { c.Gateway.Listen = listen })
		if got := m.MCP().URL; got != want {
			t.Errorf("listen %s: url %s, want %s", listen, got, want)
		}
	}
	m, _ := newMCPControl(t, func(c *config.Config) { c.Gateway.PublicURL = "https://k.example/" })
	if got := m.MCP().URL; got != "https://k.example/mcp" {
		t.Errorf("public url = %s", got)
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
	m, dir := newMCPControl(t, nil)
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
