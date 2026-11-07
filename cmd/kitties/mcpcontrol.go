package main

import (
	"errors"
	"net/url"
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

// mcpURLs are the addresses /mcp answers at, best first for a client on another computer: the
// public address, then the local network one (when the gateway listens there), then this machine.
// They follow the gateway's exposure, like the addresses its welcome block prints.
func mcpURLs(listen, publicURL string, https bool, lanIP string) []gateway.MCPConnect {
	var out []gateway.MCPConnect
	if publicURL != "" {
		out = append(out, gateway.MCPConnect{Where: "public", URL: strings.TrimRight(publicURL, "/") + gateway.MCPPath})
	}
	var local []gateway.MCPConnect
	for _, a := range pageAddresses(listen, "", https, lanIP) {
		c := gateway.MCPConnect{Where: "lan", URL: strings.TrimRight(a, "/") + gateway.MCPPath}
		if u, err := url.Parse(a); err == nil && config.IsLoopbackAddr(u.Host) {
			c.Where = "local"
			local = append(local, c)
			continue
		}
		out = append(out, c)
	}
	return append(out, local...)
}

// claudeCommand is the `claude mcp add` line for a URL, user-wide so every project of that person
// gets the fleet. token is how the header gets the token: the file read in place, or a placeholder
// the page fills in when it copies the command.
func claudeCommand(u, token string) string {
	return `claude mcp add --transport http --scope user kitties ` + u + ` --header "Authorization: Bearer ` + token + `"`
}

func (m *mcpControl) MCP() gateway.MCPInfo {
	g := m.cfg.Gateway
	lan := ""
	if config.ExposureOf(g.Listen) == config.ExposureWAN {
		lan = lanIP()
	}
	info := gateway.MCPInfo{
		MCPSettings:  m.cfg.MCPSettingsOf(),
		TokenFile:    m.tokenFile(),
		TokenManaged: g.MCP.Token != "",
		CanChange:    true,
		InContainer:  inContainer(),
		Connect:      mcpURLs(g.Listen, g.PublicURL, g.TLSCert != "", lan),
	}
	// The token file is where Claude Code runs only on this machine, outside a container: there
	// the command reads it; anywhere else the page puts the token in when it copies the command.
	readFile := `$(cat ` + info.TokenFile + `)`
	for i := range info.Connect {
		c := &info.Connect[i]
		if c.Where == "local" && !info.InContainer {
			c.Command = claudeCommand(c.URL, readFile)
		} else {
			c.Command = claudeCommand(c.URL, gateway.MCPTokenPlaceholder)
			c.WithToken = true
		}
	}
	if len(info.Connect) > 0 {
		info.URL = info.Connect[0].URL
		info.Command = info.Connect[0].Command
	}
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
