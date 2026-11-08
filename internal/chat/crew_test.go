package chat

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/notify"
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

// While a delegate works the chat shows it as helping, and a long answer is kept whole too.
func TestDelegateHelpingAndWholeAnswer(t *testing.T) {
	f, sa, sb, _ := fleet(t)
	s := service(t, f)
	sess, _ := s.Create("", []string{"ana"})
	events, stop := s.Subscribe(sess.ID)
	defer stop()
	long := strings.Repeat("a long answer, ", 40)
	sa.replies = []string{"@delegate bo {\"task\": \"write a lot\"}", "done"}
	sb.replies = []string{long}
	turns, err := s.Send(context.Background(), sess.ID, "go")
	if err != nil {
		t.Fatal(err)
	}
	helping := 0
	for len(events) > 0 {
		if ev := <-events; ev.Type == "helping" {
			helping++
		}
	}
	if helping != 2 {
		t.Errorf("helping events = %d, want start and end", helping)
	}
	if got, _ := s.Get(sess.ID); len(got.Helping) != 0 {
		t.Errorf("still helping: %+v", got.Helping)
	}
	d := turns[0].Delegations[0]
	if len(d.Result) > 310 || d.Answer != strings.TrimSpace(long) {
		t.Errorf("result %d bytes, answer %d bytes", len(d.Result), len(d.Answer))
	}
	// A short answer is not kept twice.
	if wholeAnswer("short", 300) != "" {
		t.Error("short answer kept twice")
	}
}

// How a hand-over went reaches the person (and, through tell, the lead's Reports thread), with
// secrets masked; the lead's own hand-overs are not reported back to it.
func TestDelegationIsReported(t *testing.T) {
	f, sa, sb, _ := fleet(t)
	s := service(t, f)
	b := &bell{}
	s.SetNotifier(b)
	s.HandOverWait = 0
	chat, _ := s.Create("", []string{"bo"})
	delegated := func() *notify.Notification {
		for i := range b.seen {
			if b.seen[i].Event == notify.Delegated {
				return &b.seen[i]
			}
		}
		return nil
	}
	// No lead: ana's news goes out in its own name, masked.
	sb.replies = []string{"@delegate ana {\"task\": \"count\"}", "ok"}
	sa.replies = []string{"one, two; the key is sk-proj-abcdefghijklmnopqrstuvwxyz012345"}
	if _, err := s.Send(context.Background(), chat.ID, "go"); err != nil {
		t.Fatal(err)
	}
	if n := delegated(); n == nil || n.Kitty != "ana" || n.Title != "finished bo's subtask" || strings.Contains(n.Body, "sk-proj-") || n.Chat != chat.ID {
		t.Fatalf("notification = %+v", n)
	}
	// bo leads and asked: the person is told, bo's Reports thread is not.
	f.kitties["b1"].Lead = true
	b.seen = nil
	sb.replies = []string{"@delegate ana {\"task\": \"count again\"}", "ok"}
	sa.replies = []string{"three"}
	if _, err := s.Send(context.Background(), chat.ID, "again"); err != nil {
		t.Fatal(err)
	}
	lead, _ := f.Get("bo")
	rid, _ := s.ReportsChat(lead)
	if rep, _ := s.Get(rid); delegated() == nil || len(rep.Turns) != 0 {
		t.Errorf("lead's own hand-over: notified %v, reports %d", delegated() != nil, len(rep.Turns))
	}
}

// Hand-overs of a chat that end close together reach the person as one message.
func TestHandOversFoldIntoOneNotice(t *testing.T) {
	f, sa, sb, _ := fleet(t)
	s := service(t, f)
	b := &bell{}
	s.SetNotifier(b)
	// A wait longer than the test: the notices are let go by hand once both hand-overs ended, so
	// how fast the machine runs them does not matter.
	s.HandOverWait = time.Hour
	chat, _ := s.Create("", []string{"bo"})
	sb.replies = []string{"@delegate ana {\"task\": \"count\"}\n@delegate ana {\"task\": \"spell\"}", "ok"}
	sa.replies = []string{"three", "c-a-t"}
	if _, err := s.Send(context.Background(), chat.ID, "go"); err != nil {
		t.Fatal(err)
	}
	s.FlushHandOvers()
	var got []notify.Notification
	b.mu.Lock()
	seen := append([]notify.Notification(nil), b.seen...)
	b.mu.Unlock()
	for _, n := range seen {
		if n.Event == notify.Delegated {
			got = append(got, n)
		}
	}
	if len(got) != 1 || got[0].Title != "2 hand-overs ended" || strings.Count(got[0].Body, "• ") != 2 || got[0].Chat != chat.ID {
		t.Fatalf("notices = %+v", got)
	}
	if one := combineHandOvers(got[:1]); one.Title != got[0].Title {
		t.Error("one notice is sent as it is")
	}
}

// A delegate the person takes off the chat stays off, until it is handed work there again.
func TestDismissADelegate(t *testing.T) {
	f, sa, sb, _ := fleet(t)
	s := service(t, f)
	sa.replies = []string{"@delegate bo {\"task\": \"count\"}", "ok"}
	sb.replies = []string{"three"}
	sess, _ := s.Create("", []string{"ana"})
	_, _ = s.Send(context.Background(), sess.ID, "go")
	got, err := s.RemoveMember(sess.ID, "bo")
	if err != nil || got.Crew() || len(got.Delegates) != 0 || !slices.Equal(got.Dismissed, []string{"b1"}) {
		t.Fatalf("dismissed: %+v %v", got, err)
	}
	// Saving again (old turns still name bo) does not bring it back, nor does a reload.
	s.mu.Lock()
	_ = s.save(s.sessions[sess.ID])
	s.mu.Unlock()
	if err := s.load(); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.Get(sess.ID); again.Crew() {
		t.Errorf("came back: %v", again.Kitties)
	}
	// Handed work again, it is back.
	sa.replies = []string{"@delegate bo {\"task\": \"count again\"}", "ok"}
	sb.replies = []string{"four"}
	_, _ = s.Send(context.Background(), sess.ID, "more")
	if back, _ := s.Get(sess.ID); !back.Crew() || len(back.Dismissed) != 0 {
		t.Errorf("not back: %v dismissed %v", back.Kitties, back.Dismissed)
	}
	// ana, the only member, still cannot be taken out.
	if _, err := s.RemoveMember(sess.ID, "ana"); err == nil {
		t.Error("the last member was removed")
	}
}

// Stopping sends the notices still waiting; a kitty that is removed leaves every chat's helpers.
func TestFlushHandOversAndPruneHelpers(t *testing.T) {
	f, sa, sb, _ := fleet(t)
	s := service(t, f)
	b := &bell{}
	s.SetNotifier(b)
	s.HandOverWait = time.Hour
	sess, _ := s.Create("", []string{"ana"})
	sa.replies = []string{"@delegate bo {\"task\": \"count\"}", "ok"}
	sb.replies = []string{"three"}
	if _, err := s.Send(context.Background(), sess.ID, "go"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.events(), "delegated") {
		t.Fatal("sent before the wait")
	}
	s.FlushHandOvers()
	if !strings.Contains(b.events(), "delegated") {
		t.Errorf("not sent on stop: %s", b.events())
	}
	// bo is removed from the fleet: it is no delegate of ana's chat any more, nor owner of a thread.
	if err := s.LeaveChats("b1"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(sess.ID)
	if got.Crew() || len(got.Delegates) != 0 {
		t.Errorf("bo still helps: %v", got.Kitties)
	}
	for _, c := range s.List() {
		if contains(c.Kitties, "b1") {
			t.Errorf("chat %q still lists bo: %v", c.Title, c.Kitties)
		}
	}
}

// A delegate taken off a chat can be brought back; the hand-overs are counted per kitty.
func TestRestoreDelegateAndStats(t *testing.T) {
	f, sa, sb, _ := fleet(t)
	s := service(t, f)
	sa.replies = []string{"@delegate bo {\"task\": \"count\"}", "ok"}
	sb.replies = []string{"three"}
	sess, _ := s.Create("", []string{"ana"})
	_, _ = s.Send(context.Background(), sess.ID, "go")
	if _, err := s.RestoreDelegate(sess.ID, "bo"); err == nil {
		t.Error("restored a kitty that was never taken off")
	}
	_, _ = s.RemoveMember(sess.ID, "bo")
	got, err := s.RestoreDelegate(sess.ID, "bo")
	if err != nil || !got.Crew() || len(got.Dismissed) != 0 {
		t.Fatalf("restored: %+v %v", got, err)
	}
	stats := s.HandOverStats()
	want := map[string]HandOverStat{"ana": {KittyID: "a1", Kitty: "ana", Asked: 1}, "bo": {KittyID: "b1", Kitty: "bo", Took: 1}}
	if len(stats) != 2 {
		t.Fatalf("stats = %+v", stats)
	}
	for _, st := range stats {
		w := want[st.Kitty]
		if st.Asked != w.Asked || st.Took != w.Took || st.AskedFailed != 0 || st.TookFailed != 0 {
			t.Errorf("%s: %+v", st.Kitty, st)
		}
		// Each row leads to the hand-over thread between the two.
		if len(st.Threads) != 1 || st.Threads[0].Title != "ana → bo" {
			t.Errorf("%s threads: %+v", st.Kitty, st.Threads)
		}
	}
}

// The hand-overs are counted per day for the lead's chart: today's here, the days before empty.
func TestHandOverDays(t *testing.T) {
	f, sa, sb, _ := fleet(t)
	s := service(t, f)
	if s.HandOverDays(0, time.Now()) != nil {
		t.Error("days for no days")
	}
	sa.replies = []string{"@delegate bo {\"task\": \"count\"}", "ok"}
	sb.replies = []string{"three"}
	sess, _ := s.Create("", []string{"ana"})
	_, _ = s.Send(context.Background(), sess.ID, "go")
	now := time.Now()
	days := s.HandOverDays(7, now)
	if len(days) != 7 || days[6].Day != now.Format("2006-01-02") || days[6].Count != 1 || days[6].Failed != 0 {
		t.Fatalf("days = %+v", days)
	}
	// Per kitty: ana asked it, bo took it on.
	if by := days[6].By; by["a1"].Count != 1 || by["b1"].Count != 1 || len(by) != 2 {
		t.Errorf("by kitty = %+v", by)
	}
	for _, d := range days[:6] {
		if d.Count != 0 {
			t.Errorf("a day before has hand-overs: %+v", d)
		}
	}
	// A week later, today's hand-over is out of the chart.
	for _, d := range s.HandOverDays(3, now.AddDate(0, 0, 7)) {
		if d.Count != 0 {
			t.Errorf("old hand-over still counted: %+v", d)
		}
	}
	// One that failed counts as failed.
	s.mu.Lock()
	for _, c := range s.sessions {
		for i := range c.Turns {
			for j := range c.Turns[i].Delegations {
				c.Turns[i].Delegations[j].Error = "boom"
			}
		}
	}
	s.mu.Unlock()
	if d := s.HandOverDays(1, now)[0]; d.Failed != 1 || d.By["b1"].Failed != 1 {
		t.Errorf("failed = %+v", d)
	}
}

// A kitty that fails several subtasks in a row is reported to the lead, even when the lead asked.
func TestFailStreakAlertsTheLead(t *testing.T) {
	f, _, _, _ := fleet(t)
	s := service(t, f)
	b := &bell{}
	s.SetNotifier(b)
	s.HandOverWait = 0
	f.kitties["b1"].Lead = true
	lead, _ := f.Get("bo")
	ana, _ := f.Get("ana")
	sess, _ := s.Create("", []string{"bo"})
	for i := 0; i < FailStreakAlert-1; i++ {
		s.handOverFailed(ana, errors.New("did not finish in time"), sess.ID)
	}
	rid, _ := s.ReportsChat(lead)
	if rep, _ := s.Get(rid); len(rep.Turns) != 0 {
		t.Fatalf("alerted too soon: %+v", rep.Turns)
	}
	s.handOverFailed(ana, errors.New("did not finish in time"), sess.ID)
	rep, _ := s.Get(rid)
	if len(rep.Turns) != 1 || !strings.Contains(rep.Turns[0].Text, "failed 3 subtasks in a row") {
		t.Fatalf("reports = %+v", rep.Turns)
	}
	if !strings.Contains(b.events(), "delegated") {
		t.Errorf("the person was not told: %s", b.events())
	}
	// A subtask done ends the streak.
	s.handOverDone(ana)
	s.handOverFailed(ana, errors.New("x"), sess.ID)
	if rep, _ := s.Get(rid); len(rep.Turns) != 1 {
		t.Errorf("alerted after a reset: %d", len(rep.Turns))
	}
}
