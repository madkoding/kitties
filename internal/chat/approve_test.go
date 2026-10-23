package chat

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/madkoding/kitties/internal/agenda"
	"github.com/madkoding/kitties/internal/approvals"
	"github.com/madkoding/kitties/internal/connector"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/notify"
)

// bell is a notifier that remembers what it was told.
type bell struct {
	mu   sync.Mutex
	seen []notify.Notification
}

func (b *bell) Notify(n notify.Notification) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seen = append(b.seen, n)
}

func (b *bell) events() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for _, n := range b.seen {
		out = append(out, string(n.Event))
	}
	return strings.Join(out, ",")
}

// gated is a service whose kitty ana, in "ask" mode, may use a webhook (an action that reaches
// people) and a shell connector, over a stub that counts the posts it receives.
func gated(t *testing.T) (*Service, *scripted, *int, *bell, *agenda.Journal) {
	t.Helper()
	f, sa, _, _ := fleet(t)
	posts := 0
	var mu sync.Mutex
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		mu.Lock()
		posts++
		mu.Unlock()
		_, _ = w.Write([]byte("posted " + string(data)))
	}))
	t.Cleanup(stub.Close)
	hook, err := connector.Build("webhook", connector.Settings{"url": stub.URL}, "posts to the team")
	if err != nil {
		t.Fatal(err)
	}
	shell, _ := connector.Build("shell", connector.Settings{"command": "true"}, "")
	f.conns.Add("hook", hook)
	f.conns.Add("sh", shell)
	ana, _ := f.Get("ana")
	ana.Connectors = []string{"echo", "hook", "sh"}
	ana.Approvals = approvals.ModeAsk
	s := service(t, f)
	store, err := approvals.Open(filepath.Join(t.TempDir(), "approvals.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.SetApprovals(store)
	b := &bell{}
	s.SetNotifier(b)
	j, _ := withAgenda(t, s)
	return s, sa, &posts, b, j
}

func TestActionsWaitForApproval(t *testing.T) {
	s, sa, posts, b, j := gated(t)
	sa.replies = []string{"I will tell the team.\n@connector hook {\"text\": \"We ship Friday\"}", "It is waiting for your yes."}
	sess, _ := s.Create("", []string{"ana"})
	turns, err := s.Send(context.Background(), sess.ID, "tell the team")
	if err != nil {
		t.Fatal(err)
	}
	turn := turns[0]
	if *posts != 0 || len(turn.Approvals) != 1 || turn.Approvals[0].Connector != "hook" || turn.Approvals[0].Action != "post" || !strings.Contains(turn.Approvals[0].Summary, "We ship Friday") || len(turn.Calls) != 0 {
		t.Fatalf("held = posts %d, turn %+v", *posts, turn)
	}
	// The kitty is told it was held, and not to repeat it.
	fed := sa.seen[1][len(sa.seen[1])-1].Content
	if !strings.Contains(fed, "held: the person has to approve this first") || !strings.Contains(fed, "Do not repeat it") {
		t.Errorf("feedback = %q", fed)
	}
	pending, _ := s.ListApprovals(approvals.Pending)
	if len(pending) != 1 || pending[0].KittyName != "ana" || pending[0].Chat != sess.ID || pending[0].Risk != "ask" {
		t.Fatalf("pending = %+v", pending)
	}
	if b.events() != "approval" || b.seen[0].Chat != sess.ID || b.seen[0].Kitty != "ana" {
		t.Errorf("the person was told: %v", b.seen)
	}
	if kinds(j, "a1") != "approval" {
		t.Errorf("journal = %s", kinds(j, "a1"))
	}
	// A yes runs it and puts the result in the thread.
	learned := []approvals.Approval{}
	s.learnFromApproval = func(a approvals.Approval) { learned = append(learned, a) }
	done, err := s.DecideApproval(context.Background(), pending[0].ID, true, "", false)
	if err != nil || done.Status != approvals.Approved || *posts != 1 || !strings.Contains(done.Result, "We ship Friday") {
		t.Fatalf("approve = %+v %v posts=%d", done, err, *posts)
	}
	got, _ := s.Get(sess.ID)
	last := got.Turns[len(got.Turns)-1]
	if last.From != "approval" || !strings.Contains(last.Text, "Approved and done: hook.post") || len(last.Calls) != 1 || len(learned) != 1 {
		t.Errorf("thread = %+v", last)
	}
	if kinds(j, "a1") != "approval,approval" {
		t.Errorf("journal = %s", kinds(j, "a1"))
	}
	// Deciding twice and deciding what does not exist are refused.
	if _, err := s.DecideApproval(context.Background(), pending[0].ID, true, "", false); err == nil {
		t.Error("decided twice")
	}
	// A no tells the thread why, and the kitty reads it as the next message in the thread.
	sa.replies = []string{"@connector hook {\"text\": \"Too casual one\"}", "Waiting."}
	_, _ = s.Send(context.Background(), sess.ID, "and another")
	p2, _ := s.ListApprovals(approvals.Pending)
	no, err := s.DecideApproval(context.Background(), p2[0].ID, false, "too casual", false)
	if err != nil || no.Status != approvals.Rejected || *posts != 1 {
		t.Fatalf("reject = %+v %v posts=%d", no, err, *posts)
	}
	got, _ = s.Get(sess.ID)
	if last := got.Turns[len(got.Turns)-1]; !strings.Contains(last.Text, "Rejected: hook.post") || !strings.Contains(last.Text, "Reason: too casual") {
		t.Errorf("rejection = %+v", last)
	}
	sa.replies = []string{"Understood, I will be more formal."}
	_, _ = s.Send(context.Background(), sess.ID, "ok")
	seen := sa.seen[len(sa.seen)-1]
	if body := seen[len(seen)-1].Content; !strings.Contains(body, "[approval]: Rejected") || !strings.Contains(body, "Reason: too casual") {
		t.Errorf("the kitty reads the reason: %q", body)
	}
	// A failing action says so.
	sa.replies = []string{"@connector hook {\"text\": \"x\"}", "ok"}
	_, _ = s.Send(context.Background(), sess.ID, "again")
	p3, _ := s.ListApprovals(approvals.Pending)
	s.fleet.Connectors().Remove("hook")
	failed, _ := s.DecideApproval(context.Background(), p3[0].ID, true, "", false)
	if failed.Error == "" {
		t.Errorf("a failed action: %+v", failed)
	}
	got, _ = s.Get(sess.ID)
	if last := got.Turns[len(got.Turns)-1]; !strings.Contains(last.Text, "Approved, but it failed") {
		t.Errorf("failure turn = %+v", last)
	}
}

func TestTrustAutoModeAndAlwaysAsked(t *testing.T) {
	s, sa, posts, _, j := gated(t)
	ana, _ := s.fleet.Get("ana")
	s.SetDemand(func(string) int { return 1 }) // a relaxed person: two yeses in a row
	sess, _ := s.Create("", []string{"ana"})
	say := func(text string) {
		sa.replies = []string{"@connector hook {\"text\": \"" + text + "\"}", "done"}
		if _, err := s.Send(context.Background(), sess.ID, "go"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		say("one " + string(rune('a'+i)))
		p, _ := s.ListApprovals(approvals.Pending)
		if len(p) != 1 {
			t.Fatalf("round %d: pending %d", i, len(p))
		}
		_, _ = s.DecideApproval(context.Background(), p[0].ID, true, "", false)
	}
	before := *posts
	say("on trust")
	if p, _ := s.ListApprovals(approvals.Pending); len(p) != 0 || *posts != before+1 {
		t.Errorf("trusted: pending %d posts %d (was %d)", len(p), *posts, before)
	}
	if !strings.Contains(kinds(j, "a1"), "approval") {
		t.Errorf("journal = %s", kinds(j, "a1"))
	}
	entries := j.Recent("a1", 1)
	if !strings.Contains(entries[0].Text, "went ahead on its own (trusted: 2 approvals in a row)") {
		t.Errorf("the journal says why: %q", entries[0].Text)
	}
	// A demanding person: the same history is not enough.
	s.SetDemand(func(string) int { return 5 })
	say("strict")
	if p, _ := s.ListApprovals(approvals.Pending); len(p) != 1 {
		t.Error("the strictest person is always asked")
	}
	s.SetDemand(func(string) int { return 99 }) // nonsense falls back to the middle
	if s.demandOf(ana) != 3 {
		t.Error("demandOf")
	}
	s.SetDemand(nil)
	if s.demandOf(ana) != 3 {
		t.Error("no demand source")
	}
	// Auto mode acts without asking, but never for what is always asked.
	_ = s.approvals.Forget("a1")
	ana.Approvals = "auto"
	before = *posts
	say("auto")
	if *posts != before+1 {
		t.Error("auto mode posts at once")
	}
	// Nothing is always asked: the shell the person set up runs too.
	sa.replies = []string{"@connector sh {\"text\": \"rm\"}", "ok"}
	_, _ = s.Send(context.Background(), sess.ID, "shell")
	if p, _ := s.ListApprovals(approvals.Pending); len(p) != 0 {
		t.Fatalf("nothing is held in auto mode: %+v", p)
	}
	// Rules the person made can be listed and taken back.
	ana.Approvals = approvals.ModeAsk
	say("for good")
	p, _ := s.ListApprovals(approvals.Pending)
	if _, err := s.DecideApproval(context.Background(), p[0].ID, true, "", true); err != nil {
		t.Fatal(err)
	}
	rules, _ := s.ApprovalRules()
	if len(rules) != 1 || rules[0].Mode != "auto" {
		t.Fatalf("rules = %+v", rules)
	}
	before = *posts
	say("no asking now")
	if *posts != before+1 {
		t.Error("allowed for good")
	}
	if err := s.RemoveApprovalRule(rules[0].ID); err != nil {
		t.Fatal(err)
	}
	say("asked again")
	if p, _ := s.ListApprovals(approvals.Pending); len(p) != 1 {
		t.Error("asked again after the rule was taken back")
	}
}

func TestNoLimitUnlessSet(t *testing.T) {
	s, sa, posts, _, j := gated(t)
	ana, _ := s.fleet.Get("ana")
	ana.Approvals = "" // the default: the service limits the account, not Kitties
	sess, _ := s.Create("", []string{"ana"})
	sa.replies = []string{"@connector hook {\"text\": \"hello\"}", "Posted."}
	if _, err := s.Send(context.Background(), sess.ID, "post hello"); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.ListApprovals(approvals.Pending); len(p) != 0 || *posts != 1 {
		t.Fatalf("acted at once: pending %d posts %d", len(p), *posts)
	}
	if strings.Contains(kinds(j, "a1"), "approval") {
		t.Error("an action nothing limits is not noted as a decision")
	}
	// Asked to, the kitty limits itself: ask first for posts, never for the shell.
	sa.replies = []string{"Sure.\n@limit set {\"connector\": \"hook\", \"action\": \"post\", \"mode\": \"ask\"}\n" +
		"@limit set {\"connector\": \"sh\", \"mode\": \"never\"}", "From now on I will ask first."}
	turns, err := s.Send(context.Background(), sess.ID, "ask me before posting, and never use the shell")
	if err != nil {
		t.Fatal(err)
	}
	// The turn says which limits it set, so the page can show them and take them back.
	if l := turns[0].Limits; len(l) != 2 || l[0] != (LimitRef{Connector: "hook", Action: "post", Mode: "ask"}) || l[1] != (LimitRef{Connector: "sh", Action: "*", Mode: "never"}) {
		t.Errorf("limits on the turn = %+v", l)
	}
	rules, _ := s.ApprovalRules()
	if len(rules) != 2 || rules[0].KittyName != "ana" {
		t.Fatalf("rules = %+v", rules)
	}
	fed := sa.seen[len(sa.seen)-1]
	if body := fed[len(fed)-1].Content; !strings.Contains(body, "from now on, post on hook waits for the person's yes") || !strings.Contains(body, "any action on sh is never done") {
		t.Errorf("the kitty is told what it set: %q", body)
	}
	sa.replies = []string{"@connector hook {\"text\": \"again\"}\n@connector sh {\"text\": \"ls\"}", "Waiting."}
	_, _ = s.Send(context.Background(), sess.ID, "post again and list files")
	if p, _ := s.ListApprovals(approvals.Pending); len(p) != 1 || *posts != 1 {
		t.Errorf("the post waits: pending %d posts %d", len(p), *posts)
	}
	fed = sa.seen[len(sa.seen)-1]
	if body := fed[len(fed)-1].Content; !strings.Contains(body, "not done: the person asked you never to do this") {
		t.Errorf("the shell is refused: %q", body)
	}
	// And lifts a limit when asked; nonsense is refused.
	sa.replies = []string{"@limit set {\"connector\": \"sh\", \"mode\": \"none\"}\n@limit set {\"mode\": \"ask\"}\n@limit set {\"connector\": \"nope\", \"mode\": \"ask\"}", "ok"}
	lifted, _ := s.Send(context.Background(), sess.ID, "you may use the shell again")
	if l := lifted[0].Limits; len(l) != 1 || l[0] != (LimitRef{Connector: "sh", Action: "*", Mode: "", Previous: "never"}) {
		t.Errorf("a limit lifted keeps what it was = %+v", l)
	}
	if rules, _ := s.ApprovalRules(); len(rules) != 1 {
		t.Errorf("the shell limit is gone: %+v", rules)
	}
	fed = sa.seen[len(sa.seen)-1]
	if body := fed[len(fed)-1].Content; !strings.Contains(body, "the limit on sh is lifted") || !strings.Contains(body, "which connection") || !strings.Contains(body, "not one of your connections") {
		t.Errorf("limit answers = %q", body)
	}
}

func TestSetApprovalRule(t *testing.T) {
	s, _, _, _, _ := gated(t)
	r, err := s.SetApprovalRule("ana", "hook", "post", "never")
	if err != nil || r.KittyName != "ana" || r.Kitty != "a1" || r.Mode != "never" {
		t.Fatalf("set = %+v %v", r, err)
	}
	if _, err := s.SetApprovalRule("nobody", "hook", "", "ask"); err == nil {
		t.Error("unknown kitty")
	}
	if _, err := (&Service{}).SetApprovalRule("ana", "hook", "", "ask"); err != ErrNoApprovals {
		t.Error("no store")
	}
}

func TestNoApprovalsAndReads(t *testing.T) {
	// Without a store nothing is held and the calls say so.
	f, sa, _, _ := fleet(t)
	s := service(t, f)
	if _, err := s.ListApprovals(""); err != ErrNoApprovals {
		t.Error("list")
	}
	if _, err := s.ApprovalRules(); err != ErrNoApprovals {
		t.Error("rules")
	}
	if s.RemoveApprovalRule("x") != ErrNoApprovals {
		t.Error("remove")
	}
	if _, err := s.DecideApproval(context.Background(), "x", true, "", false); err != ErrNoApprovals {
		t.Error("decide")
	}
	// A connector that is not of the catalogue (a test double) is never held; a read is not either.
	sa.replies = []string{"@connector echo go", "done"}
	sess, _ := s.Create("", []string{"ana"})
	if turns, _ := s.Send(context.Background(), sess.ID, "go"); len(turns[0].Approvals) != 0 || len(turns[0].Calls) != 1 {
		t.Errorf("echo runs: %+v", turns[0])
	}
	s2, _, _, _, _ := gated(t)
	// An unknown decision id and a thread that is gone are harmless.
	if _, err := s2.DecideApproval(context.Background(), "zz", true, "", false); err == nil {
		t.Error("unknown id")
	}
	s2.appendTurn("gone", Turn{})
	s2.appendTurn("", Turn{})
	if summaryLine(" a \n b  c ") != "a b c" {
		t.Error("summaryLine")
	}
	ana := &kitty.Kitty{ID: "x"}
	s2.tell(notify.Help, nil, "t", "b", "c")
	s2.notifier = nil
	s2.tell(notify.Help, ana, "t", "b", "c")
}

func TestHelpAndGoalNotifications(t *testing.T) {
	s, _, sa := deskService(t, "@tool click {\"x\": 1, \"y\": 1}", "@tool click {\"x\": 2, \"y\": 2}")
	_ = sa
	b := &bell{}
	s.SetNotifier(b)
	j, r := withAgenda(t, s)
	_ = j
	ana, _ := s.fleet.Get("ana")
	ana.Limits = kitty.Limits{MaxRounds: 1}
	sess, _ := s.Create("", []string{"ana"})
	_, _ = s.Send(context.Background(), sess.ID, "go")
	got := waitDone(t, s, sess.ID)
	if !got.NeedsHelp() || b.events() != "help" || b.seen[0].Title != "needs your help" || !strings.Contains(b.seen[0].Body, "stopped after 1 rounds") {
		t.Errorf("help: %v %v", b.seen, got.Turns[1].Text)
	}
	// Goals: done, blocked, delegated and escalated results.
	f, sb, _, _ := fleet(t)
	s2 := service(t, f)
	b2 := &bell{}
	s2.SetNotifier(b2)
	withAgenda(t, s2)
	ana2, _ := f.Get("ana")
	sb.replies = []string{"Checked."}
	_, _ = s2.RunGoal(context.Background(), agenda.Goal{ID: "g1", Kitty: ana2.ID, Title: "Inbox", Text: "look"}, agenda.Trigger{Kind: "schedule"})
	sb.err = context.DeadlineExceeded
	_, _ = s2.RunGoal(context.Background(), agenda.Goal{ID: "g1", Kitty: ana2.ID, Title: "Inbox", Text: "look"}, agenda.Trigger{Kind: "manual"})
	s2.reportGoal(ana2, "g1", "c", "delegated", "bo did it")
	s2.reportGoal(ana2, "g1", "c", "escalated", "asked")
	if b2.events() != "goal_done,goal_blocked,goal_done" {
		t.Errorf("goal events = %s", b2.events())
	}
	_ = r
}
