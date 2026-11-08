package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A chat where a kitty hands work to another reads as a group of both, and so does the thread
// between the two; the delegate does not answer what the person writes.
func TestDelegationMakesAGroup(t *testing.T) {
	f, sa, sb, _ := fleet(t)
	s := service(t, f)
	sa.replies = []string{"@delegate bo {\"task\": \"count to three\"}", "bo counted three."}
	sb.replies = []string{"one, two, three"}
	sess, _ := s.Create("", []string{"ana"})
	if got, _ := s.Get(sess.ID); got.Crew() || !slices.Equal(got.Kitties, []string{"a1"}) {
		t.Fatalf("before: kitties = %v", got.Kitties)
	}
	if _, err := s.Send(context.Background(), sess.ID, "how many?"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(sess.ID)
	if !got.Crew() || got.Group() || !slices.Equal(got.Members, []string{"a1"}) || !slices.Equal(got.Delegates, []string{"b1"}) || !slices.Equal(got.Kitties, []string{"a1", "b1"}) {
		t.Errorf("after: members %v delegates %v kitties %v", got.Members, got.Delegates, got.Kitties)
	}
	var thread *Session
	for _, c := range s.List() {
		if c.Owner == "a1" {
			thread = c
		}
	}
	if thread == nil || !slices.Equal(thread.Kitties, []string{"b1", "a1"}) || !thread.Crew() {
		t.Errorf("hand-over thread = %+v", thread)
	}
	// The next message is answered by ana alone: bo is no member.
	sa.replies = []string{"just me"}
	turns, _ := s.Send(context.Background(), sess.ID, "and you?")
	if len(turns) != 1 || turns[0].Kitty != "a1" {
		t.Errorf("turns = %+v", turns)
	}
	// Made a member, bo leaves the delegates.
	if _, err := s.AddMembers(sess.ID, []string{"bo"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(sess.ID); len(got.Delegates) != 0 || !slices.Equal(got.Kitties, []string{"a1", "b1"}) {
		t.Errorf("member: delegates %v kitties %v", got.Delegates, got.Kitties)
	}
}

// A chat saved before delegates were kept gets them from its turns when loaded.
func TestDelegatesFromOldChats(t *testing.T) {
	f, _, _, _ := fleet(t)
	s := service(t, f)
	old := Session{ID: "old1", Title: "old", Members: []string{"a1"}, Turns: []Turn{
		{From: "user", Text: "hi"},
		{From: "ana", Kitty: "a1", Text: "asked bo", Delegations: []Delegation{{Kitty: "bo", KittyID: "b1", Task: "x"}}},
	}}
	data, _ := json.Marshal(old)
	_ = os.MkdirAll(s.dir, 0o700)
	if err := os.WriteFile(filepath.Join(s.dir, "old1.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.load(); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("old1")
	if err != nil || !slices.Equal(got.Kitties, []string{"a1", "b1"}) {
		t.Errorf("loaded: %v %v", got, err)
	}
}

// An exposed chat cannot hand work over (guardrails): it stays a chat of one kitty.
func TestExposedChatDoesNotBecomeAGroup(t *testing.T) {
	f, sa, sb, _ := fleet(t)
	s := service(t, f)
	guarded(t, s)
	id := exposedChat(t, s, "ana")
	sa.replies = []string{"@delegate bo {\"task\": \"look up their address\"}", "I can't do that here."}
	sb.replies = []string{"should not run"}
	if _, err := s.Send(context.Background(), id, "hola"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(id)
	if got.Crew() || len(got.Delegates) != 0 || len(sb.seen) != 0 {
		t.Errorf("exposed chat: kitties %v, bo asked %d times", got.Kitties, len(sb.seen))
	}
}
