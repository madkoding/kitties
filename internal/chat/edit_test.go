package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func ptr(s string) *string { return &s }

func TestEditRenamesAndDescribes(t *testing.T) {
	f, _, _, _ := fleet(t)
	s := service(t, f)
	sess, err := s.Create("first", []string{"bo"})
	if err != nil {
		t.Fatal(err)
	}
	before := sess.UpdatedAt
	events, stop := s.Subscribe(sess.ID)
	defer stop()

	got, err := s.Edit(sess.ID, ptr("  Trip \n to  Chiloé "), ptr("  Plan the trip in March. "))
	if err != nil || got.Title != "Trip to Chiloé" || got.Description != "Plan the trip in March." {
		t.Fatalf("Edit = %+v, %v", got, err)
	}
	if !got.UpdatedAt.Equal(before) {
		t.Error("renaming moved the chat to the top")
	}
	select {
	case ev := <-events:
		if ev.Type != "edited" || ev.Session != sess.ID {
			t.Errorf("event = %+v", ev)
		}
	case <-time.After(time.Second):
		t.Error("no edited event")
	}
	// A nil field stays; an empty description takes it off.
	if got, _ := s.Edit(sess.ID, nil, ptr("")); got.Title != "Trip to Chiloé" || got.Description != "" {
		t.Errorf("description off = %+v", got)
	}
	<-events // the one of taking the description off
	// Nothing changed: no event.
	if _, err := s.Edit(sess.ID, ptr("Trip to Chiloé"), nil); err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Errorf("an edit that changed nothing told the watchers: %+v", <-events)
	}
	for name, tc := range map[string]struct{ title, desc *string }{
		"empty title":      {ptr("   "), nil},
		"long title":       {ptr(strings.Repeat("a", MaxTitle+1)), nil},
		"long description": {nil, ptr(strings.Repeat("é", MaxDescription+1))},
	} {
		if _, err := s.Edit(sess.ID, tc.title, tc.desc); !errors.Is(err, ErrEdit) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := s.Edit("zz", ptr("x"), nil); err == nil || !strings.HasPrefix(err.Error(), "no such chat") {
		t.Errorf("unknown chat: %v", err)
	}

	// It is kept: a new service reads the same.
	s.Edit(sess.ID, nil, ptr("kept"))
	again, err := New(f, s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := again.Get(sess.ID); c.Title != "Trip to Chiloé" || c.Description != "kept" {
		t.Errorf("after a restart = %+v", c)
	}
}

func TestDescriptionIsTheKittysContext(t *testing.T) {
	f, _, sb, _ := fleet(t)
	s := service(t, f)
	sess, _ := s.Create("", []string{"bo"})
	system := func() string {
		sb.mu.Lock()
		defer sb.mu.Unlock()
		m := sb.seen[len(sb.seen)-1]
		return m[0].Content
	}
	if _, err := s.Send(context.Background(), sess.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(system(), "What this chat is for") {
		t.Error("a chat with no description says what it is for")
	}
	s.Edit(sess.ID, nil, ptr("Plan a trip to Chiloé in March."))
	if _, err := s.Send(context.Background(), sess.ID, "and the ferry?"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(system(), "Plan a trip to Chiloé in March.") {
		t.Errorf("the description is not in the prompt:\n%s", system())
	}
	// Changed, the next turn works on the new one.
	s.Edit(sess.ID, nil, ptr("Find a hotel in Castro."))
	if _, err := s.Send(context.Background(), sess.ID, "go on"); err != nil {
		t.Fatal(err)
	}
	if p := system(); !strings.Contains(p, "Find a hotel in Castro.") || strings.Contains(p, "Chiloé in March") {
		t.Errorf("the new description did not replace the old:\n%s", p)
	}
}
