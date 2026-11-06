// Package whatsapp lets the kitties use a WhatsApp number of their own: the gateway links to it
// as a linked device (like WhatsApp Web, with a QR code shown in Settings), the people on the
// allow list talk to a kitty from their phone, and the kitties send messages to them with the
// whatsapp-web connector.
//
// It is off until the person turns it on, and the allow list starts empty: a number that is not
// on it is ignored, both ways. The session of the linked device (its keys) is sealed with the
// vault's key; nothing of it is written in the clear.
//
// WhatsApp Web is not an official API. Its terms forbid automated and unofficial clients, and a
// number used this way can be banned. The official way is the WhatsApp Business Cloud API (the
// "whatsapp" connector of the catalogue). See docs/WHATSAPP.md.
//
// The protocol itself is behind Client (internal/whatsapp/wameow is the real one), so this
// package needs nothing of its own and is tested with a fake.
package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/madkoding/kitties/internal/attach"
	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/connector"
	"github.com/madkoding/kitties/internal/kitty"
)

// ConnectorName is the name the kitties call the connector by.
const ConnectorName = "whatsapp-web"

// Kind is the connector's kind in the catalogue (its logo, and the risk of its actions: a send
// is held for the person until they trust the kitty with it).
const Kind = "whatsapp"

// MaxAllow is the most numbers the allow list holds.
const MaxAllow = 50

// The states of the link, as Settings shows them.
const (
	StateOff          = "off"          // turned off
	StateStarting     = "starting"     // connecting
	StateQR           = "qr"           // waiting for the QR code to be scanned
	StateQRExpired    = "qr_expired"   // the codes ran out: ask for new ones
	StateConnected    = "connected"    // linked and online
	StateDisconnected = "disconnected" // linked, reconnecting
	StateLoggedOut    = "logged_out"   // the phone removed the device
	StateError        = "error"        // it could not start
)

// EventKind is what a Client tells.
type EventKind string

// The events of a Client.
const (
	EvQR           EventKind = "qr"           // a code to show (QR)
	EvQRExpired    EventKind = "qr_expired"   // no more codes: the person did not scan in time
	EvPaired       EventKind = "paired"       // the phone scanned the code (Phone)
	EvConnected    EventKind = "connected"    // online (Phone)
	EvDisconnected EventKind = "disconnected" // the connection dropped; the client reconnects
	EvLoggedOut    EventKind = "logged_out"   // the device was removed from the phone
	EvMessage      EventKind = "message"      // a message came (From, Name, Text, ID)
	EvSession      EventKind = "session"      // the session changed: keep Session
	EvError        EventKind = "error"        // something failed (Err)
)

// Event is one thing a Client tells the bridge.
type Event struct {
	Kind EventKind
	// QR is the code to draw (EvQR).
	QR string
	// Phone is the linked number (EvPaired, EvConnected), digits only.
	Phone string
	// From is who wrote (EvMessage), digits only; Name their name on WhatsApp.
	From string
	Name string
	Text string
	ID   string
	// Group is true for a message in a group, Chat the group (a JID); Mentioned says the message
	// names this number (an @mention or a reply to it).
	Group     bool
	Chat      string
	Mentioned bool
	// ChatName is the group's name, when WhatsApp gave it.
	ChatName string
	// Ref points at the message, to mark it read or react to it.
	Ref Ref
	// Files are the pictures, documents and audios of the message (Text is their caption).
	Files []File
	// Session is the whole state of the linked device, to keep sealed (EvSession).
	Session []byte
	Err     string
}

// Ref points at a message for the Client: its chat, its sender and its id, as WhatsApp names
// them (opaque to the bridge).
type Ref struct {
	Chat, Sender, ID string
}

// File is a picture, document, video or audio that came in a message.
type File struct {
	Name string
	MIME string
	Data []byte
	// Voice is true for a voice note (an audio recorded in the chat).
	Voice bool
}

// Client is a WhatsApp connection as a linked device.
type Client interface {
	// Run connects with the kept session (nil links a new device with QR codes) and tells
	// every event to emit until ctx ends or the link is over (logged out, codes expired).
	Run(ctx context.Context, session []byte, emit func(Event)) error
	// Send writes text to a number (digits with the country code) or a group (its JID).
	Send(ctx context.Context, to, text string) error
	// SendFile sends a picture, video, audio or document, with text as its caption.
	SendFile(ctx context.Context, to string, f File, caption string) error
	// SendReply writes text to to as an answer to a message, quoting it (quoted is its text).
	SendReply(ctx context.Context, to, text string, ref Ref, quoted string) error
	// MarkRead marks a message as read (the blue ticks).
	MarkRead(ctx context.Context, ref Ref) error
	// React puts an emoji on a message ("" takes it off).
	React(ctx context.Context, ref Ref, emoji string) error
	// Typing shows (on) or stops showing that the number is writing to to.
	Typing(ctx context.Context, to string, on bool) error
	// Logout removes this device from the phone.
	Logout(ctx context.Context) error
}

// Sealer seals and opens the session (vault.Box).
type Sealer interface {
	Seal(plain, aad []byte) ([]byte, error)
	Open(sealed, aad []byte) ([]byte, error)
}

// Chats is the part of the chat service the bridge talks through.
type Chats interface {
	Create(title string, members []string) (*chat.Session, error)
	Get(id string) (*chat.Session, error)
	Send(ctx context.Context, id, text string) ([]chat.Turn, error)
	SendFiles(ctx context.Context, id, text string, files []attach.Upload) ([]chat.Turn, error)
	Subscribe(id string) (<-chan chat.Event, func())
}

// Registry is where the connector is offered to the kitties while the link is on.
type Registry interface {
	Add(name string, c connector.Connector)
	Remove(name string)
}

// Settings are what the person chooses in Settings.
type Settings struct {
	Enabled bool `json:"enabled"`
	// Allow are the numbers that may talk to the kitty and that the kitties may write to.
	Allow []string `json:"allow"`
	// Kitty answers the messages (id or name).
	Kitty string `json:"kitty"`
	// Groups lets the kitty answer in the groups of AllowGroups, when someone mentions it.
	Groups bool `json:"groups"`
	// AllowGroups are the groups (their JIDs, "…@g.us") the kitty answers in and may write to.
	AllowGroups []string `json:"allow_groups"`
	// Kitties gives a number (digits) or a group (its JID) a kitty of its own instead of Kitty.
	Kitties map[string]string `json:"kitties,omitempty"`
	// MaxPerHour is the most messages sent in an hour (0 is DefaultPerHour), to keep the number
	// from looking like a bot.
	MaxPerHour int `json:"max_per_hour,omitempty"`
	// QuietFrom and QuietTo ("22:00", "08:00", the gateway's time) are the quiet hours: what
	// comes then is answered when they end, and the kitties do not write. Empty: none.
	QuietFrom string `json:"quiet_from,omitempty"`
	QuietTo   string `json:"quiet_to,omitempty"`
	// Voice is when the kitties answer with a voice note: VoiceOff (never), VoiceMirror (a voice
	// note gets one) or VoiceAlways. It needs a speech service (Options.Speak).
	Voice string `json:"voice,omitempty"`
}

// DefaultPerHour is the hourly limit of messages when Settings sets none.
const DefaultPerHour = 60

// MaxPerHourLimit is the highest hourly limit Settings takes.
const MaxPerHourLimit = 1000

// Group is a group the linked number is in, as Settings lists it.
type Group struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// maxSeen is how many groups that wrote are kept for Settings to offer.
const maxSeen = 30

// Status is the link as Settings shows it.
type Status struct {
	Settings
	// KittyName is the name of the kitty that answers.
	KittyName string `json:"kitty_name,omitempty"`
	State     string `json:"state"`
	// QR is the code to scan while State is "qr", and QRImage the same as a PNG data URL.
	QR      string `json:"qr,omitempty"`
	QRImage string `json:"qr_png,omitempty"`
	// Phone is the linked number.
	Phone string `json:"phone,omitempty"`
	// Linked is true while a session is kept.
	Linked bool   `json:"linked"`
	Error  string `json:"error,omitempty"`
	// Ignored counts the messages from numbers not on the list since the start.
	Ignored int `json:"ignored"`
	// LastMessage is when the last allowed message came.
	LastMessage time.Time `json:"last_message,omitzero"`
	// SentLastHour counts the messages sent in the last hour, and Held the ones not sent since the
	// start because the hourly limit was reached.
	SentLastHour int `json:"sent_last_hour"`
	Held         int `json:"held"`
	// HeldLog are the last messages held by the limit, newest first, for Settings to show.
	HeldLog []HeldMessage `json:"held_log"`
	// CanSpeak is true when there is a speech service for voice notes.
	CanSpeak bool `json:"can_speak"`
	// RiskAccepted is true once the person accepted that WhatsApp may ban the number: Settings
	// asks it the first time the link is turned on.
	RiskAccepted bool `json:"risk_accepted"`
	// Quiet is true during the quiet hours; Waiting counts the messages that wait for their end.
	Quiet   bool `json:"quiet"`
	Waiting int  `json:"waiting"`
	// SeenGroups are the groups that wrote lately, newest first, to choose from.
	SeenGroups []Group `json:"seen_groups"`
	// CanChange is false when there is no vault to seal the session with; Why says so.
	CanChange bool   `json:"can_change"`
	Why       string `json:"why,omitempty"`
}

// HeldMessage is a message the hourly limit did not let go.
type HeldMessage struct {
	At time.Time `json:"at"`
	// To is the number (digits) or the group (its JID) it was for; Name the group's name.
	To   string `json:"to"`
	Name string `json:"name,omitempty"`
	// Kind is text, voice, file or reaction; Text its start (the file's name, the emoji).
	Kind string `json:"kind"`
	Text string `json:"text,omitempty"`
}

// maxHeldLog is how many held messages Settings shows, and maxHeldText how much of each.
const (
	maxHeldLog  = 50
	maxHeldText = 160
)

// Options build a Bridge.
type Options struct {
	// Dir keeps settings.json and the sealed session.bin.
	Dir string
	// Box seals the session; nil keeps the link off (there is no vault).
	Box Sealer
	// NewClient makes a connection; nil keeps the link off.
	NewClient func() Client
	Chats     Chats
	// Kitty finds the kitty that answers.
	Kitty func(ref string) (*kitty.Kitty, error)
	// Registry offers the connector while the link is on.
	Registry Registry
	// QRImage draws a code as a PNG data URL (optional).
	QRImage func(code string) (string, error)
	// Transcribe turns a voice note into text (optional: without it the audio is attached as it is).
	Transcribe func(ctx context.Context, f File) (string, error)
	// Speak reads an answer aloud as a voice note (optional: without it the kitties only write).
	Speak func(ctx context.Context, text string) (File, error)
	// File reads a file of the library by its name, for the kitties to send (optional).
	File func(name string) (File, error)
	// Defaults are the settings while none were saved from Settings (the configuration file's).
	Defaults *Settings
	// Log reports what happens (optional).
	Log func(format string, args ...any)
}

// sessionAAD binds the sealed session to its use.
var sessionAAD = []byte("whatsapp:session")

// The errors the API answers.
var (
	ErrNoVault  = errors.New("WhatsApp needs the vault to keep its session sealed, and there is none here")
	ErrNotOn    = errors.New("WhatsApp is off: turn it on in Settings first")
	ErrNotReady = errors.New("WhatsApp is not connected yet: scan the QR code in Settings")
	ErrNotAllow = errors.New("that number is not on the allow list")
	// ErrNotAllowGroup is returned for a group not on the list.
	ErrNotAllowGroup = errors.New("that group is not on the allow list")
	// ErrQuiet is returned for a message a kitty sends during the quiet hours.
	ErrQuiet = errors.New("it is the quiet hours of WhatsApp: nothing is sent until they end")
	// ErrNoSession is a reconnect without a kept session.
	ErrNoSession = errors.New("there is no linked session to reconnect: show a new QR code")
	// ErrRisk is turning the link on before the risk was accepted.
	ErrRisk = errors.New("WhatsApp Web is not an official API and WhatsApp may ban the number: confirm with accept_risk")
	// ErrNoSpeech is a voice note asked for without a speech service.
	ErrNoSpeech = errors.New("there is no speech service for voice notes: add an openai profile with a key")
	// ErrLimit is returned while the hourly limit of messages is reached.
	ErrLimit = errors.New("the hourly limit of WhatsApp messages is reached: try again later, or raise it in Settings")
)

// Bridge links WhatsApp with the kitties.
type Bridge struct {
	opt Options

	// ops runs one change of the link at a time (stopLocked lets mu go while it waits).
	ops     sync.Mutex
	mu      sync.Mutex
	set     Settings
	chats   map[string]string // number (or "g:" and a group) -> chat id
	seen    []Group
	sent    []time.Time          // when the messages of the last hour went
	held    []HeldMessage        // the last ones the limit held, oldest first
	risk    time.Time            // when the person accepted the risk (zero: not yet)
	refs    map[string]Ref       // the last message of each chat (by key), to react to
	noted   map[string]time.Time // the chats told about the quiet hours, until when
	queue   map[string]pending   // the messages waiting for the quiet hours to end, by id
	seq     int
	resumed bool
	runCtx  context.Context // the connection's, while there is one
	waiting int
	// now and sleep are the clock (a fake one in the tests).
	now    func() time.Time
	sleep  func(ctx context.Context, d time.Duration)
	status Status
	client Client
	cancel context.CancelFunc
	done   chan struct{}
	base   context.Context
	last   string // the last allowed number that wrote
	// tails is the last message of each chat being answered: the next one waits for it, so a
	// chat's messages are answered one at a time, in the order they came.
	tails map[string]chan struct{}
	wg    sync.WaitGroup
	// wait is how long a reply that works on the desktop is followed.
	wait time.Duration
}

type saved struct {
	Settings
	Chats map[string]string `json:"chats,omitempty"`
	Seen  []Group           `json:"seen_groups,omitempty"`
	// Risk is when the person accepted the risk of the unofficial API.
	Risk time.Time `json:"risk_accepted,omitzero"`
}

// New reads the saved settings. Nothing connects before Start.
func New(opt Options) (*Bridge, error) {
	if opt.Log == nil {
		opt.Log = func(string, ...any) {}
	}
	b := &Bridge{opt: opt, chats: map[string]string{}, tails: map[string]chan struct{}{}, refs: map[string]Ref{}, noted: map[string]time.Time{}, queue: map[string]pending{}, wait: 2 * time.Hour,
		now: time.Now, sleep: sleepCtx}
	data, err := os.ReadFile(b.settingsPath())
	switch {
	case errors.Is(err, os.ErrNotExist):
		if opt.Defaults != nil {
			b.set = *opt.Defaults
			b.set.Allow = []string{}
			for _, n := range opt.Defaults.Allow {
				d, err := Normalize(n)
				if err != nil {
					return nil, err
				}
				b.set.Allow = append(b.set.Allow, d)
			}
			if b.set.AllowGroups, err = groupList(opt.Defaults.AllowGroups); err != nil {
				return nil, err
			}
			if b.set.Kitties, err = b.kittiesOf(opt.Defaults.Kitties); err != nil {
				return nil, err
			}
			if b.set.QuietFrom, b.set.QuietTo, err = quietHours(opt.Defaults.QuietFrom, opt.Defaults.QuietTo); err != nil {
				return nil, err
			}
			if b.set.Voice, err = voiceMode(opt.Defaults.Voice); err != nil {
				return nil, err
			}
			if b.set.Enabled && !b.usable() {
				b.set.Enabled = false
				opt.Log("whatsapp: %v", ErrNoVault)
			}
		}
	case err != nil:
		return nil, err
	default:
		var s saved
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("%s: %w", b.settingsPath(), err)
		}
		b.set = s.Settings
		if s.Chats != nil {
			b.chats = s.Chats
		}
		b.seen = s.Seen
		b.risk = s.Risk
	}
	b.status.State = StateOff
	return b, nil
}

func (b *Bridge) settingsPath() string { return filepath.Join(b.opt.Dir, "settings.json") }
func (b *Bridge) sessionPath() string  { return filepath.Join(b.opt.Dir, "session.bin") }

func (b *Bridge) usable() bool { return b.opt.Box != nil && b.opt.NewClient != nil }

// Start connects when the link is on, and keeps ctx for the later changes.
func (b *Bridge) Start(ctx context.Context) {
	b.ops.Lock()
	defer b.ops.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.base = ctx
	if b.set.Enabled && b.usable() {
		b.startLocked()
	}
}

// Stop disconnects and waits for the messages being answered.
func (b *Bridge) Stop() {
	b.ops.Lock()
	defer b.ops.Unlock()
	b.mu.Lock()
	b.stopLocked()
	b.mu.Unlock()
	b.wg.Wait()
}

// Status is the link now.
func (b *Bridge) Status() Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.status
	st.Settings = b.set
	st.Allow = append([]string{}, b.set.Allow...)
	st.AllowGroups = append([]string{}, b.set.AllowGroups...)
	st.SeenGroups = append([]Group{}, b.seen...)
	st.Kitties = map[string]string{}
	for k, v := range b.set.Kitties {
		st.Kitties[k] = v
	}
	st.SentLastHour = b.recentLocked(b.now())
	st.HeldLog = make([]HeldMessage, 0, len(b.held))
	for i := len(b.held) - 1; i >= 0; i-- {
		st.HeldLog = append(st.HeldLog, b.held[i])
	}
	st.CanSpeak = b.opt.Speak != nil
	// Turning it on from the configuration file is accepting the risk there.
	st.RiskAccepted = !b.risk.IsZero() || b.set.Enabled
	st.Quiet, _ = b.quietLocked(b.now())
	st.Waiting = b.waiting
	st.CanChange = b.usable()
	if b.opt.Box == nil {
		st.Why = ErrNoVault.Error()
	}
	if b.set.Kitty != "" && b.opt.Kitty != nil {
		if k, err := b.opt.Kitty(b.set.Kitty); err == nil {
			st.KittyName = k.Name
		}
	}
	_, err := os.Stat(b.sessionPath())
	st.Linked = err == nil
	return st
}

// Apply keeps new settings and connects or disconnects to match. Turning the link on the first
// time needs acceptRisk: WhatsApp Web is not an official API and the number can be banned.
func (b *Bridge) Apply(s Settings, acceptRisk bool) error {
	clean, err := b.check(s)
	if err != nil {
		return err
	}
	b.ops.Lock()
	defer b.ops.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	was := b.set.Enabled
	if clean.Enabled && !was && b.risk.IsZero() {
		if !acceptRisk {
			return ErrRisk
		}
		b.risk = b.now().UTC()
	}
	b.set = clean
	if err := b.saveLocked(); err != nil {
		return err
	}
	switch {
	case clean.Enabled && !was:
		b.startLocked()
	case !clean.Enabled && was:
		b.stopLocked()
	}
	b.syncConnectorLocked()
	return nil
}

func (b *Bridge) check(s Settings) (Settings, error) {
	if s.Enabled && !b.usable() {
		return Settings{}, ErrNoVault
	}
	if len(s.Allow) > MaxAllow {
		return Settings{}, fmt.Errorf("the allow list holds at most %d numbers", MaxAllow)
	}
	out := Settings{Enabled: s.Enabled, Kitty: strings.TrimSpace(s.Kitty), Groups: s.Groups, Allow: []string{}}
	groups, err := groupList(s.AllowGroups)
	if err != nil {
		return Settings{}, err
	}
	out.AllowGroups = groups
	if s.MaxPerHour < 0 || s.MaxPerHour > MaxPerHourLimit {
		return Settings{}, fmt.Errorf("the hourly limit goes from 1 to %d messages (0 is %d)", MaxPerHourLimit, DefaultPerHour)
	}
	out.MaxPerHour = s.MaxPerHour
	if out.Voice, err = voiceMode(s.Voice); err != nil {
		return Settings{}, err
	}
	if out.QuietFrom, out.QuietTo, err = quietHours(s.QuietFrom, s.QuietTo); err != nil {
		return Settings{}, err
	}
	if out.Kitties, err = b.kittiesOf(s.Kitties); err != nil {
		return Settings{}, err
	}
	seen := map[string]bool{}
	for _, n := range s.Allow {
		d, err := Normalize(n)
		if err != nil {
			return Settings{}, err
		}
		if !seen[d] {
			seen[d] = true
			out.Allow = append(out.Allow, d)
		}
	}
	if out.Enabled && out.Kitty == "" {
		return Settings{}, errors.New("choose the kitty that answers on WhatsApp")
	}
	if out.Kitty != "" && b.opt.Kitty != nil {
		k, err := b.opt.Kitty(out.Kitty)
		if err != nil {
			return Settings{}, err
		}
		out.Kitty = k.ID
	}
	return out, nil
}

// voiceMode is a voice mode of Settings, VoiceOff for "", "off" and "never".
func voiceMode(v string) (string, error) {
	switch v = strings.ToLower(strings.TrimSpace(v)); v {
	case VoiceOff, "off", "never":
		return VoiceOff, nil
	case VoiceMirror, VoiceAlways:
		return v, nil
	}
	return "", fmt.Errorf("voice is %q (voice notes answer voice notes) or %q (every answer), or empty", VoiceMirror, VoiceAlways)
}

// ClearHeld forgets the messages the hourly limit held.
func (b *Bridge) ClearHeld() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.held = nil
	b.status.Held = 0
}

// quietHours checks the quiet hours: both or none, as HH:MM, and not the same.
func quietHours(from, to string) (string, string, error) {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == "" && to == "" {
		return "", "", nil
	}
	f, err1 := time.Parse("15:04", from)
	t, err2 := time.Parse("15:04", to)
	if err1 != nil || err2 != nil {
		return "", "", errors.New(`the quiet hours go from one time to another, like "22:00" and "08:00"`)
	}
	if f.Equal(t) {
		return "", "", errors.New("the quiet hours start and end at the same time")
	}
	return f.Format("15:04"), t.Format("15:04"), nil
}

// quietLocked reports whether now is in the quiet hours, and when they end.
func (b *Bridge) quietLocked(now time.Time) (bool, time.Time) {
	if b.set.QuietFrom == "" {
		return false, time.Time{}
	}
	f, _ := time.Parse("15:04", b.set.QuietFrom)
	t, _ := time.Parse("15:04", b.set.QuietTo)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	at := func(d time.Time, h time.Time) time.Time {
		return d.Add(time.Duration(h.Hour())*time.Hour + time.Duration(h.Minute())*time.Minute)
	}
	start, end := at(day, f), at(day, t)
	if !end.After(start) { // over midnight
		if now.Before(end) {
			return true, end
		}
		end = at(day.AddDate(0, 0, 1), t)
	}
	if !now.Before(start) && now.Before(end) {
		return true, end
	}
	return false, time.Time{}
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// quiet is what a person who writes during the quiet hours reads, once.
var quiet = map[string]string{
	"en": "🌙 {kitty} is resting until {until}; it will answer then.",
	"es": "🌙 {kitty} descansa hasta las {until}; te responde entonces.",
	"pt": "🌙 {kitty} está descansando até as {until}; responde depois.",
}

// waitQuiet holds a message until the quiet hours end, telling its chat once.
func (b *Bridge) waitQuiet(ctx context.Context, c Client, in inbound) {
	b.mu.Lock()
	on, until := b.quietLocked(b.now())
	if !on {
		b.mu.Unlock()
		return
	}
	note := !b.noted[in.key].Equal(until)
	b.noted[in.key] = until
	k := b.kittyLocked(in.who)
	b.waiting++
	b.mu.Unlock()
	if note {
		name, lang := "", ""
		if k != nil {
			name, lang = k.Name, k.Soul.Language
		}
		if name == "" {
			name = "The kitty"
		}
		text := strings.NewReplacer("{kitty}", name, "{until}", until.Format("15:04")).Replace(quiet[LanguageOf(lang, in.raw)])
		b.reply(ctx, c, in.to, text)
	}
	b.sleep(ctx, until.Sub(b.now()))
	b.mu.Lock()
	b.waiting--
	b.mu.Unlock()
}

// kittiesOf checks a map of numbers and groups to kitties, keeping the kitties' ids.
func (b *Bridge) kittiesOf(in map[string]string) (map[string]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if len(in) > 2*MaxAllow {
		return nil, fmt.Errorf("at most %d numbers and groups have a kitty of their own", 2*MaxAllow)
	}
	out := map[string]string{}
	for key, ref := range in {
		key, ref = strings.TrimSpace(key), strings.TrimSpace(ref)
		if !IsGroup(key) {
			d, err := Normalize(key)
			if err != nil {
				return nil, err
			}
			key = d
		}
		if ref == "" {
			continue
		}
		if b.opt.Kitty != nil {
			k, err := b.opt.Kitty(ref)
			if err != nil {
				return nil, err
			}
			ref = k.ID
		}
		out[key] = ref
	}
	return out, nil
}

// groupList checks and dedupes a list of group JIDs.
func groupList(in []string) ([]string, error) {
	if len(in) > MaxAllow {
		return nil, fmt.Errorf("the group list holds at most %d groups", MaxAllow)
	}
	out := []string{}
	seen := map[string]bool{}
	for _, g := range in {
		g = strings.TrimSpace(g)
		if !IsGroup(g) {
			return nil, fmt.Errorf("%q is not a WhatsApp group (like 120363012345678901@g.us)", g)
		}
		if !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	return out, nil
}

// IsGroup reports whether s is a group's JID: digits (and a dash in old groups) and "@g.us".
func IsGroup(s string) bool {
	id, ok := strings.CutSuffix(s, "@g.us")
	if !ok || id == "" || len(id) > 40 {
		return false
	}
	for _, r := range id {
		if (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

// Reconnect connects again with the kept session, without a new QR code.
func (b *Bridge) Reconnect() error {
	b.ops.Lock()
	defer b.ops.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.set.Enabled {
		return ErrNotOn
	}
	if _, err := os.Stat(b.sessionPath()); err != nil {
		return ErrNoSession
	}
	b.stopLocked()
	b.startLocked()
	return nil
}

// Link asks for new QR codes (after they expired, or the phone logged the device out).
func (b *Bridge) Link() error {
	b.ops.Lock()
	defer b.ops.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.set.Enabled {
		return ErrNotOn
	}
	b.stopLocked()
	b.startLocked()
	return nil
}

// Unlink removes the device from the phone and forgets the session.
func (b *Bridge) Unlink(ctx context.Context) error {
	b.ops.Lock()
	defer b.ops.Unlock()
	b.mu.Lock()
	c := b.client
	connected := b.status.State == StateConnected
	b.mu.Unlock()
	if c != nil && connected {
		if err := c.Logout(ctx); err != nil {
			b.opt.Log("whatsapp: logout: %v", err)
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stopLocked()
	if err := os.Remove(b.sessionPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	b.status.Phone = ""
	b.chats = map[string]string{}
	b.queue = map[string]pending{}
	b.saveQueueLocked()
	if err := b.saveLocked(); err != nil {
		return err
	}
	if b.set.Enabled {
		// Still on: show the codes for a new link.
		b.startLocked()
	}
	return nil
}

// Test sends a message to a number on the list, for the button in Settings.
func (b *Bridge) Test(ctx context.Context, to string) error {
	// The person's own test goes even in the quiet hours.
	return b.send(context.WithValue(ctx, personKey{}, true), to, "Kitties: WhatsApp is linked. ✓")
}

type personKey struct{}

// testing reports whether the person (not a kitty) asked for the message.
func (b *Bridge) testing(ctx context.Context) bool {
	on, _ := ctx.Value(personKey{}).(bool)
	return on
}

func (b *Bridge) saveLocked() error {
	if err := os.MkdirAll(b.opt.Dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(saved{Settings: b.set, Chats: b.chats, Seen: b.seen, Risk: b.risk}, "", "  ")
	if err != nil {
		return err
	}
	tmp := b.settingsPath() + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, b.settingsPath())
}

func (b *Bridge) startLocked() {
	if b.base == nil {
		return // Start was not called yet: it connects then
	}
	ctx, cancel := context.WithCancel(b.base)
	c := b.opt.NewClient()
	done := make(chan struct{})
	b.client, b.cancel, b.done, b.runCtx = c, cancel, done, ctx
	b.status.State, b.status.Error, b.status.QR, b.status.QRImage = StateStarting, "", "", ""
	session, err := b.loadSession()
	if err != nil {
		// A session that cannot be opened (another key) is not used: a new link is made.
		b.opt.Log("whatsapp: the kept session cannot be opened (%v); link the phone again", err)
		session = nil
	}
	go func() {
		defer close(done)
		err := c.Run(ctx, session, func(ev Event) { b.on(c, ev) })
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.client != c {
			return
		}
		b.client = nil
		b.syncConnectorLocked()
		if err != nil && ctx.Err() == nil && b.status.State != StateLoggedOut && b.status.State != StateQRExpired {
			b.status.State, b.status.Error = StateError, err.Error()
			b.opt.Log("whatsapp: %v", err)
		}
	}()
}

func (b *Bridge) stopLocked() {
	if b.cancel != nil {
		b.cancel()
		done := b.done
		b.mu.Unlock()
		<-done
		b.mu.Lock()
	}
	b.client, b.cancel, b.done, b.runCtx = nil, nil, nil, nil
	b.resumed = false // the next connection answers what still waits
	b.status.State, b.status.QR, b.status.QRImage = StateOff, "", ""
	b.syncConnectorLocked()
}

// syncConnectorLocked offers the connector while the link is on and connected.
func (b *Bridge) syncConnectorLocked() {
	if b.opt.Registry == nil {
		return
	}
	if b.set.Enabled && b.client != nil && b.status.State == StateConnected {
		b.opt.Registry.Add(ConnectorName, connectorOf{b})
	} else {
		b.opt.Registry.Remove(ConnectorName)
	}
}

func (b *Bridge) loadSession() ([]byte, error) {
	sealed, err := os.ReadFile(b.sessionPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return b.opt.Box.Open(sealed, sessionAAD)
}

func (b *Bridge) saveSession(data []byte) error {
	sealed, err := b.opt.Box.Seal(data, sessionAAD)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(b.opt.Dir, 0o700); err != nil {
		return err
	}
	tmp := b.sessionPath() + ".tmp"
	if err := os.WriteFile(tmp, sealed, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, b.sessionPath())
}

// on handles an event of the client c (events of a client already replaced are dropped).
func (b *Bridge) on(c Client, ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.client != c {
		return
	}
	switch ev.Kind {
	case EvQR:
		b.status.State, b.status.QR, b.status.QRImage = StateQR, ev.QR, ""
		if b.opt.QRImage != nil {
			if img, err := b.opt.QRImage(ev.QR); err == nil {
				b.status.QRImage = img
			}
		}
	case EvQRExpired:
		b.status.State, b.status.QR, b.status.QRImage = StateQRExpired, "", ""
	case EvPaired:
		b.status.Phone = ev.Phone
		b.opt.Log("whatsapp: linked to +%s", ev.Phone)
	case EvConnected:
		b.status.State, b.status.QR, b.status.QRImage, b.status.Error = StateConnected, "", "", ""
		if ev.Phone != "" {
			b.status.Phone = ev.Phone
		}
		b.resumeLocked(c)
	case EvDisconnected:
		b.status.State = StateDisconnected
	case EvLoggedOut:
		b.status.State, b.status.Phone = StateLoggedOut, ""
		_ = os.Remove(b.sessionPath())
		b.opt.Log("whatsapp: the phone removed the linked device")
	case EvSession:
		if err := b.saveSession(ev.Session); err != nil {
			b.status.Error = "the session could not be kept: " + err.Error()
			b.opt.Log("whatsapp: %s", b.status.Error)
		}
	case EvError:
		b.status.Error = ev.Err
	case EvMessage:
		b.receiveLocked(c, ev)
	}
	b.syncConnectorLocked()
}

// allowedLocked reports whether a number is on the list.
func (b *Bridge) allowedLocked(n string) bool {
	for _, a := range b.set.Allow {
		if a == n {
			return true
		}
	}
	return false
}

func (b *Bridge) receiveLocked(c Client, ev Event) {
	text := strings.TrimSpace(ev.Text)
	if (text == "" && len(ev.Files) == 0) || ev.From == "" {
		return
	}
	if ev.Group && (!b.set.Groups || !IsGroup(ev.Chat)) {
		return
	}
	in := inbound{from: ev.From, name: ev.Name, text: text, raw: text, files: ev.Files, key: ev.From, to: ev.From}
	if ev.Group {
		// In a group on the list anyone may talk to the kitty, by naming it.
		b.sawLocked(ev.Chat, ev.ChatName)
		if !ev.Mentioned && !mentions(text, b.kittyNameLocked(ev.Chat)) {
			return
		}
		if !b.groupAllowedLocked(ev.Chat) {
			b.status.Ignored++
			return
		}
		who := ev.Name
		if who == "" {
			who = "+" + ev.From
		}
		in.key, in.to, in.group, in.groupName = "g:"+ev.Chat, ev.Chat, true, ev.ChatName
		in.text = who + ": " + text
	} else {
		if !b.allowedLocked(ev.From) {
			b.status.Ignored++
			return
		}
		b.last = ev.From
	}
	b.status.LastMessage = time.Now()
	in.ref, in.who = ev.Ref, in.to
	if ev.Ref.ID != "" {
		b.refs[in.key] = ev.Ref
	}
	// A message that comes in the quiet hours is kept sealed until it is answered, so a restart
	// does not lose it.
	if on, _ := b.quietLocked(b.now()); on {
		b.seq++
		in.qid = fmt.Sprintf("%d-%d", b.now().UnixNano(), b.seq)
		b.queue[in.qid] = pendingOf(in)
		b.saveQueueLocked()
	}
	b.dispatchLocked(c, in)
}

// dispatchLocked answers in on its own, after the earlier messages of its chat.
func (b *Bridge) dispatchLocked(c Client, in inbound) {
	prev, done := b.tails[in.key], make(chan struct{})
	b.tails[in.key] = done
	// The quiet hours are waited out on the connection's context: a stop or a reconnect ends the
	// wait, and the message stays in the queue for the next connection.
	ctx, conn := b.base, b.runCtx
	if conn == nil {
		conn = ctx
	}
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		defer func() {
			close(done)
			b.mu.Lock()
			if b.tails[in.key] == done {
				delete(b.tails, in.key)
			}
			b.mu.Unlock()
		}()
		if prev != nil {
			<-prev
		}
		b.waitQuiet(conn, c, in)
		if conn.Err() != nil || ctx.Err() != nil {
			return
		}
		if in.qid != "" {
			b.mu.Lock()
			delete(b.queue, in.qid)
			b.saveQueueLocked()
			b.mu.Unlock()
		}
		b.answer(ctx, c, in)
	}()
}

// pending is a message kept for the end of the quiet hours.
type pending struct {
	From      string `json:"from"`
	Name      string `json:"name,omitempty"`
	Text      string `json:"text"`
	Raw       string `json:"raw"`
	Files     []File `json:"files,omitempty"`
	Key       string `json:"key"`
	To        string `json:"to"`
	Who       string `json:"who"`
	Group     bool   `json:"group,omitempty"`
	GroupName string `json:"group_name,omitempty"`
	Ref       Ref    `json:"ref"`
}

func pendingOf(in inbound) pending {
	return pending{From: in.from, Name: in.name, Text: in.text, Raw: in.raw, Files: in.files, Key: in.key, To: in.to,
		Who: in.who, Group: in.group, GroupName: in.groupName, Ref: in.ref}
}

func (p pending) inbound(qid string) inbound {
	return inbound{from: p.From, name: p.Name, text: p.Text, raw: p.Raw, files: p.Files, key: p.Key, to: p.To,
		who: p.Who, group: p.Group, groupName: p.GroupName, ref: p.Ref, qid: qid}
}

// queueAAD binds the sealed queue to its use.
var queueAAD = []byte("whatsapp:queue")

func (b *Bridge) queuePath() string { return filepath.Join(b.opt.Dir, "queue.bin") }

// saveQueueLocked keeps the waiting messages sealed (nothing on disk when there are none).
func (b *Bridge) saveQueueLocked() {
	if len(b.queue) == 0 {
		if err := os.Remove(b.queuePath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			b.opt.Log("whatsapp: forget the waiting messages: %v", err)
		}
		return
	}
	err := func() error {
		data, err := json.Marshal(b.queue)
		if err != nil {
			return err
		}
		sealed, err := b.opt.Box.Seal(data, queueAAD)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(b.opt.Dir, 0o700); err != nil {
			return err
		}
		tmp := b.queuePath() + ".tmp"
		if err := os.WriteFile(tmp, sealed, 0o600); err != nil {
			return err
		}
		return os.Rename(tmp, b.queuePath())
	}()
	if err != nil {
		b.opt.Log("whatsapp: keep the waiting messages: %v", err)
	}
}

// resumeLocked answers the messages kept from before a restart, once, when it connects.
func (b *Bridge) resumeLocked(c Client) {
	if b.resumed {
		return
	}
	b.resumed = true
	sealed, err := os.ReadFile(b.queuePath())
	if err != nil {
		return
	}
	var kept map[string]pending
	data, err := b.opt.Box.Open(sealed, queueAAD)
	if err == nil {
		err = json.Unmarshal(data, &kept)
	}
	if err != nil {
		b.opt.Log("whatsapp: the waiting messages cannot be read (%v); they are dropped", err)
		_ = os.Remove(b.queuePath())
		return
	}
	ids := make([]string, 0, len(kept))
	for id := range kept {
		ids = append(ids, id)
	}
	sort.Strings(ids) // in the order they came
	for _, id := range ids {
		b.queue[id] = kept[id]
		b.dispatchLocked(c, kept[id].inbound(id))
	}
	b.opt.Log("whatsapp: %d waiting messages from before the restart", len(ids))
}

// inbound is a message to answer: key names its chat (a number, or "g:" and a group), to is
// where the answer goes.
type inbound struct {
	from, name, text string
	// raw is the text as it came, to quote it.
	raw       string
	files     []File
	key, to   string
	group     bool
	groupName string
	// who is the number or group whose kitty answers; ref the message.
	who string
	ref Ref
	// qid is its place in the sealed queue while it waits for the quiet hours to end.
	qid string
}

// groupAllowedLocked reports whether a group is on the list.
func (b *Bridge) groupAllowedLocked(g string) bool {
	for _, a := range b.set.AllowGroups {
		if a == g {
			return true
		}
	}
	return false
}

// sawLocked keeps a group that wrote at the top of the ones Settings offers.
func (b *Bridge) sawLocked(id, name string) {
	if len(b.seen) > 0 && b.seen[0].ID == id && (name == "" || b.seen[0].Name == name) {
		return
	}
	g := Group{ID: id, Name: name}
	out := []Group{g}
	for _, s := range b.seen {
		if s.ID == id {
			if g.Name == "" {
				out[0].Name = s.Name
			}
			continue
		}
		if len(out) < maxSeen {
			out = append(out, s)
		}
	}
	b.seen = out
	if err := b.saveLocked(); err != nil {
		b.opt.Log("whatsapp: keep the groups: %v", err)
	}
}

// kittyRefLocked is the kitty that answers a number or a group (its JID).
func (b *Bridge) kittyRefLocked(who string) string {
	if ref := b.set.Kitties[who]; ref != "" {
		return ref
	}
	return b.set.Kitty
}

// kittyLocked is that kitty, when it is there.
func (b *Bridge) kittyLocked(who string) *kitty.Kitty {
	ref := b.kittyRefLocked(who)
	if b.opt.Kitty == nil || ref == "" {
		return nil
	}
	k, err := b.opt.Kitty(ref)
	if err != nil {
		return nil
	}
	return k
}

func (b *Bridge) kittyNameLocked(who string) string {
	if k := b.kittyLocked(who); k != nil {
		return k.Name
	}
	return ""
}

// mentions reports whether a message in a group names the kitty ("mimi, ..." or "@mimi").
func mentions(text, name string) bool {
	if name == "" {
		return false
	}
	lower, name := strings.ToLower(text), strings.ToLower(name)
	for i := strings.Index(lower, name); i >= 0; {
		end := i + len(name)
		before := i == 0 || !isWordByte(lower[i-1])
		after := end == len(lower) || !isWordByte(lower[end])
		if before && after {
			return true
		}
		next := strings.Index(lower[i+1:], name)
		if next < 0 {
			break
		}
		i += 1 + next
	}
	return false
}

func isWordByte(c byte) bool {
	return c == '_' || c == '-' || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c >= 0x80
}

// chatFor is the chat of a number or a group, made the first time it writes.
func (b *Bridge) chatFor(in inbound) (string, error) {
	b.mu.Lock()
	id, kittyRef := b.chats[in.key], b.kittyRefLocked(in.to)
	if k := b.kittyLocked(in.to); k != nil {
		kittyRef = k.ID
	}
	b.mu.Unlock()
	if id != "" {
		// A chat whose kitty is no longer the one that answers is left for a new one.
		if sess, err := b.opt.Chats.Get(id); err == nil && (len(sess.Members) == 0 || contains(sess.Members, kittyRef)) {
			return id, nil
		}
	}
	title := "WhatsApp +" + in.from
	switch {
	case in.group && in.groupName != "":
		title = "WhatsApp group: " + in.groupName
	case in.group:
		title = "WhatsApp group " + strings.TrimSuffix(in.to, "@g.us")
	case in.name != "":
		title = "WhatsApp: " + in.name
	}
	sess, err := b.opt.Chats.Create(title, []string{kittyRef})
	if err != nil {
		return "", err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.chats[in.key] = sess.ID
	return sess.ID, b.saveLocked()
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// typingEvery is how often "writing…" is shown again while the kitty thinks (WhatsApp drops it
// after some 25 seconds).
var typingEvery = 10 * time.Second

// typing shows that the kitty is writing until the returned func is called.
func (b *Bridge) typing(ctx context.Context, c Client, to string) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(typingEvery)
		defer t.Stop()
		for {
			_ = c.Typing(ctx, to, true)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	return func() {
		cancel()
		<-done
		_ = c.Typing(context.WithoutCancel(ctx), to, false)
	}
}

// uploads are the files of a message as the chat takes them; a voice note becomes its words
// when it can be transcribed.
func (b *Bridge) uploads(ctx context.Context, in *inbound) []attach.Upload {
	var out []attach.Upload
	for i, f := range in.files {
		if f.Voice && b.opt.Transcribe != nil {
			words, err := b.opt.Transcribe(ctx, f)
			if err == nil && strings.TrimSpace(words) != "" {
				in.text = strings.TrimSpace(in.text + "\n(voice note) " + strings.TrimSpace(words))
				continue
			}
			b.opt.Log("whatsapp: the voice note could not be transcribed: %v", err)
		}
		name := f.Name
		if name == "" {
			name = fmt.Sprintf("whatsapp-%d", i+1)
		}
		out = append(out, attach.Upload{Name: name, MIME: f.MIME, Data: f.Data})
	}
	return out
}

// answer posts a message in its chat and writes the kitty's replies back. A reply that goes on
// working on the desktop is followed until it is done.
func (b *Bridge) answer(ctx context.Context, c Client, in inbound) {
	id, err := b.chatFor(in)
	if err != nil {
		b.opt.Log("whatsapp: no chat for +%s: %v", in.from, err)
		return
	}
	if in.ref.ID != "" {
		if err := c.MarkRead(ctx, in.ref); err != nil {
			b.opt.Log("whatsapp: mark read: %v", err)
		}
	}
	stopTyping := b.typing(ctx, c, in.to)
	voice := b.speaks(&in)
	files := b.uploads(ctx, &in)
	events, unsubscribe := b.opt.Chats.Subscribe(id)
	defer unsubscribe()
	var turns []chat.Turn
	if len(files) > 0 {
		turns, err = b.opt.Chats.SendFiles(ctx, id, in.text, files)
	} else {
		turns, err = b.opt.Chats.Send(ctx, id, in.text)
	}
	stopTyping()
	if errors.Is(err, chat.ErrBusy) {
		b.reply(ctx, c, in.to, b.busyText(in.who, in.text))
		return
	}
	if err != nil {
		b.opt.Log("whatsapp: the message of +%s: %v", in.from, err)
		return
	}
	sess, err := b.opt.Chats.Get(id)
	if err != nil {
		return
	}
	// The replies are the last turns of the chat: their indexes tell their later updates.
	first := len(sess.Turns) - len(turns)
	working := map[int]string{}
	for i, t := range turns {
		b.say(ctx, c, in.to, &in, t.Text, voice && !t.Working)
		b.replyFiles(ctx, c, in.to, t.Artifacts)
		if t.Working {
			working[first+i] = t.Text
		}
	}
	timeout := time.NewTimer(b.wait)
	defer timeout.Stop()
	for len(working) > 0 {
		select {
		case <-ctx.Done():
			return
		case <-timeout.C:
			return
		case ev := <-events:
			sent, ok := working[ev.Index]
			if !ok || ev.Type != "done" || ev.Turn == nil {
				continue
			}
			delete(working, ev.Index)
			b.say(ctx, c, in.to, nil, strings.TrimSpace(strings.TrimPrefix(ev.Turn.Text, sent)), voice)
			b.replyFiles(ctx, c, in.to, ev.Turn.Artifacts)
		}
	}
}

// busy is what a person who writes while the kitty still works on their last message reads, in
// the kitty's language (or, when it follows the person, the message's).
var busy = map[string]string{
	"en": "⏳ I am still on your last message; I will write when it is done.",
	"es": "⏳ Sigo con tu mensaje anterior; te escribo apenas termine.",
	"pt": "⏳ Ainda estou na sua mensagem anterior; escrevo assim que terminar.",
}

func (b *Bridge) busyText(who, text string) string {
	b.mu.Lock()
	k := b.kittyLocked(who)
	b.mu.Unlock()
	lang := ""
	if k != nil {
		lang = k.Soul.Language
	}
	return busy[LanguageOf(lang, text)]
}

// LanguageOf is en, es or pt: the kitty's language when it has one, else a guess from the text.
func LanguageOf(lang, text string) string {
	l := strings.ToLower(strings.TrimSpace(lang))
	switch {
	case strings.HasPrefix(l, "es") || strings.HasPrefix(l, "spa") || strings.HasPrefix(l, "cast"):
		return "es"
	case strings.HasPrefix(l, "pt") || strings.HasPrefix(l, "por"):
		return "pt"
	case l != "":
		return "en"
	}
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 0xC0)
	})
	score := map[string]int{}
	for _, w := range words {
		switch w {
		case "hola", "gracias", "por", "favor", "que", "qué", "el", "los", "las", "una", "es", "está", "puedes", "necesito", "también", "pero", "cómo", "dónde":
			score["es"]++
		case "olá", "obrigado", "obrigada", "você", "não", "uma", "pode", "preciso", "também", "mas", "como", "onde", "isso":
			score["pt"]++
		case "hello", "hi", "thanks", "please", "the", "is", "are", "you", "can", "need", "what", "where":
			score["en"]++
		}
	}
	if strings.ContainsAny(text, "¿¡ñ") {
		score["es"] += 2
	}
	if strings.ContainsAny(text, "ãõç") {
		score["pt"] += 2
	}
	best := "en"
	for _, l := range []string{"es", "pt"} {
		if score[l] > score[best] {
			best = l
		}
	}
	return best
}

// reply writes text to a number, in parts WhatsApp takes.
func (b *Bridge) reply(ctx context.Context, c Client, to, text string) {
	b.replyTo(ctx, c, to, nil, text)
}

// replyTo writes text to a number or a group as the answer to in (nil: none). An answer that
// is a single emoji goes as a reaction; one in a group, or after newer messages of the chat,
// quotes the message it answers.
func (b *Bridge) replyTo(ctx context.Context, c Client, to string, in *inbound, text string) {
	var ref Ref
	quote := false
	if in != nil {
		ref = in.ref
		b.mu.Lock()
		quote = ref.ID != "" && (in.group || b.refs[in.key].ID != ref.ID)
		b.mu.Unlock()
	}
	if e := strings.TrimSpace(text); ref.ID != "" && IsEmoji(e) {
		if err := b.take(to, "reaction", e); err != nil {
			b.opt.Log("whatsapp: react in %s: %v", to, err)
			return
		}
		if err := c.React(ctx, ref, e); err != nil {
			b.opt.Log("whatsapp: react in %s: %v", to, err)
		}
		return
	}
	for i, part := range Split(Plain(text), maxPart) {
		if err := b.take(to, "text", part); err != nil {
			b.opt.Log("whatsapp: reply to %s: %v", to, err)
			return
		}
		var err error
		if i == 0 && quote {
			err = c.SendReply(ctx, to, part, ref, in.raw)
		} else {
			err = c.Send(ctx, to, part)
		}
		if err != nil {
			b.opt.Log("whatsapp: reply to +%s: %v", to, err)
			return
		}
	}
}

const maxPart = 4000

// take counts one message against the hourly limit, or says it is reached and keeps what it
// held (to, its kind and its text) for Settings to show.
func (b *Bridge) take(to, kind, text string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if b.fullLocked(now) {
		b.status.Held++
		h := HeldMessage{At: now, To: to, Kind: kind, Text: clip(strings.TrimSpace(text), maxHeldText)}
		for _, g := range b.seen {
			if g.ID == to {
				h.Name = g.Name
			}
		}
		b.held = append(b.held, h)
		if len(b.held) > maxHeldLog {
			b.held = b.held[len(b.held)-maxHeldLog:]
		}
		return ErrLimit
	}
	b.sent = append(b.sent, now)
	return nil
}

// fullLocked reports whether the hourly limit is reached.
func (b *Bridge) fullLocked(now time.Time) bool {
	limit := b.set.MaxPerHour
	if limit <= 0 {
		limit = DefaultPerHour
	}
	return b.recentLocked(now) >= limit
}

// clip is s cut to max runes, with an ellipsis when cut.
func clip(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max-1])) + "…"
}

// recentLocked drops the sends older than an hour and counts the rest.
func (b *Bridge) recentLocked(now time.Time) int {
	i := 0
	for i < len(b.sent) && now.Sub(b.sent[i]) >= time.Hour {
		i++
	}
	b.sent = b.sent[i:]
	return len(b.sent)
}

// IsEmoji reports whether s is one emoji (with its modifiers), the way a reaction is.
func IsEmoji(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	pictos := 0
	for _, r := range s {
		switch {
		case r == 0x200d || (r >= 0xfe00 && r <= 0xfe0f) || (r >= 0x1f3fb && r <= 0x1f3ff) || (r >= 0xe0020 && r <= 0xe007f):
			// joiners, variation selectors, skin tones, tags
		case r >= 0x1f1e6 && r <= 0x1f1ff, r >= 0x1f000 && r <= 0x1faff, r >= 0x2600 && r <= 0x27bf, r >= 0x2b00 && r <= 0x2bff, r == 0x2764:
			pictos++
		default:
			return false
		}
	}
	return pictos > 0 && pictos <= 4
}

// maxCaption is the longest text sent as a file's caption; a longer one goes before it.
const maxCaption = 1000

// replyFiles sends the files a kitty saved to the library while it answered.
func (b *Bridge) replyFiles(ctx context.Context, c Client, to string, names []string) {
	for _, name := range names {
		if err := b.sendFile(ctx, c, to, name, ""); err != nil {
			b.opt.Log("whatsapp: send %s to %s: %v", name, to, err)
		}
	}
}

// sendFile sends a file of the library, with its caption.
func (b *Bridge) sendFile(ctx context.Context, c Client, to, name, caption string) error {
	if b.opt.File == nil {
		return errors.New("there is no library to send files from here")
	}
	f, err := b.opt.File(name)
	if err != nil {
		return err
	}
	if len(f.Data) == 0 {
		return fmt.Errorf("%s is empty", name)
	}
	if len(f.Data) > MaxFile {
		return fmt.Errorf("%s is larger than %d MB", name, MaxFile>>20)
	}
	if f.Name == "" {
		f.Name = name
	}
	if err := b.take(to, "file", f.Name); err != nil {
		return err
	}
	return c.SendFile(ctx, to, f, caption)
}

// MaxFile is the largest file the kitties send (WhatsApp takes 100 MB of a document, less of
// the rest; this keeps it to what a phone downloads without asking).
const MaxFile = 16 << 20

// send is a message the person or a kitty asked for: only to a number or a group on the list,
// while connected. file is a file of the library to send with text as its caption.
func (b *Bridge) send(ctx context.Context, to, text string) error {
	return b.sendWith(ctx, to, text, "")
}

func (b *Bridge) sendWith(ctx context.Context, to, text, file string) error {
	c, n, err := b.ready(ctx, to)
	if err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text == "" && file == "" {
		return errors.New("the message is empty")
	}
	parts := Split(Plain(text), maxPart)
	caption := ""
	if file != "" && len(parts) == 1 && len(parts[0]) <= maxCaption {
		caption, parts = parts[0], nil
	}
	for _, part := range parts {
		if err := b.take(n, "text", part); err != nil {
			return err
		}
		if err := c.Send(ctx, n, part); err != nil {
			return err
		}
	}
	if file != "" {
		return b.sendFile(ctx, c, n, file, caption)
	}
	return nil
}

// ready is the connection and where a message the person or a kitty asked for goes: a number or
// a group on the list ("" is whoever wrote last, or the only number), while connected and out of
// the quiet hours.
func (b *Bridge) ready(ctx context.Context, to string) (Client, string, error) {
	b.mu.Lock()
	c, state, enabled, last := b.client, b.status.State, b.set.Enabled, b.last
	allow := append([]string(nil), b.set.Allow...)
	b.mu.Unlock()
	if !enabled {
		return nil, "", ErrNotOn
	}
	if c == nil || state != StateConnected {
		return nil, "", ErrNotReady
	}
	if strings.TrimSpace(to) == "" {
		switch {
		case last != "":
			to = last
		case len(allow) == 1:
			to = allow[0]
		default:
			return nil, "", errors.New(`say who to write to: "to" is a number on the allow list`)
		}
	}
	b.mu.Lock()
	n, err := b.destLocked(to)
	inQuiet, _ := b.quietLocked(b.now())
	b.mu.Unlock()
	if err != nil {
		return nil, "", err
	}
	if inQuiet && !b.testing(ctx) {
		return nil, "", ErrQuiet
	}
	return c, n, nil
}

// sendVoice is a voice note a kitty asked for, with text read aloud.
func (b *Bridge) sendVoice(ctx context.Context, to, text string) error {
	if b.opt.Speak == nil {
		return ErrNoSpeech
	}
	c, n, err := b.ready(ctx, to)
	if err != nil {
		return err
	}
	if strings.TrimSpace(text) == "" {
		return errors.New("the message is empty")
	}
	return b.voiceNote(ctx, c, n, text)
}

// errNotSpoken is an answer that reads better than it sounds (long, or with code in it).
var errNotSpoken = errors.New("this answer is better read than heard: it goes as text")

// speaks reports whether the answer to in goes as a voice note.
func (b *Bridge) speaks(in *inbound) bool {
	if b.opt.Speak == nil {
		return false
	}
	b.mu.Lock()
	mode := b.set.Voice
	b.mu.Unlock()
	switch mode {
	case VoiceAlways:
		return true
	case VoiceMirror:
		for _, f := range in.files {
			if f.Voice {
				return true
			}
		}
	}
	return false
}

// say answers in: as a voice note when voice is on and the answer reads well aloud, else as
// text (also when the speech service fails).
func (b *Bridge) say(ctx context.Context, c Client, to string, in *inbound, text string, voice bool) {
	if voice {
		err := b.voiceNote(ctx, c, to, text)
		if err == nil || errors.Is(err, ErrLimit) {
			return
		}
		if !errors.Is(err, errNotSpoken) {
			b.opt.Log("whatsapp: voice note to %s: %v", to, err)
		}
	}
	b.replyTo(ctx, c, to, in, text)
}

// voiceNote reads text aloud and sends it as a voice note: one message against the limit.
func (b *Bridge) voiceNote(ctx context.Context, c Client, to, text string) error {
	words := spoken(text)
	if words == "" {
		return errNotSpoken
	}
	b.mu.Lock()
	full := b.fullLocked(b.now())
	b.mu.Unlock()
	// A held voice note is not read aloud for nothing.
	if full {
		return b.take(to, "voice", words)
	}
	f, err := b.opt.Speak(ctx, words)
	if err != nil {
		return err
	}
	f.Voice = true
	if err := b.take(to, "voice", words); err != nil {
		return err
	}
	return c.SendFile(ctx, to, f, "")
}

// react puts an emoji on the last message of a number or a group on the list.
func (b *Bridge) react(ctx context.Context, to, emoji string) error {
	b.mu.Lock()
	c, state, enabled, last := b.client, b.status.State, b.set.Enabled, b.last
	b.mu.Unlock()
	if !enabled {
		return ErrNotOn
	}
	if c == nil || state != StateConnected {
		return ErrNotReady
	}
	emoji = strings.TrimSpace(emoji)
	if emoji != "" && !IsEmoji(emoji) {
		return fmt.Errorf("%q is not an emoji", emoji)
	}
	if strings.TrimSpace(to) == "" {
		to = last
	}
	b.mu.Lock()
	dest, err := b.destLocked(to)
	key := dest
	if IsGroup(dest) {
		key = "g:" + dest
	}
	ref := b.refs[key]
	inQuiet, _ := b.quietLocked(b.now())
	b.mu.Unlock()
	if err != nil {
		return err
	}
	if inQuiet {
		return ErrQuiet
	}
	if ref.ID == "" {
		return errors.New("there is no message of that chat to react to yet")
	}
	if err := b.take(dest, "reaction", emoji); err != nil {
		return err
	}
	return c.React(ctx, ref, emoji)
}

// destLocked is where a message for to goes: a number on the list, or a group on it (its JID or
// its name).
func (b *Bridge) destLocked(to string) (string, error) {
	to = strings.TrimSpace(to)
	if IsGroup(to) {
		if !b.groupAllowedLocked(to) {
			return "", ErrNotAllowGroup
		}
		return to, nil
	}
	n, err := Normalize(to)
	if err == nil {
		if !b.allowedLocked(n) {
			return "", ErrNotAllow
		}
		return n, nil
	}
	if !strings.ContainsFunc(to, unicode.IsLetter) {
		return "", err
	}
	name := strings.ToLower(strings.TrimPrefix(to, "group:"))
	for _, g := range b.seen {
		if g.Name != "" && strings.ToLower(g.Name) == strings.TrimSpace(name) && b.groupAllowedLocked(g.ID) {
			return g.ID, nil
		}
	}
	return "", fmt.Errorf("%q is not a number or a group on the allow list", to)
}

// Normalize is a phone number as digits with the country code: "+56 9 1234-5678" and
// "0056912345678" are "56912345678".
func Normalize(n string) (string, error) {
	var d strings.Builder
	for _, r := range n {
		if r >= '0' && r <= '9' {
			d.WriteRune(r)
		}
	}
	out := strings.TrimPrefix(d.String(), "00")
	if len(out) < 7 || len(out) > 15 {
		return "", fmt.Errorf("%q is not a phone number with its country code (like +56912345678)", n)
	}
	return out, nil
}

// Plain turns the Markdown a model writes into WhatsApp's own marks: **bold** is *bold*,
// a heading is a bold line, and a [link](url) is "link (url)".
func Plain(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for i, l := range lines {
		t := strings.TrimLeft(l, " ")
		if strings.HasPrefix(t, "#") {
			h := strings.TrimSpace(strings.TrimLeft(t, "#"))
			if h != "" {
				l = "*" + strings.ReplaceAll(h, "**", "") + "*"
			}
		}
		l = strings.ReplaceAll(l, "**", "*")
		l = strings.ReplaceAll(l, "__", "_")
		lines[i] = links(l)
	}
	return strings.Join(lines, "\n")
}

// links writes [text](url) as "text (url)".
func links(l string) string {
	for {
		open := strings.Index(l, "](")
		if open < 0 {
			return l
		}
		start := strings.LastIndex(l[:open], "[")
		end := strings.Index(l[open:], ")")
		if start < 0 || end < 0 {
			return l
		}
		end += open
		l = l[:start] + l[start+1:open] + " (" + l[open+2:end] + ")" + l[end+1:]
	}
}

// Split cuts text in parts of at most max bytes, at a line or a space when it can.
func Split(text string, max int) []string {
	text = strings.TrimSpace(text)
	var out []string
	for len(text) > max {
		cut := strings.LastIndex(text[:max], "\n")
		if cut <= 0 {
			cut = strings.LastIndex(text[:max], " ")
		}
		if cut <= 0 {
			cut = max
			for cut > 0 && !utf8Start(text[cut]) {
				cut--
			}
		}
		out = append(out, strings.TrimSpace(text[:cut]))
		text = strings.TrimSpace(text[cut:])
	}
	if text != "" {
		out = append(out, text)
	}
	return out
}

func utf8Start(c byte) bool { return c&0xC0 != 0x80 }

// connectorOf is the bridge as the kitties' connector.
type connectorOf struct{ b *Bridge }

// Kind implements connector.Kinder.
func (connectorOf) Kind() string { return Kind }

// Describe implements connector.Connector.
func (c connectorOf) Describe() string {
	c.b.mu.Lock()
	allow := strings.Join(append(prefixed(c.b.set.Allow), c.b.groupNamesLocked()...), ", ")
	c.b.mu.Unlock()
	voice := ""
	if c.b.opt.Speak != nil {
		voice = `{"action": "voice", "to": "+56912345678", "text": "..."} sends "text" read aloud as a voice note (short, plain words: no lists or code). `
	}
	return `WhatsApp from your own linked number: {"action": "send", "to": "+56912345678", "text": "..."} ` +
		`writes to a number or a group on the allow list (` + allow + `; "to" left out writes to whoever wrote last). ` +
		`Add "file": "<name of a file in the library>" to send a picture, video, audio or document, with "text" as its caption. ` +
		`Files you save to the library while answering someone on WhatsApp are sent to them by themselves. ` +
		`{"action": "react", "to": "+56912345678", "emoji": "👍"} puts an emoji on the last message of that chat ("to" left out: whoever wrote last); ` +
		`an answer that is only an emoji goes as a reaction too. ` + voice +
		`{"action": "status"} says whether it is connected. The people on the list also write to you here.`
}

// groupNamesLocked are the groups on the list as the kitties name them.
func (b *Bridge) groupNamesLocked() []string {
	var out []string
	for _, id := range b.set.AllowGroups {
		name := id
		for _, g := range b.seen {
			if g.ID == id && g.Name != "" {
				name = g.Name + " (" + id + ")"
			}
		}
		out = append(out, "group "+name)
	}
	return out
}

func prefixed(list []string) []string {
	out := make([]string, len(list))
	for i, n := range list {
		out[i] = "+" + n
	}
	return out
}

// Call implements connector.Connector.
func (c connectorOf) Call(ctx context.Context, args map[string]any) (string, error) {
	str := func(k string) string { s, _ := args[k].(string); return s }
	switch action := strings.ToLower(str("action")); action {
	case "", "send":
		if err := c.b.sendWith(ctx, str("to"), str("text"), str("file")); err != nil {
			return "", err
		}
		return "sent", nil
	case "react":
		if err := c.b.react(ctx, str("to"), str("emoji")); err != nil {
			return "", err
		}
		return "reacted", nil
	case "voice":
		if err := c.b.sendVoice(ctx, str("to"), str("text")); err != nil {
			return "", err
		}
		return "sent as a voice note", nil
	case "status":
		st := c.b.Status()
		out := "WhatsApp is " + st.State
		if st.Phone != "" {
			out += " as +" + st.Phone
		}
		c.b.mu.Lock()
		to := append(prefixed(st.Allow), c.b.groupNamesLocked()...)
		c.b.mu.Unlock()
		return out + "; it may write to " + strings.Join(to, ", "), nil
	default:
		return "", fmt.Errorf("unknown action %q (send, voice, react, status)", action)
	}
}
