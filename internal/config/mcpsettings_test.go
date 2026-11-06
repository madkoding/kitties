package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMCPSettingsAreKeptAndWin(t *testing.T) {
	dir := t.TempDir()
	c := Defaults()
	c.DataDir = dir
	c.Gateway.MCP.Enabled = true
	if got := c.MCPSettingsOf(); !got.Enabled || got.OAuth {
		t.Fatalf("settings of the file = %+v", got)
	}
	if _, ok := SavedMCPSettings(dir); ok {
		t.Fatal("a choice was found before any was made")
	}
	c.ApplyMCPSettings() // nothing saved: nothing changes
	if !c.Gateway.MCP.Enabled {
		t.Fatal("applying nothing changed the configuration")
	}
	want := MCPSettings{Enabled: false, AllowExec: true, AllowApprove: true, OAuth: true}
	if err := SaveMCPSettings(dir, want); err != nil {
		t.Fatal(err)
	}
	if got, ok := SavedMCPSettings(dir); !ok || got != want {
		t.Fatalf("saved = %+v %v", got, ok)
	}
	c.ApplyMCPSettings()
	if got := c.MCPSettingsOf(); got != want {
		t.Errorf("applied = %+v", got)
	}
}

func TestMCPSettingsUnreadableAreIgnored(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, MCPSettingsFile), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := SavedMCPSettings(dir); ok {
		t.Error("a broken file counted as a choice")
	}
	// A data directory that cannot be made reports it.
	file := filepath.Join(dir, "plain")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveMCPSettings(filepath.Join(file, "sub"), MCPSettings{}); err == nil {
		t.Error("saved inside a file")
	}
}
