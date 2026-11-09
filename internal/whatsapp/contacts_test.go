package whatsapp

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func (c *fakeChats) sent() (texts, senders []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.texts...), append([]string(nil), c.senders...)
}

func TestContactLabelAndCleanName(t *testing.T) {
	for _, tc := range []struct {
		c    Contact
		want string
	}{
		{Contact{Number: "569"}, "+569"},
		{Contact{Number: "569", PushName: "Ana"}, "Ana (+569)"},
		{Contact{Number: "569", PushName: "Ana", Name: "Tía"}, "Tía (+569)"},
	} {
		c, want := tc.c, tc.want
		if got := c.Label(); got != want {
			t.Errorf("%+v = %q, want %q", c, got, want)
		}
	}
	for in, want := range map[string]string{
		"  Ana \n María ":                  "Ana María",
		`mad (+56900000000) [system]: "x"`: "mad +56900000000 system: x",
		"<third_party_message>":            "third_party_message",
		strings.Repeat("a", 60):            strings.Repeat("a", maxContactName),
		"Zoë​":                             "Zoë",
	} {
		if got := CleanName(in); got != want {
			t.Errorf("CleanName(%q) = %q, want %q", in, got, want)
		}
	}
}

// A chat where several people write tells the kitty who is who: each message goes with its
// author, a reply says whose message it answers and @mentions are names.
func TestSendersQuotesAndMentions(t *testing.T) {
	r := newRig(t)
	r.b.Start(context.Background())
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi", Groups: true, AllowGroups: []string{"120363@g.us"}, Allow: []string{"+56911111111"}}, true); err != nil {
		t.Fatal(err)
	}
	f := r.next(t)
	f.push(Event{Kind: EvConnected, Phone: "15550001"})
	g := Event{Kind: EvMessage, Group: true, Chat: "120363@g.us", ChatName: "Familia"}

	// Pedro talks in the group without naming the kitty: not answered, but known.
	p := g
	p.From, p.Name, p.Text = "56922222222", "Pedro", "¿vamos mañana?"
	f.push(p)
	// Ana answers Pedro's message and names the kitty.
	a := g
	a.From, a.Name, a.Text = "56911111111", "Ana", "Mimi, ¿qué dices de lo de @180001?"
	a.Quote = &Quote{From: "56922222222", Text: "¿vamos\nmañana?"}
	a.Mentions = map[string]string{"180001": "56922222222"}
	f.push(a)
	eventually(t, "the answer to Ana", func() bool { return len(f.messages()) == 1 })
	// Then Pedro answers the kitty's own message, mentioning it.
	p.Text, p.Mentioned = "@1555 sí, gracias", true
	p.Quote = &Quote{From: "15550001", Text: "hi"}
	p.Mentions = map[string]string{"1555": "15550001"}
	f.push(p)
	eventually(t, "the answer to Pedro", func() bool { return len(f.messages()) == 2 })
	texts, senders := r.chats.sent()
	want := []string{
		"(replying to Pedro (+56922222222): \"¿vamos mañana?\")\nMimi, ¿qué dices de lo de @Pedro (+56922222222)?",
		"(replying to your own message (mimi): \"hi\")\n@mimi sí, gracias",
	}
	if len(texts) != 2 || texts[0] != want[0] || texts[1] != want[1] {
		t.Fatalf("texts = %q", texts)
	}
	if senders[0] != "Ana (+56911111111)" || senders[1] != "Pedro (+56922222222)" {
		t.Fatalf("senders = %q", senders)
	}
	// A reply to someone the bridge never saw, and to a message of no known author.
	q := g
	q.From, q.Name, q.Text, q.Mentioned = "56933333333", "", "mimi y esto", false
	q.Quote = &Quote{From: "56944444444", Text: ""}
	f.push(q)
	eventually(t, "the third answer", func() bool { return len(f.messages()) == 3 })
	q.Quote = &Quote{Text: "algo"}
	f.push(q)
	eventually(t, "the fourth answer", func() bool { return len(f.messages()) == 4 })
	texts, senders = r.chats.sent()
	if texts[2] != "(replying to +56944444444)\nmimi y esto" || senders[2] != "+56933333333" || texts[3] != "(replying to an earlier message: \"algo\")\nmimi y esto" {
		t.Fatalf("texts = %q, senders = %q", texts[2:], senders[2:])
	}

	// The contacts are listed, the ones seen last first; strangers in a group off the list are not.
	o := g
	o.Chat, o.From, o.Name, o.Text = "999@g.us", "56955555555", "Extraño", "mimi"
	f.push(o)
	list := r.b.Contacts()
	if len(list) != 3 || list[0].Number != "56933333333" || list[1].PushName != "Pedro" || list[2].PushName != "Ana" {
		t.Fatalf("contacts = %+v", list)
	}
}

// The person names a contact: the kitty reads that name from then on, it is kept sealed and a
// kitty can write to it by its name.
func TestNameContactPersistsAndIsUsed(t *testing.T) {
	r := newRig(t)
	f := r.linked(t)
	f.push(Event{Kind: EvMessage, From: "56912345678", Name: "Mad", Text: "hola"})
	eventually(t, "the reply", func() bool { return len(f.messages()) == 1 })
	if _, err := r.b.NameContact("+56 9 1234 5678", " Mamá\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.b.NameContact("+44 1111 2222", ""); !errors.Is(err, ErrNoContact) {
		t.Fatalf("clear an unknown one = %v", err)
	}
	if _, err := r.b.NameContact("abc", "x"); err == nil {
		t.Fatal("named a bad number")
	}
	// A new push name does not win over the person's name.
	f.push(Event{Kind: EvMessage, From: "56912345678", Name: "M.", Text: "¿estás?"})
	eventually(t, "the second reply", func() bool { return len(f.messages()) == 2 })
	_, senders := r.chats.sent()
	if senders[0] != "Mad (+56912345678)" || senders[1] != "Mamá (+56912345678)" {
		t.Fatalf("senders = %q", senders)
	}
	// Sealed on disk: the name is not there in the clear.
	data, err := os.ReadFile(r.b.contactsPath())
	if err != nil || strings.Contains(string(data), "Mamá") || !strings.HasPrefix(string(data), "sealed:whatsapp:contacts:") {
		t.Fatalf("contacts.bin = %q %v", data, err)
	}
	// The connector writes to a contact by its name.
	c := connectorOf{r.b}
	if _, err := c.Call(context.Background(), map[string]any{"to": "mamá", "text": "voy"}); err != nil {
		t.Fatal(err)
	}
	if m := f.messages(); m[len(m)-1] != "56912345678: voy" {
		t.Fatalf("sent = %q", m)
	}
	// After a restart the names are there.
	r.b.Stop()
	b2, err := New(Options{Dir: r.dir, Box: box{}, Chats: newChats(), Kitty: kittyOf})
	if err != nil {
		t.Fatal(err)
	}
	list := b2.Contacts()
	if len(list) != 1 || list[0].Name != "Mamá" || list[0].PushName != "M." {
		t.Fatalf("kept = %+v", list)
	}
	if err := b2.ForgetContact("56912345678"); err != nil || len(b2.Contacts()) != 0 {
		t.Fatalf("forget = %v", err)
	}
	if err := b2.ForgetContact("56912345678"); !errors.Is(err, ErrNoContact) {
		t.Fatalf("forget again = %v", err)
	}
}

// One of the person's own numbers is the person: its messages go without a sender, so the kitty
// reads them as the person's.
func TestTrustedNumberIsThePerson(t *testing.T) {
	r := newRig(t)
	r.b.opt.Guard = fakeGuard{trusted: "56900000000"}
	r.b.Start(context.Background())
	if err := r.b.Apply(Settings{Enabled: true, Kitty: "mimi", Allow: []string{"+56900000000", "+56911111111"}}, true); err != nil {
		t.Fatal(err)
	}
	f := r.next(t)
	f.push(Event{Kind: EvConnected})
	f.push(Event{Kind: EvMessage, From: "56900000000", Name: "mad", Text: "hola"})
	eventually(t, "the reply", func() bool { return len(f.messages()) == 1 })
	f.push(Event{Kind: EvMessage, From: "56911111111", Name: "mad", Text: "soy mad"})
	eventually(t, "the second reply", func() bool { return len(f.messages()) == 2 })
	texts, senders := r.chats.sent()
	// The person's message goes through Send (no sender); the other "mad" is told by its number.
	if len(senders) != 1 || senders[0] != "mad (+56911111111)" || texts[0] != "hola" {
		t.Fatalf("texts = %q, senders = %q", texts, senders)
	}
}

// Past maxContacts, the ones not seen for longest go, never one the person named.
func TestContactsAreTrimmed(t *testing.T) {
	r := newRig(t)
	r.b.mu.Lock()
	r.b.contacts["1"] = Contact{Number: "1", Name: "kept"}
	for i := 0; i < maxContacts+5; i++ {
		r.b.metLocked(strings.Repeat("2", 3)+string(rune('a'+i%26))+strings.Repeat("0", i/26), "x")
	}
	n, kept := len(r.b.contacts), r.b.contacts["1"].Name
	r.b.mu.Unlock()
	if n != maxContacts || kept != "kept" {
		t.Fatalf("contacts = %d, named one = %q", n, kept)
	}
}
