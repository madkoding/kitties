package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/config"
	"github.com/madkoding/kitties/internal/orchestrator"
	"github.com/madkoding/kitties/internal/vault"
)

func TestWhatsAppBridge(t *testing.T) {
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	fleet, err := orchestrator.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	logf := func(string, ...any) {}
	var warn bytes.Buffer
	// No vault: there, but it cannot be turned on.
	a := &app{cfg: cfg, fleet: fleet}
	b := a.whatsappBridge(&warn, logf)
	if b == nil || b.Status().CanChange {
		t.Fatalf("without a vault = %+v", b)
	}
	// With the vault it can.
	if a.vault, err = vault.Open(cfg.DataDir); err != nil {
		t.Fatal(err)
	}
	if b := a.whatsappBridge(&warn, logf); b == nil || !b.Status().CanChange {
		t.Fatal("with a vault it cannot be turned on")
	}
	// A broken settings file keeps it off, and says so.
	dir := filepath.Join(cfg.DataDir, "whatsapp")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{"), 0o600)
	if b := a.whatsappBridge(&warn, logf); b != nil || !strings.Contains(warn.String(), "WhatsApp stays off") {
		t.Fatalf("broken settings = %v %q", b, warn.String())
	}
}
