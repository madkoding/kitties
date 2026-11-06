package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// MCPSettingsFile is the file in the data directory that keeps what the person chose for the MCP
// connector in Settings. Like the exposure it wins over the configuration file and the
// environment, so a choice made on the page holds.
const MCPSettingsFile = "mcp-settings.json"

// MCPSettings are the switches of the connector that Settings can change.
type MCPSettings struct {
	Enabled      bool `json:"enabled"`
	AllowExec    bool `json:"allow_exec"`
	AllowApprove bool `json:"allow_approve"`
	OAuth        bool `json:"oauth"`
}

// MCPSettingsOf are the switches of this configuration.
func (c Config) MCPSettingsOf() MCPSettings {
	m := c.Gateway.MCP
	return MCPSettings{Enabled: m.Enabled, AllowExec: m.AllowExec, AllowApprove: m.AllowApprove, OAuth: m.OAuth}
}

// SavedMCPSettings reads the choice kept in dataDir (false when there is none or it is unreadable).
func SavedMCPSettings(dataDir string) (MCPSettings, bool) {
	var s MCPSettings
	data, err := os.ReadFile(filepath.Join(dataDir, MCPSettingsFile))
	if err != nil || json.Unmarshal(data, &s) != nil {
		return MCPSettings{}, false
	}
	return s, true
}

// SaveMCPSettings keeps the choice in dataDir.
func SaveMCPSettings(dataDir string, s MCPSettings) error {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dataDir, MCPSettingsFile), append(data, '\n'), 0o600)
}

// ApplyMCPSettings makes the saved choice, if any, the connector's configuration.
func (c *Config) ApplyMCPSettings() {
	if s, ok := SavedMCPSettings(c.DataDirOr()); ok {
		c.Gateway.MCP.Enabled = s.Enabled
		c.Gateway.MCP.AllowExec = s.AllowExec
		c.Gateway.MCP.AllowApprove = s.AllowApprove
		c.Gateway.MCP.OAuth = s.OAuth
	}
}
