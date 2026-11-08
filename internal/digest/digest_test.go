package digest

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/agenda"
	"github.com/madkoding/kitties/internal/budget"
	"github.com/madkoding/kitties/internal/notify"
)

type rig struct {
	r     *Reporter
	cfg   notify.Config
	sent  []notify.Notification
	fail  error
	clock time.Time
	path  string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	g := &rig{cfg: notify.Config{Connector: "phone", Digest: "09:00"}, clock: time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC), path: filepath.Join(t.TempDir(), "digest.json")}
	at := func(h int) time.Time { return g.clock.Add(-time.Duration(h) * time.Hour) }
	g.r = New(Source{
		Kitties: func() []Kitty { return []Kitty{{"b1", "tom"}, {"a1", "mimi"}} },
		Journal: func(id string, n int) []agenda.Entry {
			if id == "a1" {
				return []agenda.Entry{
					{At: at(1), Kind: agenda.KindDone}, {At: at(2), Kind: agenda.KindDone}, {At: at(3), Kind: agenda.KindBlocked},
					{At: at(4), Kind: agenda.KindApproval, Text: "approved: slack.post"},
					{At: at(5), Kind: agenda.KindApproval, Text: "waiting for approval: slack.post"},
					{At: at(30), Kind: agenda.KindDone}, // older than a day: not counted
				}
			}
			return nil
		},
		Waiting: func() []string { return g.waiting() },
		Today: func(id string) budget.Day {
			if id == "a1" {
				return budget.Day{Tokens: 12400, Actions: 3}
			}
			return budget.Day{}
		},
		Config: func() notify.Config { return g.cfg },
		Deliver: func(_ context.Context, n notify.Notification) error {
			if g.fail != nil {
				return g.fail
			}
			g.sent = append(g.sent, n)
			return nil
		},
	}, g.path)
	g.r.now = func() time.Time { return g.clock }
	return g
}

func (g *rig) waiting() []string { return []string{"slack.post: hello"} }

func TestBuildCountsTheLastDay(t *testing.T) {
	g := newRig(t)
	n := g.r.Build(g.clock)
	if n.Event != notify.Digest || n.Title == "" {
		t.Fatalf("n = %+v", n)
	}
	want := "mimi: 2 done, 1 blocked, 1 actions out (12.4k tokens, 3 actions today)\ntom: quiet\nWaiting for your yes (1):\n- slack.post: hello"
	if n.Body != want {
		t.Errorf("body =\n%s\nwant\n%s", n.Body, want)
	}
}

func TestBuildTrimsLongListsAndEmptyFleets(t *testing.T) {
	g := newRig(t)
	g.r.src.Waiting = func() []string { return []string{"1", "2", "3", "4", "5", "6", "7"} }
	if b := g.r.Build(g.clock).Body; !strings.Contains(b, "(7):") || !strings.Contains(b, "- and 2 more") || strings.Contains(b, "- 6") {
		t.Errorf("body = %s", b)
	}
	g.r.src.Kitties = func() []Kitty { return nil }
	g.r.src.Waiting = func() []string { return nil }
	if b := g.r.Build(g.clock).Body; b != "No kitties yet." {
		t.Errorf("empty = %q", b)
	}
	for in, want := range map[int]string{5: "5", 1500: "1.5k", 2_500_000: "2.5M"} {
		if short(in) != want {
			t.Errorf("short(%d) = %s", in, short(in))
		}
	}
}

func TestTickSendsOncePerDayInsideTheWindow(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	g.clock = time.Date(2026, 3, 2, 8, 59, 0, 0, time.UTC)
	if sent, _ := g.r.Tick(ctx); sent {
		t.Fatal("before the time")
	}
	g.clock = time.Date(2026, 3, 2, 9, 5, 0, 0, time.UTC)
	if sent, err := g.r.Tick(ctx); !sent || err != nil || len(g.sent) != 1 {
		t.Fatalf("at the time: %v %v", sent, err)
	}
	if sent, _ := g.r.Tick(ctx); sent {
		t.Fatal("not twice in a day")
	}
	// A restart does not repeat it.
	again := New(g.r.src, g.path)
	again.now = g.r.now
	if sent, _ := again.Tick(ctx); sent {
		t.Fatal("a restart repeated the summary")
	}
	// Tomorrow it is due again; too late in the day it is not sent at all.
	g.clock = time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC)
	if sent, _ := g.r.Tick(ctx); !sent {
		t.Fatal("tomorrow")
	}
	g.clock = time.Date(2026, 3, 4, 20, 0, 0, 0, time.UTC)
	if sent, _ := g.r.Tick(ctx); sent {
		t.Fatal("turned on late in the evening: nothing yesterday's")
	}
}

func TestTickWhenOffBroken(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	g.cfg.Digest = ""
	if sent, _ := g.r.Tick(ctx); sent {
		t.Fatal("off")
	}
	g.cfg = notify.Config{Digest: "09:00"}
	if sent, _ := g.r.Tick(ctx); sent {
		t.Fatal("no channel")
	}
	g.cfg = notify.Config{Connector: "phone", Digest: "09:00"}
	g.fail = errors.New("ntfy is down")
	if sent, err := g.r.Tick(ctx); sent || err == nil {
		t.Fatalf("a failed send is reported: %v %v", sent, err)
	}
	g.fail = nil
	if sent, err := g.r.Tick(ctx); !sent || err != nil {
		t.Fatalf("and tried again: %v %v", sent, err)
	}
}

func TestSendNowAndRun(t *testing.T) {
	g := newRig(t)
	g.cfg.Connector = ""
	if err := g.r.SendNow(context.Background()); err == nil {
		t.Fatal("needs a channel")
	}
	g.cfg.Connector = "phone"
	if err := g.r.SendNow(context.Background()); err != nil || len(g.sent) != 1 {
		t.Fatalf("send now: %v", err)
	}
	// Run ticks at once, reports a failure and stops with the context.
	g.fail = errors.New("down")
	g.clock = time.Date(2026, 3, 9, 9, 1, 0, 0, time.UTC)
	logged := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { g.r.Run(ctx, func(f string, a ...any) { logged <- f }); close(done) }()
	if msg := <-logged; !strings.HasPrefix(msg, "digest:") {
		t.Errorf("log = %q", msg)
	}
	cancel()
	<-done
	// Without a logger a failure is quietly kept for the next tick.
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	g.r.Run(ctx2, nil)
}

func TestFollowUpsInTheSummary(t *testing.T) {
	now := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	at := func(h int) time.Time { return now.Add(-time.Duration(h) * time.Hour) }
	// Newest first, as the journal gives them.
	entries := []agenda.Entry{
		{At: at(1), Kind: agenda.KindRun, Goal: "g3", Text: "Report (follow_up)"},
		{At: at(2), Kind: agenda.KindRetry, Goal: "g2", Text: "Groups: blocked; no follow-up left, it waits for its schedule, an event or you"},
		{At: at(3), Kind: agenda.KindBlocked, Goal: "g2", Text: "stuck"},
		{At: at(4), Kind: agenda.KindRun, Goal: "g2", Text: "Groups (follow_up)"},
		{At: at(5), Kind: agenda.KindDone, Goal: "g1", Text: "ok"},
		{At: at(6), Kind: agenda.KindRun, Goal: "g1", Text: "Inbox (follow_up)"},
		{At: at(7), Kind: agenda.KindBlocked, Goal: "g1", Text: "first run"},
		{At: at(8), Kind: agenda.KindRun, Goal: "g1", Text: "Inbox (schedule)"},
		{At: at(9), Kind: agenda.KindDone, Text: "a chat, no goal"},
		{At: at(30), Kind: agenda.KindRun, Goal: "g9", Text: "Old (follow_up)"},
	}
	if got := followUps(entries, at(24)); got != "Inbox → done, Groups → blocked (gave up), Report → running" {
		t.Errorf("follow-ups = %q", got)
	}
	if followUps(entries[4:8], at(24)) != "Inbox → done" || followUps(nil, at(24)) != "" {
		t.Error("partial / empty")
	}
	g := newRig(t)
	g.r.src.Journal = func(string, int) []agenda.Entry { return entries }
	if body := g.r.Build(now).Body; !strings.Contains(body, "mimi: 2 done, 2 blocked, 1 retried (12.4k tokens, 3 actions today)\n  follow-ups: Inbox → done, Groups → blocked (gave up), Report → running\n") {
		t.Errorf("body = %s", body)
	}
}

func TestBuildSaysWhatAKittyDidOnItsDesktop(t *testing.T) {
	g := newRig(t)
	var since time.Time
	g.r.src.Plans = func(id string, from time.Time, n int) []string {
		since = from
		if id != "a1" {
			return nil
		}
		return []string{"I pay.", "I fill in the form.", "I open the shop.", "I look for tickets."}[:n]
	}
	b := g.r.Build(g.clock).Body
	if !strings.Contains(b, "\n  on its desktop: I open the shop; I fill in the form; I pay; and more\n") || !since.Equal(g.clock.Add(-24*time.Hour)) {
		t.Errorf("body = %s (since %v)", b, since)
	}
	g.r.src.Plans = func(string, time.Time, int) []string { return []string{"I open the shop."} }
	if b := g.r.Build(g.clock).Body; !strings.Contains(b, "on its desktop: I open the shop\n") || strings.Contains(b, "and more") {
		t.Errorf("one plan = %s", b)
	}
}

// The summary lists the scheduled tasks to look at, each with the link to its runs.
func TestBuildListsTheTasksToLookAt(t *testing.T) {
	g := newRig(t)
	g.r.src.Waiting = func() []string { return nil }
	g.r.src.Attention = func() []Task {
		return []Task{{Title: "Inbox", Kitty: "mimi", Result: "blocked", Chat: "c1"}, {Title: "Prices", Kitty: "mimi", Result: "failed"}}
	}
	g.r.src.Link = func(chat string) string { return "https://k.example/#chat/" + chat }
	b := g.r.Build(g.clock).Body
	if !strings.HasSuffix(b, "\nScheduled tasks to look at (2):\n- Inbox (mimi): blocked https://k.example/#chat/c1\n- Prices (mimi): failed") {
		t.Errorf("body = %s", b)
	}
	// None to look at: nothing is said.
	g.r.src.Attention = func() []Task { return nil }
	if b := g.r.Build(g.clock).Body; strings.Contains(b, "look at") {
		t.Errorf("body = %s", b)
	}
}
