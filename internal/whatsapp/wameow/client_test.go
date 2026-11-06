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
	presences  []string
	files      map[string][]byte // by media type
	downErr    error
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

func (f *fakeConn) SendChatPresence(_ context.Context, to types.JID, state types.ChatPresence, _ types.ChatPresenceMedia) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.presences = append(f.presences, to.String()+" "+string(state))
	return nil
}

func (f *fakeConn) Download(_ context.Context, m whatsmeow.DownloadableMessage) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.downErr != nil {
		return nil, f.downErr
	}
	type typed interface{ GetMimetype() string }
	return f.files[m.(typed).GetMimetype()], nil
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

func TestTypingAndGroups(t *testing.T) {
	s, _ := paired(t)
	blob, _ := s.Export()
	f := &fakeConn{}
	c := clientWith(f)
	if err := c.Typing(context.Background(), "1", true); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("typing before the start = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx, blob, func(whatsapp.Event) {}) }()
	wait(t, "the connection", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.connected })
	if err := c.Typing(context.Background(), "56912345678", true); err != nil {
		t.Fatal(err)
	}
	if err := c.Typing(context.Background(), "120363@g.us", false); err != nil {
		t.Fatal(err)
	}
	if err := c.Send(context.Background(), "120363@g.us", "hola grupo"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"a:x@s.whatsapp.net"} {
		if err := c.Send(context.Background(), bad, "x"); err == nil {
			t.Fatalf("sent to %q", bad)
		}
		if err := c.Typing(context.Background(), bad, true); err == nil {
			t.Fatalf("typed to %q", bad)
		}
	}
	cancel()
	<-done
	if got := strings.Join(f.presences, ","); got != "56912345678@s.whatsapp.net composing,120363@g.us paused" {
		t.Fatalf("presences = %s", got)
	}
	if f.sent[0] != "120363@g.us hola grupo" {
		t.Fatalf("sent = %q", f.sent)
	}
}

func TestGroupMessagesAndCaptions(t *testing.T) {
	s, dev := paired(t)
	pn := types.NewJID("56912345678", types.DefaultUserServer)
	group := types.MessageInfo{MessageSource: types.MessageSource{Sender: pn, Chat: types.NewJID("120363", types.GroupServer), IsGroup: true}}
	msg := func(m *waE2E.Message) whatsapp.Event {
		return translate(s, dev, &events.Message{Info: group, Message: m})[0]
	}
	plain := msg(&waE2E.Message{Conversation: proto.String("hola")})
	if plain.Chat != "120363@g.us" || plain.Mentioned {
		t.Fatalf("plain = %+v", plain)
	}
	at := msg(&waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("@kitty hola"),
		ContextInfo: &waE2E.ContextInfo{MentionedJID: []string{"15550001@s.whatsapp.net"}}}})
	if !at.Mentioned {
		t.Fatalf("mention = %+v", at)
	}
	lid := msg(&waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("mira"),
		ContextInfo: &waE2E.ContextInfo{MentionedJID: []string{"999@lid"}}}})
	if !lid.Mentioned || lid.Text != "mira" {
		t.Fatalf("lid mention = %+v", lid)
	}
	reply := msg(&waE2E.Message{AudioMessage: &waE2E.AudioMessage{ContextInfo: &waE2E.ContextInfo{Participant: proto.String("15550001:7@s.whatsapp.net")}}})
	if !reply.Mentioned {
		t.Fatalf("reply = %+v", reply)
	}
	other := msg(&waE2E.Message{VideoMessage: &waE2E.VideoMessage{Caption: proto.String("video"),
		ContextInfo: &waE2E.ContextInfo{MentionedJID: []string{"1@s.whatsapp.net", "bad:jid:x@"}}}})
	if other.Mentioned || other.Text != "video" {
		t.Fatalf("other = %+v", other)
	}
	doc := msg(&waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{Caption: proto.String("el informe")}})
	if doc.Text != "el informe" || doc.Mentioned {
		t.Fatalf("doc = %+v", doc)
	}
}

func TestMedia(t *testing.T) {
	f := &fakeConn{files: map[string][]byte{
		"image/png": []byte("png"), "video/mp4": []byte("mp4"), "application/pdf": []byte("pdf"), "audio/ogg; codecs=opus": []byte("ogg"),
	}}
	m := &waE2E.Message{
		ImageMessage:    &waE2E.ImageMessage{Mimetype: proto.String("image/png")},
		VideoMessage:    &waE2E.VideoMessage{Mimetype: proto.String("video/mp4")},
		DocumentMessage: &waE2E.DocumentMessage{Mimetype: proto.String("application/pdf"), FileName: proto.String("informe.pdf")},
		AudioMessage:    &waE2E.AudioMessage{Mimetype: proto.String("audio/ogg; codecs=opus"), PTT: proto.Bool(true)},
	}
	files := media(context.Background(), f, m)
	var got []string
	for _, x := range files {
		got = append(got, x.Name+"="+string(x.Data))
	}
	if strings.Join(got, ",") != "picture.png=png,video.mp4=mp4,informe.pdf=pdf,audio.ogg=ogg" || !files[3].Voice || files[0].Voice {
		t.Fatalf("files = %v", got)
	}
	// Too big, empty, or failed downloads are left out.
	big := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/png"), FileLength: proto.Uint64(maxMedia + 1)}}
	if len(media(context.Background(), f, big)) != 0 {
		t.Fatal("a file too big was taken")
	}
	if len(media(context.Background(), f, &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{Mimetype: proto.String("text/plain")}})) != 0 {
		t.Fatal("an empty file was taken")
	}
	f.downErr = errors.New("gone")
	if len(media(context.Background(), f, m)) != 0 {
		t.Fatal("a failed download was taken")
	}
	if len(media(context.Background(), f, &waE2E.Message{Conversation: proto.String("hola")})) != 0 {
		t.Fatal("text has files")
	}
}

func TestExt(t *testing.T) {
	for mime, want := range map[string]string{
		"image/png": ".png", "image/webp": ".webp", "image/jpeg": ".jpg", "audio/mpeg": ".mp3", "audio/mp4": ".m4a",
		"audio/ogg; codecs=opus": ".ogg", "video/mp4": ".mp4", "x/unknown": ".bin",
	} {
		if got := ext(mime, ".bin"); got != want {
			t.Fatalf("ext(%q) = %q", mime, got)
		}
	}
}

func TestMediaThroughRun(t *testing.T) {
	s, _ := paired(t)
	blob, _ := s.Export()
	f := &fakeConn{files: map[string][]byte{"image/jpeg": []byte("jpg")}}
	c := clientWith(f)
	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx, blob, rec.emit) }()
	wait(t, "the handler", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.handler != nil && f.connected })
	f.fire(&events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Sender: types.NewJID("56912345678", types.DefaultUserServer)}},
		Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg"), Caption: proto.String("mira")}}})
	cancel()
	<-done
	ev, ok := rec.last(whatsapp.EvMessage)
	if !ok || ev.Text != "mira" || len(ev.Files) != 1 || ev.Files[0].Name != "picture.jpg" {
		t.Fatalf("message = %+v", ev)
	}
}
