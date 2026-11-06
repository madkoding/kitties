package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/madkoding/kitties/internal/config"
	"github.com/madkoding/kitties/internal/gateway"
)

// mcpControl is the MCP connector as Settings changes it: the switches are kept in the data
// directory (they win over the file and the environment) and the gateway restarts to apply them.
type mcpControl struct{ cfg config.Config }

func mcpControlOf(cfg config.Config) *mcpControl { return &mcpControl{cfg: cfg} }

func (m *mcpControl) tokenFile() string { return filepath.Join(m.cfg.DataDirOr(), "mcp.token") }

// url is where /mcp answers: the public address, else the one the gateway listens on (a wildcard
// listen address is reached through loopback).
func (m *mcpControl) url() string {
	g := m.cfg.Gateway
	if g.PublicURL != "" {
		return strings.TrimRight(g.PublicURL, "/") + gateway.MCPPath
	}
	listen := g.Listen
	for _, wild := range []string{"0.0.0.0:", "[::]:", ":"} {
		if strings.HasPrefix(listen, wild) {
			listen = "127.0.0.1:" + strings.TrimPrefix(listen, wild)
			break
		}
	}
	return "http://" + listen + gateway.MCPPath
}

func (m *mcpControl) MCP() gateway.MCPInfo {
	info := gateway.MCPInfo{
		MCPSettings:  m.cfg.MCPSettingsOf(),
		URL:          m.url(),
		TokenFile:    m.tokenFile(),
		TokenManaged: m.cfg.Gateway.MCP.Token != "",
		CanChange:    true,
	}
	info.Command = `claude mcp add --transport http kitties ` + info.URL + ` --header "Authorization: Bearer $(cat ` + info.TokenFile + `)"`
	return info
}

func (m *mcpControl) SetMCP(s config.MCPSettings) error {
	if s.OAuth && !s.Enabled {
		return errors.New("sign-in with OAuth needs the connector to be on")
	}
	return config.SaveMCPSettings(m.cfg.DataDirOr(), s)
}

// Token is the connector's token: the configured one, else the one kept in the token file.
func (m *mcpControl) Token() (string, error) {
	if t := m.cfg.Gateway.MCP.Token; t != "" {
		return t, nil
	}
	data, err := os.ReadFile(m.tokenFile())
	if err != nil {
		return "", errors.New("the connector has no token yet: turn it on first")
	}
	return strings.TrimSpace(string(data)), nil
}

// RotateToken writes a new token; the gateway restarts with it and every connection that used the
// old one stops working. A token set in the configuration or the environment is not ours to change.
func (m *mcpControl) RotateToken() error {
	if m.cfg.Gateway.MCP.Token != "" {
		return errors.New("the token is set in the configuration or KITTIES_MCP_TOKEN: change it there")
	}
	if !m.cfg.Gateway.MCP.Enabled {
		return errors.New("turn the connector on first")
	}
	return os.WriteFile(m.tokenFile(), []byte(gateway.NewToken()+"\n"), 0o600)
}

func (m *mcpControl) Relaunch() error { return relaunch() }
