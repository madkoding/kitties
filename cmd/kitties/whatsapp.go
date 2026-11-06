package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/madkoding/kitties/internal/config"
	"github.com/madkoding/kitties/internal/provider"
	"github.com/madkoding/kitties/internal/vault"
	"github.com/madkoding/kitties/internal/whatsapp"
	"github.com/madkoding/kitties/internal/whatsapp/wameow"
)

// whatsappBridge is the WhatsApp link of the gateway (off until Settings or the file turns it
// on). Its session is sealed with the vault's key: without a vault it stays off and Settings says
// why.
func (a *app) whatsappBridge(warn io.Writer, logf func(string, ...any)) *whatsapp.Bridge {
	dataDir := a.cfg.DataDirOr()
	w := a.cfg.WhatsApp
	opt := whatsapp.Options{
		Dir: filepath.Join(dataDir, "whatsapp"), Chats: a.chats, Kitty: a.fleet.Get, Registry: a.fleet.Connectors(),
		QRImage: wameow.QRImage, Log: logf,
	}
	if a.vault != nil {
		if box, err := vault.NewBox(dataDir); err == nil {
			opt.Box, opt.NewClient = box, wameow.New
		}
	}
	if w.Set() {
		opt.Defaults = &whatsapp.Settings{Enabled: w.Enabled, Allow: w.Allow, Kitty: w.Kitty, Groups: w.Groups}
	}
	if t, ok := a.transcriber(w.Transcribe, os.Getenv); ok {
		opt.Transcribe = t.Transcribe
	}
	b, err := whatsapp.New(opt)
	if err != nil {
		fmt.Fprintf(warn, "kitties: WhatsApp stays off: %v\n", err)
		return nil
	}
	return b
}

// transcriber is the speech API voice notes go to: the named profile, else the first openai or
// groq profile with a key ("off" turns it off).
func (a *app) transcriber(name string, getenv func(string) string) (whatsapp.Transcriber, bool) {
	if name == "off" {
		return whatsapp.Transcriber{}, false
	}
	names := a.fleet.ProfileNames()
	if name != "" {
		names = []string{name}
	}
	for _, n := range names {
		p, ok := a.fleet.Profile(n)
		if !ok {
			continue
		}
		if t, ok := speechOf(p, getenv); ok {
			return t, true
		}
	}
	return whatsapp.Transcriber{}, false
}

// speechOf is the speech API of a profile, when its provider has one and it has a key.
func speechOf(p config.Provider, getenv func(string) string) (whatsapp.Transcriber, bool) {
	if p.Kind != "openai" && p.Kind != "groq" {
		return whatsapp.Transcriber{}, false
	}
	kind, _ := provider.Lookup(p.Kind)
	base := p.BaseURL
	if base == "" {
		base = kind.BaseURL
	}
	key := p.APIKey
	if key == "" && p.APIKeyEnv != "" {
		key = getenv(p.APIKeyEnv)
	}
	if key == "" {
		key = getenv(kind.KeyEnv)
	}
	if key == "" {
		return whatsapp.Transcriber{}, false
	}
	return whatsapp.Transcriber{BaseURL: base, Key: key, Model: whatsapp.TranscribeModel(base)}, true
}
