// Package notify tells the person what happened while they were away, outside the interface:
// a kitty asking for help, an action waiting for approval, a goal that finished or got stuck, a
// container that was brought back. It writes through a connector the person chose (ntfy for a
// push to the phone, Telegram, Slack, email...), so it needs nothing of its own.
package notify

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/madkoding/kitties/internal/connector"
)

// Event is what happened.
type Event string

// The events a person can be told about.
const (
	Help        Event = "help"         // a kitty gave up and needs the person
	Approval    Event = "approval"     // an action waits for a yes
	GoalDone    Event = "goal_done"    // a goal's run finished
	GoalBlocked Event = "goal_blocked" // a goal's run could not finish
	Recovered   Event = "recovered"    // a container was brought back
	Budget      Event = "budget"       // a kitty spent its day's tokens
	// Digest is the daily summary. It is not one of Events: the person turns it on by giving
	// Config.Digest a time of day.
	Digest Event = "digest"
)

// Events lists them in order, for the page.
var Events = []Event{Help, Approval, GoalDone, GoalBlocked, Recovered, Budget}

// Default is what is sent when the person chose nothing: the two that need them.
var Default = []Event{Help, Approval}

// Notification is one thing to tell.
type Notification struct {
	Event Event
	// Kitty is the name of the kitty it is about.
	Kitty string
	Title string
	Body  string
	// Chat is the thread to open, for the link.
	Chat string
}

// Sender is what the rest of the program calls.
type Sender interface {
	Notify(n Notification)
}

// Config says where and what to send.
type Config struct {
	// Connector is the name of the connector to write through; empty turns notifications off.
	Connector string `json:"connector" yaml:"connector"`
	// On lists the events to send; empty means Default.
	On []string `json:"on" yaml:"on"`
	// Digest is the time of day ("09:00", the gateway's clock) a summary of the last day is
	// sent; empty sends none.
	Digest string `json:"digest,omitempty" yaml:"digest"`
}

// DigestAt parses Digest into hour and minute; ok is false when none is set.
func (c Config) DigestAt() (hour, minute int, ok bool) {
	if c.Digest == "" {
		return 0, 0, false
	}
	t, err := time.Parse("15:04", c.Digest)
	if err != nil {
		return 0, 0, false
	}
	return t.Hour(), t.Minute(), true
}

// Wants reports whether the config sends an event.
func (c Config) Wants(e Event) bool {
	if c.Connector == "" {
		return false
	}
	if e == Digest {
		_, _, ok := c.DigestAt()
		return ok
	}
	on := c.On
	if len(on) == 0 {
		for _, d := range Default {
			if d == e {
				return true
			}
		}
		return false
	}
	for _, o := range on {
		if Event(o) == e {
			return true
		}
	}
	return false
}

// Validate checks the connector exists (when a registry is given) and the events are known.
func (c Config) Validate(reg *connector.Registry) error {
	for _, o := range c.On {
		known := false
		for _, e := range Events {
			known = known || string(e) == o
		}
		if !known {
			return fmt.Errorf("notify.on: %q is not one of %s", o, eventList())
		}
	}
	if c.Digest != "" {
		if _, _, ok := c.DigestAt(); !ok {
			return fmt.Errorf("notify.digest: %q is not a time of day like 09:00", c.Digest)
		}
		if c.Connector == "" {
			return fmt.Errorf("notify.digest: choose a connector to send it through")
		}
	}
	if c.Connector != "" && reg != nil {
		if _, ok := reg.Get(c.Connector); !ok {
			return fmt.Errorf("notify.connector: no such connector %q", c.Connector)
		}
	}
	return nil
}

func eventList() string {
	names := make([]string, len(Events))
	for i, e := range Events {
		names[i] = string(e)
	}
	return strings.Join(names, ", ")
}

// Notifier sends notifications through the connector the config names.
type Notifier struct {
	reg *connector.Registry
	// cfg returns the current config, so a change on the page applies at once.
	cfg func() Config
	// base is where the interface is reached from outside, for the link.
	base func() string
	logf func(string, ...any)
	now  func() time.Time

	mu   sync.Mutex
	last map[string]time.Time
	wg   sync.WaitGroup
	// Quiet is how long the same notification is held back after it went out.
	Quiet time.Duration
}

// New creates the notifier. base is the public address of the interface (may be nil).
func New(reg *connector.Registry, cfg func() Config, base func() string, logf func(string, ...any)) *Notifier {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if base == nil {
		base = func() string { return "" }
	}
	return &Notifier{reg: reg, cfg: cfg, base: base, logf: logf, now: time.Now, last: map[string]time.Time{}, Quiet: 10 * time.Minute}
}

// SetLog sets where a failed notification is reported.
func (n *Notifier) SetLog(logf func(string, ...any)) {
	if logf != nil {
		n.logf = logf
	}
}

// Link is where a person opens a chat.
func (n *Notifier) Link(chat string) string {
	base := strings.TrimRight(n.base(), "/")
	if base == "" || chat == "" {
		return base
	}
	return base + "/#chat/" + chat
}

// Notify sends a notification when the config wants it and the same one did not just go out.
// It never blocks the caller: the connector is called in the background.
func (n *Notifier) Notify(note Notification) {
	cfg := n.cfg()
	if !cfg.Wants(note.Event) {
		return
	}
	key := string(note.Event) + "\x00" + note.Kitty + "\x00" + note.Chat + "\x00" + note.Title
	n.mu.Lock()
	if t, ok := n.last[key]; ok && n.now().Sub(t) < n.Quiet {
		n.mu.Unlock()
		return
	}
	n.last[key] = n.now()
	if len(n.last) > 1000 {
		for k, t := range n.last {
			if n.now().Sub(t) > n.Quiet {
				delete(n.last, k)
			}
		}
	}
	n.mu.Unlock()
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		if err := n.send(cfg.Connector, note); err != nil {
			n.logf("notify: %s: %v", cfg.Connector, err)
		}
	}()
}

// Deliver sends a notification through the configured connector now and says how it went. It
// ignores the choice of events and the quiet time: a summary the person asked for is not a
// repeat.
func (n *Notifier) Deliver(ctx context.Context, note Notification) error {
	name := n.cfg().Connector
	if name == "" {
		return fmt.Errorf("no notification channel is set")
	}
	return n.call(ctx, name, note)
}

// Wait blocks until the notifications in flight have gone out.
func (n *Notifier) Wait() { n.wg.Wait() }

// Test sends a notification now, ignoring the config's events and the quiet time, and says how it went.
func (n *Notifier) Test(ctx context.Context, name string) error {
	return n.call(ctx, name, Notification{Event: Help, Kitty: "kitties", Title: "Notifications work", Body: "This is a test from your gateway."})
}

func (n *Notifier) send(name string, note Notification) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return n.call(ctx, name, note)
}

// call writes the notification through the connector. A push connector gets the title and the
// link apart; the rest get one text.
func (n *Notifier) call(ctx context.Context, name string, note Notification) error {
	c, ok := n.reg.Get(name)
	if !ok {
		return fmt.Errorf("no such connector %q", name)
	}
	link := n.Link(note.Chat)
	title := note.Title
	if note.Kitty != "" {
		title = note.Kitty + ": " + title
	}
	args := map[string]any{"action": "send"}
	kind := ""
	if k, ok := c.(connector.Kinder); ok {
		kind = k.Kind()
	}
	if kind == "shell" || kind == "webhook" || kind == "motita" {
		return fmt.Errorf("%s cannot carry a notification: pick a messaging connector", name)
	}
	text := note.Body
	if kind == "ntfy" {
		args["title"] = title
		if link != "" {
			args["click"] = link
		}
		if note.Event == Help || note.Event == Approval {
			args["priority"] = "4"
		}
	} else {
		text = title + "\n" + note.Body
		if link != "" {
			text += "\n" + link
		}
		args["subject"] = title
	}
	args["text"] = strings.TrimSpace(text)
	res := n.reg.Call(ctx, connector.Call{Name: name, Args: args}, nil)
	if res.Err != "" {
		return fmt.Errorf("%s", res.Err)
	}
	return nil
}
