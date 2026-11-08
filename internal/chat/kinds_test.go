package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/agenda"
)

// A hand-over's thread is no chat of the person: it is out of the chats, the answer shows once
// (in the chat that asked), and it goes a while after its work ended unless it is kept.
func TestHandOverThreadsGoAfterAWhile(t *testing.T) {
	f, sa, sb, _ := fleet(t)
	s := service(t, f)
	now := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	sa.replies = []string{"@delegate bo {\"task\": \"count to three\"}", "bo counted it."}
	sb.replies = []string{"one, two, three"}
	sess, _ := s.Create("", []string{"ana"})
	if _, err := s.Send(context.Background(), sess.ID, "how many?"); err != nil {
		t.Fatal(err)
	}
	var handover *Session
	for _, c := range s.List() {
		switch c.ID {
		case sess.ID:
			if c.Kind != KindChat || !c.Listed() {
				t.Errorf("the person's chat = %s listed %v", c.Kind, c.Listed())
			}
		default:
			handover = c
		}
	}
	if handover == nil || handover.Kind != KindHandOver || handover.Listed() {
		t.Fatalf("hand-over = %+v", handover)
	}
	if !handover.EndedAt.Equal(now) || !handover.ExpiresAt.Equal(now.Add(DefaultHandOverKeep)) {
		t.Errorf("ended %v expires %v", handover.EndedAt, handover.ExpiresAt)
	}
	// Before its time nothing goes.
	if n, err := s.ExpireHandOvers(); n != 0 || err != nil {
		t.Errorf("early expiry = %d %v", n, err)
	}
	// Kept: it stays, and shows among the chats.
	kept, err := s.KeepChat(handover.ID, true)
	if err != nil || !kept.Keep || !kept.Listed() || !kept.ExpiresAt.IsZero() {
		t.Fatalf("keep = %+v %v", kept, err)
	}
	now = now.Add(DefaultHandOverKeep + time.Hour)
	if n, _ := s.ExpireHandOvers(); n != 0 {
		t.Error("a kept hand-over went")
	}
	// Let go again: it goes its whole time from now.
	if _, err := s.KeepChat(handover.ID, false); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.ExpireHandOvers(); n != 0 {
		t.Error("a hand-over just let go went at once")
	}
	// The setting decides how long: zero keeps them all.
	s.SetHandOverKeep(func() time.Duration { return 0 })
	now = now.Add(30 * 24 * time.Hour)
	if n, _ := s.ExpireHandOvers(); n != 0 {
		t.Error("hand-overs kept forever went")
	}
	s.SetHandOverKeep(func() time.Duration { return time.Hour })
	if n, err := s.ExpireHandOvers(); n != 1 || err != nil {
		t.Fatalf("expiry = %d %v", n, err)
	}
	if _, err := s.Get(handover.ID); err == nil {
		t.Error("the hand-over thread is still there")
	}
	// The chat that asked keeps the answer.
	got, _ := s.Get(sess.ID)
	if len(got.Turns) < 2 || len(got.Turns[1].Delegations) != 1 || got.Turns[1].Delegations[0].Result != "one, two, three" {
		t.Errorf("the asking chat = %+v", got.Turns)
	}
	// Only hand-overs can be kept.
	if _, err := s.KeepChat(sess.ID, true); !errors.Is(err, ErrNotHandOver) {
		t.Errorf("keeping a chat: %v", err)
	}
}

// A hand-over thread from before they ended (no EndedAt) goes by its last word.
func TestOldHandOverThreadsGoByTheirLastWord(t *testing.T) {
	f, _, _, _ := fleet(t)
	s := service(t, f)
	now := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	old, _ := s.Create("ana → bo", []string{"bo"})
	s.mu.Lock()
	s.sessions[old.ID].Owner, s.sessions[old.ID].Depth = "a1", 1
	s.sessions[old.ID].UpdatedAt = now.Add(-100 * time.Hour)
	_ = s.save(s.sessions[old.ID])
	s.mu.Unlock()
	// After a restart it is a hand-over, out of the chats.
	again, err := New(f, s.dir)
	if err != nil {
		t.Fatal(err)
	}
	again.now = s.now
	got, _ := again.Get(old.ID)
	if got.Kind != KindHandOver || got.Listed() || !got.ExpiresAt.Equal(now.Add(-100*time.Hour+DefaultHandOverKeep)) {
		t.Fatalf("old hand-over = %s listed %v expires %v", got.Kind, got.Listed(), got.ExpiresAt)
	}
	if n, _ := again.ExpireHandOvers(); n != 1 {
		t.Errorf("expiry = %d", n)
	}
}

// A goal's thread is the scheduled task's, not a chat: its runs are the task's history, and a
// goal that is gone leaves its thread among the chats.
func TestGoalRunsAndTidy(t *testing.T) {
	f, sa, _, _ := fleet(t)
	s := service(t, f)
	withAgenda(t, s)
	sa.replies = []string{"Checked the inbox: nothing new.", "(ana could not answer: down)", "Two new mails."}
	g := agenda.Goal{ID: "g1", Kitty: "a1", Title: "Inbox", Text: "Look at the inbox."}
	id, err := s.RunGoal(context.Background(), g, agenda.Trigger{Kind: "schedule"})
	if err != nil {
		t.Fatal(err)
	}
	g.Chat = id
	if _, err := s.RunGoal(context.Background(), g, agenda.Trigger{Kind: "manual"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunGoal(context.Background(), g, agenda.Trigger{Kind: "manual"}); err != nil {
		t.Fatal(err)
	}
	sess, _ := s.Get(id)
	if sess.Kind != KindGoal || sess.Listed() {
		t.Errorf("goal thread = %s listed %v", sess.Kind, sess.Listed())
	}
	chatID, runs, err := s.Runs("g1", 0)
	if err != nil || chatID != id || len(runs) != 3 {
		t.Fatalf("runs = %s %+v %v", chatID, runs, err)
	}
	// Newest first, each with why it ran, how it ended and the kitty's last words.
	if runs[0].Answer != "Two new mails." || runs[0].Result != "done" || runs[0].Kitty != "ana" || runs[0].Turns != 2 {
		t.Errorf("last run = %+v", runs[0])
	}
	// The journal says how a run ended; without it, the run's last turn does.
	if runs[1].Result != "done" {
		t.Errorf("second run = %+v", runs[1])
	}
	if r := splitRuns(sess.Turns); r[1].Result != "failed" {
		t.Errorf("failed run by its turns = %+v", r[1])
	}
	if runs[2].Why != "the schedule" || runs[2].Answer != "Checked the inbox: nothing new." || runs[2].Turn != 0 {
		t.Errorf("first run = %+v", runs[2])
	}
	if _, last, _ := s.Runs("g1", 1); len(last) != 1 || last[0].Answer != "Two new mails." {
		t.Errorf("one run = %+v", last)
	}
	if c, none, _ := s.Runs("nope", 0); c != "" || len(none) != 0 {
		t.Errorf("no goal = %s %+v", c, none)
	}
	// The goal is removed: its thread becomes a chat again.
	if n := s.TidyGoals(func(string) bool { return true }); n != 0 {
		t.Errorf("tidy with every goal = %d", n)
	}
	if n := s.TidyGoals(func(string) bool { return false }); n != 1 {
		t.Errorf("tidy = %d", n)
	}
	if got, _ := s.Get(id); got.Kind != KindChat || !got.Listed() {
		t.Errorf("after tidy = %s", got.Kind)
	}
}
