package reply

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/notify"
)

type fleet []*kitty.Kitty

func (f fleet) List() []*kitty.Kitty { return append([]*kitty.Kitty(nil), f...) }

// chats is a fake of the chats: each kitty answers "<name> heard: <text>".
type chats struct {
	mu       sync.Mutex
	fleet    fleet
	sessions map[string]*chat.Session
	order    []string
	helped   []string
	work     bool // the next answer starts a desktop task
	sendErr  error
	watchers []chan chat.Event
}

func newChats(f fleet) *chats { return &chats{fleet: f, sessions: map[string]*chat.Session{}} }

func (c *chats) List() []*chat.Session {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []*chat.Session
	for i := len(c.order) - 1; i >= 0; i-- {
		s := *c.sessions[c.order[i]]
		out = append(out, &s)
	}
	return out
}

func (c *chats) Get(id string) (*chat.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.sessions[id]
	if !ok {
		return nil, fmt.Errorf("no such chat %q", id)
	}
	cp := *s
	cp.Turns = append([]chat.Turn(nil), s.Turns...)
	return &cp, nil
}

func (c *chats) Create(title string, members []string) (*chat.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := fmt.Sprintf("c%d", len(c.order)+1)
	c.sessions[id] = &chat.Session{ID: id, Title: title, Members: members}
	c.order = append(c.order, id)
	return c.sessions[id], nil
}

func (c *chats) kitty(id string) *kitty.Kitty {
	for _, k := range c.fleet {
		if k.ID == id {
			return k
		}
	}
	return nil
}

func (c *chats) Send(_ context.Context, id, text string) ([]chat.Turn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sendErr != nil {
		return nil, c.sendErr
	}
	s := c.sessions[id]
	s.Turns = append(s.Turns, chat.Turn{From: "user", Text: text})
	var out []chat.Turn
	for _, m := range s.Members {
		k := c.kitty(m)
		if len(s.Members) > 1 && !strings.Contains(text, "@"+k.Name) {
			continue
		}
		t := chat.Turn{At: time.Now(), From: k.Name, Kitty: k.ID, Text: k.Name + " heard: " + text, Working: c.work}
		s.Turns = append(s.Turns, t)
		out = append(out, t)
	}
	return out, nil
}

func (c *chats) AnswerHelp(id string, index int, a chat.HelpAnswer) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.sessions[id]
	if index != len(s.Turns)-1 || !s.Turns[index].NeedsHelp {
		return chat.ErrNoHelp
	}
	c.helped = append(c.helped, a.Text)
	return nil
}

func (c *chats) Subscribe(string) (<-chan chat.Event, func()) {
	ch := make(chan chat.Event, 8)
	c.mu.Lock()
	c.watchers = append(c.watchers, ch)
	c.mu.Unlock()
	return ch, func() {}
}

// finish ends the desktop task of a chat's last turn with more text.
func (c *chats) finish(id, more string) {
	c.mu.Lock()
	s := c.sessions[id]
	t := &s.Turns[len(s.Turns)-1]
	t.Working = false
	t.Text += "\n\n" + more
	ws := c.watchers
	c.mu.Unlock()
	for _, w := range ws {
		w <- chat.Event{Type: "update", Session: id}
	}
}

type outbox struct {
	mu   sync.Mutex
	sent []notify.Notification
	err  error
}

func (o *outbox) Deliver(_ context.Context, n notify.Notification) error {
	o.mu.Lock()
	o.sent = append(o.sent, n)
	o.mu.Unlock()
	return o.err
}

func (o *outbox) all() []notify.Notification {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]notify.Notification(nil), o.sent...)
}

func (o *outbox) bodies() string {
	var b []string
	for _, n := range o.all() {
		b = append(b, n.Kitty+"|"+n.Body)
	}
	return strings.Join(b, "\n")
}

var (
	luna = &kitty.Kitty{ID: "k1", Name: "luna"}
	bo   = &kitty.Kitty{ID: "k2", Name: "bo"}
)

func setup(ks ...*kitty.Kitty) (*Service, *chats, *outbox) {
	c := newChats(fleet(ks))
	o := &outbox{}
	return New(c, fleet(ks), o, nil), c, o
}

func TestNamedKittyAnswersBack(t *testing.T) {
	for _, text := range []string{"luna: what's up", "Luna: what's up", "@luna what's up"} {
		s, _, o := setup(luna, bo)
		if err := s.Handle(context.Background(), "", text); err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		if got := o.bodies(); got != "luna|luna heard: what's up" {
			t.Errorf("%q: sent %q", text, got)
		}
		if n := o.all()[0]; n.Event != notify.Reply || n.Chat == "" {
			t.Errorf("%q: notification %+v", text, n)
		}
	}
}

func TestTitleNamesTheKitty(t *testing.T) {
	s, _, o := setup(luna, bo)
	_ = s.Handle(context.Background(), "bo", "hola")
	if got := o.bodies(); got != "bo|bo heard: hola" {
		t.Errorf("sent %q", got)
	}
}

func TestSeveralAndAll(t *testing.T) {
	for _, text := range []string{"luna y bo: hi", "luna, bo: hi", "@luna @bo hi", "todos: hi", "all: hi"} {
		s, _, o := setup(luna, bo)
		_ = s.Handle(context.Background(), "", text)
		got := o.bodies()
		if !strings.Contains(got, "luna|luna heard: hi") || !strings.Contains(got, "bo|bo heard: hi") || len(o.all()) != 2 {
			t.Errorf("%q: sent %q", text, got)
		}
	}
}

func TestNoNameGoesToWhoNotified(t *testing.T) {
	s, c, o := setup(luna, bo)
	help, _ := c.Create("", []string{bo.ID})
	s.Saw(notify.Notification{Event: notify.Help, Kitty: "bo", Chat: help.ID})
	// "Nota:" is not a kitty: the whole message goes to bo, in the chat it notified about.
	_ = s.Handle(context.Background(), "", "Nota: hecho")
	if got := o.bodies(); got != "bo|bo heard: Nota: hecho" {
		t.Errorf("sent %q", got)
	}
	if o.all()[0].Chat != help.ID {
		t.Errorf("answered in %q, want %q", o.all()[0].Chat, help.ID)
	}
}

func TestNoNameGoesToTheLead(t *testing.T) {
	s, _, o := setup(luna, bo)
	s.Saw(notify.Notification{Event: notify.Help, Kitty: "bo", Chat: "x"})
	s.Lead = func() *kitty.Kitty { return luna }
	_ = s.Handle(context.Background(), "", "hola")
	if got := o.bodies(); got != "luna|luna heard: hola" {
		t.Errorf("sent %q", got)
	}
}

func TestNoNameNobodyToAsk(t *testing.T) {
	s, _, o := setup(luna, bo)
	if err := s.Handle(context.Background(), "", "hola"); err == nil {
		t.Error("a message with no kitty to go to was taken")
	}
	if got := o.bodies(); !strings.Contains(got, "Who is it for?") || !strings.Contains(got, "bo: …") {
		t.Errorf("sent %q", got)
	}
	// With one kitty there is nobody else to ask.
	s, _, o = setup(luna)
	_ = s.Handle(context.Background(), "", "hola")
	if got := o.bodies(); got != "luna|luna heard: hola" {
		t.Errorf("one kitty: sent %q", got)
	}
}

func TestOldNotificationDoesNotDecide(t *testing.T) {
	s, _, o := setup(luna, bo)
	s.Saw(notify.Notification{Event: notify.Help, Kitty: "bo", Chat: "x"})
	s.now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	_ = s.Handle(context.Background(), "", "hola")
	if got := o.bodies(); !strings.Contains(got, "Who is it for?") {
		t.Errorf("sent %q", got)
	}
}

func TestGroupChatMentionsTheKitty(t *testing.T) {
	s, c, o := setup(luna, bo)
	g, _ := c.Create("", []string{luna.ID, bo.ID})
	s.Saw(notify.Notification{Event: notify.Help, Kitty: "bo", Chat: g.ID})
	_ = s.Handle(context.Background(), "", "bo: sigue")
	if got := o.bodies(); got != "bo|bo heard: @bo sigue" {
		t.Errorf("sent %q", got)
	}
}

func TestReusesTheKittysChat(t *testing.T) {
	s, c, _ := setup(luna, bo)
	mine, _ := c.Create("", []string{luna.ID})
	_ = s.Handle(context.Background(), "", "luna: one")
	if len(c.order) != 1 {
		t.Fatalf("chats %v, want only %s", c.order, mine.ID)
	}
	_ = s.Handle(context.Background(), "", "bo: two")
	if len(c.order) != 2 {
		t.Errorf("bo got no chat of its own: %v", c.order)
	}
}

func TestAnswersAKittyWaitingForHelp(t *testing.T) {
	s, c, o := setup(luna, bo)
	sess, _ := c.Create("", []string{luna.ID})
	c.sessions[sess.ID].Turns = []chat.Turn{{From: "user", Text: "do it"}, {From: "luna", Kitty: luna.ID, Working: true, NeedsHelp: true}}
	s.Saw(notify.Notification{Event: notify.Help, Kitty: "luna", Chat: sess.ID})
	_ = s.Handle(context.Background(), "", "the code is 1234")
	if len(c.helped) != 1 || c.helped[0] != "the code is 1234" {
		t.Errorf("help answers %v", c.helped)
	}
	if got := o.bodies(); !strings.Contains(got, "got your answer") {
		t.Errorf("sent %q", got)
	}
}

func TestFollowsADesktopTask(t *testing.T) {
	s, c, o := setup(luna)
	c.work = true
	_ = s.Handle(context.Background(), "", "luna: open the site")
	if got := o.bodies(); !strings.Contains(got, "Working on it") {
		t.Fatalf("sent %q", got)
	}
	c.finish("c1", "Done: it is open.")
	s.Wait()
	all := o.all()
	if last := all[len(all)-1]; last.Body != "Done: it is open." {
		t.Errorf("result %q", last.Body)
	}
}

func TestLimits(t *testing.T) {
	s, _, o := setup(luna)
	if err := s.Handle(context.Background(), "", strings.Repeat("a", MaxText+1)); err == nil {
		t.Error("a message too long was taken")
	}
	if !strings.Contains(o.bodies(), "too long") {
		t.Errorf("sent %q", o.bodies())
	}
	for i := 0; i < perHour; i++ {
		_ = s.Handle(context.Background(), "", "hi")
	}
	if err := s.Handle(context.Background(), "", "one more"); err == nil {
		t.Error("the hour's allowance was not kept")
	}
	_ = s.Handle(context.Background(), "", "   ")
}

func TestLeadSignedNotificationReachesTheKittyByName(t *testing.T) {
	s, c, o := setup(luna, bo)
	s.Lead = func() *kitty.Kitty { return luna }
	help, _ := c.Create("", []string{bo.ID})
	c.sessions[help.ID].Turns = []chat.Turn{{From: "user", Text: "go"}, {From: "bo", Kitty: bo.ID, Working: true, NeedsHelp: true}}
	// The lead signs bo's request for help.
	s.Saw(notify.Notification{Event: notify.Help, Kitty: "luna", Chat: help.ID})
	_ = s.Handle(context.Background(), "", "bo: 1234")
	if len(c.helped) != 1 || c.helped[0] != "1234" {
		t.Errorf("help answers %v, sent %q", c.helped, o.bodies())
	}
}

func TestSendFailures(t *testing.T) {
	s, c, o := setup(luna)
	c.sendErr = chat.ErrBusy
	_ = s.Handle(context.Background(), "", "hola")
	if got := o.bodies(); !strings.Contains(got, "is busy with a task") {
		t.Errorf("busy: sent %q", got)
	}
	s, c, o = setup(luna)
	c.sendErr = errors.New("boom")
	_ = s.Handle(context.Background(), "", "hola")
	if got := o.bodies(); !strings.Contains(got, "did not get your message") {
		t.Errorf("error: sent %q", got)
	}
}

func TestSayEdges(t *testing.T) {
	var logged []string
	c := newChats(fleet{luna})
	o := &outbox{err: errors.New("down")}
	s := New(c, fleet{luna}, o, func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) })
	s.say(context.Background(), "luna", strings.Repeat("x", 4000), "")
	if n := o.all()[0]; len(n.Body) > 3600 || !strings.HasSuffix(n.Body, "…") {
		t.Errorf("long body %d", len(n.Body))
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "down") {
		t.Errorf("logged %v", logged)
	}
	New(c, fleet{luna}, nil, nil).say(context.Background(), "luna", "x", "")
	// The digest names nobody to answer, and an empty fleet is shown an example name.
	s.Saw(notify.Notification{Event: notify.Digest, Kitty: "luna"})
	if s.whom != "" {
		t.Errorf("the digest set who to answer: %q", s.whom)
	}
	if e := New(c, fleet{}, nil, nil).example(); e != "luna: …" {
		t.Errorf("example %q", e)
	}
	if sessLen(c, "nope") != 0 {
		t.Error("an unknown chat has turns")
	}
}

func TestFollowGivesUp(t *testing.T) {
	defer func(d time.Duration) { follow = d }(follow)
	follow = 30 * time.Millisecond
	s, c, o := setup(luna)
	c.work = true
	_ = s.Handle(context.Background(), "", "hola")
	s.Wait()
	if n := len(o.all()); n != 1 {
		t.Errorf("sent %d after the task was given up on", n)
	}
	// A chat that went away ends the following at once.
	s.followTask(luna, "gone", 0, "")
}
