package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/kitty"
)

func TestRiskIsAcceptedOnce(t *testing.T) {
	dir := t.TempDir()
	r := newRigAt(t, dir)
	r.b.Start(context.Background())
	on := Settings{Enabled: true, Kitty: "mimi"}
	if err := r.b.Apply(on, false); !errors.Is(err, ErrRisk) {
		t.Fatalf("on without the risk = %v", err)
	}
	if st := r.b.Status(); st.Enabled || st.RiskAccepted {
		t.Fatalf("status = %+v", st)
	}
	if err := r.b.Apply(on, true); err != nil {
		t.Fatal(err)
	}
	r.next(t)
	if !r.b.Status().RiskAccepted {
		t.Fatal("the risk is not kept")
	}
	// Off and on again: it is not asked again, not even after a restart.
	if err := r.b.Apply(Settings{Kitty: "mimi"}, false); err != nil {
		t.Fatal(err)
	}
	if err := r.b.Apply(on, false); err != nil {
		t.Fatalf("on again = %v", err)
	}
	r.next(t)
	r.b.Stop()
	again := newRigAt(t, dir)
	if st := again.b.Status(); !st.RiskAccepted {
		t.Fatalf("after a restart = %+v", st)
	}
}

func TestRiskFromTheFile(t *testing.T) {
	b, err := New(Options{Dir: t.TempDir(), Box: box{}, NewClient: func() Client { return newFake() }, Defaults: &Settings{Enabled: true, Voice: "Always"}})
	if err != nil {
		t.Fatal(err)
	}
	if st := b.Status(); !st.RiskAccepted || st.Voice != VoiceAlways {
		t.Fatalf("turned on in the file = %+v", st)
	}
	if _, err := New(Options{Dir: t.TempDir(), Defaults: &Settings{Voice: "loud"}}); err == nil {
		t.Fatal("a bad voice mode in the file was taken")
	}
}

func TestHeldLog(t *testing.T) {
	r := newRig(t)
	r.linked(t)
	r.b.mu.Lock()
	r.b.seen = []Group{{ID: "120363@g.us", Name: "Familia"}}
	r.b.mu.Unlock()
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi", Allow: []string{"+56912345678"}, AllowGroups: []string{"120363@g.us"}, MaxPerHour: 1}, false); err != nil {
		t.Fatal(err)
	}
	c := connectorOf{r.b}
	ctx := context.Background()
	if _, err := c.Call(ctx, map[string]any{"to": "+56912345678", "text": "first"}); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("palabra ", 40)
	if _, err := c.Call(ctx, map[string]any{"to": "+56912345678", "text": long}); !errors.Is(err, ErrLimit) {
		t.Fatalf("over the limit = %v", err)
	}
	if _, err := c.Call(ctx, map[string]any{"to": "120363@g.us", "text": "hola grupo"}); !errors.Is(err, ErrLimit) {
		t.Fatalf("over the limit = %v", err)
	}
	st := r.b.Status()
	if st.Held != 2 || len(st.HeldLog) != 2 {
		t.Fatalf("status = %+v", st)
	}
	g, n := st.HeldLog[0], st.HeldLog[1]
	if g.To != "120363@g.us" || g.Name != "Familia" || g.Kind != "text" || g.Text != "hola grupo" || g.At.IsZero() {
		t.Fatalf("newest = %+v", g)
	}
	if n.To != "56912345678" || n.Name != "" || len([]rune(n.Text)) != maxHeldText || !strings.HasSuffix(n.Text, "…") {
		t.Fatalf("oldest = %+v", n)
	}
	for i := 0; i < maxHeldLog+5; i++ {
		_, _ = c.Call(ctx, map[string]any{"to": "+56912345678", "text": "x"})
	}
	if st := r.b.Status(); len(st.HeldLog) != maxHeldLog || st.Held != maxHeldLog+7 {
		t.Fatalf("held %d, log %d", st.Held, len(st.HeldLog))
	}
	r.b.ClearHeld()
	if st := r.b.Status(); st.Held != 0 || len(st.HeldLog) != 0 || st.HeldLog == nil {
		t.Fatalf("after clearing = %+v", st)
	}
}

func TestClip(t *testing.T) {
	if clip("hola", 10) != "hola" || clip("ñandú grande", 6) != "ñandú…" {
		t.Fatalf("clip = %q", clip("ñandú grande", 6))
	}
}

// speech is a fake speech service that counts what it reads aloud.
type speech struct {
	mu     sync.Mutex
	said   []string
	voices []string
	fail   bool
}

func (s *speech) speak(_ context.Context, text, voice string) (File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return File{}, errors.New("no credit")
	}
	s.said = append(s.said, text)
	s.voices = append(s.voices, voice)
	return File{Name: "voice.ogg", MIME: VoiceMIME, Data: []byte("OggS")}, nil
}

func (s *speech) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.said)
}

func TestVoiceReplies(t *testing.T) {
	r := newRig(t)
	sp := &speech{}
	r.b.opt.Speak = sp.speak
	r.b.opt.Transcribe = func(context.Context, File) (string, error) { return "qué tal", nil }
	f := r.linked(t)
	if !r.b.Status().CanSpeak {
		t.Fatal("it cannot speak")
	}
	set := Settings{Enabled: true, Kitty: "mimi", Allow: []string{"+56912345678"}, Voice: " Mirror "}
	if err := r.b.Apply(set, false); err != nil {
		t.Fatal(err)
	}
	if v := r.b.Status().Voice; v != VoiceMirror {
		t.Fatalf("voice = %q", v)
	}
	note := []File{{Name: "audio.ogg", MIME: "audio/ogg", Data: []byte("v"), Voice: true}}
	// Mirror: a text gets text, a voice note gets one.
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "hola"})
	eventually(t, "the text reply", func() bool { return len(f.messages()) == 1 })
	f.push(Event{Kind: EvMessage, From: "56912345678", Files: note})
	eventually(t, "the voice reply", func() bool { return len(f.messages()) == 2 })
	if m := f.messages()[1]; m != "56912345678: [voice.ogg|"+VoiceMIME+"|OggS] " || sp.said[0] != "hi you said (voice note) qué tal" {
		t.Fatalf("voice reply = %q, said %q", m, sp.said)
	}
	// An answer with code goes as text; so does one when the speech service fails.
	r.chats.reply = func(string) ([]chat.Turn, error) { return []chat.Turn{{From: "mimi", Text: "```go\nx := 1\n```"}}, nil }
	f.push(Event{Kind: EvMessage, From: "56912345678", Files: note})
	eventually(t, "the code reply", func() bool { return len(f.messages()) == 3 })
	sp.fail = true
	r.chats.reply = func(string) ([]chat.Turn, error) { return []chat.Turn{{From: "mimi", Text: "vale"}}, nil }
	f.push(Event{Kind: EvMessage, From: "56912345678", Files: note})
	eventually(t, "the fallback reply", func() bool { return len(f.messages()) == 4 })
	if m := f.messages(); !strings.HasSuffix(m[2], "x := 1\n```") || m[3] != "56912345678: vale" || sp.count() != 1 {
		t.Fatalf("messages = %q", m)
	}
	if !strings.Contains(r.logString(), "voice note to 56912345678: no credit") {
		t.Fatalf("log = %s", r.logString())
	}
	// Always: a text gets a voice note too; an emoji is still a reaction.
	sp.fail = false
	set.Voice = VoiceAlways
	if err := r.b.Apply(set, false); err != nil {
		t.Fatal(err)
	}
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "dime algo"})
	eventually(t, "the spoken reply", func() bool { return sp.count() == 2 && len(f.messages()) == 5 })
	r.chats.reply = func(string) ([]chat.Turn, error) { return []chat.Turn{{From: "mimi", Text: "👍"}}, nil }
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "ok", Ref: Ref{ID: "m9"}})
	eventually(t, "the reaction", func() bool { _, reacts := f.snapshot(); return len(reacts) == 1 })
	// At the limit a voice note is held without reading it aloud.
	set.MaxPerHour = 1
	if err := r.b.Apply(set, false); err != nil {
		t.Fatal(err)
	}
	r.chats.reply = func(string) ([]chat.Turn, error) { return []chat.Turn{{From: "mimi", Text: "otra"}}, nil }
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "otra"})
	eventually(t, "the held voice note", func() bool { return len(r.b.Status().HeldLog) == 1 })
	if h := r.b.Status().HeldLog[0]; h.Kind != "voice" || h.Text != "otra" || sp.count() != 2 {
		t.Fatalf("held = %+v, said %d", h, sp.count())
	}
	if err := r.b.Apply(Settings{Kitty: "mimi", Voice: "loud"}, false); err == nil {
		t.Fatal("a bad voice mode was taken")
	}
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi", Voice: "OFF"}, false); err != nil || r.b.Status().Voice != VoiceOff {
		t.Fatalf("off = %v", err)
	}
}

func TestFollowedReplyIsSpoken(t *testing.T) {
	r := newRig(t)
	sp := &speech{}
	r.b.opt.Speak = sp.speak
	r.b.wait = time.Second
	f := r.linked(t)
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi", Allow: []string{"+56912345678"}, Voice: VoiceAlways}, false); err != nil {
		t.Fatal(err)
	}
	r.chats.reply = func(string) ([]chat.Turn, error) {
		return []chat.Turn{{From: "mimi", Text: "Voy al escritorio.", Working: true}}, nil
	}
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "abre el navegador"})
	eventually(t, "the working note", func() bool { return len(f.messages()) == 1 })
	if m := f.messages()[0]; m != "56912345678: Voy al escritorio." {
		t.Fatalf("working note = %q", m)
	}
	r.chats.emit("c1", chat.Event{Type: "done", Index: 1, Turn: &chat.Turn{Text: "Voy al escritorio. Listo, está abierto."}})
	eventually(t, "the spoken follow-up", func() bool { return sp.count() == 1 && len(f.messages()) == 2 })
	if sp.said[0] != "Listo, está abierto." {
		t.Fatalf("said = %q", sp.said)
	}
}

func TestVoiceAction(t *testing.T) {
	r := newRig(t)
	f := r.linked(t)
	c := connectorOf{r.b}
	ctx := context.Background()
	if strings.Contains(c.Describe(), `"voice"`) {
		t.Fatal("the voice action is offered without a speech service")
	}
	if _, err := c.Call(ctx, map[string]any{"action": "voice", "text": "hola"}); !errors.Is(err, ErrNoSpeech) {
		t.Fatalf("without speech = %v", err)
	}
	sp := &speech{}
	r.b.opt.Speak = sp.speak
	if !strings.Contains(c.Describe(), `"voice"`) {
		t.Fatal("the voice action is not offered")
	}
	if out, err := c.Call(ctx, map[string]any{"action": "voice", "to": "+56912345678", "text": "**Hola** _mad_"}); err != nil || out != "sent as a voice note" {
		t.Fatalf("voice = %q %v", out, err)
	}
	if sp.said[0] != "Hola mad" || len(f.messages()) != 1 {
		t.Fatalf("said %q, sent %q", sp.said, f.messages())
	}
	if _, err := c.Call(ctx, map[string]any{"action": "voice", "text": "  "}); err == nil {
		t.Fatal("an empty voice note was sent")
	}
	if _, err := c.Call(ctx, map[string]any{"action": "voice", "to": "+1 555 000 9999", "text": "hola"}); !errors.Is(err, ErrNotAllow) {
		t.Fatalf("to a stranger = %v", err)
	}
	if _, err := c.Call(ctx, map[string]any{"action": "voice", "text": strings.Repeat("a ", maxSpoken)}); !errors.Is(err, errNotSpoken) {
		t.Fatalf("too long = %v", err)
	}
	if _, err := c.Call(ctx, map[string]any{"action": "dance"}); err == nil || !strings.Contains(err.Error(), "voice") {
		t.Fatalf("unknown = %v", err)
	}
}

func TestSpeaker(t *testing.T) {
	var got map[string]string
	answer, status := "OggS\x00rest", http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/speech" || r.Header.Get("Authorization") != "Bearer k" {
			http.Error(w, "wrong", http.StatusNotFound)
			return
		}
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &got)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	defer srv.Close()
	sp := Speaker{BaseURL: srv.URL + "/v1/", Key: "k"}
	f, err := sp.Speak(context.Background(), "hola", "")
	if err != nil {
		t.Fatal(err)
	}
	if !f.Voice || f.MIME != VoiceMIME || string(f.Data) != answer || got["model"] != SpeakModel || got["voice"] != DefaultVoice || got["response_format"] != "opus" || got["input"] != "hola" {
		t.Fatalf("file = %+v, asked %v", f, got)
	}
	sp.Model, sp.Voice = "tts-1", "nova"
	if _, err := sp.Speak(context.Background(), "x", ""); err != nil || got["model"] != "tts-1" || got["voice"] != "nova" {
		t.Fatalf("named = %v %v", err, got)
	}
	if _, err := sp.Speak(context.Background(), "x", "onyx"); err != nil || got["voice"] != "onyx" {
		t.Fatalf("the kitty's voice = %v %v", err, got)
	}
	answer = "RIFFwav"
	if _, err := sp.Speak(context.Background(), "x", ""); err == nil || !strings.Contains(err.Error(), "Ogg") {
		t.Fatalf("not ogg = %v", err)
	}
	answer, status = strings.Repeat("e", 400), http.StatusTooManyRequests
	if _, err := sp.Speak(context.Background(), "x", ""); err == nil || !strings.Contains(err.Error(), "429") || len(err.Error()) > 400 {
		t.Fatalf("error = %v", err)
	}
	if _, err := (Speaker{}).Speak(context.Background(), "x", ""); err == nil {
		t.Fatal("spoke with no service")
	}
	if _, err := (Speaker{BaseURL: "http://127.0.0.1:1", Key: "k"}).Speak(context.Background(), "x", ""); err == nil {
		t.Fatal("spoke with no server")
	}
	if _, err := (Speaker{BaseURL: "::bad", Key: "k"}).Speak(context.Background(), "x", ""); err == nil {
		t.Fatal("spoke to a bad address")
	}
}

func TestOggSecondsAndSpoken(t *testing.T) {
	page := func(g uint64) []byte {
		b := []byte("OggS\x00\x04")
		for i := 0; i < 8; i++ {
			b = append(b, byte(g>>(8*i)))
		}
		return b
	}
	cases := map[string]uint32{"none": 0, "short": 0, "2s": 2, "unknown": 0, "huge": 0}
	data := map[string][]byte{"none": []byte("RIFF"), "short": []byte("OggS\x00"), "2s": append(page(1000), page(96000)...),
		"unknown": page(^uint64(0)), "huge": page(1 << 60)}
	for name, want := range cases {
		if got := OggSeconds(data[name]); got != want {
			t.Errorf("%s = %d", name, got)
		}
	}
	for text, want := range map[string]string{
		"**Hola**, [mira](http://x)": "Hola, mira (http://x)", "👍": "", "  ": "", "```x```": "",
		strings.Repeat("a", maxSpoken+1): "",
	} {
		if got := spoken(text); got != want {
			t.Errorf("spoken(%.20q) = %q", text, got)
		}
	}
}

func TestEachKittyHasItsVoice(t *testing.T) {
	old := typingEvery
	typingEvery = 5 * time.Millisecond
	t.Cleanup(func() { typingEvery = old })
	r := newRig(t)
	sp := &speech{}
	r.b.opt.Speak = sp.speak
	f := r.linked(t)
	set := Settings{Enabled: true, Kitty: "mimi", Allow: []string{"+56912345678", "+15550001111"},
		Kitties: map[string]string{"+15550001111": "luna"}, Voice: VoiceAlways}
	if err := r.b.Apply(set, false); err != nil {
		t.Fatal(err)
	}
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "hola"})
	eventually(t, "mimi's voice note", func() bool { return sp.count() == 1 })
	f.push(Event{Kind: EvMessage, From: "15550001111", Text: "hola"})
	eventually(t, "luna's voice note", func() bool { return sp.count() == 2 })
	sp.mu.Lock()
	voices := append([]string(nil), sp.voices...)
	sp.mu.Unlock()
	if voices[0] != "" || voices[1] != "nova" {
		t.Fatalf("voices = %q", voices)
	}
	// An answer that goes as a voice note shows "recording audio…".
	typed := strings.Join(f.typings(), ",")
	if !strings.Contains(typed, "56912345678:true:audio") || !strings.Contains(typed, "15550001111:false:audio") {
		t.Fatalf("presence = %s", typed)
	}
	// "default" is the gateway's voice, and so is a kitty that is gone.
	r.b.opt.Kitty = func(string) (*kitty.Kitty, error) {
		k := &kitty.Kitty{ID: "k1", Name: "mimi"}
		k.Soul.Voice = " Default "
		return k, nil
	}
	if v := r.b.voiceOf("56912345678"); v != "" {
		t.Fatalf("default = %q", v)
	}
	r.b.opt.Kitty = func(string) (*kitty.Kitty, error) { return nil, errors.New("gone") }
	if v := r.b.voiceOf("56912345678"); v != "" {
		t.Fatalf("no kitty = %q", v)
	}
}

func TestResendHeld(t *testing.T) {
	r := newRig(t)
	sp := &speech{}
	r.b.opt.Speak = sp.speak
	r.b.opt.File = func(name string) (File, error) {
		return File{Name: name, MIME: "application/pdf", Data: []byte("pdf")}, nil
	}
	f := r.linked(t)
	set := Settings{Enabled: true, Kitty: "mimi", Allow: []string{"+56912345678"}, MaxPerHour: 1}
	if err := r.b.Apply(set, false); err != nil {
		t.Fatal(err)
	}
	c := connectorOf{r.b}
	ctx := context.Background()
	if _, err := c.Call(ctx, map[string]any{"text": "first"}); err != nil {
		t.Fatal(err)
	}
	// A text, a voice note, a file and a reaction are held.
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "hola", Ref: Ref{Chat: "c", ID: "m1"}})
	eventually(t, "the held reply", func() bool { return len(r.b.Status().HeldLog) == 1 })
	for _, call := range []map[string]any{
		{"action": "voice", "text": "nos vemos"},
		{"file": "informe.pdf"},
		{"action": "react", "emoji": "👍"},
	} {
		if _, err := c.Call(ctx, call); !errors.Is(err, ErrLimit) {
			t.Fatalf("%v = %v", call, err)
		}
	}
	log := r.b.Status().HeldLog
	if len(log) != 4 || log[0].Kind != "reaction" || log[1].Kind != "file" || log[1].Text != "informe.pdf" || log[2].Kind != "voice" || log[3].Kind != "text" || log[0].ID == log[1].ID {
		t.Fatalf("log = %+v", log)
	}
	if sp.count() != 0 {
		t.Fatal("a held voice note was read aloud")
	}
	// While the limit is full nothing goes, and an unknown id is said so.
	if err := r.b.Resend(ctx, log[3].ID); !errors.Is(err, ErrLimit) {
		t.Fatalf("resend while full = %v", err)
	}
	if err := r.b.Resend(ctx, "nope"); !errors.Is(err, ErrNoHeld) {
		t.Fatalf("unknown = %v", err)
	}
	// With room, each goes again and leaves the list.
	room := func() {
		r.b.mu.Lock()
		r.b.sent = nil
		r.b.mu.Unlock()
	}
	for _, h := range log {
		room()
		if err := r.b.Resend(ctx, h.ID); err != nil {
			t.Fatalf("resend %s = %v", h.Kind, err)
		}
	}
	st := r.b.Status()
	if len(st.HeldLog) != 0 || st.Held != 0 {
		t.Fatalf("after = %+v", st)
	}
	_, reacts := f.snapshot()
	msgs := f.messages()
	if len(reacts) != 1 || reacts[0] != "c/m1 👍" || sp.count() != 1 || len(msgs) != 4 ||
		msgs[1] != "56912345678: [informe.pdf|application/pdf|pdf] " || msgs[3] != "56912345678: *hi* you said hola" {
		t.Fatalf("reacts %q, said %d, sent %q", reacts, sp.count(), msgs)
	}
	// A number taken off the list is not written to again.
	room()
	if _, err := c.Call(ctx, map[string]any{"text": "uno"}); err != nil {
		t.Fatal(err)
	}
	_, _ = c.Call(ctx, map[string]any{"text": "dos"})
	id := r.b.Status().HeldLog[0].ID
	set.Allow = []string{"+15550001111"}
	if err := r.b.Apply(set, false); err != nil {
		t.Fatal(err)
	}
	room()
	if err := r.b.Resend(ctx, id); !errors.Is(err, ErrNotAllow) {
		t.Fatalf("to a number off the list = %v", err)
	}
	if len(r.b.Status().HeldLog) != 1 {
		t.Fatal("a message that was not sent left the list")
	}
}

func TestHeldSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	r := newRigAt(t, dir)
	r.linked(t)
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi", Allow: []string{"+56912345678"}, MaxPerHour: 1}, false); err != nil {
		t.Fatal(err)
	}
	c := connectorOf{r.b}
	_, _ = c.Call(context.Background(), map[string]any{"text": "uno"})
	_, _ = c.Call(context.Background(), map[string]any{"text": "secreto"})
	r.b.Stop()
	data, err := os.ReadFile(filepath.Join(dir, "held.bin"))
	if err != nil || strings.Contains(string(data), "secreto") {
		t.Fatalf("held.bin = %q %v", data, err)
	}
	again := newRigAt(t, dir)
	st := again.b.Status()
	if st.Held != 1 || len(st.HeldLog) != 1 || st.HeldLog[0].Text != "secreto" {
		t.Fatalf("after a restart = %+v", st)
	}
	// Cleared, nothing is left on disk.
	again.b.ClearHeld()
	if _, err := os.Stat(filepath.Join(dir, "held.bin")); !os.IsNotExist(err) {
		t.Fatalf("held.bin after clearing: %v", err)
	}
	again.b.ClearHeld()
	// A list that does not open is dropped and said so.
	if err := os.WriteFile(filepath.Join(dir, "held.bin"), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	third := newRigAt(t, dir)
	if len(third.b.Status().HeldLog) != 0 || !strings.Contains(third.logString(), "held messages could not be read") {
		t.Fatalf("damaged = %s", third.logString())
	}
	if _, err := os.Stat(filepath.Join(dir, "held.bin")); !os.IsNotExist(err) {
		t.Fatal("the damaged list is still there")
	}
}

func TestHeldThatCannotBeKept(t *testing.T) {
	r := newRig(t)
	r.b.opt.Box = box{fail: true}
	r.b.mu.Lock()
	r.b.set.MaxPerHour = 1
	r.b.sent = []time.Time{r.b.now()}
	r.b.mu.Unlock()
	if err := r.b.take("56912345678", "text", "x", Ref{}); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	if !strings.Contains(r.logString(), "keep the held messages: sealed shut") {
		t.Fatalf("log = %s", r.logString())
	}
	// Without a vault nothing is written or read.
	r.b.opt.Box = nil
	r.b.saveHeldLocked()
	r.b.loadHeld()
	if _, err := os.Stat(filepath.Join(r.dir, "held.bin")); !os.IsNotExist(err) {
		t.Fatal("kept without a vault")
	}
	// A list that cannot be removed is said so.
	if err := os.MkdirAll(filepath.Join(r.dir, "held.bin", "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	r.b.opt.Box = box{}
	r.b.ClearHeld()
	if !strings.Contains(r.logString(), "forget the held messages") {
		t.Fatalf("log = %s", r.logString())
	}
}
