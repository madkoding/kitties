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

func TestSuggestReadsTheChat(t *testing.T) {
	f, _, sb, _ := fleet(t)
	s := service(t, f)
	sess, _ := s.Create("bo", []string{"bo"})
	if _, err := s.Suggest(context.Background(), sess.ID); !errors.Is(err, ErrNothingToRead) {
		t.Errorf("an empty chat: %v", err)
	}
	if _, err := s.Send(context.Background(), sess.ID, "help me plan the trip to Chiloé, my card is 4111 1111 1111 1111"); err != nil {
		t.Fatal(err)
	}
	sb.mu.Lock()
	sb.replies = []string{"Sure:\n```json\n{\"title\": \"“Trip to Chiloé.”\", \"description\": \"Plan the March trip; pay with 4111 1111 1111 1111.\"}\n```"}
	sb.mu.Unlock()
	got, err := s.Suggest(context.Background(), sess.ID)
	if err != nil || got.Title != "Trip to Chiloé" || !strings.HasPrefix(got.Description, "Plan the March trip") ||
		strings.Contains(got.Description, "4111") {
		t.Fatalf("Suggest = %+v, %v", got, err)
	}
	sb.mu.Lock()
	read := sb.seen[len(sb.seen)-1][1].Content
	sb.mu.Unlock()
	if !strings.Contains(read, "plan the trip to Chiloé") || !strings.Contains(read, "Current title: bo") {
		t.Errorf("the kitty did not read the chat:\n%s", read)
	}
	// Nothing was saved.
	if c, _ := s.Get(sess.ID); c.Title != "bo" || c.Description != "" {
		t.Errorf("Suggest saved: %+v", c)
	}
	sb.mu.Lock()
	sb.replies = []string{"I cannot"}
	sb.mu.Unlock()
	if _, err := s.Suggest(context.Background(), sess.ID); err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Errorf("an answer without JSON: %v", err)
	}
	if _, err := s.Suggest(context.Background(), "zz"); err == nil {
		t.Error("unknown chat")
	}
}

func TestParseSuggestionKeepsTheLimits(t *testing.T) {
	long := strings.Repeat("word ", 200)
	got, err := parseSuggestion(`{"title": "` + long + `", "description": "` + long + `"}`)
	if err != nil || len([]rune(got.Title)) > MaxTitle || len([]rune(got.Description)) > MaxDescription {
		t.Errorf("parseSuggestion = %+v, %v", got, err)
	}
	if _, err := parseSuggestion(`{"title": " ", "description": "x"}`); err == nil {
		t.Error("an empty title")
	}
}
