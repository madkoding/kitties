package wameow

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/madkoding/kitties/internal/whatsapp"
)

// fakeConn stands for whatsmeow.Client.
type fakeConn struct {
	mu         sync.Mutex
	dev        *store.Device
	handler    whatsmeow.EventHandler
	codes      chan whatsmeow.QRChannelItem
	qrErr      error
	connectErr error
	connected  bool
	sent       []string
	sendErr    error
	logouts    int
}

func (f *fakeConn) AddEventHandler(h whatsmeow.EventHandler) uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handler = h
	return 1
}

func (f *fakeConn) GetQRChannel(context.Context) (<-chan whatsmeow.QRChannelItem, error) {
	return f.codes, f.qrErr
}

func (f *fakeConn) Connect() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connected = f.connectErr == nil
	return f.connectErr
}

func (f *fakeConn) Disconnect() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connected = false
}

func (f *fakeConn) SendMessage(_ context.Context, to types.JID, m *waE2E.Message, _ ...whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, to.String()+" "+m.GetConversation())
	return whatsmeow.SendResponse{}, f.sendErr
}

func (f *fakeConn) Logout(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logouts++
	return nil
}

func (f *fakeConn) fire(evt any) {
	f.mu.Lock()
	h := f.handler
	f.mu.Unlock()
	h(evt)
}

type recorder struct {
	mu  sync.Mutex
	evs []whatsapp.Event
}

func (r *recorder) emit(ev whatsapp.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evs = append(r.evs, ev)
}

func (r *recorder) kinds() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, e := range r.evs {
		out = append(out, string(e.Kind))
	}
	return strings.Join(out, ",")
}

func (r *recorder) last(kind whatsapp.EventKind) (whatsapp.Event, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.evs) - 1; i >= 0; i-- {
		if r.evs[i].Kind == kind {
			return r.evs[i], true
		}
	}
	return whatsapp.Event{}, false
}

func wait(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func clientWith(f *fakeConn) *Client {
	return &Client{flushEvery: 10 * time.Millisecond, newConn: func(dev *store.Device) conn {
		f.mu.Lock()
		f.dev = dev
		f.mu.Unlock()
		return f
	}}
}

func TestLinkPairAndKeep(t *testing.T) {
	f := &fakeConn{codes: make(chan whatsmeow.QRChannelItem, 4)}
	c := clientWith(f)
	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx, nil, rec.emit) }()
	f.codes <- whatsmeow.QRChannelItem{Event: whatsmeow.QRChannelEventCode, Code: "2@code"}
	wait(t, "the code", func() bool { _, ok := rec.last(whatsapp.EvQR); return ok })
	if ev, _ := rec.last(whatsapp.EvQR); ev.QR != "2@code" {
		t.Fatalf("qr = %q", ev.QR)
	}
	// What whatsmeow does on a scan: the device gets its id and is kept.
	f.mu.Lock()
	dev := f.dev
	f.mu.Unlock()
	id := types.NewADJID("15550001", 0, 7)
	dev.ID = &id
	if err := dev.Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.fire(&events.PairSuccess{ID: id})
	f.codes <- whatsmeow.QRChannelSuccess
	close(f.codes)
	f.fire(&events.Connected{})
	wait(t, "the session", func() bool { _, ok := rec.last(whatsapp.EvSession); return ok })
	if ev, _ := rec.last(whatsapp.EvConnected); ev.Phone != "15550001" {
		t.Fatalf("connected = %+v", ev)
	}
	if ev, _ := rec.last(whatsapp.EvPaired); ev.Phone != "15550001" {
		t.Fatalf("paired = %+v", ev)
	}
	if err := c.Send(context.Background(), "56912345678", "hola"); err != nil {
		t.Fatal(err)
	}
	if f.sent[0] != "56912345678@s.whatsapp.net hola" {
		t.Fatalf("sent = %q", f.sent)
	}
	if err := c.Logout(context.Background()); err != nil || f.logouts != 1 {
		t.Fatalf("logout = %v %d", err, f.logouts)
	}
	// A change after the start is handed out on the next tick, and at the end.
	_ = dev.PushName
	_ = dev.Contacts.PutContactName(context.Background(), types.NewJID("1", types.DefaultUserServer), "A", "A B")
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	ev, _ := rec.last(whatsapp.EvSession)
	// The kept blob opens again, linked.
	_, dev2, err := Open(ev.Session, nil)
	if err != nil || dev2.ID.User != "15550001" {
		t.Fatalf("reopen = %v %v", dev2, err)
	}
	if err := c.Send(context.Background(), "1", "x"); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("send after the end = %v", err)
	}
	if err := c.Logout(context.Background()); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("logout after the end = %v", err)
	}
	if f.connected {
		t.Fatal("still connected")
	}
}

func TestResumeAndLogout(t *testing.T) {
	s, _ := paired(t)
	blob, _ := s.Export()
	f := &fakeConn{}
	c := clientWith(f)
	rec := &recorder{}
	done := make(chan error, 1)
	go func() { done <- c.Run(context.Background(), blob, rec.emit) }()
	wait(t, "the handler", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.handler != nil && f.connected })
	f.fire(&events.Disconnected{})
	f.fire(&events.TemporaryBan{Code: events.TempBanSentToTooManyPeople, Expire: time.Hour})
	f.fire(&events.ConnectFailure{Message: "nope"})
	f.fire(&events.ClientOutdated{})
	f.fire(&events.KeepAliveTimeout{})
	f.fire(&events.LoggedOut{})
	f.fire(&events.LoggedOut{}) // a second one does not block
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := rec.kinds(); got != "disconnected,error,error,error,logged_out,logged_out" {
		t.Fatalf("events = %s", got)
	}
}

func TestRunFailures(t *testing.T) {
	rec := &recorder{}
	if err := clientWith(&fakeConn{}).Run(context.Background(), []byte("{"), rec.emit); err == nil {
		t.Fatal("a damaged session ran")
	}
	if err := clientWith(&fakeConn{qrErr: errors.New("no qr")}).Run(context.Background(), nil, rec.emit); err == nil {
		t.Fatal("ran without a QR channel")
	}
	if err := clientWith(&fakeConn{connectErr: errors.New("offline")}).Run(context.Background(), nil, rec.emit); err == nil {
		t.Fatal("ran without a connection")
	}
	// The codes run out.
	f := &fakeConn{codes: make(chan whatsmeow.QRChannelItem, 2)}
	f.codes <- whatsmeow.QRChannelTimeout
	if err := clientWith(f).Run(context.Background(), nil, rec.emit); err != nil {
		t.Fatal(err)
	}
	if _, ok := rec.last(whatsapp.EvQRExpired); !ok {
		t.Fatal("no expiry")
	}
	// A failed link.
	for _, item := range []whatsmeow.QRChannelItem{whatsmeow.QRChannelClientOutdated, {Event: "error", Error: errors.New("bad pair")}} {
		f := &fakeConn{codes: make(chan whatsmeow.QRChannelItem, 1)}
		f.codes <- item
		if err := clientWith(f).Run(context.Background(), nil, rec.emit); err == nil || !strings.Contains(err.Error(), "link failed") {
			t.Fatalf("%v: err = %v", item.Event, err)
		}
	}
}

func TestMessages(t *testing.T) {
	s, dev := paired(t)
	_ = s.PutLIDMapping(context.Background(), types.NewJID("777", types.HiddenUserServer), types.NewJID("56912345678", types.DefaultUserServer))
	pn := types.NewJID("56912345678", types.DefaultUserServer)
	lid := types.NewJID("777", types.HiddenUserServer)
	msg := func(info types.MessageInfo, m *waE2E.Message) whatsapp.Event {
		evs := translate(s, dev, &events.Message{Info: info, Message: m})
		if len(evs) == 0 {
			return whatsapp.Event{}
		}
		return evs[0]
	}
	text := &waE2E.Message{Conversation: proto.String("hola")}
	if ev := msg(types.MessageInfo{MessageSource: types.MessageSource{Sender: pn}, ID: "m1", PushName: "Mad"}, text); ev.From != "56912345678" || ev.Text != "hola" || ev.Name != "Mad" || ev.ID != "m1" {
		t.Fatalf("pn = %+v", ev)
	}
	ext := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("link https://x")}}
	if ev := msg(types.MessageInfo{MessageSource: types.MessageSource{Sender: lid, SenderAlt: pn}}, ext); ev.From != "56912345678" || ev.Text != "link https://x" {
		t.Fatalf("alt = %+v", ev)
	}
	if ev := msg(types.MessageInfo{MessageSource: types.MessageSource{Sender: lid}}, text); ev.From != "56912345678" {
		t.Fatalf("lid = %+v", ev)
	}
	if ev := msg(types.MessageInfo{MessageSource: types.MessageSource{Sender: types.NewJID("1", types.HiddenUserServer)}}, text); ev.From != "" {
		t.Fatalf("unknown lid = %+v", ev)
	}
	if ev := msg(types.MessageInfo{MessageSource: types.MessageSource{Sender: pn, IsGroup: true}}, text); !ev.Group {
		t.Fatalf("group = %+v", ev)
	}
	if ev := msg(types.MessageInfo{MessageSource: types.MessageSource{Sender: pn, IsFromMe: true}}, text); ev.Kind != "" {
		t.Fatal("our own message came in")
	}
	if ev := msg(types.MessageInfo{MessageSource: types.MessageSource{Sender: pn}}, nil); ev.Kind != "" {
		t.Fatal("an empty message came in")
	}
	if evs := translate(s, dev, "something else"); evs != nil {
		t.Fatal("an unknown event was translated")
	}
}

func TestSendFails(t *testing.T) {
	s, _ := paired(t)
	blob, _ := s.Export()
	f := &fakeConn{sendErr: errors.New("socket closed")}
	c := clientWith(f)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx, blob, func(whatsapp.Event) {}) }()
	wait(t, "the connection", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.connected })
	if err := c.Send(context.Background(), "1", "x"); err == nil {
		t.Fatal("a failed send said sent")
	}
	cancel()
	<-done
}

func TestQRImageAndNew(t *testing.T) {
	img, err := QRImage("2@" + strings.Repeat("a", 240))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(img, "data:image/png;base64,"))
	if err != nil || string(raw[1:4]) != "PNG" {
		t.Fatalf("not a PNG: %v", err)
	}
	if _, err := QRImage(strings.Repeat("a", 5000)); err == nil {
		t.Fatal("a code too long for a QR was drawn")
	}
	c, ok := New().(*Client)
	if !ok || c.flushEvery == 0 || c.newConn == nil {
		t.Fatal("New is not a client")
	}
	// The real connection is made without touching the network.
	_, dev, _ := Open(nil, nil)
	if realConn(dev) == nil {
		t.Fatal("no real connection")
	}
}
