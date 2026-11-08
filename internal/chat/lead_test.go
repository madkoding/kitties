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
	"github.com/madkoding/kitties/internal/approvals"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/notify"
	"github.com/madkoding/kitties/internal/provider"
	"github.com/madkoding/kitties/internal/soul"
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

// limitsFleet is the fake fleet that can change a kitty's limits, as the orchestrator does.
type limitsFleet struct{ *fakeFleet }

func (f limitsFleet) SetTaskLimits(ref string, t kitty.TaskLimits) (*kitty.Kitty, error) {
	k, err := f.Get(ref)
	if err != nil {
		return nil, err
	}
	k.Limits = t.Apply(k.Limits)
	return k, nil
}

func TestLeadSetsLimits(t *testing.T) {
	ff, _, sb, _ := fleet(t)
	f := limitsFleet{ff}
	s := service(t, f)
	j, _ := withAgenda(t, s)
	ana, _ := f.Get("ana")
	bo, _ := f.Get("bo")
	// Nobody leads: nobody gets the tool, and the line is refused.
	if s.canSetLimits(bo) {
		t.Fatal("only the lead")
	}
	sb.replies = []string{"@task_limits ana {\"max_rounds\": 60}", "ok"}
	c, _ := s.Create("", []string{"bo"})
	if _, err := s.Send(context.Background(), c.ID, "go"); err != nil {
		t.Fatal(err)
	}
	if ana.Limits.MaxRounds != 0 {
		t.Fatalf("a kitty that does not lead changed ana's rounds: %d", ana.Limits.MaxRounds)
	}

	bo.Lead = true
	if !s.canSetLimits(bo) || s.canSetLimits(ana) {
		t.Fatal("the lead, and only it, may")
	}
	found := false
	for _, tool := range s.toolSet(bo, nil) {
		found = found || tool.Name == "task_limits"
		if tool.Name == "delegate" {
			if _, ok := tool.Parameters["properties"].(map[string]any)["max_rounds"]; !ok {
				t.Error("the lead's delegate takes limits for one task")
			}
		}
	}
	if !found {
		t.Error("the lead is offered the task_limits tool")
	}
	sb.replies = []string{"@task_limits ana {\"max_rounds\": 60, \"max_tokens\": 500000, \"max_seconds\": 1800}", "Gave ana more."}
	if _, err := s.Send(context.Background(), c.ID, "ana needs more"); err != nil {
		t.Fatal(err)
	}
	if l := ana.Limits; l.MaxRounds != 60 || l.MaxTokens != 500000 || l.MaxSeconds != 1800 {
		t.Fatalf("limits = %+v", l)
	}
	if kinds(j, "a1") != "limit" || !strings.Contains(kinds(j, "b1"), "limit") {
		t.Errorf("journals: ana %s, bo %s", kinds(j, "a1"), kinds(j, "b1"))
	}
	reports, _ := s.ReportsChat(bo)
	rep, _ := s.Get(reports)
	if last := rep.Turns[len(rep.Turns)-1]; last.Report == nil || last.Report.Event != "limits" || !strings.Contains(last.Text, "rounds default → 60, tokens default → 500000, seconds default → 1800") {
		t.Errorf("reported = %+v", last)
	}
	if p := sb.seen[len(sb.seen)-1][0].Content; !strings.Contains(p, "@task_limits") || !strings.Contains(p, "for that task alone with delegate") {
		t.Errorf("lead prompt names the tools: %s", p)
	}

	// Refused: past a cap, itself, nobody, nothing to change.
	for line, want := range map[string]string{
		"@task_limits ana {\"max_rounds\": 9999}":    "1 to 500",
		"@task_limits ana {\"max_tokens\": 9999999}": "1 to 2000000",
		"@task_limits ana {\"max_seconds\": 99999}":  "1 to 7200",
		"@task_limits bo {\"max_rounds\": 10}":       "not its own",
		"@task_limits zed {\"max_rounds\": 10}":      "no such kitty",
		"@task_limits ana {}":                        "say what to change",
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
	if o := s.runTaskLimits(c.ID, action{kind: "task_limits", name: "ana", args: map[string]any{"max_rounds": "0"}}, bo, &Turn{}); o.err != "" || ana.Limits.MaxRounds != 0 || ana.Limits.MaxTokens != 500000 {
		t.Errorf("back to the default: %+v %+v", o, ana.Limits)
	}
	if a := fromCalls([]provider.ToolCall{{ID: "1", Name: "task_limits", Args: map[string]any{"kitty": "ana", "max_rounds": 30.0}}}); a[0].kind != "task_limits" || a[0].name != "ana" {
		t.Errorf("native call = %+v", a[0])
	}
}

func TestLeadLimitsForOneTask(t *testing.T) {
	ff, _, sb, _ := fleet(t)
	f := limitsFleet{ff}
	s := service(t, f)
	j, _ := withAgenda(t, s)
	ana, _ := f.Get("ana")
	bo, _ := f.Get("bo")
	bo.Lead = true
	ana.Limits.MaxRounds = 5
	c, _ := s.Create("", []string{"bo"})
	// While ana works on the task, it has the rounds the lead gave it; then its own again.
	var during kitty.Limits
	ff.brains["a1"] = probe(func() {
		s.mu.RLock()
		var ids []string
		for id := range s.overrides {
			ids = append(ids, id)
		}
		s.mu.RUnlock()
		for _, id := range ids {
			during = s.limitsFor(ana, id)
		}
	})
	sb.replies = []string{"@delegate ana {\"task\": \"a long one\", \"max_rounds\": 200}", "ana did it."}
	if _, err := s.Send(context.Background(), c.ID, "do the long one"); err != nil {
		t.Fatal(err)
	}
	if during.MaxRounds != 200 {
		t.Errorf("during the task = %+v", during)
	}
	if ana.Limits.MaxRounds != 5 || len(s.overrides) != 0 {
		t.Errorf("after the task: %+v, %d overrides", ana.Limits, len(s.overrides))
	}
	got, _ := s.Get(c.ID)
	if d := got.Turns[1].Delegations; len(d) != 1 || !strings.Contains(d[0].Limits, "for this task only, ana's rounds 5 → 200") {
		t.Errorf("delegation = %+v", got.Turns[1].Delegations)
	}
	if kinds(j, "a1") != "limit" {
		t.Errorf("ana's journal = %s", kinds(j, "a1"))
	}
	// Past the cap, refused; and a kitty that does not lead cannot give more.
	sb.replies = []string{"@delegate ana {\"task\": \"x\", \"max_rounds\": 9999}", "ok"}
	_, _ = s.Send(context.Background(), c.ID, "again")
	if msgs := sb.seen[len(sb.seen)-1]; !strings.Contains(msgs[len(msgs)-1].Content, "1 to 500") {
		t.Errorf("past the cap: %s", msgs[len(msgs)-1].Content)
	}
	bo.Lead = false
	sb.replies = []string{"@delegate ana {\"task\": \"x\", \"max_rounds\": 300}", "ok"}
	_, _ = s.Send(context.Background(), c.ID, "once more")
	got, _ = s.Get(c.ID)
	if d := got.Turns[len(got.Turns)-1].Delegations; len(d) == 1 && d[0].Limits != "" {
		t.Errorf("only the lead gives more: %+v", d)
	}
}

func TestLeadLimitsWaitForAYes(t *testing.T) {
	ff, _, _, _ := fleet(t)
	f := limitsFleet{ff}
	s := service(t, f)
	b := &bell{}
	s.SetNotifier(b)
	withAgenda(t, s)
	store, err := approvals.Open(filepath.Join(t.TempDir(), "approvals.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.SetApprovals(store)
	ask := true
	s.SetLeadLimitsAsk(func() bool { return ask })
	ana, _ := f.Get("ana")
	bo, _ := f.Get("bo")
	bo.Lead = true
	c, _ := s.Create("", []string{"bo"})
	var turn Turn
	o := s.runTaskLimits(c.ID, action{kind: "task_limits", name: "ana", args: map[string]any{"max_rounds": 120.0}}, bo, &turn)
	if !strings.Contains(o.text, "held") || ana.Limits.MaxRounds != 0 || len(turn.Approvals) != 1 {
		t.Fatalf("held = %+v, limits %+v", o, ana.Limits)
	}
	if b.events() != "approval" {
		t.Errorf("the person is told: %s", b.events())
	}
	if _, err := s.DecideApproval(context.Background(), turn.Approvals[0].ID, true, "", false); err != nil {
		t.Fatal(err)
	}
	if ana.Limits.MaxRounds != 120 {
		t.Errorf("approved = %+v", ana.Limits)
	}
	got, _ := s.Get(c.ID)
	if last := got.Turns[len(got.Turns)-1]; !strings.Contains(last.Text, "Approved and done: bo changed ana's limits per task: rounds default → 120") {
		t.Errorf("the thread hears it: %q", last.Text)
	}
	// A no changes nothing; with ask off, the change is made at once.
	turn = Turn{}
	s.runTaskLimits(c.ID, action{kind: "task_limits", name: "ana", args: map[string]any{"max_rounds": 7.0}}, bo, &turn)
	if _, err := s.DecideApproval(context.Background(), turn.Approvals[0].ID, false, "too few", false); err != nil || ana.Limits.MaxRounds != 120 {
		t.Errorf("rejected: %v %+v", err, ana.Limits)
	}
	ask = false
	if o := s.runTaskLimits(c.ID, action{kind: "task_limits", name: "ana", args: map[string]any{"max_rounds": 7.0}}, bo, &Turn{}); o.err != "" || ana.Limits.MaxRounds != 7 {
		t.Errorf("auto: %+v %+v", o, ana.Limits)
	}
}

// probe is a brain that calls fn each time it is asked, and answers "done".
type probe func()

func (p probe) Complete(context.Context, []provider.Message) (provider.Reply, error) {
	p()
	return provider.Reply{Content: "done"}, nil
}
func (p probe) Describe() string { return "probe" }

func TestHandOversToSeveralKittiesRunAtOnce(t *testing.T) {
	f, sa, _, _ := fleet(t)
	var mu sync.Mutex
	in := 0
	both := make(chan struct{})
	together := 0
	// Each helper waits for the other: in a row, the first would wait in vain.
	meet := func() {
		mu.Lock()
		in++
		if in == 2 {
			close(both)
		}
		mu.Unlock()
		select {
		case <-both:
			mu.Lock()
			together++
			mu.Unlock()
		case <-time.After(2 * time.Second):
		}
	}
	for _, name := range []string{"cy", "di"} {
		k := &kitty.Kitty{ID: name + "1", Name: name, Soul: soul.Default(name)}
		f.kitties[k.ID] = k
		f.brains[k.ID] = probe(meet)
	}
	s := service(t, f)
	sa.replies = []string{"@delegate cy {\"task\": \"one\"}\n@delegate di {\"task\": \"two\"}", "both done"}
	sess, _ := s.Create("", []string{"ana"})
	turns, err := s.Send(context.Background(), sess.ID, "split it")
	if err != nil {
		t.Fatal(err)
	}
	d := turns[0].Delegations
	if together != 2 || len(d) != 2 || d[0].Kitty != "cy" || d[1].Kitty != "di" {
		t.Fatalf("together = %d, delegations = %+v", together, d)
	}
}

func TestHandOversWaitTheirTurn(t *testing.T) {
	f, sa, _, _ := fleet(t)
	var mu sync.Mutex
	running, most := 0, 0
	var queuedSeen int
	work := func() {
		mu.Lock()
		running++
		if running > most {
			most = running
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		running--
		mu.Unlock()
	}
	for _, name := range []string{"cy", "di", "ed"} {
		k := &kitty.Kitty{ID: name + "1", Name: name, Soul: soul.Default(name)}
		f.kitties[k.ID] = k
		f.brains[k.ID] = probe(work)
	}
	s := service(t, f)
	s.SetParallel(func() int { return 1 })
	sess, _ := s.Create("", []string{"ana"})
	events, stop := s.Subscribe(sess.ID)
	defer stop()
	go func() {
		for range events {
			if c, err := s.Get(sess.ID); err == nil {
				for _, h := range c.Helping {
					if h.Queued {
						mu.Lock()
						queuedSeen++
						mu.Unlock()
					}
				}
			}
		}
	}()
	sa.replies = []string{"@delegate cy {\"task\": \"one\"}\n@delegate di {\"task\": \"two\"}\n@delegate ed {\"task\": \"three\"}", "all done"}
	turns, err := s.Send(context.Background(), sess.ID, "split it")
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if most != 1 || len(turns[0].Delegations) != 3 || queuedSeen == 0 {
		t.Fatalf("at most %d at once, %d delegations, queued seen %d", most, len(turns[0].Delegations), queuedSeen)
	}
	if c, _ := s.Get(sess.ID); len(c.Helping) != 0 {
		t.Fatalf("still helping: %+v", c.Helping)
	}
	// Unset, a few run at once; zero or less is one.
	s.SetParallel(nil)
	if s.parallelOr() != DefaultParallel {
		t.Error("default")
	}
	s.SetParallel(func() int { return 0 })
	if s.parallelOr() != 1 {
		t.Error("at least one")
	}
}

func TestLeadSummaries(t *testing.T) {
	f, _, sb, _ := fleet(t)
	s := service(t, f)
	ctx := context.Background()
	if _, err := s.LeadSummary(ctx); !errors.Is(err, ErrNoLead) {
		t.Fatalf("no lead: %v", err)
	}
	now := time.Date(2026, 11, 8, 17, 59, 0, 0, time.UTC)
	var sent []notify.Notification
	l := &LeadSummaries{Chats: s, At: func() (int, int, bool) { return 18, 0, true }, Path: filepath.Join(t.TempDir(), "s.json"),
		Now:     func() time.Time { return now },
		Deliver: func(_ context.Context, n notify.Notification) error { sent = append(sent, n); return nil }}
	// No lead: nothing to ask.
	now = now.Add(2 * time.Minute)
	if ok, err := l.Tick(ctx); ok || err != nil {
		t.Fatalf("no lead: %v %v", ok, err)
	}
	f.kitties["b1"].Lead = true
	now = time.Date(2026, 11, 8, 17, 59, 0, 0, time.UTC)
	if ok, _ := l.Tick(ctx); ok {
		t.Fatal("asked before its time")
	}
	now = now.Add(2 * time.Minute)
	sb.replies = []string{"- ana: inbox done\n- nothing waits for you"}
	if ok, err := l.Tick(ctx); !ok || err != nil {
		t.Fatalf("at its time: %v %v", ok, err)
	}
	if len(sent) != 1 || sent[0].Kitty != "bo" || sent[0].Event != notify.Summary || !strings.Contains(sent[0].Body, "inbox done") {
		t.Fatalf("sent = %+v", sent)
	}
	c, _ := s.Get(sent[0].Chat)
	if !c.Reports || c.Turns[0].Text != SummaryAsk {
		t.Fatalf("thread = %+v", c)
	}
	// Once a day, even after a restart.
	if ok, _ := l.Tick(ctx); ok {
		t.Fatal("asked twice")
	}
	l2 := &LeadSummaries{Chats: s, At: l.At, Path: l.Path, Now: l.Now}
	if ok, _ := l2.Tick(ctx); ok {
		t.Fatal("asked again after a restart")
	}
	// Too late in the day, or turned off: not asked.
	now = now.Add(24*time.Hour + SummaryWindow + time.Minute)
	if ok, _ := l.Tick(ctx); ok {
		t.Fatal("asked too late")
	}
	l.At = func() (int, int, bool) { return 0, 0, false }
	if ok, _ := l.Tick(ctx); ok {
		t.Fatal("asked while off")
	}
}

// The news of one chat that come close together are one report: a hand-over that ended and the
// goal that ended through it do not read as two messages saying the same thing.
func TestReportsFoldTheNewsOfOneChat(t *testing.T) {
	f, _, _, _ := fleet(t)
	s := service(t, f)
	now := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	f.kitties["b1"].Lead = true
	lead, _ := f.Get("bo")
	ana, _ := f.Get("ana")
	s.report(lead, ana, notify.Delegated, "finished a subtask", "counted", "c1")
	s.report(lead, ana, notify.Delegated, "finished a subtask", "counted", "c1") // the same again
	now = now.Add(time.Minute)
	s.report(lead, lead, notify.GoalDone, "finished a goal", "through ana", "c1")
	id, _ := s.ReportsChat(lead)
	rep, _ := s.Get(id)
	if len(rep.Turns) != 1 || rep.Turns[0].Text != "finished a subtask: counted\nbo: finished a goal: through ana" || rep.Turns[0].Report.Folded != 1 {
		t.Fatalf("one report = %+v", rep.Turns)
	}
	// Another chat, or the same one later, is a report of its own.
	s.report(lead, ana, notify.GoalDone, "finished a goal", "x", "c2")
	now = now.Add(FoldReportsWithin + time.Second)
	s.report(lead, ana, notify.GoalDone, "finished a goal", "y", "c2")
	if rep, _ = s.Get(id); len(rep.Turns) != 3 {
		t.Errorf("reports = %d", len(rep.Turns))
	}
}
