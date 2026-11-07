package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/agenda"
	"github.com/madkoding/kitties/internal/notify"
)

func TestLeadReportsForTheOthers(t *testing.T) {
	f, sa, sb, _ := fleet(t)
	s := service(t, f)
	b := &bell{}
	s.SetNotifier(b)
	withAgenda(t, s)
	if s.Lead() != nil {
		t.Fatal("no lead yet")
	}
	// Without a lead a kitty speaks for itself, and nothing is kept for anyone.
	ana, _ := f.Get("ana")
	s.tell(notify.GoalDone, ana, "finished a goal", "done", "")
	if b.seen[0].Kitty != "ana" || b.seen[0].Title != "finished a goal" || len(s.List()) != 0 {
		t.Fatalf("no lead: %+v, %d chats", b.seen[0], len(s.List()))
	}

	f.kitties["b1"].Lead = true
	if l := s.Lead(); l == nil || l.Name != "bo" {
		t.Fatalf("lead = %v", l)
	}
	sa.replies = []string{"Checked the inbox."}
	if _, err := s.RunGoal(context.Background(), agenda.Goal{ID: "g1", Kitty: "a1", Title: "Inbox", Text: "look"}, agenda.Trigger{Kind: "schedule"}); err != nil {
		t.Fatal(err)
	}
	// The news goes out in the lead's name, and lands in its Reports thread.
	last := b.seen[len(b.seen)-1]
	if last.Kitty != "bo" || last.Title != "ana finished a goal" || last.Event != notify.GoalDone {
		t.Errorf("signed = %+v", last)
	}
	lead, _ := f.Get("bo")
	id, err := s.ReportsChat(lead)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := s.ReportsChat(lead)
	if again != id {
		t.Error("one Reports thread")
	}
	rep, _ := s.Get(id)
	if !rep.Reports || rep.Title != ReportsTitle || len(rep.Members) != 1 || rep.Members[0] != "b1" || len(rep.Turns) != 1 {
		t.Fatalf("reports = %+v", rep)
	}
	turn := rep.Turns[0]
	if turn.From != "ana" || turn.Report == nil || turn.Report.Event != "goal_done" || turn.Report.Chat == "" || !strings.Contains(turn.Text, "Checked the inbox") {
		t.Errorf("report turn = %+v", turn)
	}
	// The lead's own news is not reported to itself.
	s.tell(notify.Help, lead, "needs your help", "x", "")
	if rep, _ = s.Get(id); len(rep.Turns) != 1 || b.seen[len(b.seen)-1].Kitty != "bo" || b.seen[len(b.seen)-1].Title != "needs your help" {
		t.Errorf("the lead's own news: %d turns, %+v", len(rep.Turns), b.seen[len(b.seen)-1])
	}

	// Asked in its Reports thread, the lead is told it orchestrates, sees its team and the reports.
	sb.replies = []string{"ana checked the inbox."}
	if _, err := s.Send(context.Background(), id, "how is it going?"); err != nil {
		t.Fatal(err)
	}
	msgs := sb.seen[len(sb.seen)-1]
	system := msgs[0].Content
	if !strings.Contains(system, "You are the lead kitty") || !strings.Contains(system, "Your team:\n- ana:") || !strings.Contains(system, "has a computer") || !strings.Contains(system, "uses echo") {
		t.Errorf("lead prompt = %s", system)
	}
	if strings.Contains(system, "never for the whole task") {
		t.Error("the lead may hand whole tasks out")
	}
	all := ""
	for _, m := range msgs[1:] {
		all += m.Content
	}
	if !strings.Contains(all, "[ana]: finished a goal: Checked the inbox") {
		t.Errorf("the lead reads the reports: %s", all)
	}
	// The others know who leads.
	sa.replies = []string{"ok"}
	direct, _ := s.Create("", []string{"ana"})
	if _, err := s.Send(context.Background(), direct.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	waitDone(t, s, direct.ID)
	if p := sa.seen[len(sa.seen)-1][0].Content; !strings.Contains(p, "bo is the lead kitty") || strings.Contains(p, "Your team:") {
		t.Errorf("a member's prompt = %s", p)
	}

	// The daily summary comes from the lead, and stays in its Reports thread.
	n := s.LeadDigest(notify.Notification{Event: notify.Digest, Title: "Your kitties", Body: "ana: 1 goal"})
	if n.Kitty != "bo" || n.Chat != id {
		t.Errorf("digest = %+v", n)
	}
	if rep, _ = s.Get(id); rep.Turns[len(rep.Turns)-1].Report == nil || rep.Turns[len(rep.Turns)-1].Report.Event != "digest" {
		t.Errorf("digest turn = %+v", rep.Turns[len(rep.Turns)-1])
	}
	f.kitties["b1"].Lead = false
	if n := s.LeadDigest(notify.Notification{Title: "x"}); n.Kitty != "" || n.Chat != "" {
		t.Errorf("no lead, digest as it was: %+v", n)
	}
}
