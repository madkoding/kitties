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
	// Group is true for a message in a group (they are not answered).
	Group bool
	// Session is the whole state of the linked device, to keep sealed (EvSession).
	Session []byte
	Err     string
}

// Client is a WhatsApp connection as a linked device.
type Client interface {
	// Run connects with the kept session (nil links a new device with QR codes) and tells
	// every event to emit until ctx ends or the link is over (logged out, codes expired).
	Run(ctx context.Context, session []byte, emit func(Event)) error
	// Send writes text to a number (digits with the country code).
	Send(ctx context.Context, phone, text string) error
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
}

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
)

// Bridge links WhatsApp with the kitties.
type Bridge struct {
	opt Options

	// ops runs one change of the link at a time (stopLocked lets mu go while it waits).
	ops     sync.Mutex
	mu      sync.Mutex
	set     Settings
	chats   map[string]string // number -> chat id
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
	out := Settings{Enabled: s.Enabled, Kitty: strings.TrimSpace(s.Kitty), Allow: []string{}}
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
	data, err := json.MarshalIndent(saved{Settings: b.set, Chats: b.chats}, "", "  ")
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
	if ev.Group || text == "" || ev.From == "" {
		return
	}
	if !b.allowedLocked(ev.From) {
		b.status.Ignored++
		return
	}
	b.status.LastMessage = time.Now()
	b.last = ev.From
	lock := b.numbers[ev.From]
	if lock == nil {
		lock = &sync.Mutex{}
		b.numbers[ev.From] = lock
	}
	ctx := b.base
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		// One message of a number at a time, in order.
		lock.Lock()
		defer lock.Unlock()
		b.answer(ctx, c, ev.From, ev.Name, text)
	}()
}

// chatFor is the chat of a number, made the first time it writes.
func (b *Bridge) chatFor(from, name string) (string, error) {
	b.mu.Lock()
	id, kittyRef := b.chats[from], b.set.Kitty
	b.mu.Unlock()
	if id != "" {
		if _, err := b.opt.Chats.Get(id); err == nil {
			return id, nil
		}
	}
	title := "WhatsApp +" + from
	if name != "" {
		title = "WhatsApp: " + name
	}
	sess, err := b.opt.Chats.Create(title, []string{kittyRef})
	if err != nil {
		return "", err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.chats[from] = sess.ID
	return sess.ID, b.saveLocked()
}

// answer posts a message in the number's chat and writes the kitty's replies back. A reply that
// goes on working on the desktop is followed until it is done.
func (b *Bridge) answer(ctx context.Context, c Client, from, name, text string) {
	id, err := b.chatFor(from, name)
	if err != nil {
		b.opt.Log("whatsapp: no chat for +%s: %v", from, err)
		return
	}
	events, unsubscribe := b.opt.Chats.Subscribe(id)
	defer unsubscribe()
	turns, err := b.opt.Chats.Send(ctx, id, text)
	if errors.Is(err, chat.ErrBusy) {
		b.reply(ctx, c, from, "⏳ I am still on your last message; I will write when it is done.")
		return
	}
	if err != nil {
		b.opt.Log("whatsapp: the message of +%s: %v", from, err)
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
		b.reply(ctx, c, from, t.Text)
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
			b.reply(ctx, c, from, strings.TrimSpace(strings.TrimPrefix(ev.Turn.Text, sent)))
		}
	}
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

// send is a message the person or a kitty asked for: only to a number on the list, while
// connected.
func (b *Bridge) send(ctx context.Context, to, text string) error {
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
	n, err := Normalize(to)
	if err != nil {
		return err
	}
	b.mu.Lock()
	ok := b.allowedLocked(n)
	b.mu.Unlock()
	if !ok {
		return ErrNotAllow
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("the message is empty")
	}
	for _, part := range Split(Plain(text), maxPart) {
		if err := c.Send(ctx, n, part); err != nil {
			return err
		}
	}
	return nil
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
	allow := strings.Join(prefixed(c.b.set.Allow), ", ")
	c.b.mu.Unlock()
	return `WhatsApp from your own linked number: {"action": "send", "to": "+56912345678", "text": "..."} ` +
		`writes to a number on the allow list (` + allow + `; "to" left out writes to whoever wrote last). ` +
		`{"action": "status"} says whether it is connected. The people on the list also write to you here.`
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
		to := str("to")
		if err := c.b.send(ctx, to, str("text")); err != nil {
			return "", err
		}
		return "sent", nil
	case "status":
		st := c.b.Status()
		out := "WhatsApp is " + st.State
		if st.Phone != "" {
			out += " as +" + st.Phone
		}
		return out + "; it may write to " + strings.Join(prefixed(st.Allow), ", "), nil
	default:
		return "", fmt.Errorf("unknown action %q (send, status)", action)
	}
}
