package whatsapp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/chat"
)

func (f *fakeClient) SendMentions(_ context.Context, to, text string, m Mentions, ref Ref, quoted string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return f.sendErr
	}
	who := strings.Join(m.Numbers, ",")
	if m.All {
		who += "+all"
	}
	line := to + ": @[" + who + "] "
	if ref.ID != "" {
		line += "> " + ref.ID + " " + quoted + " | "
	}
	f.sent = append(f.sent, line+text)
	return nil
}

// etiquetteChats is a chat service that keeps how to write in each chat.
type etiquetteChats struct {
	*fakeChats
	manners map[string]string
}

func (c *etiquetteChats) SetEtiquette(id, text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.manners[id] = text
	return nil
}

func (c *etiquetteChats) manner(id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.manners[id]
}

const familia = "120363@g.us"

// groupRig is a bridge linked to a group where Ana and Pedro wrote.
func groupRig(t *testing.T, s Settings) (*rig, *fakeClient) {
	t.Helper()
	r := newRig(t)
	r.b.Start(context.Background())
	s.Enabled, s.Kitty, s.Groups, s.AllowGroups = true, "mimi", true, []string{familia}
	if err := r.b.Apply(s, true); err != nil {
		t.Fatal(err)
	}
	f := r.next(t)
	f.push(Event{Kind: EvConnected, Phone: "15550001"})
	// Pedro writes without naming the kitty: he is not answered, but known in the group.
	f.push(Event{Kind: EvMessage, Group: true, Chat: familia, From: "56922222222", Name: "Pedro", Text: "hola", Ref: Ref{ID: "p1"}})
	return r, f
}

func (r *rig) ask(t *testing.T, f *fakeClient, id, text string, n int) string {
	t.Helper()
	f.push(Event{Kind: EvMessage, Group: true, Chat: familia, From: "56911111111", Name: "Ana", Text: text, Mentioned: true,
		Ref: Ref{Chat: familia, Sender: "56911111111@s.whatsapp.net", ID: id}})
	eventually(t, "the answer", func() bool { return len(f.messages()) == n })
	return f.messages()[n-1]
}

// A kitty's answer in a group @mentions the members it names (by name or number) so WhatsApp
// notifies them, and everyone only when it writes @all and the limit lets it.
func TestGroupMentions(t *testing.T) {
	r, f := groupRig(t, Settings{MentionAll: MentionAllAsked})
	clock := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	r.b.mu.Lock()
	r.b.opt.Guard = fakeGuard{trusted: "56911111111"} // Ana is the person
	r.b.now = func() time.Time { return clock }
	r.b.mu.Unlock()
	answer := "@Pedro (+56922222222), Ana pregunta por ti. @Luis no está. Llama al @+56933333333."
	r.chats.reply = func(string) ([]chat.Turn, error) { return []chat.Turn{{From: "mimi", Text: answer}}, nil }

	got := r.ask(t, f, "a1", "Mimi, avísale a Pedro", 1)
	// Pedro is named by the message: mentioned. Luis is nobody known, and the number was not in the
	// message: both go as plain names, not as tags.
	want := familia + ": @[56922222222] > a1 Mimi, avísale a Pedro | @56922222222, Ana pregunta por ti. Luis no está. Llama al +56933333333."
	if got != want {
		t.Fatalf("answer =\n%q\nwant\n%q", got, want)
	}

	// @all mentions everyone once; again within the gap it goes as text only.
	answer = "@todos mañana no hay clases"
	// Pedro is not the person: his ask never mentions everyone.
	f.push(Event{Kind: EvMessage, Group: true, Chat: familia, From: "56922222222", Name: "Pedro", Text: "Mimi avisa a todos", Mentioned: true,
		Ref: Ref{Chat: familia, ID: "p2"}})
	eventually(t, "Pedro's answer", func() bool { return len(f.messages()) == 2 })
	if got := f.messages()[1]; got != familia+": > p2 Mimi avisa a todos | @todos mañana no hay clases" {
		t.Fatalf("Pedro's @all = %q", got)
	}
	if !strings.Contains(r.logString(), "only an answer to one of the person's own numbers") {
		t.Fatalf("logs = %s", r.logString())
	}
	if got := r.ask(t, f, "a2", "Mimi avisa a todos", 3); !strings.Contains(got, "@[+all]") {
		t.Fatalf("first @all = %q", got)
	}
	clock = clock.Add(time.Hour)
	if got := r.ask(t, f, "a3", "Mimi avisa a todos otra vez", 4); got != familia+": > a3 Mimi avisa a todos otra vez | @todos mañana no hay clases" {
		t.Fatalf("second @all = %q", got)
	}
	if !strings.Contains(r.logString(), "less than 24 hours ago") {
		t.Fatalf("logs = %s", r.logString())
	}
	clock = clock.Add(24 * time.Hour)
	if got := r.ask(t, f, "a4", "Mimi, avisa a todos de nuevo", 5); !strings.Contains(got, "@[+all]") {
		t.Fatalf("@all after the gap = %q", got)
	}
}

func TestGroupMentionAllOff(t *testing.T) {
	r, f := groupRig(t, Settings{MentionAll: "off"})
	r.chats.reply = func(string) ([]chat.Turn, error) { return []chat.Turn{{From: "mimi", Text: "@all ojo"}}, nil }
	if got := r.ask(t, f, "a1", "Mimi avisa", 1); got != familia+": > a1 Mimi avisa | @all ojo" {
		t.Fatalf("answer = %q", got)
	}
	// The connector says why @all did not mention everyone.
	out, err := connectorOf{r.b}.Call(context.Background(), map[string]any{"to": familia, "text": "@everyone hola @Pedro"})
	if err != nil || !strings.Contains(out, "plain text") || !strings.Contains(out, "off in Settings") {
		t.Fatalf("connector = %q, %v", out, err)
	}
	if got := f.messages()[1]; got != familia+": @[56922222222] @everyone hola @56922222222" {
		t.Fatalf("sent = %q", got)
	}
}

// Two members with the same name are not guessed: the kitty must use the number.
func TestGroupMentionAmbiguousName(t *testing.T) {
	r, f := groupRig(t, Settings{})
	f.push(Event{Kind: EvMessage, Group: true, Chat: familia, From: "56944444444", Name: "Pedro", Text: "yo también"})
	eventually(t, "the second Pedro", func() bool { return len(r.b.Contacts()) == 2 })
	r.chats.reply = func(string) ([]chat.Turn, error) { return []chat.Turn{{From: "mimi", Text: "@Pedro hola"}}, nil }
	if got := r.ask(t, f, "a1", "Mimi saluda a Pedro", 1); got != familia+": > a1 Mimi saluda a Pedro | Pedro hola" {
		t.Fatalf("answer = %q", got)
	}
}

// A 1:1 chat has nobody to mention: the text goes as written.
func TestNoMentionsOutsideGroups(t *testing.T) {
	r := newRig(t)
	f := r.linked(t)
	r.chats.reply = func(string) ([]chat.Turn, error) { return []chat.Turn{{From: "mimi", Text: "@all @+56933333333"}}, nil }
	f.push(Event{Kind: EvMessage, From: "56912345678", Text: "hi"})
	eventually(t, "the answer", func() bool { return len(f.messages()) == 1 })
	if got := f.messages()[0]; got != "56912345678: @all @+56933333333" {
		t.Fatalf("answer = %q", got)
	}
}

// The chat of a group says how to write there: who may be mentioned and when everyone.
func TestGroupEtiquette(t *testing.T) {
	r := newRig(t)
	ec := &etiquetteChats{fakeChats: r.chats, manners: map[string]string{}}
	r.b.opt.Chats = ec
	r.b.Start(context.Background())
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi", Groups: true, AllowGroups: []string{familia}, MentionAll: MentionAllAsked, MentionAllHours: 12}, true); err != nil {
		t.Fatal(err)
	}
	f := r.next(t)
	f.push(Event{Kind: EvConnected, Phone: "15550001"})
	f.push(Event{Kind: EvMessage, Group: true, Chat: familia, From: "56911111111", Name: "Ana", Text: "Mimi hola", Mentioned: true})
	eventually(t, "the answer", func() bool { return len(f.messages()) == 1 })
	m := ec.manner("c1")
	for _, w := range []string{"never @mention them", "unless the message you answer asks you to call", "@all only when the person you work for", "once every 12 hours"} {
		if !strings.Contains(m, w) {
			t.Fatalf("etiquette lacks %q: %s", w, m)
		}
	}
}

func TestMentionSettings(t *testing.T) {
	r := newRig(t)
	if err := r.b.Apply(Settings{Kitty: "mimi", MentionAll: "sometimes"}, true); err == nil {
		t.Fatal("a bad mode was taken")
	}
	if err := r.b.Apply(Settings{Kitty: "mimi", MentionAllHours: MaxMentionAllHours + 1}, true); err == nil {
		t.Fatal("a gap too long was taken")
	}
	if err := r.b.Apply(Settings{Kitty: "mimi", MentionAllHours: MinMentionAllHours - 1}, true); err == nil {
		t.Fatal("a gap too short was taken")
	}
	if err := r.b.Apply(Settings{Kitty: "mimi", MentionAll: "Asked"}, true); err != nil {
		t.Fatal(err)
	}
	if st := r.b.Status(); st.MentionAll != MentionAllAsked {
		t.Fatalf("mode = %q", st.MentionAll)
	}
	// The first version's "needed" reads as off.
	if err := r.b.Apply(Settings{Kitty: "mimi", MentionAll: "needed"}, true); err != nil {
		t.Fatal(err)
	}
	if st := r.b.Status(); st.MentionAll != MentionAllOff {
		t.Fatalf("legacy mode = %q", st.MentionAll)
	}
	if err := r.b.Apply(Settings{Kitty: "mimi"}, true); err != nil {
		t.Fatal(err)
	}
	if st := r.b.Status(); st.MentionAll != MentionAllOff {
		t.Fatalf("default mode = %q", st.MentionAll)
	}
}

// The answer of a long task in a group quotes the message it answers, after newer ones came.
func TestGroupLongTaskQuotes(t *testing.T) {
	r, f := groupRig(t, Settings{})
	r.chats.reply = func(string) ([]chat.Turn, error) {
		return []chat.Turn{{From: "mimi", Text: "Voy.", Working: true}}, nil
	}
	r.ask(t, f, "a1", "Mimi busca vuelos", 1)
	r.chats.emit("c1", chat.Event{Type: "done", Index: 1, Turn: &chat.Turn{Text: "Voy.\n\nListo: hay dos vuelos."}})
	eventually(t, "the final answer", func() bool { return len(f.messages()) == 2 })
	if got := f.messages()[1]; got != familia+": > a1 Mimi busca vuelos | Listo: hay dos vuelos." {
		t.Fatalf("final = %q", got)
	}
}

func TestLabelTagNeedsTheName(t *testing.T) {
	r := newRig(t)
	r.b.mu.Lock()
	defer r.b.mu.Unlock()
	r.b.contacts["56922222222"] = Contact{Number: "56922222222", PushName: "Pedro", Groups: []string{familia}}
	text, m := r.b.mentionsLocked(familia, "@Pedro, llama a (+56922222222)", nil)
	if text != "@56922222222, llama a (+56922222222)" || len(m.Numbers) != 1 {
		t.Fatalf("text = %q, %+v", text, m)
	}
}

func TestMentionPart(t *testing.T) {
	m := Mentions{Numbers: []string{"5691234567", "56912345678"}, All: true}
	got := mentionPart("hola @56912345678 y todos", m, true)
	if len(got.Numbers) != 1 || got.Numbers[0] != "56912345678" || got.All {
		t.Fatalf("part = %+v", got)
	}
}

// An answer tags nobody it is not for: not its author (the quote notifies them), not someone the
// message did not name, and never everyone unless the message asked to tell the group.
func TestAnswerTagsOnlyWhoItIsFor(t *testing.T) {
	r, f := groupRig(t, Settings{})
	f.push(Event{Kind: EvMessage, Group: true, Chat: familia, From: "56933333333", Name: "Luis", Text: "hola"})
	eventually(t, "Luis", func() bool { return len(r.b.Contacts()) == 2 })
	r.chats.reply = func(string) ([]chat.Turn, error) {
		return []chat.Turn{{From: "mimi", Text: "@Ana (+56911111111) claro. @Pedro y @Luis también. @todos ojo."}}, nil
	}
	got := r.ask(t, f, "a1", "Mimi, ¿vamos el sábado?", 1)
	if got != familia+": > a1 Mimi, ¿vamos el sábado? | Ana claro. Pedro y Luis también. @todos ojo." {
		t.Fatalf("answer = %q", got)
	}
	if !strings.Contains(r.logString(), "off in Settings") {
		t.Fatalf("logs = %s", r.logString())
	}
	// The message @mentions Luis: the answer may mention him back, and its quote shows names.
	f.push(Event{Kind: EvMessage, Group: true, Chat: familia, From: "56911111111", Name: "Ana", Text: "Mimi pregúntale a @222 si viene",
		Mentioned: true, Mentions: map[string]string{"222": "56933333333"}, Ref: Ref{Chat: familia, ID: "a2"}})
	eventually(t, "the second answer", func() bool { return len(f.messages()) == 2 })
	if got := f.messages()[1]; got != familia+": @[56933333333] > a2 Mimi pregúntale a Luis si viene | Ana claro. Pedro y @56933333333 también. @todos ojo." {
		t.Fatalf("second = %q", got)
	}
}

// A message a kitty sends on its own (the connector) may mention any member.
func TestConnectorMentionsAnyMember(t *testing.T) {
	r, f := groupRig(t, Settings{})
	if _, err := (connectorOf{r.b}).Call(context.Background(), map[string]any{"to": familia, "text": "@Pedro te espero"}); err != nil {
		t.Fatal(err)
	}
	if got := f.messages()[0]; got != familia+": @[56922222222] @56922222222 te espero" {
		t.Fatalf("sent = %q", got)
	}
}

// A kitty never mentions everyone on its own, even with it allowed in Settings.
func TestConnectorNeverMentionsEveryone(t *testing.T) {
	r, f := groupRig(t, Settings{MentionAll: MentionAllAsked})
	out, err := (connectorOf{r.b}).Call(context.Background(), map[string]any{"to": familia, "text": "@todos hola"})
	if err != nil || !strings.Contains(out, "own numbers") {
		t.Fatalf("connector = %q, %v", out, err)
	}
	if got := f.messages()[0]; got != familia+": @todos hola" {
		t.Fatalf("sent = %q", got)
	}
}

// The answer to a message that quotes the kitty's own is for its author alone: it quotes their
// message (which notifies them) and tags nobody else, not the people the kitty's quoted message
// named, nor everyone, even when the kitty writes their tags.
func TestAnswerToAQuoteOfTheKitty(t *testing.T) {
	r, f := groupRig(t, Settings{MentionAll: MentionAllAsked})
	r.b.mu.Lock()
	r.b.opt.Guard = fakeGuard{trusted: "56911111111"} // Ana is the person
	r.b.mu.Unlock()
	r.chats.reply = func(string) ([]chat.Turn, error) {
		return []chat.Turn{{From: "mimi", Text: "@Ana @Pedro @todos es a las 5"}}, nil
	}
	f.push(Event{Kind: EvMessage, Group: true, Chat: familia, From: "56911111111", Name: "Ana", Text: "¿a qué hora?", Mentioned: true,
		Quote: &Quote{From: "15550001", Text: "Pedro y todos vienen mañana"},
		Ref:   Ref{Chat: familia, Sender: "56911111111@s.whatsapp.net", ID: "a1"}})
	eventually(t, "the answer", func() bool { return len(f.messages()) == 1 })
	if got, want := f.messages()[0], familia+": > a1 ¿a qué hora? | Ana Pedro @todos es a las 5"; got != want {
		t.Fatalf("answer =\n%q\nwant\n%q", got, want)
	}
}

// A progress line of a task in a group quotes nothing and tags nobody the message did not involve.
func TestGroupProgressTagsNobody(t *testing.T) {
	r, f := groupRig(t, Settings{})
	r.chats.mu.Lock()
	r.chats.sessions["c9"] = &chat.Session{ID: "c9", Turns: []chat.Turn{{Steps: []chat.Step{{Action: chat.StepPlan, Result: "Le escribo a @Pedro y a @todos."}}}}}
	r.chats.mu.Unlock()
	in := &inbound{from: "56911111111", to: familia, who: familia, group: true, raw: "Mimi, ¿hora?", ref: Ref{Chat: familia, ID: "a1"}}
	r.b.progress(context.Background(), f, in, "c9", 0, &progressState{seen: map[int]int{}})
	eventually(t, "the line", func() bool { return len(f.messages()) == 1 })
	if got := f.messages()[0]; got != familia+": ⏳ Le escribo a Pedro y a @todos." {
		t.Fatalf("line = %q", got)
	}
}
