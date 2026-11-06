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

func TestWhatsAppFromTheFile(t *testing.T) {
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.WhatsApp = config.WhatsApp{Enabled: true, Allow: []string{"+56 9 1234 5678"}, Groups: true}
	cfg.Providers = map[string]config.Provider{"main": {Kind: "groq", APIKey: "gk"}}
	fleet, err := orchestrator.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: cfg, fleet: fleet}
	if a.vault, err = vault.Open(cfg.DataDir); err != nil {
		t.Fatal(err)
	}
	var warn bytes.Buffer
	b := a.whatsappBridge(&warn, func(string, ...any) {})
	if b == nil {
		t.Fatal(warn.String())
	}
	st := b.Status()
	if !st.Enabled || !st.Groups || len(st.Allow) != 1 || st.Allow[0] != "56912345678" {
		t.Fatalf("from the file = %+v", st)
	}
}

func TestTranscriber(t *testing.T) {
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Providers = map[string]config.Provider{
		"a-claude": {Kind: "anthropic", APIKey: "ak"},
		"b-groq":   {Kind: "groq", APIKeyEnv: "MY_GROQ"},
		"c-openai": {Kind: "openai", BaseURL: "http://local/v1"},
	}
	fleet, err := orchestrator.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: cfg, fleet: fleet}
	env := map[string]string{"MY_GROQ": "gk", "OPENAI_API_KEY": "ok"}
	getenv := func(k string) string { return env[k] }
	if tr, ok := a.transcriber("", getenv); !ok || tr.Key != "gk" || tr.BaseURL != "https://api.groq.com/openai/v1" || tr.Model != "whisper-large-v3" {
		t.Fatalf("first = %+v %v", tr, ok)
	}
	if tr, ok := a.transcriber("c-openai", getenv); !ok || tr.Key != "ok" || tr.BaseURL != "http://local/v1" || tr.Model != "whisper-1" {
		t.Fatalf("named = %+v %v", tr, ok)
	}
	for _, name := range []string{"off", "a-claude", "nobody"} {
		if _, ok := a.transcriber(name, getenv); ok {
			t.Fatalf("%q transcribes", name)
		}
	}
	if _, ok := a.transcriber("", func(string) string { return "" }); ok {
		t.Fatal("transcribes without a key")
	}
}
