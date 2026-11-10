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

	"github.com/madkoding/kitties/internal/attach"
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
	typing  []string
	reads   []string
	reacts  []string
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

func (f *fakeClient) SendFile(_ context.Context, to string, file File, caption string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return f.sendErr
	}
	f.sent = append(f.sent, to+": ["+file.Name+"|"+file.MIME+"|"+string(file.Data)+"] "+caption)
	return nil
}

func (f *fakeClient) SendReply(_ context.Context, to, text string, ref Ref, quoted string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return f.sendErr
	}
	f.sent = append(f.sent, to+": > "+ref.ID+" "+quoted+" | "+text)
	return nil
}

func (f *fakeClient) MarkRead(_ context.Context, ref Ref) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, ref.ID)
	return nil
}

func (f *fakeClient) React(_ context.Context, ref Ref, emoji string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reacts = append(f.reacts, ref.Chat+"/"+ref.ID+" "+emoji)
	return nil
}

func (f *fakeClient) snapshot() (reads, reacts []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reads...), append([]string(nil), f.reacts...)
}

func (f *fakeClient) Typing(_ context.Context, to string, on, audio bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := fmt.Sprintf("%s:%v", to, on)
	if audio {
		p += ":audio"
	}
	f.typing = append(f.typing, p)
	return nil
}

func (f *fakeClient) typings() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.typing...)
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
	files    []string
	texts    []string
	senders  []string
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
	c.texts = append(c.texts, text)
	s := c.sessions[id]
	s.Turns = append(s.Turns, chat.Turn{From: "user", Text: text})
	s.Turns = append(s.Turns, turns...)
	return turns, nil
}

func (c *fakeChats) SendFiles(ctx context.Context, id, text string, files []attach.Upload) ([]chat.Turn, error) {
	c.mu.Lock()
	for _, f := range files {
		c.files = append(c.files, f.Name+"|"+f.MIME+"|"+string(f.Data))
	}
	c.mu.Unlock()
	return c.Send(ctx, id, text)
}

func (c *fakeChats) SendAs(ctx context.Context, id, sender, text string, files []attach.Upload) ([]chat.Turn, error) {
	c.mu.Lock()
	c.senders = append(c.senders, sender)
	c.mu.Unlock()
	if len(files) > 0 {
		return c.SendFiles(ctx, id, text, files)
	}
	return c.Send(ctx, id, text)
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
	case "luna", "k2":
		k := &kitty.Kitty{ID: "k2", Name: "luna"}
		k.Soul.Language = "Spanish"
		k.Soul.Voice = "nova"
		return k, nil
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
	return newRigAt(t, t.TempDir())
}

func newRigAt(t *testing.T, dir string) *rig {
	t.Helper()
	r := &rig{chats: newChats(), reg: &registry{items: map[string]connector.Connector{}}, clients: make(chan *fakeClient, 8), dir: dir, logs: &strings.Builder{}}
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
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi", Allow: []string{"+56 9 1234 5678"}}, true); err != nil {
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
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi"}, true); err != nil {
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
	if err := r.b.Apply(Settings{Kitty: "mimi"}, true); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.reg.get(ConnectorName); ok {
		t.Fatal("the connector stays offered once off")
	}
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi"}, true); err != nil {
		t.Fatal(err)
	}
	if g := r.next(t); string(g.session) != `{"secret":"keys"}` {
		t.Fatalf("session = %q", g.session)
	}
}

func TestSettingsAreKeptAndChecked(t *testing.T) {
	r := newRig(t)
	if err := r.b.Apply(Settings{Kitty: "mimi", Allow: []string{"+56 9 1234 5678", "0056912345678", "+1 (555) 000-1111"}}, true); err != nil {
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
		if err := r.b.Apply(bad, true); err == nil {
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
	if err := b.Apply(Settings{Enabled: true, Kitty: "x"}, true); !errors.Is(err, ErrNoVault) {
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
	// The contact's name is remembered, even when a message comes without it.
	if s, err := r.chats.Get("c2"); err != nil || s.Title != "WhatsApp: Mad" {
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
	if err := r.b.Apply(Settings{Kitty: "mimi", Allow: []string{"+56912345678", "+15550001111"}}, true); err != nil {
		t.Fatal(err)
	}
	c := connectorOf{r.b}
	ctx := context.Background()
	if _, err := c.Call(ctx, map[string]any{"text": "x", "to": "+56912345678"}); !errors.Is(err, ErrNotOn) {
		t.Fatalf("off = %v", err)
	}
	r.b.Start(ctx)
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi", Allow: []string{"+56912345678", "+15550001111"}}, true); err != nil {
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
	if err := r.b.Apply(Settings{Kitty: "mimi"}, true); err != nil {
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
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi"}, true); err != nil {
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
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi"}, true); err != nil {
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
	if err := r.b.Apply(Settings{Kitty: "mimi"}, true); err == nil {
		t.Fatal("a save into a file was accepted")
	}
	if err := r.b.saveSession([]byte("x")); err == nil {
		t.Fatal("a session save into a file was accepted")
	}
	if _, err := r.b.loadSession(); err == nil {
		t.Fatal("a session read from a file was accepted")
	}
}

func TestTypingWhileTheKittyThinks(t *testing.T) {
	old := typingEvery
	typingEvery = 5 * time.Millisecond
	t.Cleanup(func() { typingEvery = old })
	r := newRig(t)
	f := r.linked(t)
	release := make(chan struct{})
	r.chats.reply = func(string) ([]chat.Turn, error) {
		<-release
		return []chat.Turn{{Text: "ok"}}, nil
	}
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "think"})
	eventually(t, "typing shown again", func() bool { return len(f.typings()) >= 2 })
	close(release)
	eventually(t, "the reply", func() bool { return len(f.messages()) == 1 })
	got := f.typings()
	if got[0] != "56912345678:true" || got[len(got)-1] != "56912345678:false" {
		t.Fatalf("typing = %v", got)
	}
}

func TestBusyInTheKittysLanguage(t *testing.T) {
	r := newRig(t)
	r.b.Start(context.Background())
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "luna", Allow: []string{"+56912345678"}}, true); err != nil {
		t.Fatal(err)
	}
	f := r.next(t)
	f.push(Event{Kind: EvConnected})
	r.chats.reply = func(string) ([]chat.Turn, error) { return nil, chat.ErrBusy }
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "hello"})
	eventually(t, "the busy note", func() bool { return len(f.messages()) == 1 })
	if !strings.Contains(f.messages()[0], "Sigo con tu mensaje") {
		t.Fatalf("busy = %q", f.messages()[0])
	}
}

func TestLanguageOf(t *testing.T) {
	for _, tc := range []struct{ lang, text, want string }{
		{"Spanish", "", "es"}, {"es-CL", "", "es"}, {"Português", "", "pt"}, {"pt-BR", "", "pt"}, {"French", "hola", "en"},
		{"", "hola, ¿puedes ayudarme?", "es"}, {"", "olá, você pode ver isso?", "pt"}, {"", "hi, can you help", "en"}, {"", "ok", "en"},
	} {
		if got := LanguageOf(tc.lang, tc.text); got != tc.want {
			t.Fatalf("%q %q = %s", tc.lang, tc.text, got)
		}
	}
}

func TestGroups(t *testing.T) {
	r := newRig(t)
	f := r.linked(t)
	group := Event{Kind: EvMessage, From: "56912345678", Name: "Mad", Group: true, Chat: "120363@g.us", ChatName: "Familia"}
	// Groups are off: nothing, and the group is not even kept.
	g := group
	g.Text = "mimi, hola"
	f.push(g)
	if st := r.b.Status(); len(st.SeenGroups) != 0 {
		t.Fatalf("seen while off = %+v", st.SeenGroups)
	}
	set := Settings{Enabled: true, Kitty: "mimi", Groups: true, Allow: []string{"+56912345678"}}
	if err := r.b.Apply(set, true); err != nil {
		t.Fatal(err)
	}
	// On, but the group is not on the list: it is offered in Settings and the mention is ignored.
	f.push(g)
	o := group
	o.Chat, o.ChatName, o.Text = "999@g.us", "", "hola"
	f.push(o)
	st := r.b.Status()
	if st.Ignored != 1 || len(st.SeenGroups) != 2 || st.SeenGroups[0].ID != "999@g.us" || st.SeenGroups[1].Name != "Familia" {
		t.Fatalf("status = %+v", st)
	}
	// The same group again, without its name, keeps the name it had.
	f.push(g)
	g2 := group
	g2.ChatName, g2.Text = "", "nada"
	f.push(g2)
	if st := r.b.Status(); st.SeenGroups[0] != (Group{ID: "120363@g.us", Name: "Familia"}) {
		t.Fatalf("seen = %+v", st.SeenGroups)
	}
	set.AllowGroups = []string{"120363@g.us", "120363@g.us"}
	if err := r.b.Apply(set, true); err != nil {
		t.Fatal(err)
	}
	if st := r.b.Status(); len(st.AllowGroups) != 1 {
		t.Fatalf("allow groups = %v", st.AllowGroups)
	}
	// Nobody names the kitty: nothing.
	g.Text = "hola a todos, mimimi"
	f.push(g)
	// Named, or @mentioned, by anyone in the group: answered in the group, with who wrote.
	g.Text = "¿Qué opinas, Mimi?"
	f.push(g)
	eventually(t, "the group reply", func() bool { return len(f.messages()) == 1 })
	m := group
	m.From, m.Text, m.Mentioned = "4411112222", "@15550001 dime", true
	f.push(m)
	eventually(t, "the mention reply", func() bool { return len(f.messages()) == 2 })
	if got := f.messages()[0]; !strings.HasPrefix(got, "120363@g.us: ") {
		t.Fatalf("reply = %q", got)
	}
	r.chats.mu.Lock()
	texts := append([]string(nil), r.chats.texts...)
	r.chats.mu.Unlock()
	// Who wrote goes with the message (its sender), by name and number: two people with the same
	// name stay apart.
	if texts[0] != "¿Qué opinas, Mimi?" || r.chats.senders[0] != "Mad (+56912345678)" || r.chats.senders[1] != "Mad (+4411112222)" || r.chats.created != 1 {
		t.Fatalf("texts = %q, senders = %q, chats = %d", texts, r.chats.senders, r.chats.created)
	}
	s, _ := r.chats.Get("c1")
	if s.Title != "WhatsApp group: Familia" {
		t.Fatalf("title = %q", s.Title)
	}
	// A group message without a name still says who: the name seen before.
	n := group
	n.Name, n.Text, n.Mentioned = "", "hey", true
	f.push(n)
	eventually(t, "the third reply", func() bool { return len(f.messages()) == 3 })
	r.chats.mu.Lock()
	if r.chats.texts[2] != "hey" || r.chats.senders[2] != "Mad (+56912345678)" {
		t.Fatalf("text = %q from %q", r.chats.texts[2], r.chats.senders[2])
	}
	r.chats.mu.Unlock()
	// A group without a name has its id in the title.
	r.b.Stop()
	b2, err := New(Options{Dir: r.dir, Box: box{}, Chats: newChats(), Kitty: kittyOf})
	if err != nil {
		t.Fatal(err)
	}
	if id, err := b2.chatFor(inbound{group: true, to: "77@g.us", key: "g:77@g.us"}); err != nil || b2.opt.Chats.(*fakeChats).sessions[id].Title != "WhatsApp group 77" {
		t.Fatalf("untitled = %v", err)
	}
	// The groups seen are kept.
	if st := b2.Status(); len(st.SeenGroups) != 2 || len(st.AllowGroups) != 1 {
		t.Fatalf("kept = %+v", st)
	}
}

func TestGroupList(t *testing.T) {
	r := newRig(t)
	for _, bad := range []string{"120363", "abc@g.us", "@g.us", "1@s.whatsapp.net", strings.Repeat("1", 41) + "@g.us"} {
		if err := r.b.Apply(Settings{Kitty: "mimi", AllowGroups: []string{bad}}, true); err == nil {
			t.Fatalf("%q was taken", bad)
		}
	}
	many := make([]string, MaxAllow+1)
	for i := range many {
		many[i] = fmt.Sprintf("%d@g.us", i+1)
	}
	if err := r.b.Apply(Settings{Kitty: "mimi", AllowGroups: many}, true); err == nil {
		t.Fatal("too many groups")
	}
	if err := r.b.Apply(Settings{Kitty: "mimi", AllowGroups: []string{" 1203-63@g.us "}}, true); err != nil {
		t.Fatal(err)
	}
	// Only the last maxSeen groups are kept.
	for i := 0; i < maxSeen+5; i++ {
		r.b.mu.Lock()
		r.b.sawLocked(fmt.Sprintf("%d@g.us", i), "")
		r.b.mu.Unlock()
	}
	if st := r.b.Status(); len(st.SeenGroups) != maxSeen || st.SeenGroups[0].ID != fmt.Sprintf("%d@g.us", maxSeen+4) {
		t.Fatalf("seen = %d", len(st.SeenGroups))
	}
	if _, err := New(Options{Dir: t.TempDir(), Defaults: &Settings{AllowGroups: []string{"x"}}}); err == nil {
		t.Fatal("a bad group in the file was taken")
	}
}

func TestSendToGroupsAndFiles(t *testing.T) {
	r := newRig(t)
	lib := map[string]File{
		"report.pdf": {MIME: "application/pdf", Data: []byte("%PDF")},
		"empty.txt":  {},
		"huge.bin":   {Name: "huge.bin", Data: make([]byte, MaxFile+1)},
	}
	r.b.opt.File = func(name string) (File, error) {
		f, ok := lib[name]
		if !ok {
			return File{}, errors.New("no such file")
		}
		return f, nil
	}
	f := r.linked(t)
	r.b.mu.Lock()
	r.b.sawLocked("120363@g.us", "Familia")
	r.b.sawLocked("555@g.us", "Trabajo")
	r.b.mu.Unlock()
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi", Allow: []string{"+56912345678"}, Groups: true, AllowGroups: []string{"120363@g.us"}}, true); err != nil {
		t.Fatal(err)
	}
	c := connectorOf{r.b}
	ctx := context.Background()
	if d := c.Describe(); !strings.Contains(d, "group Familia (120363@g.us)") || !strings.Contains(d, `"file"`) {
		t.Fatalf("describe = %s", d)
	}
	if out, _ := c.Call(ctx, map[string]any{"action": "status"}); !strings.Contains(out, "group Familia") {
		t.Fatalf("status = %s", out)
	}
	for to, want := range map[string]string{"120363@g.us": "120363@g.us", "familia": "120363@g.us", "group:Familia": "120363@g.us"} {
		if _, err := c.Call(ctx, map[string]any{"to": to, "text": "hola"}); err != nil {
			t.Fatalf("%s: %v", to, err)
		}
		if got := f.messages()[len(f.messages())-1]; got != want+": hola" {
			t.Fatalf("%s: sent = %q", to, got)
		}
	}
	for to, want := range map[string]error{"555@g.us": ErrNotAllowGroup, "+4411112222": ErrNotAllow} {
		if _, err := c.Call(ctx, map[string]any{"to": to, "text": "x"}); !errors.Is(err, want) {
			t.Fatalf("%s: %v", to, err)
		}
	}
	if _, err := c.Call(ctx, map[string]any{"to": "Trabajo", "text": "x"}); err == nil {
		t.Fatal("wrote to a group off the list by its name")
	}
	// A file, with its caption; a long text goes before it.
	n := len(f.messages())
	if _, err := c.Call(ctx, map[string]any{"to": "+56912345678", "text": "El **informe**", "file": "report.pdf"}); err != nil {
		t.Fatal(err)
	}
	if got := f.messages()[n]; got != "56912345678: [report.pdf|application/pdf|%PDF] El *informe*" {
		t.Fatalf("file = %q", got)
	}
	if _, err := c.Call(ctx, map[string]any{"to": "+56912345678", "text": strings.Repeat("a ", maxCaption), "file": "report.pdf"}); err != nil {
		t.Fatal(err)
	}
	if got := f.messages()[n+2]; !strings.HasSuffix(got, "%PDF] ") {
		t.Fatalf("long caption = %q", got)
	}
	if _, err := c.Call(ctx, map[string]any{"to": "+56912345678", "file": "report.pdf"}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"nope.pdf", "empty.txt", "huge.bin"} {
		if _, err := c.Call(ctx, map[string]any{"to": "+56912345678", "file": bad}); err == nil {
			t.Fatalf("%s was sent", bad)
		}
	}
	r.b.opt.File = nil
	if _, err := c.Call(ctx, map[string]any{"to": "+56912345678", "file": "report.pdf"}); err == nil {
		t.Fatal("sent without a library")
	}
}

func TestRepliesCarryTheirFiles(t *testing.T) {
	r := newRig(t)
	r.b.opt.File = func(name string) (File, error) {
		if name == "gone.png" {
			return File{}, errors.New("deleted")
		}
		return File{Name: name, MIME: "image/png", Data: []byte("png")}, nil
	}
	f := r.linked(t)
	r.chats.reply = func(string) ([]chat.Turn, error) {
		return []chat.Turn{{From: "mimi", Text: "Here.", Artifacts: []string{"chart.png", "gone.png"}, Working: true}}, nil
	}
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "a chart please"})
	eventually(t, "the reply and its file", func() bool { return len(f.messages()) == 2 })
	if got := f.messages()[1]; got != "56912345678: [chart.png|image/png|png] " {
		t.Fatalf("file = %q", got)
	}
	r.chats.emit("c1", chat.Event{Type: "done", Index: 1, Turn: &chat.Turn{Text: "Here.\n\nAnd the table.", Artifacts: []string{"table.png"}}})
	eventually(t, "the final file", func() bool { return len(f.messages()) == 4 })
	if got := f.messages()[3]; got != "56912345678: [table.png|image/png|png] " {
		t.Fatalf("final file = %q", got)
	}
	if !strings.Contains(r.logString(), "gone.png") {
		t.Fatal("the missing file was not logged")
	}
}

func TestMentions(t *testing.T) {
	for text, want := range map[string]bool{"mimi": true, "Hola @mimi!": true, "mimimi": false, "xmimi": false, "mimi2": false, "a mimi y b": true, "mimimi y mimi": true} {
		if got := mentions(text, "mimi"); got != want {
			t.Fatalf("%q = %v", text, got)
		}
	}
	if mentions("mimi", "") {
		t.Fatal("no name matched")
	}
}

func TestFilesAndVoiceNotes(t *testing.T) {
	r := newRig(t)
	var heard []string
	r.b.opt.Transcribe = func(_ context.Context, f File) (string, error) {
		if string(f.Data) == "mumble" {
			return "", errors.New("unclear")
		}
		heard = append(heard, f.Name)
		return "buy milk", nil
	}
	f := r.linked(t)
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "look", Files: []File{
		{Name: "picture.jpg", MIME: "image/jpeg", Data: []byte("jpg")},
		{MIME: "application/pdf", Data: []byte("pdf")},
	}})
	eventually(t, "the reply", func() bool { return len(f.messages()) == 1 })
	f.push(Event{Kind: EvMessage, From: "56912345678", Files: []File{{Name: "audio.ogg", MIME: "audio/ogg", Data: []byte("voice"), Voice: true}}})
	eventually(t, "the voice reply", func() bool { return len(f.messages()) == 2 })
	f.push(Event{Kind: EvMessage, From: "56912345678", Files: []File{{Name: "audio.ogg", MIME: "audio/ogg", Data: []byte("mumble"), Voice: true}}})
	eventually(t, "the third reply", func() bool { return len(f.messages()) == 3 })
	r.chats.mu.Lock()
	defer r.chats.mu.Unlock()
	if strings.Join(r.chats.files, ",") != "picture.jpg|image/jpeg|jpg,whatsapp-2|application/pdf|pdf,audio.ogg|audio/ogg|mumble" {
		t.Fatalf("files = %q", r.chats.files)
	}
	if r.chats.texts[1] != "(voice note) buy milk" || len(heard) != 1 {
		t.Fatalf("texts = %q", r.chats.texts)
	}
	if !strings.Contains(r.logString(), "could not be transcribed") {
		t.Fatal("the failed transcription is not logged")
	}
}

func TestDefaultsFromTheFile(t *testing.T) {
	dir := t.TempDir()
	b, err := New(Options{Dir: dir, Box: box{}, NewClient: func() Client { return newFake() }, Kitty: kittyOf,
		Defaults: &Settings{Enabled: true, Kitty: "mimi", Groups: true, Allow: []string{"+56 9 1234 5678"}}})
	if err != nil {
		t.Fatal(err)
	}
	if st := b.Status(); !st.Enabled || !st.Groups || st.Allow[0] != "56912345678" {
		t.Fatalf("status = %+v", st)
	}
	// Without a vault the file cannot turn it on.
	var logs []string
	b, err = New(Options{Dir: dir, Defaults: &Settings{Enabled: true}, Log: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }})
	if err != nil || b.Status().Enabled || len(logs) != 1 {
		t.Fatalf("no vault = %v %v", err, logs)
	}
	if _, err := New(Options{Dir: dir, Defaults: &Settings{Allow: []string{"12"}}}); err == nil {
		t.Fatal("a bad number in the file was accepted")
	}
	// Once saved from Settings, the file no longer counts.
	b, _ = New(Options{Dir: dir, Box: box{}, NewClient: func() Client { return newFake() }, Kitty: kittyOf})
	if err := b.Apply(Settings{Kitty: "mimi"}, true); err != nil {
		t.Fatal(err)
	}
	b, _ = New(Options{Dir: dir, Box: box{}, Defaults: &Settings{Enabled: true, Kitty: "mimi"}})
	if b.Status().Enabled {
		t.Fatal("the file won over Settings")
	}
}

func TestReadAndReact(t *testing.T) {
	r := newRig(t)
	f := r.linked(t)
	c := connectorOf{r.b}
	ctx := context.Background()
	if _, err := c.Call(ctx, map[string]any{"action": "react", "to": "+56912345678", "emoji": "👍"}); err == nil {
		t.Fatal("reacted with no message")
	}
	// The kitty answers with an emoji only: it goes as a reaction; the message is marked read.
	r.chats.reply = func(string) ([]chat.Turn, error) { return []chat.Turn{{From: "mimi", Text: " 👍🏽 "}}, nil }
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "listo?", Ref: Ref{Chat: "56912345678@s.whatsapp.net", ID: "m1"}})
	eventually(t, "the reaction", func() bool { _, re := f.snapshot(); return len(re) == 1 })
	reads, reacts := f.snapshot()
	if reads[0] != "m1" || reacts[0] != "56912345678@s.whatsapp.net/m1 👍🏽" || len(f.messages()) != 0 {
		t.Fatalf("reads = %v, reacts = %v, sent = %v", reads, reacts, f.messages())
	}
	// The connector reacts to the last message of a chat.
	for _, args := range []map[string]any{{"action": "react", "emoji": "❤️"}, {"action": "react", "to": "+56912345678", "emoji": ""}} {
		if out, err := c.Call(ctx, args); err != nil || out != "reacted" {
			t.Fatalf("%v = %q %v", args, out, err)
		}
	}
	for _, args := range []map[string]any{{"action": "react", "emoji": "hola"}, {"action": "react", "to": "+4411112222", "emoji": "👍"}} {
		if _, err := c.Call(ctx, args); err == nil {
			t.Fatalf("%v reacted", args)
		}
	}
	if _, reacts := f.snapshot(); len(reacts) != 3 || reacts[1] != "56912345678@s.whatsapp.net/m1 ❤️" {
		t.Fatalf("reacts = %v", reacts)
	}
	if !strings.Contains(c.Describe(), `"react"`) {
		t.Fatal("react is not described")
	}
	// Off, or not connected.
	r.b.mu.Lock()
	r.b.status.State = StateDisconnected
	r.b.mu.Unlock()
	if _, err := c.Call(ctx, map[string]any{"action": "react", "emoji": "👍"}); !errors.Is(err, ErrNotReady) {
		t.Fatalf("not ready = %v", err)
	}
	if err := r.b.Apply(Settings{Kitty: "mimi", Allow: []string{"+56912345678"}}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(ctx, map[string]any{"action": "react", "emoji": "👍"}); !errors.Is(err, ErrNotOn) {
		t.Fatalf("off = %v", err)
	}
}

func TestIsEmoji(t *testing.T) {
	for s, want := range map[string]bool{
		"👍": true, "👍🏽": true, "❤️": true, "🇨🇱": true, "👨‍👩‍👧": true, "☀": true, "😀😀": true,
		"": false, "ok": false, "👍 ok": false, "1": false, "😀😀😀😀😀": false, "\ufe0f": false, strings.Repeat("😀", 9): false,
	} {
		if got := IsEmoji(s); got != want {
			t.Fatalf("IsEmoji(%q) = %v", s, got)
		}
	}
}

func TestKittyPerChat(t *testing.T) {
	r := newRig(t)
	f := r.linked(t)
	set := Settings{Enabled: true, Kitty: "mimi", Allow: []string{"+56912345678", "+15550001111"}, Groups: true,
		AllowGroups: []string{"120363@g.us"}, Kitties: map[string]string{"+1 555 000 1111": "luna", "120363@g.us": "luna", "56912345678": ""}}
	if err := r.b.Apply(set, true); err != nil {
		t.Fatal(err)
	}
	if st := r.b.Status(); len(st.Kitties) != 2 || st.Kitties["15550001111"] != "k2" || st.Kitties["120363@g.us"] != "k2" {
		t.Fatalf("kitties = %v", st.Kitties)
	}
	f.push(Event{Kind: EvMessage, From: "15550001111", Text: "hola"})
	eventually(t, "luna's chat", func() bool { return len(f.messages()) == 1 })
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "hi"})
	eventually(t, "mimi's chat", func() bool { return len(f.messages()) == 2 })
	// In the group luna answers to her own name, not mimi's.
	g := Event{Kind: EvMessage, From: "4411112222", Group: true, Chat: "120363@g.us", Text: "mimi?"}
	f.push(g)
	g.Text = "luna, ¿estás?"
	f.push(g)
	eventually(t, "the group chat", func() bool { return len(f.messages()) == 3 })
	r.chats.mu.Lock()
	members := map[string]string{}
	for _, s := range r.chats.sessions {
		members[s.Title] = strings.Join(s.Members, ",")
	}
	r.chats.mu.Unlock()
	if members["WhatsApp +15550001111"] != "k2" || members["WhatsApp +56912345678"] != "k1" || members["WhatsApp group 120363"] != "k2" {
		t.Fatalf("members = %v", members)
	}
	// Moving a number to another kitty starts a new chat with it.
	set.Kitties = map[string]string{"56912345678": "luna"}
	if err := r.b.Apply(set, true); err != nil {
		t.Fatal(err)
	}
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "again"})
	eventually(t, "the new chat", func() bool { return len(f.messages()) == 4 })
	r.chats.mu.Lock()
	created := r.chats.created
	r.chats.mu.Unlock()
	if created != 4 {
		t.Fatalf("chats = %d", created)
	}
	// Bad entries.
	for _, bad := range []map[string]string{{"12": "mimi"}, {"+56912345678": "nobody"}} {
		set.Kitties = bad
		if err := r.b.Apply(set, true); err == nil {
			t.Fatalf("%v was taken", bad)
		}
	}
	many := map[string]string{}
	for i := 0; i <= 2*MaxAllow; i++ {
		many[fmt.Sprintf("+5691234%04d", i)] = "mimi"
	}
	set.Kitties = many
	if err := r.b.Apply(set, true); err == nil {
		t.Fatal("too many kitties were taken")
	}
	if _, err := New(Options{Dir: t.TempDir(), Kitty: kittyOf, Defaults: &Settings{Kitties: map[string]string{"x": "mimi"}}}); err == nil {
		t.Fatal("a bad number in the file was taken")
	}
}

func TestHourlyLimit(t *testing.T) {
	r := newRig(t)
	f := r.linked(t)
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi", Allow: []string{"+56912345678"}, MaxPerHour: 2}, true); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []int{-1, MaxPerHourLimit + 1} {
		if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi", MaxPerHour: bad}, true); err == nil {
			t.Fatalf("%d was taken", bad)
		}
	}
	c := connectorOf{r.b}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := c.Call(ctx, map[string]any{"to": "+56912345678", "text": "x"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.Call(ctx, map[string]any{"to": "+56912345678", "text": "x"}); !errors.Is(err, ErrLimit) {
		t.Fatalf("over the limit = %v", err)
	}
	r.b.opt.File = func(string) (File, error) { return File{Data: []byte("x")}, nil }
	if _, err := c.Call(ctx, map[string]any{"to": "+56912345678", "file": "a.txt"}); !errors.Is(err, ErrLimit) {
		t.Fatalf("a file over the limit = %v", err)
	}
	// A reply over the limit is held, and so is a reaction.
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "hola", Ref: Ref{ID: "m1"}})
	eventually(t, "the held reply", func() bool { return r.b.Status().Held == 3 })
	if _, err := c.Call(ctx, map[string]any{"action": "react", "emoji": "👍"}); !errors.Is(err, ErrLimit) {
		t.Fatalf("a reaction over the limit = %v", err)
	}
	r.chats.reply = func(string) ([]chat.Turn, error) { return []chat.Turn{{From: "mimi", Text: "👍"}}, nil }
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "ok?", Ref: Ref{ID: "m2"}})
	eventually(t, "the held reaction", func() bool { return r.b.Status().Held == 5 })
	if st := r.b.Status(); st.SentLastHour != 2 || len(f.messages()) != 2 {
		t.Fatalf("status = %+v, sent %v", st, f.messages())
	}
	// An hour later there is room again; the default limit is larger.
	r.b.mu.Lock()
	for i := range r.b.sent {
		r.b.sent[i] = r.b.sent[i].Add(-time.Hour)
	}
	r.b.set.MaxPerHour = 0
	r.b.mu.Unlock()
	if _, err := c.Call(ctx, map[string]any{"to": "+56912345678", "text": "x"}); err != nil {
		t.Fatal(err)
	}
	if st := r.b.Status(); st.SentLastHour != 1 {
		t.Fatalf("sent = %d", st.SentLastHour)
	}
}

func TestQuotes(t *testing.T) {
	r := newRig(t)
	f := r.linked(t)
	// A direct answer to the last message does not quote it.
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "hola", Ref: Ref{ID: "m1"}})
	eventually(t, "the reply", func() bool { return len(f.messages()) == 1 })
	if got := f.messages()[0]; strings.Contains(got, "> ") {
		t.Fatalf("quoted = %q", got)
	}
	// Once a newer message came, the answer quotes the one it answers.
	in := &inbound{key: "56912345678", to: "56912345678", raw: "¿y lo otro?", ref: Ref{ID: "m2"}}
	r.b.mu.Lock()
	r.b.refs["56912345678"] = Ref{ID: "m3"}
	r.b.mu.Unlock()
	r.b.replyTo(context.Background(), f, in.to, in, "Ya va.")
	if got := f.messages()[1]; got != "56912345678: > m2 ¿y lo otro? | Ya va." {
		t.Fatalf("quote = %q", got)
	}
	// In a group the answer always quotes, only its first part.
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi", Allow: []string{"+56912345678"}, Groups: true, AllowGroups: []string{"120363@g.us"}}, true); err != nil {
		t.Fatal(err)
	}
	r.chats.reply = func(string) ([]chat.Turn, error) {
		return []chat.Turn{{From: "mimi", Text: strings.Repeat("a", maxPart) + " b"}}, nil
	}
	f.push(Event{Kind: EvMessage, From: "4411112222", Name: "Ana", Group: true, Chat: "120363@g.us", Text: "mimi, ¿hora?", Ref: Ref{ID: "g1"}})
	eventually(t, "the group reply", func() bool { return len(f.messages()) == 4 })
	if got := f.messages(); !strings.HasPrefix(got[2], "120363@g.us: > g1 mimi, ¿hora? | aaa") || got[3] != "120363@g.us: b" {
		t.Fatalf("group = %q", got[2:])
	}
}

func TestQuietHours(t *testing.T) {
	r := newRig(t)
	clock := time.Date(2026, 11, 6, 23, 0, 0, 0, time.Local)
	var slept []time.Duration
	var mu sync.Mutex
	r.b.now = func() time.Time { return clock }
	r.b.sleep = func(_ context.Context, d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		slept = append(slept, d)
	}
	f := r.linked(t)
	set := Settings{Enabled: true, Kitty: "luna", Allow: []string{"+56912345678"}, QuietFrom: "22:00", QuietTo: "08:00"}
	if err := r.b.Apply(set, true); err != nil {
		t.Fatal(err)
	}
	if st := r.b.Status(); !st.Quiet || st.QuietFrom != "22:00" {
		t.Fatalf("status = %+v", st)
	}
	// The kitties do not write; the person's test does.
	c := connectorOf{r.b}
	if _, err := c.Call(context.Background(), map[string]any{"to": "+56912345678", "text": "x"}); !errors.Is(err, ErrQuiet) {
		t.Fatalf("send = %v", err)
	}
	if err := r.b.Test(context.Background(), "+56912345678"); err != nil {
		t.Fatal(err)
	}
	// Two messages: one note in luna's language, then both answered when the hours end.
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "hola", Ref: Ref{ID: "m1"}})
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "¿estás?", Ref: Ref{ID: "m2"}})
	eventually(t, "both answers", func() bool { return len(f.messages()) == 4 })
	got := f.messages()
	if got[1] != "56912345678: 🌙 luna descansa hasta las 08:00; te responde entonces." || strings.Contains(got[3], "🌙") {
		t.Fatalf("sent = %q", got)
	}
	if _, err := c.Call(context.Background(), map[string]any{"action": "react", "emoji": "👍"}); !errors.Is(err, ErrQuiet) {
		t.Fatalf("react = %v", err)
	}
	mu.Lock()
	if len(slept) != 2 || slept[0] != 9*time.Hour {
		t.Fatalf("slept = %v", slept)
	}
	mu.Unlock()
	if st := r.b.Status(); st.Waiting != 0 {
		t.Fatalf("waiting = %d", st.Waiting)
	}
}

func TestQuietWindow(t *testing.T) {
	b := &Bridge{}
	day := func(h, m int) time.Time { return time.Date(2026, 11, 6, h, m, 0, 0, time.UTC) }
	cases := []struct {
		from, to string
		at       time.Time
		on       bool
		until    time.Time
	}{
		{"22:00", "08:00", day(23, 0), true, day(8, 0).AddDate(0, 0, 1)},
		{"22:00", "08:00", day(3, 0), true, day(8, 0)},
		{"22:00", "08:00", day(12, 0), false, time.Time{}},
		{"13:00", "14:30", day(13, 59), true, day(14, 30)},
		{"13:00", "14:30", day(14, 30), false, time.Time{}},
		{"", "", day(1, 0), false, time.Time{}},
	}
	for _, tc := range cases {
		b.set.QuietFrom, b.set.QuietTo = tc.from, tc.to
		on, until := b.quietLocked(tc.at)
		if on != tc.on || !until.Equal(tc.until) {
			t.Fatalf("%s-%s at %s = %v %v", tc.from, tc.to, tc.at, on, until)
		}
	}
	for _, bad := range [][2]string{{"22:00", ""}, {"25:00", "08:00"}, {"08:00", "08:00"}, {"x", "y"}} {
		if _, _, err := quietHours(bad[0], bad[1]); err == nil {
			t.Fatalf("%v was taken", bad)
		}
	}
	if f, to, err := quietHours(" 7:05 ", "9:00"); err != nil || f != "07:05" || to != "09:00" {
		t.Fatalf("unpadded = %s %s %v", f, to, err)
	}
	if _, err := New(Options{Dir: t.TempDir(), Defaults: &Settings{QuietFrom: "22:00"}}); err == nil {
		t.Fatal("half quiet hours in the file were taken")
	}
	// sleepCtx ends with its context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sleepCtx(ctx, time.Hour)
	sleepCtx(context.Background(), time.Millisecond)
}

func TestReconnect(t *testing.T) {
	r := newRig(t)
	if err := r.b.Reconnect(); !errors.Is(err, ErrNotOn) {
		t.Fatalf("off = %v", err)
	}
	f := r.linked(t)
	if err := r.b.Reconnect(); !errors.Is(err, ErrNoSession) {
		t.Fatalf("no session = %v", err)
	}
	f.push(Event{Kind: EvSession, Session: []byte("state")})
	eventually(t, "the session", func() bool { return r.b.Status().Linked })
	if err := r.b.Reconnect(); err != nil {
		t.Fatal(err)
	}
	f2 := r.next(t)
	f2.mu.Lock()
	session := string(f2.session)
	f2.mu.Unlock()
	if f2 == f || session != "state" {
		t.Fatalf("reconnected with %q", session)
	}
}

func TestQuietQueueSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	r := newRigAt(t, dir)
	night := time.Date(2026, 11, 6, 23, 0, 0, 0, time.Local)
	r.b.now = func() time.Time { return night }
	r.b.sleep = func(ctx context.Context, _ time.Duration) { <-ctx.Done() } // the gateway stops before 08:00
	f := r.linked(t)
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi", Allow: []string{"+56912345678"}, QuietFrom: "22:00", QuietTo: "08:00"}, true); err != nil {
		t.Fatal(err)
	}
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "first", Ref: Ref{ID: "m1"}, Files: []File{{Name: "a.txt", MIME: "text/plain", Data: []byte("hi")}}})
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "second", Ref: Ref{ID: "m2"}})
	eventually(t, "the note", func() bool { return len(f.messages()) == 1 })
	sealed, err := os.ReadFile(filepath.Join(dir, "queue.bin"))
	if err != nil || bytes.Contains(sealed, []byte("first")) {
		t.Fatalf("the queue is not sealed: %v", err)
	}
	r.b.Stop()
	if r.chats.created != 0 {
		t.Fatal("answered in the quiet hours")
	}
	// After the restart, in the morning, both are answered in order and the queue goes away.
	r2 := newRigAt(t, dir)
	r2.b.now = func() time.Time { return night.Add(10 * time.Hour) }
	r2.b.Start(context.Background())
	f2 := r2.next(t)
	f2.push(Event{Kind: EvConnected, Phone: "15550001"})
	f2.push(Event{Kind: EvConnected, Phone: "15550001"}) // a reconnect does not answer them again
	eventually(t, "both answers", func() bool { return len(f2.messages()) == 2 })
	r2.b.wg.Wait()
	r2.chats.mu.Lock()
	texts, files := append([]string(nil), r2.chats.texts...), append([]string(nil), r2.chats.files...)
	r2.chats.mu.Unlock()
	if strings.Join(texts, ",") != "first,second" || len(files) != 1 {
		t.Fatalf("texts = %q, files = %q", texts, files)
	}
	if _, err := os.Stat(filepath.Join(dir, "queue.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the queue stays: %v", err)
	}
	if !strings.Contains(r2.logString(), "2 waiting messages") {
		t.Fatal(r2.logString())
	}
}

func TestQuietQueueDamaged(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "queue.bin"), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newRigAt(t, dir)
	f := r.linked(t)
	_ = f
	eventually(t, "the drop", func() bool { return strings.Contains(r.logString(), "cannot be read") })
	if _, err := os.Stat(filepath.Join(dir, "queue.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a damaged queue stays")
	}
	// A queue that cannot be sealed is reported.
	r.b.mu.Lock()
	r.b.opt.Box = box{fail: true}
	r.b.queue["x"] = pending{}
	r.b.saveQueueLocked()
	r.b.queue = map[string]pending{}
	r.b.mu.Unlock()
	if !strings.Contains(r.logString(), "keep the waiting messages") {
		t.Fatal(r.logString())
	}
	// Unlinking forgets them.
	r.b.mu.Lock()
	r.b.opt.Box = box{}
	r.b.queue["y"] = pending{}
	r.b.saveQueueLocked()
	r.b.mu.Unlock()
	if err := r.b.Unlink(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "queue.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unlink kept the queue")
	}
}

func TestDesktopTaskSendsItsPlans(t *testing.T) {
	gap := progressGap
	t.Cleanup(func() { progressGap = gap })
	progressGap = 0
	r := newRig(t)
	f := r.linked(t)
	r.chats.reply = func(string) ([]chat.Turn, error) {
		return []chat.Turn{{From: "mimi", Text: "On it.", Working: true}}, nil
	}
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "buy the tickets"})
	eventually(t, "the first reply", func() bool { return len(f.messages()) == 1 })
	plan := func(lines ...string) {
		r.chats.mu.Lock()
		steps := []chat.Step{{Action: "click"}}
		for _, l := range lines {
			steps = append(steps, chat.Step{Action: chat.StepPlan, Result: l})
		}
		r.chats.sessions["c1"].Turns[1].Steps = steps
		r.chats.mu.Unlock()
		r.chats.emit("c1", chat.Event{Type: "update", Index: 1, Turn: &chat.Turn{}})
	}
	// A new plan line goes out as it comes; a step that is no plan does not.
	plan("I open the shop and look for the tickets.")
	eventually(t, "the plan", func() bool { return len(f.messages()) == 2 })
	if got := f.messages()[1]; got != "56912345678: ⏳ I open the shop and look for the tickets." {
		t.Fatalf("plan = %q", got)
	}
	plan("I open the shop and look for the tickets.")
	// When several came meanwhile, the newest goes out.
	plan("I open the shop and look for the tickets.", "I fill in the form.", "I pay.")
	eventually(t, "the newest plan", func() bool { return len(f.messages()) == 3 })
	if got := f.messages()[2]; got != "56912345678: ⏳ I pay." {
		t.Fatalf("newest = %q", got)
	}
	r.chats.emit("c1", chat.Event{Type: "done", Index: 1, Turn: &chat.Turn{Text: "On it.\n\nBought."}})
	eventually(t, "the final reply", func() bool { return len(f.messages()) == 4 })
}

func TestProgressWaitsItsGap(t *testing.T) {
	gap := progressGap
	t.Cleanup(func() { progressGap = gap })
	progressGap = time.Hour
	r := newRig(t)
	f := r.linked(t)
	r.chats.sessions["c9"] = &chat.Session{ID: "c9", Turns: []chat.Turn{{Steps: []chat.Step{{Action: chat.StepPlan, Result: "I look."}}}}}
	st := &progressState{seen: map[int]int{}, last: time.Now()}
	in := &inbound{from: "56912345678", to: "56912345678", who: "56912345678"}
	r.b.progress(context.Background(), f, in, "c9", 0, st)
	r.b.progress(context.Background(), f, in, "c9", 5, st)
	r.b.progress(context.Background(), f, in, "gone", 0, st)
	if len(f.messages()) != 0 || st.seen[0] != 0 {
		t.Fatalf("within the gap nothing goes out: %v %v", f.messages(), st.seen)
	}
}

func TestTheLeadAnswersWhenNoKittyIsChosen(t *testing.T) {
	r := newRig(t)
	if err := r.b.Apply(Settings{Enabled: true, Allow: []string{"+56 9 1234 5678"}}, true); err == nil {
		t.Fatal("on with no kitty and no lead was accepted")
	}
	var lead *kitty.Kitty
	r.b.opt.Lead = func() *kitty.Kitty { return lead }
	lead, _ = kittyOf("luna")
	r.b.Start(context.Background())
	if err := r.b.Apply(Settings{Enabled: true, Allow: []string{"+56 9 1234 5678"}}, true); err != nil {
		t.Fatal(err)
	}
	if st := r.b.Status(); st.Kitty != "" || st.KittyName != "luna" {
		t.Fatalf("status = %+v", st)
	}
	f := r.next(t)
	f.push(Event{Kind: EvConnected, Phone: "15550001"})
	f.push(Event{Kind: EvMessage, From: "56912345678", Name: "Mad", Text: "hola"})
	eventually(t, "the reply", func() bool { return len(f.messages()) == 1 })
	if s, _ := r.chats.Get("c1"); s.Members[0] != "k2" {
		t.Fatalf("chat = %+v", s)
	}
}

func TestKittiesThatAnswerWorkOnTheirOwn(t *testing.T) {
	r := newRig(t)
	if r.b.Independent("k1") {
		t.Fatal("with no kitty chosen, nobody works on its own")
	}
	set := Settings{Kitty: "mimi", Kitties: map[string]string{"+56912345678": "luna"}}
	if err := r.b.Apply(set, false); err != nil {
		t.Fatal(err)
	}
	if !r.b.Independent("k1") || !r.b.Independent("k2") || r.b.Independent("k3") || r.b.Independent("") {
		t.Fatal("the kitties that answer on WhatsApp work on their own")
	}
	set.ShareWithTeam = true
	if err := r.b.Apply(set, false); err != nil {
		t.Fatal(err)
	}
	if r.b.Independent("k1") || r.b.Independent("k2") || !r.b.Status().ShareWithTeam {
		t.Fatal("share_with_team lets them work with the others")
	}
}

func TestStatusSaysWhenTheLeadAnswers(t *testing.T) {
	r := newRig(t)
	lead := &kitty.Kitty{ID: "k1", Name: "mimi", Lead: true}
	r.b.opt.Lead = func() *kitty.Kitty { return lead }
	r.b.opt.Kitty = func(ref string) (*kitty.Kitty, error) {
		if ref == "k1" || ref == "mimi" {
			return lead, nil
		}
		return kittyOf(ref)
	}
	if st := r.b.Status(); !st.LeadAnswers || st.KittyName != "mimi" {
		t.Fatalf("status = %+v", st)
	}
	if err := r.b.Apply(Settings{Kitty: "luna"}, false); err != nil {
		t.Fatal(err)
	}
	if st := r.b.Status(); st.LeadAnswers || st.KittyName != "luna" {
		t.Fatalf("status = %+v", st)
	}
}

func TestRelaySaysTextInTheChatsWhatsApp(t *testing.T) {
	r := newRig(t)
	f := r.linked(t)
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "hola"})
	eventually(t, "the answer", func() bool { return len(f.messages()) == 1 })
	r.chats.mu.Lock()
	var id string
	for _, s := range r.chats.sessions {
		id = s.ID
	}
	r.chats.mu.Unlock()
	if err := r.b.Relay(context.Background(), id, "The lead says yes."); err != nil {
		t.Fatal(err)
	}
	if got := f.messages(); len(got) != 2 || !strings.Contains(got[1], "The lead says yes.") {
		t.Fatalf("sent = %v", got)
	}
	if err := r.b.Relay(context.Background(), "nope", "x"); err == nil {
		t.Fatal("a chat that is not WhatsApp's was relayed")
	}
}

func TestOffLANAndTaskMinutes(t *testing.T) {
	r := newRig(t)
	set := Settings{Kitty: "mimi", TaskMinutes: 10, TaskMinutesFor: map[string]int{"+56912345678": 2, "123@g.us": 30, "56900000000": 0}}
	if err := r.b.Apply(set, false); err != nil {
		t.Fatal(err)
	}
	if !r.b.OffLAN("k1") || r.b.OffLAN("k3") {
		t.Fatal("the kitty that works on its own stays off the local network, the others do not")
	}
	got := r.b.Status().TaskMinutesFor
	if len(got) != 2 || got["56912345678"] != 2 || got["123@g.us"] != 30 {
		t.Fatalf("minutes kept = %v", got)
	}
	r.b.mu.Lock()
	r.b.chats["56912345678"], r.b.chats["g:123@g.us"], r.b.chats["56977777777"] = "c1", "c2", "c3"
	r.b.mu.Unlock()
	if r.b.TasksPerDay("c1") != DefaultTasksPerDay || r.b.TasksPerDay("other") != 0 {
		t.Fatalf("tasks per day = %d", r.b.TasksPerDay("c1"))
	}
	if r.b.TaskMinutes("c1") != 2 || r.b.TaskMinutes("c2") != 30 || r.b.TaskMinutes("c3") != 10 || r.b.TaskMinutes("other") != 0 {
		t.Fatalf("minutes = %d %d %d %d", r.b.TaskMinutes("c1"), r.b.TaskMinutes("c2"), r.b.TaskMinutes("c3"), r.b.TaskMinutes("other"))
	}
	set.TaskMinutes = 0
	set.LocalNetwork = true
	if err := r.b.Apply(set, false); err != nil {
		t.Fatal(err)
	}
	if r.b.OffLAN("k1") || r.b.TaskMinutes("c3") != DefaultTaskMinutes {
		t.Fatal("local_network lets it reach the local network; no minutes is the default")
	}
	set.TasksPerDay, set.TasksPerDayFor = 3, map[string]int{"123@g.us": 1}
	if err := r.b.Apply(set, false); err != nil {
		t.Fatal(err)
	}
	if r.b.TasksPerDay("c2") != 1 || r.b.TasksPerDay("c3") != 3 {
		t.Fatalf("tasks per day = %d %d", r.b.TasksPerDay("c2"), r.b.TasksPerDay("c3"))
	}
	for _, bad := range []Settings{{TaskMinutes: 61}, {TaskMinutes: -1}, {TaskMinutesFor: map[string]int{"123@g.us": 99}}, {TasksPerDay: 201}} {
		if err := r.b.Apply(bad, false); err == nil {
			t.Errorf("%+v was taken", bad)
		}
	}
}
