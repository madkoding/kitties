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
	// Files are the pictures, documents and audios of the message (Text is their caption).
	Files []File
	// Session is the whole state of the linked device, to keep sealed (EvSession).
	Session []byte
	Err     string
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
}

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
	// SeenGroups are the groups that wrote lately, newest first, to choose from.
	SeenGroups []Group `json:"seen_groups"`
	// CanChange is false when there is no vault to seal the session with; Why says so.
	CanChange bool   `json:"can_change"`
	Why       string `json:"why,omitempty"`
}

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
	status  Status
	client  Client
	cancel  context.CancelFunc
	done    chan struct{}
	base    context.Context
	last    string // the last allowed number that wrote
	numbers map[string]*sync.Mutex
	wg      sync.WaitGroup
	// wait is how long a reply that works on the desktop is followed.
	wait time.Duration
}

type saved struct {
	Settings
	Chats map[string]string `json:"chats,omitempty"`
	Seen  []Group           `json:"seen_groups,omitempty"`
}

// New reads the saved settings. Nothing connects before Start.
func New(opt Options) (*Bridge, error) {
	if opt.Log == nil {
		opt.Log = func(string, ...any) {}
	}
	b := &Bridge{opt: opt, chats: map[string]string{}, numbers: map[string]*sync.Mutex{}, wait: 2 * time.Hour}
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

// Apply keeps new settings and connects or disconnects to match.
func (b *Bridge) Apply(s Settings) error {
	clean, err := b.check(s)
	if err != nil {
		return err
	}
	b.ops.Lock()
	defer b.ops.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	was := b.set.Enabled
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
	return b.send(ctx, to, "Kitties: WhatsApp is linked. ✓")
}

func (b *Bridge) saveLocked() error {
	if err := os.MkdirAll(b.opt.Dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(saved{Settings: b.set, Chats: b.chats, Seen: b.seen}, "", "  ")
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
	b.client, b.cancel, b.done = c, cancel, done
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
	b.client, b.cancel, b.done = nil, nil, nil
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
	in := inbound{from: ev.From, name: ev.Name, text: text, files: ev.Files, key: ev.From, to: ev.From}
	if ev.Group {
		// In a group on the list anyone may talk to the kitty, by naming it.
		b.sawLocked(ev.Chat, ev.ChatName)
		if !ev.Mentioned && !mentions(text, b.kittyNameLocked()) {
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
	lock := b.numbers[in.key]
	if lock == nil {
		lock = &sync.Mutex{}
		b.numbers[in.key] = lock
	}
	ctx := b.base
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		// One message of a chat at a time, in order.
		lock.Lock()
		defer lock.Unlock()
		b.answer(ctx, c, in)
	}()
}

// inbound is a message to answer: key names its chat (a number, or "g:" and a group), to is
// where the answer goes.
type inbound struct {
	from, name, text string
	files            []File
	key, to          string
	group            bool
	groupName        string
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

func (b *Bridge) kittyNameLocked() string {
	if b.opt.Kitty == nil || b.set.Kitty == "" {
		return ""
	}
	if k, err := b.opt.Kitty(b.set.Kitty); err == nil {
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
	id, kittyRef := b.chats[in.key], b.set.Kitty
	b.mu.Unlock()
	if id != "" {
		if _, err := b.opt.Chats.Get(id); err == nil {
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
	stopTyping := b.typing(ctx, c, in.to)
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
		b.reply(ctx, c, in.to, b.busyText(in.text))
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
		b.reply(ctx, c, in.to, t.Text)
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
			b.reply(ctx, c, in.to, strings.TrimSpace(strings.TrimPrefix(ev.Turn.Text, sent)))
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

func (b *Bridge) busyText(text string) string {
	b.mu.Lock()
	ref := b.set.Kitty
	b.mu.Unlock()
	lang := ""
	if b.opt.Kitty != nil && ref != "" {
		if k, err := b.opt.Kitty(ref); err == nil {
			lang = k.Soul.Language
		}
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
	for _, part := range Split(Plain(text), maxPart) {
		if err := c.Send(ctx, to, part); err != nil {
			b.opt.Log("whatsapp: reply to +%s: %v", to, err)
			return
		}
	}
}

const maxPart = 4000

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
	b.mu.Lock()
	c, state, enabled, last := b.client, b.status.State, b.set.Enabled, b.last
	allow := append([]string(nil), b.set.Allow...)
	b.mu.Unlock()
	if !enabled {
		return ErrNotOn
	}
	if c == nil || state != StateConnected {
		return ErrNotReady
	}
	if strings.TrimSpace(to) == "" {
		switch {
		case last != "":
			to = last
		case len(allow) == 1:
			to = allow[0]
		default:
			return errors.New(`say who to write to: "to" is a number on the allow list`)
		}
	}
	b.mu.Lock()
	n, err := b.destLocked(to)
	b.mu.Unlock()
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
		if err := c.Send(ctx, n, part); err != nil {
			return err
		}
	}
	if file != "" {
		return b.sendFile(ctx, c, n, file, caption)
	}
	return nil
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
	return `WhatsApp from your own linked number: {"action": "send", "to": "+56912345678", "text": "..."} ` +
		`writes to a number or a group on the allow list (` + allow + `; "to" left out writes to whoever wrote last). ` +
		`Add "file": "<name of a file in the library>" to send a picture, video, audio or document, with "text" as its caption. ` +
		`Files you save to the library while answering someone on WhatsApp are sent to them by themselves. ` +
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
		return "", fmt.Errorf("unknown action %q (send, status)", action)
	}
}
