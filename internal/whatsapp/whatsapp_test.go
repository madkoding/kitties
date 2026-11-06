package whatsapp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/connector"
	"github.com/madkoding/kitties/internal/kitty"
)

// fakeClient is a WhatsApp that the test drives: it emits what the test pushes and records what
// is sent.
type fakeClient struct {
	mu      sync.Mutex
	session []byte
	emit    func(Event)
	sent    []string
	sendErr error
	logout  int
	runErr  error
	ready   chan struct{}
	stop    chan error
}

func newFake() *fakeClient {
	return &fakeClient{ready: make(chan struct{}), stop: make(chan error, 1)}
}

func (f *fakeClient) Run(ctx context.Context, session []byte, emit func(Event)) error {
	f.mu.Lock()
	f.session, f.emit = session, emit
	f.mu.Unlock()
	close(f.ready)
	if f.runErr != nil {
		return f.runErr
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-f.stop:
		return err
	}
}

func (f *fakeClient) push(ev Event) {
	<-f.ready
	f.mu.Lock()
	emit := f.emit
	f.mu.Unlock()
	emit(ev)
}

func (f *fakeClient) Send(_ context.Context, phone, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return f.sendErr
	}
	f.sent = append(f.sent, phone+": "+text)
	return nil
}

func (f *fakeClient) Logout(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logout++
	return errors.New("offline")
}

func (f *fakeClient) messages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sent...)
}

// box seals by reversing the bytes and prefixing the aad: enough to see nothing is in the clear.
type box struct{ fail bool }

func (b box) Seal(plain, aad []byte) ([]byte, error) {
	if b.fail {
		return nil, errors.New("sealed shut")
	}
	out := append([]byte("sealed:"+string(aad)+":"), plain...)
	for i, j := len("sealed:"+string(aad)+":"), len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func (b box) Open(sealed, aad []byte) ([]byte, error) {
	prefix := []byte("sealed:" + string(aad) + ":")
	if !bytes.HasPrefix(sealed, prefix) {
		return nil, errors.New("wrong key")
	}
	out := append([]byte(nil), sealed[len(prefix):]...)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// fakeChats answers every message with what reply says.
type fakeChats struct {
	mu       sync.Mutex
	sessions map[string]*chat.Session
	watchers map[string][]chan chat.Event
	reply    func(text string) ([]chat.Turn, error)
	created  int
	failNew  bool
}

func newChats() *fakeChats {
	return &fakeChats{sessions: map[string]*chat.Session{}, watchers: map[string][]chan chat.Event{},
		reply: func(text string) ([]chat.Turn, error) {
			return []chat.Turn{{From: "mimi", Text: "**hi** you said " + text}}, nil
		}}
}

func (c *fakeChats) Create(title string, members []string) (*chat.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failNew {
		return nil, errors.New("no kitty")
	}
	c.created++
	s := &chat.Session{ID: fmt.Sprintf("c%d", c.created), Title: title, Members: members}
	c.sessions[s.ID] = s
	return s, nil
}

func (c *fakeChats) Get(id string) (*chat.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.sessions[id]
	if !ok {
		return nil, errors.New("no such chat")
	}
	cp := *s
	cp.Turns = append([]chat.Turn(nil), s.Turns...)
	return &cp, nil
}

func (c *fakeChats) Send(_ context.Context, id, text string) ([]chat.Turn, error) {
	turns, err := c.reply(text)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.sessions[id]
	s.Turns = append(s.Turns, chat.Turn{From: "user", Text: text})
	s.Turns = append(s.Turns, turns...)
	return turns, nil
}

func (c *fakeChats) Subscribe(id string) (<-chan chat.Event, func()) {
	ch := make(chan chat.Event, 8)
	c.mu.Lock()
	c.watchers[id] = append(c.watchers[id], ch)
	c.mu.Unlock()
	return ch, func() {}
}

func (c *fakeChats) emit(id string, ev chat.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, w := range c.watchers[id] {
		w <- ev
	}
}

type registry struct {
	mu    sync.Mutex
	items map[string]connector.Connector
}

func (r *registry) Add(name string, c connector.Connector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[name] = c
}

func (r *registry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.items, name)
}

func (r *registry) get(name string) (connector.Connector, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.items[name]
	return c, ok
}

func kittyOf(ref string) (*kitty.Kitty, error) {
	switch ref {
	case "mimi", "k1":
		return &kitty.Kitty{ID: "k1", Name: "mimi"}, nil
	}
	return nil, fmt.Errorf("no kitty %q", ref)
}

type rig struct {
	b       *Bridge
	chats   *fakeChats
	reg     *registry
	clients chan *fakeClient
	dir     string
	logs    *strings.Builder
	logMu   sync.Mutex
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{chats: newChats(), reg: &registry{items: map[string]connector.Connector{}}, clients: make(chan *fakeClient, 8), dir: t.TempDir(), logs: &strings.Builder{}}
	b, err := New(Options{
		Dir: r.dir, Box: box{}, Chats: r.chats, Kitty: kittyOf, Registry: r.reg,
		NewClient: func() Client {
			f := newFake()
			r.clients <- f
			return f
		},
		QRImage: func(code string) (string, error) { return "data:image/png;base64," + code, nil },
		Log: func(format string, args ...any) {
			r.logMu.Lock()
			defer r.logMu.Unlock()
			fmt.Fprintf(r.logs, format+"\n", args...)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.b = b
	t.Cleanup(b.Stop)
	return r
}

func (r *rig) next(t *testing.T) *fakeClient {
	t.Helper()
	select {
	case f := <-r.clients:
		<-f.ready
		return f
	case <-time.After(5 * time.Second):
		t.Fatal("no client was started")
		return nil
	}
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// linked turns the bridge on and connects it, allowing +56 9 1234 5678.
func (r *rig) linked(t *testing.T) *fakeClient {
	t.Helper()
	r.b.Start(context.Background())
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi", Allow: []string{"+56 9 1234 5678"}}); err != nil {
		t.Fatal(err)
	}
	f := r.next(t)
	f.push(Event{Kind: EvConnected, Phone: "15550001"})
	return f
}

func TestOffByDefault(t *testing.T) {
	r := newRig(t)
	r.b.Start(context.Background())
	st := r.b.Status()
	if st.Enabled || st.State != StateOff || len(st.Allow) != 0 || st.Linked || !st.CanChange {
		t.Fatalf("status = %+v", st)
	}
	if _, ok := r.reg.get(ConnectorName); ok {
		t.Fatal("the connector is offered while off")
	}
	select {
	case <-r.clients:
		t.Fatal("a client started while off")
	default:
	}
}

func TestLinkWithQRAndSealedSession(t *testing.T) {
	r := newRig(t)
	r.b.Start(context.Background())
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi"}); err != nil {
		t.Fatal(err)
	}
	f := r.next(t)
	if f.session != nil {
		t.Fatal("a new link got a session")
	}
	f.push(Event{Kind: EvQR, QR: "2@abc"})
	st := r.b.Status()
	if st.State != StateQR || st.QR != "2@abc" || st.QRImage != "data:image/png;base64,2@abc" || st.KittyName != "mimi" || st.Kitty != "k1" {
		t.Fatalf("status = %+v", st)
	}
	f.push(Event{Kind: EvPaired, Phone: "15550001"})
	f.push(Event{Kind: EvSession, Session: []byte(`{"secret":"keys"}`)})
	f.push(Event{Kind: EvConnected})
	st = r.b.Status()
	if st.State != StateConnected || st.Phone != "15550001" || st.QR != "" || !st.Linked {
		t.Fatalf("status = %+v", st)
	}
	raw, err := os.ReadFile(filepath.Join(r.dir, "session.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"secret":"keys"`)) {
		t.Fatal("the session is kept in the clear")
	}
	info, _ := os.Stat(filepath.Join(r.dir, "session.bin"))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("session mode = %v", info.Mode())
	}
	if _, ok := r.reg.get(ConnectorName); !ok {
		t.Fatal("the connector is not offered once connected")
	}
	// Off and on again: the kept session is handed back.
	if err := r.b.Apply(Settings{Kitty: "mimi"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.reg.get(ConnectorName); ok {
		t.Fatal("the connector stays offered once off")
	}
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi"}); err != nil {
		t.Fatal(err)
	}
	if g := r.next(t); string(g.session) != `{"secret":"keys"}` {
		t.Fatalf("session = %q", g.session)
	}
}

func TestSettingsAreKeptAndChecked(t *testing.T) {
	r := newRig(t)
	if err := r.b.Apply(Settings{Kitty: "mimi", Allow: []string{"+56 9 1234 5678", "0056912345678", "+1 (555) 000-1111"}}); err != nil {
		t.Fatal(err)
	}
	b2, err := New(Options{Dir: r.dir, Box: box{}, NewClient: func() Client { return newFake() }, Kitty: kittyOf})
	if err != nil {
		t.Fatal(err)
	}
	st := b2.Status()
	if strings.Join(st.Allow, ",") != "56912345678,15550001111" || st.Kitty != "k1" {
		t.Fatalf("kept = %+v", st)
	}
	for _, bad := range []Settings{
		{Enabled: true},
		{Kitty: "nobody"},
		{Allow: []string{"12"}},
		{Allow: make([]string, MaxAllow+1)},
	} {
		if err := r.b.Apply(bad); err == nil {
			t.Fatalf("%+v was accepted", bad)
		}
	}
	if err := os.WriteFile(filepath.Join(r.dir, "settings.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Options{Dir: r.dir}); err == nil {
		t.Fatal("a broken settings file was read")
	}
	if _, err := New(Options{Dir: filepath.Join(r.dir, "settings.json", "x")}); err == nil {
		t.Fatal("an unreadable folder was read")
	}
}

func TestNoVault(t *testing.T) {
	b, err := New(Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	st := b.Status()
	if st.CanChange || st.Why == "" {
		t.Fatalf("status = %+v", st)
	}
	if err := b.Apply(Settings{Enabled: true, Kitty: "x"}); !errors.Is(err, ErrNoVault) {
		t.Fatalf("err = %v", err)
	}
	if err := b.Link(); !errors.Is(err, ErrNotOn) {
		t.Fatalf("link = %v", err)
	}
	if err := b.Test(context.Background(), "+56912345678"); !errors.Is(err, ErrNotOn) {
		t.Fatalf("test = %v", err)
	}
}

func TestConversation(t *testing.T) {
	r := newRig(t)
	f := r.linked(t)
	// Not on the list, a group, empty, or no sender: not answered.
	f.push(Event{Kind: EvMessage, From: "4411112222", Text: "hello"})
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "group", Group: true})
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "  "})
	if st := r.b.Status(); st.Ignored != 1 {
		t.Fatalf("ignored = %d", st.Ignored)
	}
	f.push(Event{Kind: EvMessage, From: "56912345678", Name: "Mad", Text: "hola"})
	eventually(t, "the reply", func() bool { return len(f.messages()) == 1 })
	if got := f.messages()[0]; got != "56912345678: *hi* you said hola" {
		t.Fatalf("reply = %q", got)
	}
	// The same chat the next time.
	f.push(Event{Kind: EvMessage, From: "56912345678", Name: "Mad", Text: "otra"})
	eventually(t, "the second reply", func() bool { return len(f.messages()) == 2 })
	if r.chats.created != 1 {
		t.Fatalf("chats made = %d", r.chats.created)
	}
	s, _ := r.chats.Get("c1")
	if s.Title != "WhatsApp: Mad" || s.Members[0] != "k1" {
		t.Fatalf("chat = %+v", s)
	}
	if st := r.b.Status(); st.LastMessage.IsZero() {
		t.Fatal("no last message")
	}
	// A chat deleted from the page is made again.
	r.chats.mu.Lock()
	delete(r.chats.sessions, "c1")
	r.chats.mu.Unlock()
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "again"})
	eventually(t, "the third reply", func() bool { return len(f.messages()) == 3 })
	if s, err := r.chats.Get("c2"); err != nil || s.Title != "WhatsApp +56912345678" {
		t.Fatalf("new chat = %+v %v", s, err)
	}
}

func TestBusyAndFailures(t *testing.T) {
	r := newRig(t)
	f := r.linked(t)
	r.chats.reply = func(string) ([]chat.Turn, error) { return nil, chat.ErrBusy }
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "one"})
	eventually(t, "the busy note", func() bool { return len(f.messages()) == 1 })
	if !strings.Contains(f.messages()[0], "still on your last message") {
		t.Fatalf("busy = %q", f.messages()[0])
	}
	r.chats.reply = func(string) ([]chat.Turn, error) { return nil, errors.New("model down") }
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "two"})
	eventually(t, "the failure log", func() bool { return strings.Contains(r.logString(), "model down") })
	r.chats.mu.Lock()
	r.chats.failNew = true
	r.chats.sessions = map[string]*chat.Session{}
	r.chats.mu.Unlock()
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "three"})
	eventually(t, "the chat failure", func() bool { return strings.Contains(r.logString(), "no chat for") })
	r.chats.mu.Lock()
	r.chats.failNew = false
	r.chats.mu.Unlock()
	r.chats.reply = func(string) ([]chat.Turn, error) { return []chat.Turn{{Text: "ok"}}, nil }
	f.mu.Lock()
	f.sendErr = errors.New("socket closed")
	f.mu.Unlock()
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "four"})
	eventually(t, "the send failure", func() bool { return strings.Contains(r.logString(), "socket closed") })
}

func (r *rig) logString() string {
	r.logMu.Lock()
	defer r.logMu.Unlock()
	return r.logs.String()
}

func TestDesktopTaskIsFollowed(t *testing.T) {
	r := newRig(t)
	f := r.linked(t)
	r.chats.reply = func(string) ([]chat.Turn, error) {
		return []chat.Turn{{From: "mimi", Text: "On it.", Working: true}}, nil
	}
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "open the site"})
	eventually(t, "the first reply", func() bool { return len(f.messages()) == 1 })
	// An update of another turn, and a step, change nothing; the done of the turn is written.
	r.chats.emit("c1", chat.Event{Type: "done", Index: 0, Turn: &chat.Turn{Text: "not this"}})
	r.chats.emit("c1", chat.Event{Type: "update", Index: 1, Turn: &chat.Turn{Text: "On it. step"}})
	r.chats.emit("c1", chat.Event{Type: "done", Index: 1, Turn: &chat.Turn{Text: "On it.\n\nDone: the site is open."}})
	eventually(t, "the final reply", func() bool { return len(f.messages()) == 2 })
	if got := f.messages()[1]; got != "56912345678: Done: the site is open." {
		t.Fatalf("final = %q", got)
	}
}

func TestDesktopTaskGivesUp(t *testing.T) {
	r := newRig(t)
	r.b.wait = 20 * time.Millisecond
	f := r.linked(t)
	r.chats.reply = func(string) ([]chat.Turn, error) {
		return []chat.Turn{{Text: "On it.", Working: true}}, nil
	}
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "slow"})
	eventually(t, "the first reply", func() bool { return len(f.messages()) == 1 })
	r.b.wg.Wait() // it stops following after wait
}

func TestConnector(t *testing.T) {
	r := newRig(t)
	if err := r.b.Apply(Settings{Kitty: "mimi", Allow: []string{"+56912345678", "+15550001111"}}); err != nil {
		t.Fatal(err)
	}
	c := connectorOf{r.b}
	ctx := context.Background()
	if _, err := c.Call(ctx, map[string]any{"text": "x", "to": "+56912345678"}); !errors.Is(err, ErrNotOn) {
		t.Fatalf("off = %v", err)
	}
	r.b.Start(ctx)
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi", Allow: []string{"+56912345678", "+15550001111"}}); err != nil {
		t.Fatal(err)
	}
	f := r.next(t)
	if _, err := c.Call(ctx, map[string]any{"text": "x", "to": "+56912345678"}); !errors.Is(err, ErrNotReady) {
		t.Fatalf("not ready = %v", err)
	}
	f.push(Event{Kind: EvConnected, Phone: "15550009"})
	got, ok := r.reg.get(ConnectorName)
	if !ok {
		t.Fatal("not offered")
	}
	if k, _ := got.(connector.Kinder); k == nil || k.Kind() != Kind || connector.RiskOfCall(Kind, map[string]any{"action": "send"}) != connector.RiskAsk {
		t.Fatal("a send is not held for the person")
	}
	if d := got.Describe(); !strings.Contains(d, "+56912345678, +15550001111") {
		t.Fatalf("describe = %q", d)
	}
	cases := []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"text": "hi"}, `say who`},
		{map[string]any{"text": "hi", "to": "+4411112222"}, "not on the allow list"},
		{map[string]any{"text": "hi", "to": "12"}, "not a phone number"},
		{map[string]any{"text": " ", "to": "+56912345678"}, "empty"},
		{map[string]any{"action": "dance"}, "unknown action"},
	}
	for _, tc := range cases {
		if _, err := c.Call(ctx, tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%v: err = %v", tc.args, err)
		}
	}
	if out, err := c.Call(ctx, map[string]any{"action": "send", "to": "+1 555 000 1111", "text": "**ready**"}); err != nil || out != "sent" {
		t.Fatalf("send = %q %v", out, err)
	}
	if out, _ := c.Call(ctx, map[string]any{"action": "status"}); !strings.Contains(out, "connected as +15550009") {
		t.Fatalf("status = %q", out)
	}
	// "to" left out: whoever wrote last.
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "hola"})
	eventually(t, "the reply", func() bool { return len(f.messages()) == 2 })
	if _, err := c.Call(ctx, map[string]any{"text": "follow-up"}); err != nil {
		t.Fatal(err)
	}
	if err := r.b.Test(ctx, "+56912345678"); err != nil {
		t.Fatal(err)
	}
	msgs := f.messages()
	if msgs[0] != "15550001111: *ready*" || msgs[2] != "56912345678: follow-up" || !strings.Contains(msgs[3], "linked") {
		t.Fatalf("sent = %q", msgs)
	}
	f.mu.Lock()
	f.sendErr = errors.New("gone")
	f.mu.Unlock()
	if _, err := c.Call(ctx, map[string]any{"text": "x", "to": "+56912345678"}); err == nil {
		t.Fatal("a failed send said sent")
	}
}

func TestOnlyOneAllowedIsTheDefault(t *testing.T) {
	r := newRig(t)
	f := r.linked(t)
	if err := r.b.send(context.Background(), "", "hi"); err != nil {
		t.Fatal(err)
	}
	if got := f.messages(); len(got) != 1 || got[0] != "56912345678: hi" {
		t.Fatalf("sent = %q", got)
	}
}

func TestLinkUnlinkAndLogout(t *testing.T) {
	r := newRig(t)
	f := r.linked(t)
	f.push(Event{Kind: EvSession, Session: []byte("s1")})
	f.push(Event{Kind: EvDisconnected})
	if st := r.b.Status(); st.State != StateDisconnected {
		t.Fatalf("state = %s", st.State)
	}
	f.push(Event{Kind: EvError, Err: "hiccup"})
	if st := r.b.Status(); st.Error != "hiccup" {
		t.Fatalf("error = %q", st.Error)
	}
	f.push(Event{Kind: EvConnected})
	// Unlink: the phone is told, the session forgotten, and new codes come.
	if err := r.b.Unlink(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.logout != 1 {
		t.Fatalf("logout = %d", f.logout)
	}
	g := r.next(t)
	if g.session != nil {
		t.Fatal("the session survived the unlink")
	}
	if st := r.b.Status(); st.Linked || st.Phone != "" {
		t.Fatalf("status = %+v", st)
	}
	// Events of the old client are dropped.
	f.emit(Event{Kind: EvConnected, Phone: "999"})
	if st := r.b.Status(); st.State == StateConnected {
		t.Fatal("an old client's event counted")
	}
	// The codes run out: Link asks for new ones.
	g.push(Event{Kind: EvQRExpired})
	g.stop <- nil
	eventually(t, "the run to end", func() bool { return r.b.Status().State == StateQRExpired })
	if err := r.b.Link(); err != nil {
		t.Fatal(err)
	}
	h := r.next(t)
	h.push(Event{Kind: EvSession, Session: []byte("s2")})
	h.push(Event{Kind: EvConnected})
	// The phone removes the device.
	h.push(Event{Kind: EvLoggedOut})
	h.stop <- nil
	eventually(t, "logged out", func() bool { st := r.b.Status(); return st.State == StateLoggedOut && !st.Linked })
	if _, ok := r.reg.get(ConnectorName); ok {
		t.Fatal("still offered after a logout")
	}
	// Unlink while off forgets too.
	if err := r.b.Apply(Settings{Kitty: "mimi"}); err != nil {
		t.Fatal(err)
	}
	if err := r.b.Unlink(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRunFailsAndBadSession(t *testing.T) {
	r := newRig(t)
	r.b.opt.NewClient = func() Client {
		f := newFake()
		f.runErr = errors.New("no network")
		r.clients <- f
		return f
	}
	r.b.Start(context.Background())
	// A session sealed with another key is not used.
	if err := os.WriteFile(filepath.Join(r.dir, "session.bin"), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi"}); err != nil {
		t.Fatal(err)
	}
	f := r.next(t)
	if f.session != nil {
		t.Fatal("a session that cannot be opened was used")
	}
	eventually(t, "the error", func() bool { st := r.b.Status(); return st.State == StateError && st.Error == "no network" })
	if !strings.Contains(r.logString(), "cannot be opened") {
		t.Fatalf("log = %q", r.logString())
	}
}

func TestSessionThatCannotBeKept(t *testing.T) {
	r := newRig(t)
	r.b.opt.Box = box{fail: true}
	f := r.linked(t)
	f.push(Event{Kind: EvSession, Session: []byte("s")})
	if st := r.b.Status(); !strings.Contains(st.Error, "could not be kept") {
		t.Fatalf("error = %q", st.Error)
	}
}

func TestApplyBeforeStartWaits(t *testing.T) {
	r := newRig(t)
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-r.clients:
		t.Fatal("connected before Start")
	default:
	}
	r.b.Start(context.Background())
	r.next(t)
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{"+56 9 1234-5678": "56912345678", "0056912345678": "56912345678", "(555) 000 1111 22": "555000111122"} {
		if got, err := Normalize(in); err != nil || got != want {
			t.Fatalf("%q = %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "123", "1234567890123456"} {
		if _, err := Normalize(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestPlainAndSplit(t *testing.T) {
	in := "## Title\n**bold** and __it__ and [the docs](https://x.y/z) and [x](\n#"
	want := "*Title*\n*bold* and _it_ and the docs (https://x.y/z) and [x](\n#"
	if got := Plain(in); got != want {
		t.Fatalf("plain = %q", got)
	}
	if got := Split("aaa bbb\nccc", 5); strings.Join(got, "|") != "aaa|bbb|ccc" {
		t.Fatalf("split = %q", got)
	}
	if got := Split("abcdefgh", 3); strings.Join(got, "|") != "abc|def|gh" {
		t.Fatalf("split long = %q", got)
	}
	if got := Split("ñññ", 3); strings.Join(got, "|") != "ñ|ñ|ñ" {
		t.Fatalf("split utf8 = %q", got)
	}
	if got := Split("  ", 3); len(got) != 0 {
		t.Fatalf("split empty = %q", got)
	}
}

func TestSaveFails(t *testing.T) {
	r := newRig(t)
	// The folder is a file: nothing can be kept.
	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r.b.opt.Dir = filepath.Join(file, "x")
	if err := r.b.Apply(Settings{Kitty: "mimi"}); err == nil {
		t.Fatal("a save into a file was accepted")
	}
	if err := r.b.saveSession([]byte("x")); err == nil {
		t.Fatal("a session save into a file was accepted")
	}
	if _, err := r.b.loadSession(); err == nil {
		t.Fatal("a session read from a file was accepted")
	}
}
