package wameow

import (
	"context"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/madkoding/kitties/internal/whatsapp"
)

const lidGroup = "120363@g.us"

// lidMembers is a group that hides numbers: Ana and the kitty by LID, Pedro by number.
var lidMembers = []types.GroupParticipant{
	{JID: types.NewJID("111", types.HiddenUserServer), PhoneNumber: types.NewJID("56911111111", types.DefaultUserServer), LID: types.NewJID("111", types.HiddenUserServer)},
	{JID: types.NewJID("56922222222", types.DefaultUserServer)},
	{JID: types.NewJID("999", types.HiddenUserServer), PhoneNumber: types.NewJID("15550001", types.DefaultUserServer)},
}

func running(t *testing.T, f *fakeConn, emit func(whatsapp.Event)) (*Client, func()) {
	t.Helper()
	s, _ := paired(t)
	blob, _ := s.Export()
	c := clientWith(f)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx, blob, emit) }()
	wait(t, "the connection", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.connected && f.handler != nil })
	return c, func() { cancel(); <-done }
}

// A mention goes by the JID the group knows the member by, and @all mentions every member but
// the kitty, unseen.
func TestSendMentions(t *testing.T) {
	f := &fakeConn{groups: map[string]string{lidGroup: "Familia"}, members: map[string][]types.GroupParticipant{lidGroup: lidMembers}}
	c, stop := running(t, f, func(whatsapp.Event) {})
	ctx := context.Background()
	ref := whatsapp.Ref{Chat: lidGroup, Sender: "111@lid", ID: "m1"}
	if err := c.SendMentions(ctx, lidGroup, "@56911111111 y @56922222222, @56933333333 no está", whatsapp.Mentions{Numbers: []string{"56911111111", "56922222222", "56933333333"}}, ref, "¿quién viene?"); err != nil {
		t.Fatal(err)
	}
	if err := c.SendMentions(ctx, lidGroup, "@todos mañana no hay clases", whatsapp.Mentions{All: true}, whatsapp.Ref{}, ""); err != nil {
		t.Fatal(err)
	}
	stop()
	e := f.msgs[0].GetExtendedTextMessage()
	if e.GetText() != "@111 y @56922222222, @56933333333 no está" {
		t.Fatalf("text = %q", e.GetText())
	}
	ci := e.GetContextInfo()
	if strings.Join(ci.GetMentionedJID(), ",") != "111@lid,56922222222@s.whatsapp.net" || ci.GetStanzaID() != "m1" || ci.GetQuotedMessage().GetConversation() != "¿quién viene?" {
		t.Fatalf("context = %v", ci)
	}
	all := f.msgs[1].GetExtendedTextMessage()
	if all.GetText() != "@todos mañana no hay clases" || strings.Join(all.GetContextInfo().GetMentionedJID(), ",") != "111@lid,56922222222@s.whatsapp.net" || all.GetContextInfo().StanzaID != nil {
		t.Fatalf("all = %v", all)
	}
	if f.groupAsks != 1 {
		t.Fatalf("asked %d times", f.groupAsks)
	}
}

// Without the members list, the numbers are mentioned as they are.
func TestSendMentionsWithoutMembers(t *testing.T) {
	f := &fakeConn{}
	c, stop := running(t, f, func(whatsapp.Event) {})
	if err := c.SendMentions(context.Background(), lidGroup, "@56922222222 hola", whatsapp.Mentions{Numbers: []string{"56922222222"}}, whatsapp.Ref{}, ""); err != nil {
		t.Fatal(err)
	}
	if err := c.SendMentions(context.Background(), lidGroup, "nada", whatsapp.Mentions{All: true}, whatsapp.Ref{}, ""); err != nil {
		t.Fatal(err)
	}
	stop()
	if got := f.msgs[0].GetExtendedTextMessage().GetContextInfo().GetMentionedJID(); len(got) != 1 || got[0] != "56922222222@s.whatsapp.net" {
		t.Fatalf("mentions = %v", got)
	}
	if f.msgs[1].GetConversation() != "nada" {
		t.Fatalf("plain = %v", f.msgs[1])
	}
}

// In a group that hides numbers, a sender, a quoted author and a mention the store does not know
// yet are read from the members list: the kitty always knows who wrote.
func TestUnknownLIDsFromMembers(t *testing.T) {
	f := &fakeConn{groups: map[string]string{lidGroup: "Familia"}, members: map[string][]types.GroupParticipant{lidGroup: lidMembers}}
	rec := &recorder{}
	_, stop := running(t, f, rec.emit)
	f.fire(&events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{
		Sender: types.NewJID("111", types.HiddenUserServer), Chat: types.NewJID("120363", types.GroupServer), IsGroup: true}, ID: "x1"},
		Message: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("@111 ¿y tú?"),
			ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("q1"), Participant: proto.String("111@lid"),
				QuotedMessage: &waE2E.Message{Conversation: proto.String("hola")}, MentionedJID: []string{"111@lid"}}}}})
	stop()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var ev *whatsapp.Event
	for i := range rec.evs {
		if rec.evs[i].Kind == whatsapp.EvMessage {
			ev = &rec.evs[i]
		}
	}
	if ev == nil || ev.From != "56911111111" || ev.Quote == nil || ev.Quote.From != "56911111111" || ev.Mentions["111"] != "56911111111" || ev.ChatName != "Familia" {
		t.Fatalf("event = %+v", ev)
	}
}

func TestReplaceTag(t *testing.T) {
	if got := replaceTag("@569 @5691 @569.", "569", "1"); got != "@1 @5691 @1." {
		t.Fatalf("got %q", got)
	}
}
