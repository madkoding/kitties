package chat

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/approvals"
	"github.com/madkoding/kitties/internal/budget"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/provider"
)

func TestDayTokensStopTheKittyOnceAndTellOnce(t *testing.T) {
	s, sa, _, b, j := gated(t)
	st, err := budget.Open(filepath.Join(t.TempDir(), "budget.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.SetBudget(st)
	ana, _ := s.fleet.Get("ana")
	ana.Limits = kitty.Limits{DayTokens: 6} // every reply costs three
	sa.replies = []string{"hello"}
	sess, _ := s.Create("", []string{"ana"})
	for i := 0; i < 2; i++ {
		if turns, _ := s.Send(context.Background(), sess.ID, "hi"); strings.Contains(turns[0].Text, "could not answer") {
			t.Fatalf("round %d refused too soon: %q", i, turns[0].Text)
		}
	}
	for i := 0; i < 2; i++ {
		turns, _ := s.Send(context.Background(), sess.ID, "again")
		if !strings.Contains(turns[0].Text, "the daily token budget (6) is spent; it resumes tomorrow") {
			t.Fatalf("over budget: %q", turns[0].Text)
		}
	}
	if b.events() != "budget" {
		t.Errorf("told %q, want once", b.events())
	}
	if kinds(j, "a1") != "blocked" {
		t.Errorf("journal = %s", kinds(j, "a1"))
	}
	if sp := s.SpentBy(ana); sp.Tokens != 6 || sp.DayTokens != 6 {
		t.Errorf("spent = %+v", sp)
	}
	// No limit, no budget store: nothing stops it.
	ana.Limits = kitty.Limits{}
	if err := s.overTokens(ana); err != nil {
		t.Error(err)
	}
	s.SetBudget(nil)
	ana.Limits = kitty.Limits{DayTokens: 1, DayActions: 1}
	if s.overTokens(ana) != nil || s.overActions(ana) || s.SpentBy(ana).Tokens != 0 {
		t.Error("without a store nothing is counted")
	}
	s.spendTokens(ana, provider.Usage{Input: 1})
	s.spendAction(ana)
}

func TestDayActionsForceAnApprovalEvenWhenActingAlone(t *testing.T) {
	s, sa, posts, _, _ := gated(t)
	st, _ := budget.Open(filepath.Join(t.TempDir(), "budget.json"))
	s.SetBudget(st)
	ana, _ := s.fleet.Get("ana")
	ana.Approvals = "auto"
	ana.Limits = kitty.Limits{DayActions: 1}
	sess, _ := s.Create("", []string{"ana"})
	say := func(text string) {
		sa.replies = []string{"@connector hook {\"text\": \"" + text + "\"}", "ok"}
		if _, err := s.Send(context.Background(), sess.ID, "go"); err != nil {
			t.Fatal(err)
		}
	}
	say("first")
	if *posts != 1 {
		t.Fatalf("the first action goes ahead on its own: posts %d", *posts)
	}
	say("second")
	p, _ := s.ListApprovals(approvals.Pending)
	if *posts != 1 || len(p) != 1 || !strings.Contains(p[0].Summary, "(over today's 1 actions)") {
		t.Fatalf("the second waits: posts %d, pending %+v", *posts, p)
	}
	if sp := s.SpentBy(ana); sp.Actions != 1 {
		t.Errorf("a held action is not spent yet: %+v", sp)
	}
	if _, err := s.DecideApproval(context.Background(), p[0].ID, true, "", false); err != nil {
		t.Fatal(err)
	}
	if sp := s.SpentBy(ana); *posts != 2 || sp.Actions != 2 {
		t.Errorf("a yes runs it and counts it: posts %d, %+v", *posts, sp)
	}
}

func TestDayTokensStopATaskInTheBackground(t *testing.T) {
	s, _, _ := deskService(t, "@tool click {\"x\": 1, \"y\": 1}", "@tool click {\"x\": 2, \"y\": 2}", "@tool done {}")
	st, _ := budget.Open(filepath.Join(t.TempDir(), "budget.json"))
	s.SetBudget(st)
	ana, _ := s.fleet.Get("ana")
	ana.Limits = kitty.Limits{DayTokens: 4} // the first reply is three, the second spends the rest
	sess, _ := s.Create("", []string{"ana"})
	_, _ = s.Send(context.Background(), sess.ID, "go")
	got := waitDone(t, s, sess.ID)
	if !strings.Contains(got.Turns[1].Text, "the daily token budget (4) is spent") {
		t.Errorf("task: %+v", got.Turns[1])
	}
}
