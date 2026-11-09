package main

import (
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/madkoding/kitties/internal/attach"
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
		Dir: filepath.Join(dataDir, "whatsapp"), Chats: a.chats, Kitty: a.fleet.Get, Lead: a.fleet.Lead, Registry: a.fleet.Connectors(),
		QRImage: wameow.QRImage, Log: logf,
	}
	if a.guard != nil {
		opt.Guard = a.guard
	}
	if a.vault != nil {
		if box, err := vault.NewBox(dataDir); err == nil {
			opt.Box, opt.NewClient = box, wameow.New
		}
	}
	if w.Set() {
		opt.Defaults = &whatsapp.Settings{Enabled: w.Enabled, Allow: w.Allow, Kitty: w.Kitty, Groups: w.Groups, AllowGroups: w.AllowGroups,
			Kitties: w.Kitties, MaxPerHour: w.MaxPerHour, QuietFrom: w.Quiet.From, QuietTo: w.Quiet.To, Voice: w.Voice,
			ShareWithTeam: w.ShareWithTeam, MentionAll: w.MentionAll, MentionAllHours: w.MentionAllHours,
			LocalNetwork: w.LocalNetwork, TaskMinutes: w.TaskMinutes, TaskMinutesFor: w.TaskMinutesFor,
			TasksPerDay: w.TasksPerDay, TasksPerDayFor: w.TasksPerDayFor}
	}
	if a.arts != nil {
		opt.File = libraryFile(a.arts.Get)
	}
	if t, ok := a.transcriber(w.Transcribe, os.Getenv); ok {
		opt.Transcribe = t.Transcribe
	}
	if sp, ok := a.speaker(w.Speak, w.SpeakVoice, os.Getenv); ok {
		opt.Speak = sp.Speak
	}
	b, err := whatsapp.New(opt)
	if err != nil {
		fmt.Fprintf(warn, "kitties: WhatsApp stays off: %v\n", err)
		return nil
	}
	// The kitty that answers on WhatsApp works on its own, apart from the others (chat/independent.go).
	if a.chats != nil {
		a.chats.SetIndependent(b.Independent)
		a.chats.SetRelay(b.Relay)
		a.chats.SetExposedMinutes(b.TaskMinutes)
		a.chats.SetExposedTasks(b.TasksPerDay)
	}
	// Its computer reaches the internet but not the local network (kitty-nolan in the desktop).
	if a.fleet != nil {
		a.fleet.SetOffLAN(b.OffLAN)
	}
	return b
}

// libraryFile reads a file of the library for the kitties to send, with its media type from its
// bytes (or its name, for audio and video).
func libraryFile(get func(string) ([]byte, error)) func(string) (whatsapp.File, error) {
	return func(name string) (whatsapp.File, error) {
		data, err := get(name)
		if err != nil {
			return whatsapp.File{}, err
		}
		mt, _ := attach.Sniff(name, data)
		if byName := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); byName != "" &&
			(strings.HasPrefix(byName, "audio/") || strings.HasPrefix(byName, "video/")) {
			mt = byName
		}
		return whatsapp.File{Name: name, MIME: mt, Data: data}, nil
	}
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

// speaker is the speech API that reads the kitties' answers aloud: the named profile, else the
// first openai profile with a key ("off" turns it off). Groq's speech answers only WAV, which
// WhatsApp does not play as a voice note.
func (a *app) speaker(name, voice string, getenv func(string) string) (whatsapp.Speaker, bool) {
	if name == "off" {
		return whatsapp.Speaker{}, false
	}
	names := a.fleet.ProfileNames()
	if name != "" {
		names = []string{name}
	}
	for _, n := range names {
		p, ok := a.fleet.Profile(n)
		if !ok || p.Kind != "openai" {
			continue
		}
		if t, ok := speechOf(p, getenv); ok {
			return whatsapp.Speaker{BaseURL: t.BaseURL, Key: t.Key, Model: whatsapp.SpeakModel, Voice: voice}, true
		}
	}
	return whatsapp.Speaker{}, false
}
