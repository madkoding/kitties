package chat

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/agenda"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/provider"
)

// reports remembers what the chats reported about goals.
type reports struct {
	mu   sync.Mutex
	seen []string
}

func (r *reports) Report(goal, result string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, goal+"="+result)
}

func (r *reports) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seen...)
}

// withAgenda gives a service a journal and a reporter.
func withAgenda(t *testing.T, s *Service) (*agenda.Journal, *reports) {
	t.Helper()
	j := agenda.NewJournal(filepath.Join(t.TempDir(), "journal"))
	r := &reports{}
	s.SetAgenda(j, r)
	return j, r
}

func kinds(j *agenda.Journal, kitty string) string {
	var out []string
	for _, e := range j.Recent(kitty, 0) {
		out = append(out, e.Kind)
	}
	// Recent is newest first; the story reads better oldest first.
	for i, k := 0, len(out)-1; i < k; i, k = i+1, k-1 {
		out[i], out[k] = out[k], out[i]
	}
	return strings.Join(out, ",")
}

func TestRetryAnotherWay(t *testing.T) {
	// One round per attempt: the first attempt runs out of rounds, the second finishes.
	s, _, sa := deskService(t, "@tool click {\"x\": 1, \"y\": 1}", "@tool click {\"x\": 2, \"y\": 2}", "@tool done {\"summary\": \"found it the other way\"}")
	j, _ := withAgenda(t, s)
	ana, _ := s.fleet.Get("ana")
	ana.Limits = kitty.Limits{MaxRounds: 1, Retries: 1}
	sess, _ := s.Create("", []string{"ana"})
	if _, err := s.Send(context.Background(), sess.ID, "go"); err != nil {
		t.Fatal(err)
	}
	got := waitDone(t, s, sess.ID)
	turn := got.Turns[1]
	if !strings.Contains(turn.Text, "(attempt 1 failed: stopped after 1 rounds without finishing; trying another way)") || !strings.Contains(turn.Text, "found it the other way") || turn.NeedsHelp {
		t.Errorf("turn = %+v", turn)
	}
	// The model was told to try something else, without a screenshot in between.
	last := sa.seen[len(sa.seen)-1]
	if msg := last[len(last)-1]; msg.Role != "user" || !strings.Contains(msg.Content, "Try a different approach") || len(msg.Images) != 0 {
		t.Errorf("retry message = %+v", msg)
	}
	if kinds(j, "a1") != "retry" {
		t.Errorf("journal = %s", kinds(j, "a1"))
	}
	// Out of retries and with nobody to hand over to, the person is asked.
	s2, _, _ := deskService(t, "@tool click {\"x\": 1, \"y\": 1}", "@tool click {\"x\": 2, \"y\": 2}", "@tool click {\"x\": 3, \"y\": 3}")
	j2, _ := withAgenda(t, s2)
	ana2, _ := s2.fleet.Get("ana")
	ana2.Limits = kitty.Limits{MaxRounds: 1, Retries: 1}
	sess2, _ := s2.Create("", []string{"ana"})
	_, _ = s2.Send(context.Background(), sess2.ID, "go")
	got2 := waitDone(t, s2, sess2.ID)
	if !got2.NeedsHelp() || !got2.Help || !strings.Contains(got2.Turns[1].Text, "(ana needs help: stopped after 1 rounds") {
		t.Errorf("escalated turn = %+v", got2.Turns[1])
	}
	if list := s2.List(); len(list) != 1 || !list[0].Help {
		t.Error("the list says who needs help")
	}
	if kinds(j2, "a1") != "retry,escalated" {
		t.Errorf("journal = %s", kinds(j2, "a1"))
	}
}

func TestBudgets(t *testing.T) {
	// Tokens: every reply costs three; a budget of two is spent on the first round.
	s, _, _ := deskService(t, "@tool click {\"x\": 1, \"y\": 1}", "@tool click {\"x\": 2, \"y\": 2}", "@tool done {}")
	ana, _ := s.fleet.Get("ana")
	ana.Limits = kitty.Limits{MaxTokens: 2, Retries: 3}
	sess, _ := s.Create("", []string{"ana"})
	_, _ = s.Send(context.Background(), sess.ID, "go")
	got := waitDone(t, s, sess.ID)
	if !strings.Contains(got.Turns[1].Text, "the token budget (2) is spent") || strings.Contains(got.Turns[1].Text, "attempt") || !got.NeedsHelp() {
		t.Errorf("tokens: %+v", got.Turns[1])
	}
	// Time: a display that never answers, one second to work.
	s2, d, _ := deskService(t, "@tool click {\"x\": 1, \"y\": 1}", "@tool done {}")
	d.block = make(chan struct{})
	ana2, _ := s2.fleet.Get("ana")
	ana2.Limits = kitty.Limits{MaxSeconds: 1, Retries: 3}
	sess2, _ := s2.Create("", []string{"ana"})
	_, _ = s2.Send(context.Background(), sess2.ID, "go")
	got2 := waitDone(t, s2, sess2.ID)
	if !strings.Contains(got2.Turns[1].Text, "the time budget (1s) is spent") || !got2.NeedsHelp() {
		t.Errorf("time: %+v", got2.Turns[1])
	}
	// A cancel by a person is final: no retry, no hand-over, no cry for help.
	s3, d3, _ := deskService(t, "@tool click {\"x\": 1, \"y\": 1}", "@tool done {}")
	d3.block = make(chan struct{})
	ana3, _ := s3.fleet.Get("ana")
	ana3.Limits = kitty.Limits{Retries: 3}
	ana3.Backup = "bo"
	sess3, _ := s3.Create("", []string{"ana"})
	_, _ = s3.Send(context.Background(), sess3.ID, "go")
	for !s3.Cancel(sess3.ID) {
		time.Sleep(time.Millisecond)
	}
	got3 := waitDone(t, s3, sess3.ID)
	if !strings.Contains(got3.Turns[1].Text, "the task was cancelled") || got3.NeedsHelp() || len(got3.Turns[1].Delegations) != 0 {
		t.Errorf("cancel: %+v", got3.Turns[1])
	}
	if stopReason(context.Background(), kitty.Limits{}) != "the task was cancelled" {
		t.Error("stopReason without a deadline")
	}
}

func TestBackupTakesOver(t *testing.T) {
	s, _, _ := deskService(t, "@tool click {\"x\": 1, \"y\": 1}", "@tool click {\"x\": 2, \"y\": 2}")
	j, _ := withAgenda(t, s)
	f := s.fleet.(*deskFleet)
	f.brains["b1"].(*scripted).replies = []string{"Done: I counted three."}
	ana, _ := s.fleet.Get("ana")
	ana.Limits = kitty.Limits{MaxRounds: 1}
	ana.Backup = "bo"
	sess, _ := s.Create("", []string{"ana"})
	_, _ = s.Send(context.Background(), sess.ID, "count the things")
	got := waitDone(t, s, sess.ID)
	turn := got.Turns[1]
	if got.NeedsHelp() || !strings.Contains(turn.Text, "Handed over to bo, who answered:\nDone: I counted three.") || len(turn.Delegations) != 1 || turn.Delegations[0].Kitty != "bo" || turn.Delegations[0].Result != "Done: I counted three." {
		t.Errorf("turn = %+v", turn)
	}
	// bo got the story in a thread of its own, opened by ana.
	var handover *Session
	for _, c := range s.List() {
		if c.Owner == "a1" {
			handover, _ = s.Get(c.ID)
		}
	}
	if handover == nil || handover.Title != "ana → bo" || handover.Depth != 1 || len(handover.Turns) != 2 || handover.Turns[0].From != "ana" || handover.Turns[0].Kitty != "a1" || !strings.Contains(handover.Turns[0].Text, "The task: count the things") {
		t.Fatalf("handover = %+v", handover)
	}
	if kinds(j, "a1") != "delegated" {
		t.Errorf("journal = %s", kinds(j, "a1"))
	}
	// A backup that cannot help: the person is asked, with both failures.
	s2, _, _ := deskService(t, "@tool click {\"x\": 1, \"y\": 1}", "@tool click {\"x\": 2, \"y\": 2}")
	f2 := s2.fleet.(*deskFleet)
	f2.brains["b1"].(*scripted).err = errors.New("bo is down")
	ana2, _ := s2.fleet.Get("ana")
	ana2.Limits = kitty.Limits{MaxRounds: 1}
	ana2.Backup = "bo"
	sess2, _ := s2.Create("", []string{"ana"})
	_, _ = s2.Send(context.Background(), sess2.ID, "go")
	got2 := waitDone(t, s2, sess2.ID)
	if !got2.NeedsHelp() || !strings.Contains(got2.Turns[1].Text, "bo could not take over") || len(got2.Turns[1].Delegations) != 1 || got2.Turns[1].Delegations[0].Error == "" {
		t.Errorf("failed hand-over = %+v", got2.Turns[1])
	}
	// A backup that is gone, or the kitty itself: the person is asked.
	for _, backup := range []string{"nobody", "ana"} {
		s3, _, _ := deskService(t, "@tool click {\"x\": 1, \"y\": 1}", "@tool click {\"x\": 2, \"y\": 2}")
		ana3, _ := s3.fleet.Get("ana")
		ana3.Limits = kitty.Limits{MaxRounds: 1}
		ana3.Backup = backup
		sess3, _ := s3.Create("", []string{"ana"})
		_, _ = s3.Send(context.Background(), sess3.ID, "go")
		if got3 := waitDone(t, s3, sess3.ID); !got3.NeedsHelp() {
			t.Errorf("backup %q: %+v", backup, got3.Turns[1])
		}
	}
}

func TestDelegateAction(t *testing.T) {
	f, sa, sb, _ := fleet(t)
	s := service(t, f)
	j, _ := withAgenda(t, s)
	sa.replies = []string{"Let me ask bo.\n@delegate bo {\"task\": \"count to three\"}", "bo counted; here is the answer."}
	sb.replies = []string{"one, two, three"}
	sess, _ := s.Create("", []string{"ana"})
	turns, err := s.Send(context.Background(), sess.ID, "how many?")
	if err != nil {
		t.Fatal(err)
	}
	turn := turns[0]
	if turn.Text != "Let me ask bo.\n\nbo counted; here is the answer." || len(turn.Delegations) != 1 || turn.Delegations[0].Kitty != "bo" || turn.Delegations[0].Task != "count to three" || turn.Delegations[0].Result != "one, two, three" {
		t.Errorf("turn = %+v", turn)
	}
	// ana saw bo's answer; bo saw ana's request labelled by name, and knows ana as a peer too.
	fed := sa.seen[1][len(sa.seen[1])-1].Content
	if !strings.Contains(fed, "bo answered:") || !strings.Contains(fed, "one, two, three") {
		t.Errorf("feedback = %q", fed)
	}
	if !strings.Contains(sb.seen[0][len(sb.seen[0])-1].Content, "[ana]: count to three") || !strings.Contains(sb.seen[0][0].Content, "hand a subtask to: ana") {
		t.Errorf("bo saw = %q", sb.seen[0][len(sb.seen[0])-1].Content)
	}
	if kinds(j, "a1") != "delegated" {
		t.Errorf("journal = %s", kinds(j, "a1"))
	}
	// The same pair reuses their thread.
	sa.replies = []string{"@delegate bo {\"task\": \"and to four\"}", "ok"}
	sb.replies = []string{"four"}
	_, _ = s.Send(context.Background(), sess.ID, "more")
	threads := 0
	for _, c := range s.List() {
		if c.Owner == "a1" {
			threads++
			if full, _ := s.Get(c.ID); len(full.Turns) != 4 {
				t.Errorf("reused thread has %d turns", len(full.Turns))
			}
		}
	}
	if threads != 1 {
		t.Errorf("threads = %d", threads)
	}
	// The refusals.
	for _, tc := range []struct{ reply, want string }{
		{"@delegate zed {\"task\": \"x\"}", "no such kitty zed"},
		{"@delegate ana {\"task\": \"x\"}", "cannot delegate to itself"},
		{"@delegate bo {}", "say what to do"},
		{"@delegate bo plain words", "bo answered"}, // words alone are the task
	} {
		sa.replies = []string{tc.reply, "end"}
		sb.replies = []string{"fine"}
		_, _ = s.Send(context.Background(), sess.ID, "again")
		if fed := sa.seen[len(sa.seen)-1][len(sa.seen[len(sa.seen)-1])-1].Content; !strings.Contains(fed, tc.want) {
			t.Errorf("%s: feedback = %q", tc.reply, fed)
		}
	}
	// Without a name (a native call missing it) the action says so.
	var tr Turn
	if o := s.delegate(context.Background(), sess.ID, action{kind: "delegate"}, func() *kitty.Kitty { k, _ := f.Get("ana"); return k }(), &tr); !strings.Contains(o.err, "say which kitty") {
		t.Errorf("no name: %+v", o)
	}
	// Too deep: a thread already handed over as many times as allowed cannot go further.
	s.MaxDepth = 1
	deep, _ := s.Create("deep", []string{"ana"})
	s.mu.Lock()
	s.sessions[deep.ID].Depth = 1
	s.mu.Unlock()
	sa.replies = []string{"@delegate bo {\"task\": \"x\"}", "end"}
	_, _ = s.Send(context.Background(), deep.ID, "go")
	if fed := sa.seen[len(sa.seen)-1][len(sa.seen[len(sa.seen)-1])-1].Content; !strings.Contains(fed, "handed over 1 times already") {
		t.Errorf("depth: %q", fed)
	}
	if sa.seen[len(sa.seen)-1][0].Content == "" || strings.Contains(sa.seen[len(sa.seen)-1][0].Content, "hand a subtask") {
		t.Error("at the limit the prompt names no peers")
	}
	// With no other kitty, or no depth at all, there is nothing to delegate to.
	s.MaxDepth = 0
	if ana, _ := f.Get("ana"); s.canDelegate(ana) || len(s.toolSet(ana, nil)) == 0 {
		t.Error("MaxDepth 0 turns delegation off")
	}
	s.MaxDepth = 2
	alone := &fakeFleet{kitties: map[string]*kitty.Kitty{"a1": f.kitties["a1"]}, brains: f.brains, conns: f.conns}
	if s2 := service(t, alone); s2.canDelegate(f.kitties["a1"]) || s2.peers(f.kitties["a1"], "x") != nil {
		t.Error("alone, no delegation")
	}
	// Native: the tool is offered and its call is a delegate action.
	ana, _ := f.Get("ana")
	found := false
	for _, tool := range s.toolSet(ana, nil) {
		found = found || tool.Name == "delegate"
	}
	if !found {
		t.Error("the delegate tool is offered")
	}
	if a := fromCalls([]provider.ToolCall{{ID: "1", Name: "delegate", Args: map[string]any{"kitty": "bo", "task": "x"}}}); a[0].kind != "delegate" || a[0].name != "bo" {
		t.Errorf("native delegate = %+v", a[0])
	}
}

func TestDelegateWaitsForTheDesktop(t *testing.T) {
	// bo (no desktop) hands work to ana, who goes to her display and finishes later.
	s, _, sa := deskService(t, "On it.\n@tool click {\"x\": 1, \"y\": 1}", "@tool done {\"summary\": \"counted four\"}")
	f := s.fleet.(*deskFleet)
	sb := f.brains["b1"].(*scripted)
	sb.replies = []string{"@delegate ana {\"task\": \"count on your screen\"}", "ana says: counted four."}
	sess, _ := s.Create("", []string{"bo"})
	turns, err := s.Send(context.Background(), sess.ID, "count")
	if err != nil {
		t.Fatal(err)
	}
	if len(turns[0].Delegations) != 1 || !strings.Contains(turns[0].Delegations[0].Result, "counted four") || turns[0].Text != "ana says: counted four." {
		t.Errorf("turn = %+v", turns[0])
	}
	_ = sa
	// ana never finishing in time: the wait ends, her task is cancelled, bo hears why.
	s2, d2, _ := deskService(t, "@tool click {\"x\": 1, \"y\": 1}", "@tool done {}")
	d2.block = make(chan struct{})
	f2 := s2.fleet.(*deskFleet)
	ana2, _ := f2.Get("ana")
	ana2.Limits = kitty.Limits{MaxSeconds: 1}
	sb2 := f2.brains["b1"].(*scripted)
	sb2.replies = []string{"@delegate ana {\"task\": \"x\"}", "end"}
	sess2, _ := s2.Create("", []string{"bo"})
	_, _ = s2.Send(context.Background(), sess2.ID, "go")
	s2.Wait()
	if fed := sb2.seen[1][len(sb2.seen[1])-1].Content; !strings.Contains(fed, "ana: did not finish in time") {
		t.Errorf("timeout feedback = %q", fed)
	}
	// ana giving up on it: bo hears that too.
	s3, _, _ := deskService(t, "@tool click {\"x\": 1, \"y\": 1}", "@tool click {\"x\": 2, \"y\": 2}")
	f3 := s3.fleet.(*deskFleet)
	ana3, _ := f3.Get("ana")
	ana3.Limits = kitty.Limits{MaxRounds: 1}
	sb3 := f3.brains["b1"].(*scripted)
	sb3.replies = []string{"@delegate ana {\"task\": \"x\"}", "end"}
	sess3, _ := s3.Create("", []string{"bo"})
	_, _ = s3.Send(context.Background(), sess3.ID, "go")
	s3.Wait()
	if fed := sb3.seen[1][len(sb3.seen[1])-1].Content; !strings.Contains(fed, "ana: could not do it") {
		t.Errorf("give-up feedback = %q", fed)
	}
	// ana busy with something else: bo is told to try later.
	s4, d4, _ := deskService(t, "@tool click {\"x\": 1, \"y\": 1}", "@tool done {}")
	d4.block = make(chan struct{})
	f4 := s4.fleet.(*deskFleet)
	busy, _ := s4.Create("", []string{"ana"})
	_, _ = s4.Send(context.Background(), busy.ID, "stay busy")
	// bo's hand-over must land in the thread ana is busy in: make that thread the pair's.
	s4.mu.Lock()
	s4.sessions[busy.ID].Owner, s4.sessions[busy.ID].Depth = "b1", 1
	s4.mu.Unlock()
	sb4 := f4.brains["b1"].(*scripted)
	sb4.replies = []string{"@delegate ana {\"task\": \"x\"}", "end"}
	sess4, _ := s4.Create("", []string{"bo"})
	_, _ = s4.Send(context.Background(), sess4.ID, "go")
	if fed := sb4.seen[1][len(sb4.seen[1])-1].Content; !strings.Contains(fed, "ana: is busy") {
		t.Errorf("busy feedback = %q", fed)
	}
	close(d4.block)
	s4.Wait()
}

func TestRunGoal(t *testing.T) {
	f, sa, _, _ := fleet(t)
	s := service(t, f)
	j, r := withAgenda(t, s)
	sa.replies = []string{"Checked the inbox: nothing new."}
	g := agenda.Goal{ID: "g1", Kitty: "a1", Title: "Inbox", Text: "Look at the inbox."}
	id, err := s.RunGoal(context.Background(), g, agenda.Trigger{Kind: "schedule"})
	if err != nil {
		t.Fatal(err)
	}
	sess, _ := s.Get(id)
	if sess.Goal != "g1" || sess.Title != "Inbox" || len(sess.Turns) != 2 || sess.Turns[0].From != "scheduler" || !strings.Contains(sess.Turns[0].Text, "Goal: Inbox\nLook at the inbox.\n\nWhy now: the schedule") || sess.Turns[1].Text != "Checked the inbox: nothing new." {
		t.Errorf("goal chat = %+v", sess)
	}
	if got := r.all(); len(got) != 1 || got[0] != "g1=done" {
		t.Errorf("reports = %v", got)
	}
	if kinds(j, "a1") != "done" {
		t.Errorf("journal = %s", kinds(j, "a1"))
	}
	// The next run lands in the same thread, by the goal's chat or by its id.
	g.Chat = id
	sa.replies = []string{"Still nothing."}
	if id2, _ := s.RunGoal(context.Background(), g, agenda.Trigger{Kind: "manual"}); id2 != id {
		t.Error("the goal's chat is reused")
	}
	g.Chat = "gone"
	sa.replies = []string{"Nothing."}
	if id3, _ := s.RunGoal(context.Background(), g, agenda.Trigger{Kind: "manual"}); id3 != id {
		t.Error("found by goal id")
	}
	if sess, _ = s.Get(id); len(sess.Turns) != 6 {
		t.Errorf("turns = %d", len(sess.Turns))
	}
	// A brain that fails marks the run blocked; an unknown kitty cannot run at all.
	sa.err = errors.New("no key")
	if _, err := s.RunGoal(context.Background(), g, agenda.Trigger{Kind: "manual"}); err != nil {
		t.Errorf("a failed answer is a turn, not an error: %v", err)
	}
	if got := r.all(); got[len(got)-1] != "g1=blocked" {
		t.Errorf("reports = %v", got)
	}
	if _, err := s.RunGoal(context.Background(), agenda.Goal{ID: "g2", Kitty: "zz", Title: "x"}, agenda.Trigger{}); err == nil {
		t.Error("unknown kitty")
	}
	// A goal with a desktop task reports when the task ends.
	ds, _, dsa := deskService(t, "@tool open {\"url\": \"https://x\"}", "@tool done {\"summary\": \"opened it\"}")
	dj, dr := withAgenda(t, ds)
	_ = dsa
	did, err := ds.RunGoal(context.Background(), agenda.Goal{ID: "g3", Kitty: "a1", Title: "Open", Text: "open x"}, agenda.Trigger{Kind: "event", Event: &agenda.Event{Source: "hook", Text: "ping"}})
	if err != nil {
		t.Fatal(err)
	}
	got := waitDone(t, ds, did)
	if !strings.Contains(got.Turns[0].Text, "an event from hook:\nping") || !strings.Contains(got.Turns[1].Text, "opened it") {
		t.Errorf("event goal = %+v", got.Turns)
	}
	if all := dr.all(); len(all) != 1 || all[0] != "g3=done" || kinds(dj, "a1") != "done" {
		t.Errorf("task goal reports = %v journal=%s", all, kinds(dj, "a1"))
	}
	// Without an agenda nothing is written and nothing reported.
	plain := service(t, f)
	plain.note(nil, "x", "", "", "")
	plain.reportGoal(f.kitties["a1"], "g", "c", "done", "")
	if s.lastRequest("nope", 0) != "" || s.lastRequest(id, 0) != "(no request found in the thread)" {
		t.Error("lastRequest edges")
	}
	if clip("abc", 2) != "ab…" || firstLine(" a\nb ") != "a" {
		t.Error("helpers")
	}
}
