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
	cfg.WhatsApp = config.WhatsApp{Enabled: true, Allow: []string{"+56 9 1234 5678"}, Groups: true, AllowGroups: []string{"1@g.us"}, MaxPerHour: 9}
	cfg.WhatsApp.Quiet.From, cfg.WhatsApp.Quiet.To = "22:00", "08:00"
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
	if !st.Enabled || !st.Groups || len(st.Allow) != 1 || st.Allow[0] != "56912345678" || st.AllowGroups[0] != "1@g.us" || st.MaxPerHour != 9 || st.QuietTo != "08:00" {
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

func TestSpeaker(t *testing.T) {
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Providers = map[string]config.Provider{
		"a-groq":   {Kind: "groq", APIKey: "gk"},
		"b-openai": {Kind: "openai", BaseURL: "http://local/v1"},
	}
	fleet, err := orchestrator.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: cfg, fleet: fleet}
	getenv := func(k string) string { return map[string]string{"OPENAI_API_KEY": "ok"}[k] }
	// Groq's speech is skipped: only openai reads aloud in Ogg Opus.
	if sp, ok := a.speaker("", "nova", getenv); !ok || sp.Key != "ok" || sp.BaseURL != "http://local/v1" || sp.Voice != "nova" || sp.Model != "gpt-4o-mini-tts" {
		t.Fatalf("first = %+v %v", sp, ok)
	}
	for _, name := range []string{"off", "a-groq", "nobody"} {
		if _, ok := a.speaker(name, "", getenv); ok {
			t.Fatalf("%q speaks", name)
		}
	}
	if _, ok := a.speaker("", "", func(string) string { return "" }); ok {
		t.Fatal("speaks without a key")
	}
}

func TestLibraryFile(t *testing.T) {
	lib := map[string][]byte{"a.png": []byte("\x89PNG\r\n\x1a\nxx"), "v.mp4": []byte("....ftypmp42"), "n.ogg": []byte("OggS"), "t.txt": []byte("hola")}
	get := libraryFile(func(name string) ([]byte, error) {
		d, ok := lib[name]
		if !ok {
			return nil, os.ErrNotExist
		}
		return d, nil
	})
	for name, want := range map[string]string{"a.png": "image/png", "v.mp4": "video/mp4", "n.ogg": "audio/ogg"} {
		if f, err := get(name); err != nil || f.MIME != want || f.Name != name {
			t.Fatalf("%s = %+v %v", name, f, err)
		}
	}
	if f, _ := get("t.txt"); !strings.HasPrefix(f.MIME, "text/") {
		t.Fatalf("text = %q", f.MIME)
	}
	if _, err := get("none"); err == nil {
		t.Fatal("a missing file was read")
	}
}
