package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/agenda"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/notify"
	"github.com/madkoding/kitties/internal/provider"
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

// roundsFleet is the fake fleet that can change a kitty's rounds, as the orchestrator does.
type roundsFleet struct{ *fakeFleet }

func (f roundsFleet) SetRounds(ref string, n int) (*kitty.Kitty, error) {
	k, err := f.Get(ref)
	if err != nil {
		return nil, err
	}
	k.Limits.MaxRounds = n
	return k, nil
}

func TestLeadSetsRounds(t *testing.T) {
	ff, sa, sb, _ := fleet(t)
	f := roundsFleet{ff}
	s := service(t, f)
	j, _ := withAgenda(t, s)
	ana, _ := f.Get("ana")
	bo, _ := f.Get("bo")
	// Nobody leads: nobody gets the tool, and the line is refused.
	if s.canSetRounds(bo) {
		t.Fatal("only the lead")
	}
	sb.replies = []string{"@rounds ana {\"max_rounds\": 60}", "ok"}
	c, _ := s.Create("", []string{"bo"})
	if _, err := s.Send(context.Background(), c.ID, "go"); err != nil {
		t.Fatal(err)
	}
	if ana.Limits.MaxRounds != 0 {
		t.Fatalf("a kitty that does not lead changed ana's rounds: %d", ana.Limits.MaxRounds)
	}

	bo.Lead = true
	if !s.canSetRounds(bo) || s.canSetRounds(ana) {
		t.Fatal("the lead, and only it, may")
	}
	found := false
	for _, tool := range s.toolSet(bo, nil) {
		found = found || tool.Name == "rounds"
	}
	if !found {
		t.Error("the lead is offered the rounds tool")
	}
	sb.replies = []string{"@rounds ana {\"max_rounds\": 60}", "Gave ana 60 rounds."}
	if _, err := s.Send(context.Background(), c.ID, "ana needs more rounds"); err != nil {
		t.Fatal(err)
	}
	if ana.Limits.MaxRounds != 60 {
		t.Fatalf("rounds = %d", ana.Limits.MaxRounds)
	}
	if kinds(j, "a1") != "limit" || !strings.Contains(kinds(j, "b1"), "limit") {
		t.Errorf("journals: ana %s, bo %s", kinds(j, "a1"), kinds(j, "b1"))
	}
	reports, _ := s.ReportsChat(bo)
	rep, _ := s.Get(reports)
	if last := rep.Turns[len(rep.Turns)-1]; last.Report == nil || last.Report.Event != "rounds" || !strings.Contains(last.Text, "from the default to 60") {
		t.Errorf("reported = %+v", last)
	}
	// The lead's prompt shows the team's rounds.
	if p := sb.seen[len(sb.seen)-1][0].Content; !strings.Contains(p, "@rounds") {
		t.Errorf("lead prompt names the tool: %s", p)
	}

	// Refused: past the cap, itself, nobody, no number.
	for line, want := range map[string]string{
		"@rounds ana {\"max_rounds\": 9999}": "1 to 500",
		"@rounds bo {\"max_rounds\": 10}":    "not its own",
		"@rounds zed {\"max_rounds\": 10}":   "no such kitty",
		"@rounds ana {}":                     "say how many",
	} {
		sb.replies = []string{line, "ok"}
		if _, err := s.Send(context.Background(), c.ID, "x"); err != nil {
			t.Fatal(err)
		}
		msgs := sb.seen[len(sb.seen)-1]
		if got := msgs[len(msgs)-1].Content; !strings.Contains(got, want) {
			t.Errorf("%s: %s", line, got)
		}
	}
	if ana.Limits.MaxRounds != 60 {
		t.Errorf("a refused change changed nothing: %d", ana.Limits.MaxRounds)
	}
	// Plain words work too, and 0 goes back to the default.
	if o := s.runRounds(c.ID, action{kind: "rounds", name: "ana", args: map[string]any{"max_rounds": "0"}}, bo); o.err != "" || ana.Limits.MaxRounds != 0 {
		t.Errorf("back to the default: %+v %d", o, ana.Limits.MaxRounds)
	}
	if a := fromCalls([]provider.ToolCall{{ID: "1", Name: "rounds", Args: map[string]any{"kitty": "ana", "max_rounds": 30.0}}}); a[0].kind != "rounds" || a[0].name != "ana" {
		t.Errorf("native call = %+v", a[0])
	}
	_ = sa
}
