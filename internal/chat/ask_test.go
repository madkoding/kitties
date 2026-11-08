package chat

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/agenda"
	"github.com/madkoding/kitties/internal/notify"
)

// lastText is the text of a chat's last turn.
func lastText(t *testing.T, s *Service, id string) string {
	t.Helper()
	sess, err := s.Get(id)
	if err != nil || len(sess.Turns) == 0 {
		t.Fatalf("chat %s: %v", id, err)
	}
	return sess.Turns[len(sess.Turns)-1].Text
}

func TestKittyAsksTheLeadAndGoesOn(t *testing.T) {
	f, sa, sb, _ := fleet(t)
	s := service(t, f)
	s.HandOverWait = 0
	b := &bell{}
	s.SetNotifier(b)
	j, _ := withAgenda(t, s)
	f.kitties["b1"].Lead = true

	sa.replies = []string{`@ask_lead {"question": "Which folder do the invoices go to?"}`, "Waiting for bo's answer.", "Saved them in Invoices."}
	sb.replies = []string{`@answer_kitty ana {"text": "The Invoices folder."}`, "Told ana."}
	id, err := s.RunGoal(context.Background(), agenda.Goal{ID: "g1", Kitty: "a1", Title: "Invoices", Text: "file them"}, agenda.Trigger{Kind: "schedule"})
	if err != nil {
		t.Fatal(err)
	}
	s.Wait()

	// The kitty was told it may ask the lead, and asked it rather than the person.
	if system := sa.seen[0][0].Content; !strings.Contains(system, "ask bo with @ask_lead") || !strings.Contains(system, "is the one who talks with") {
		t.Errorf("ana's prompt = %s", system)
	}
	sess, _ := s.Get(id)
	if sess.Turns[1].AskedLead != "bo" {
		t.Errorf("asking turn = %+v", sess.Turns[1])
	}
	// The question reached the lead in its Reports thread, and the answer came back.
	lead, _ := f.Get("bo")
	rid, _ := s.ReportsChat(lead)
	rep, _ := s.Get(rid)
	var q *Turn
	for i := range rep.Turns {
		if rep.Turns[i].From == "ana" && strings.Contains(rep.Turns[i].Text, "Question from ana") {
			q = &rep.Turns[i]
		}
	}
	if q == nil || !strings.Contains(q.Text, "Which folder") || !strings.Contains(q.Text, `"Invoices"`) {
		t.Fatalf("reports = %+v", rep.Turns)
	}
	if len(rep.Asks) != 1 || rep.Asks[0].Answered.IsZero() || rep.Asks[0].Chat != id {
		t.Errorf("asks = %+v", rep.Asks)
	}
	if system := sb.seen[0][0].Content; !strings.Contains(system, "@answer_kitty") || !strings.Contains(system, "starts it fresh") {
		t.Errorf("lead prompt = %s", system)
	}
	sess, _ = s.Get(id)
	answer := sess.Turns[len(sess.Turns)-2]
	if answer.From != "bo" || !strings.Contains(answer.Text, "The Invoices folder.") {
		t.Errorf("answer turn = %+v", answer)
	}
	if got := lastText(t, s, id); got != "Saved them in Invoices." {
		t.Errorf("ana went on with %q", got)
	}
	// The person was not asked: they heard once, from the lead, that the goal was done.
	for _, n := range b.seen {
		if n.Event == notify.Help {
			t.Errorf("the person was asked: %+v", n)
		}
	}
	if last := b.seen[len(b.seen)-1]; last.Event != notify.GoalDone || last.Kitty != "bo" {
		t.Errorf("last notice = %+v", last)
	}
	if k := kinds(j, "a1"); !strings.Contains(k, "escalated") || !strings.HasSuffix(k, "done") {
		t.Errorf("journal = %s", k)
	}
}

func TestLeadAsksThePersonThenPassesTheAnswerOn(t *testing.T) {
	f, sa, sb, _ := fleet(t)
	s := service(t, f)
	s.HandOverWait = 0
	b := &bell{}
	s.SetNotifier(b)
	withAgenda(t, s)
	f.kitties["b1"].Lead = true

	sa.replies = []string{`@ask_lead {"question": "May I delete the old backups?"}`, "Waiting.", "Deleted them."}
	sb.replies = []string{"I do not know: may ana delete the old backups?"}
	id, err := s.RunGoal(context.Background(), agenda.Goal{ID: "g1", Kitty: "a1", Title: "Disk", Text: "free space"}, agenda.Trigger{Kind: "schedule"})
	if err != nil {
		t.Fatal(err)
	}
	s.Wait()
	lead, _ := f.Get("bo")
	rid, _ := s.ReportsChat(lead)
	// The lead could not answer: the person is asked, in the lead's name, in its Reports thread.
	help := b.seen[len(b.seen)-1]
	if help.Event != notify.Help || help.Kitty != "bo" || help.Chat != rid || !strings.Contains(help.Body, "old backups") {
		t.Fatalf("help = %+v", help)
	}
	if rep, _ := s.Get(rid); !rep.Help {
		t.Error("the Reports thread waits for the person")
	}
	// The person answers the lead, which passes it on; ana goes on.
	sb.replies = []string{`@answer_kitty ana {"text": "Yes, delete them."}`, "Passed it on."}
	if _, err := s.Send(context.Background(), rid, "yes"); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	if rep, _ := s.Get(rid); rep.Help || len(rep.Asks) != 1 || rep.Asks[0].Answered.IsZero() {
		t.Errorf("reports after the answer = %+v", rep.Asks)
	}
	if got := lastText(t, s, id); got != "Deleted them." {
		t.Errorf("ana went on with %q", got)
	}
	// Answering a kitty that asked nothing is refused.
	if o := s.runAnswerKitty(action{kind: "answer_kitty", name: "ana", args: map[string]any{"text": "x"}}, lead); o.err == "" {
		t.Error("no question waiting")
	}
}

func TestWhoMayAskTheLead(t *testing.T) {
	f, _, _, _ := fleet(t)
	s := service(t, f)
	ana, _ := f.Get("ana")
	own, _ := s.Create("mine", []string{"a1"})
	if s.canAskLead(ana, own.ID) != nil {
		t.Error("no lead, nobody to ask")
	}
	f.kitties["b1"].Lead = true
	bo, _ := f.Get("bo")
	if s.canAskLead(ana, own.ID) == nil || s.canAskLead(bo, own.ID) != nil {
		t.Error("the others ask the lead, the lead does not ask itself")
	}
	// A hand-over (the lead waits for it) and an exposed chat never reach the lead.
	hand, _ := s.delegationChat(bo, ana, 1)
	exposed, _ := s.Create("contact", []string{"a1"})
	s.mu.Lock()
	s.sessions[exposed.ID].Exposed = true
	s.mu.Unlock()
	if s.canAskLead(ana, hand) != nil || s.canAskLead(ana, exposed.ID) != nil {
		t.Error("hand-over or exposed chat")
	}
	// A thread asks at most maxLeadAsks times between two of the person's messages.
	s.mu.Lock()
	sess := s.sessions[own.ID]
	sess.Turns = append(sess.Turns, Turn{From: "user", Text: "go"})
	for i := 0; i < maxLeadAsks; i++ {
		sess.Turns = append(sess.Turns, Turn{From: "ana", Kitty: "a1", AskedLead: "bo"})
	}
	s.mu.Unlock()
	if s.canAskLead(ana, own.ID) != nil {
		t.Error("asked enough")
	}
	s.mu.Lock()
	sess.Turns = append(sess.Turns, Turn{From: "user", Text: "again"})
	s.mu.Unlock()
	if s.canAskLead(ana, own.ID) == nil {
		t.Error("the person wrote again: it may ask anew")
	}
}

func TestLeadTasksAreReadFresh(t *testing.T) {
	f, sa, sb, _ := fleet(t)
	s := service(t, f)
	s.HandOverWait = 0
	f.kitties["b1"].Lead = true
	chat, _ := s.Create("plan", []string{"b1"})
	sb.replies = []string{`@delegate ana {"task": "count the apples"}`, "Done.", `@delegate ana {"task": "count the pears"}`, "Done."}
	sa.replies = []string{"12 apples.", "7 pears."}
	for _, ask := range []string{"apples?", "pears?"} {
		if _, err := s.Send(context.Background(), chat.ID, ask); err != nil {
			t.Fatal(err)
		}
	}
	// The second task is all ana sees of the thread with the lead: not the first, nor its answer.
	msgs := sa.seen[len(sa.seen)-1]
	var all []string
	for _, m := range msgs[1:] {
		all = append(all, m.Content)
	}
	joined := strings.Join(all, "\n")
	if !strings.Contains(joined, "count the pears") || strings.Contains(joined, "apples") {
		t.Errorf("ana saw: %s", joined)
	}
}

func TestTeamGoalsReachThePersonAsOneMessage(t *testing.T) {
	f, _, _, _ := fleet(t)
	s := service(t, f)
	s.HandOverWait = time.Hour
	b := &bell{}
	s.SetNotifier(b)
	f.kitties["b1"].Lead = true
	ana, _ := f.Get("ana")
	s.tell(notify.GoalDone, ana, "finished a goal", "inbox", "c1")
	s.tell(notify.GoalDone, ana, "finished a goal", "invoices", "c2")
	s.FlushHandOvers()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.seen) != 1 {
		t.Fatalf("notices = %+v", b.seen)
	}
	n := b.seen[0]
	lead, _ := f.Get("bo")
	rid, _ := s.ReportsChat(lead)
	if n.Kitty != "bo" || n.Title != "2 goals finished" || n.Event != notify.GoalDone || n.Chat != rid || !strings.Contains(n.Body, "invoices") {
		t.Errorf("one notice = %+v", n)
	}
}

func TestOwnGoalWaitsForTheLeadsTask(t *testing.T) {
	f, _, _, _ := fleet(t)
	s := service(t, f)
	f.kitties["b1"].Lead = true
	ana, _ := f.Get("ana")
	bo, _ := f.Get("bo")
	hand, _ := s.delegationChat(bo, ana, 1)
	s.mu.Lock()
	s.sessions[hand].Turns = append(s.sessions[hand].Turns, Turn{From: "ana", Kitty: "a1", Working: true})
	s.mu.Unlock()
	if !s.leadWork(ana) {
		t.Fatal("ana works for the lead")
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		s.edit(hand, 0, func(t *Turn) { t.Working = false })
	}()
	start := time.Now()
	if !s.waitForLeadWork(context.Background(), ana) || time.Since(start) < 20*time.Millisecond {
		t.Error("the goal waited for the lead's task")
	}
	if s.waitForLeadWork(context.Background(), ana) {
		t.Error("nothing to wait for")
	}
}
