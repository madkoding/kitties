package chat

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/budget"
	"github.com/madkoding/kitties/internal/kitty"
)

func withBudget(t *testing.T, s *Service) {
	t.Helper()
	st, err := budget.Open(filepath.Join(t.TempDir(), "budget.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.SetBudget(st)
}

func TestParkedWordsGoOnWhenTokensAreBack(t *testing.T) {
	f, sa, _, _ := fleet(t)
	s := service(t, f)
	withBudget(t, s)
	ana, _ := s.fleet.Get("ana")
	ana.Limits = kitty.Limits{DayTokens: 3} // one reply
	sa.replies = []string{"first", "went on"}
	sess, _ := s.Create("", []string{"ana"})
	_, _ = s.Send(context.Background(), sess.ID, "hi")
	turns, _ := s.Send(context.Background(), sess.ID, "more")
	if !turns[0].Parked {
		t.Fatalf("over its day: %+v", turns[0])
	}
	// Still out of tokens: nothing wakes up.
	if ids := s.ResumeParked(context.Background()); len(ids) != 0 {
		t.Fatalf("woke %v while still out of tokens", ids)
	}
	// The person lifts the limit: it goes on by itself, once.
	ana.Limits = kitty.Limits{}
	if ids := s.ResumeParked(context.Background()); len(ids) != 1 || ids[0] != sess.ID {
		t.Fatalf("woke %v", ids)
	}
	s.Wait()
	got, _ := s.Get(sess.ID)
	n := len(got.Turns)
	if got.Turns[n-2].From != "scheduler" || !strings.Contains(got.Turns[n-2].Text, "Continue where you left off") || got.Turns[n-1].Text != "went on" || got.Turns[n-1].Parked {
		t.Fatalf("turns = %+v", got.Turns[n-2:])
	}
	if ids := s.ResumeParked(context.Background()); len(ids) != 0 {
		t.Error("a chat is woken once")
	}
}

func TestParkedTaskAndResumer(t *testing.T) {
	s, d, _ := deskService(t, "@tool click {\"x\": 1, \"y\": 1}", "@tool click {\"x\": 2, \"y\": 2}", "@tool done {\"summary\": \"finished\"}")
	withBudget(t, s)
	ana, _ := d.Get("ana")
	ana.Limits = kitty.Limits{DayTokens: 3} // the first reply starts the task, the next one is over
	s.MaxSteps = 50
	sess, _ := s.Create("", []string{"ana"})
	_, _ = s.Send(context.Background(), sess.ID, "go")
	turn := waitDone(t, s, sess.ID).Turns[1]
	if !turn.Parked || !turn.Resumable || !strings.Contains(turn.Detail, "daily token budget") {
		t.Fatalf("task: %+v", turn)
	}
	// An empty chat is left alone; the parked one goes on through the resumer.
	_, _ = s.Create("", []string{"ana"})
	ana.Limits = kitty.Limits{}
	d.cat = "- [ ] the rest"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.RunResumer(ctx, time.Millisecond); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, _ := s.Get(sess.ID)
		if last := got.Turns[len(got.Turns)-1]; len(got.Turns) > 3 && !last.Working {
			if !strings.Contains(last.Text, "finished") {
				t.Errorf("resumed: %+v", last)
			}
			if call := got.Turns[2].Text; !strings.Contains(call, "Continue where you left off") || !strings.Contains(call, "- [ ] the rest") {
				t.Errorf("the wake-up call: %q", call)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the resumer never woke it up")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	s.Wait()
	// The kitty that parked is gone: nothing to wake.
	f, sa, _, _ := fleet(t)
	s2 := service(t, f)
	withBudget(t, s2)
	a2, _ := f.Get("ana")
	a2.Limits = kitty.Limits{DayTokens: 3}
	sa.replies = []string{"x"}
	c, _ := s2.Create("", []string{"ana"})
	_, _ = s2.Send(context.Background(), c.ID, "a")
	_, _ = s2.Send(context.Background(), c.ID, "b")
	delete(f.kitties, "a1")
	if ids := s2.ResumeParked(context.Background()); len(ids) != 0 {
		t.Errorf("woke %v for a kitty that is gone", ids)
	}
}
