package notify

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/connector"
)

// sink is a connector that remembers what it was asked.
type sink struct {
	mu    sync.Mutex
	calls []map[string]any
	kind  string
	err   error
}

func (s *sink) Describe() string { return "sink" }
func (s *sink) Kind() string     { return s.kind }
func (s *sink) Call(_ context.Context, a map[string]any) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, a)
	return "ok", s.err
}
func (s *sink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func setup(cfg Config, kind string) (*Notifier, *sink) {
	reg := connector.NewRegistry()
	sk := &sink{kind: kind}
	reg.Add("push", sk)
	hook, _ := connector.Build("webhook", connector.Settings{"url": "http://hook.invalid"}, "")
	reg.Add("hook", hook)
	n := New(reg, func() Config { return cfg }, func() string { return "https://kitties.example.com/" }, nil)
	return n, sk
}

func TestWants(t *testing.T) {
	for _, tc := range []struct {
		cfg  Config
		e    Event
		want bool
	}{
		{Config{}, Help, false},
		{Config{Connector: "push"}, Help, true},
		{Config{Connector: "push"}, Approval, true},
		{Config{Connector: "push"}, GoalDone, false},
		{Config{Connector: "push", On: []string{"goal_done", "recovered"}}, GoalDone, true},
		{Config{Connector: "push", On: []string{"goal_done"}}, Help, false},
	} {
		if got := tc.cfg.Wants(tc.e); got != tc.want {
			t.Errorf("%+v %s = %v", tc.cfg, tc.e, got)
		}
	}
}

func TestValidate(t *testing.T) {
	reg := connector.NewRegistry()
	reg.Add("push", &sink{})
	if err := (Config{Connector: "push", On: []string{"help", "recovered"}}).Validate(reg); err != nil {
		t.Error(err)
	}
	if err := (Config{}).Validate(reg); err != nil {
		t.Error("off is valid")
	}
	if err := (Config{Connector: "nope"}).Validate(reg); err == nil || !strings.Contains(err.Error(), "no such connector") {
		t.Errorf("unknown connector: %v", err)
	}
	if err := (Config{Connector: "nope"}).Validate(nil); err != nil {
		t.Error("without a registry only the events are checked")
	}
	if err := (Config{Connector: "push", On: []string{"boom"}}).Validate(reg); err == nil || !strings.Contains(err.Error(), "help, approval, goal_done") {
		t.Errorf("unknown event: %v", err)
	}
}

func TestNotifyPushAndText(t *testing.T) {
	n, sk := setup(Config{Connector: "push"}, "ntfy")
	n.Notify(Notification{Event: Help, Kitty: "mimi", Title: "needs your help", Body: "stopped after 3 rounds", Chat: "c1"})
	n.Wait()
	if sk.count() != 1 {
		t.Fatalf("calls = %d", sk.count())
	}
	c := sk.calls[0]
	if c["title"] != "mimi: needs your help" || c["text"] != "stopped after 3 rounds" || c["click"] != "https://kitties.example.com/#chat/c1" || c["priority"] != "4" {
		t.Errorf("a push carries the title, the link and a priority apart: %v", c)
	}
	// Events the config does not want, and a config with no connector, send nothing.
	n.Notify(Notification{Event: GoalDone, Kitty: "mimi", Title: "done"})
	off, sk2 := setup(Config{}, "ntfy")
	off.Notify(Notification{Event: Help, Title: "x"})
	off.Wait()
	n.Wait()
	if sk.count() != 1 || sk2.count() != 0 {
		t.Error("unwanted events are not sent")
	}
	// Another kind of connector gets one text with the link, and a calm event a calm priority.
	t2, sk3 := setup(Config{Connector: "push", On: []string{"goal_done"}}, "slack")
	t2.Notify(Notification{Event: GoalDone, Kitty: "luna", Title: "Inbox check finished", Body: "nothing new", Chat: "c9"})
	t2.Wait()
	if c := sk3.calls[0]; c["text"] != "luna: Inbox check finished\nnothing new\nhttps://kitties.example.com/#chat/c9" || c["title"] != nil || c["subject"] != "luna: Inbox check finished" {
		t.Errorf("text = %v", c)
	}
	t3, sk4 := setup(Config{Connector: "push", On: []string{"recovered"}}, "ntfy")
	t3.Notify(Notification{Event: Recovered, Title: "back", Body: "b"})
	t3.Wait()
	if c := sk4.calls[0]; c["priority"] != nil || c["click"] != "https://kitties.example.com" || c["title"] != "back" {
		t.Errorf("calm push = %v", c)
	}
}

func TestQuietFailuresAndTest(t *testing.T) {
	var logged []string
	var mu sync.Mutex
	n, sk := setup(Config{Connector: "push"}, "ntfy")
	n.logf = func(f string, a ...any) { mu.Lock(); logged = append(logged, f); mu.Unlock() }
	now := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	n.now = func() time.Time { return now }
	note := Notification{Event: Approval, Kitty: "mimi", Title: "slack.send waits", Body: "hi"}
	n.Notify(note)
	n.Notify(note) // the same one just went out
	n.Wait()
	if sk.count() != 1 {
		t.Errorf("held back: %d", sk.count())
	}
	now = now.Add(11 * time.Minute)
	n.Notify(note)
	n.Notify(Notification{Event: Approval, Kitty: "mimi", Title: "another"})
	n.Wait()
	if sk.count() != 3 {
		t.Errorf("after the quiet time: %d", sk.count())
	}
	// The table of what went out is kept small.
	for i := 0; i < 1100; i++ {
		n.last["k"+string(rune(i))] = now.Add(-time.Hour)
	}
	now = now.Add(time.Hour)
	n.Notify(Notification{Event: Approval, Title: "sweep"})
	n.Wait()
	if len(n.last) > 10 {
		t.Errorf("stale entries are swept: %d", len(n.last))
	}
	// A connector that fails is logged, not fatal; a connector that cannot carry text is refused.
	sk.err = context.DeadlineExceeded
	n.Notify(Notification{Event: Help, Title: "fails", Body: "x"})
	n.Wait()
	mu.Lock()
	if len(logged) != 1 {
		t.Errorf("logged = %v", logged)
	}
	mu.Unlock()
	sk.err = nil
	if err := n.Test(context.Background(), "push"); err != nil || sk.count() < 5 {
		t.Errorf("Test = %v", err)
	}
	if err := n.Test(context.Background(), "hook"); err == nil || !strings.Contains(err.Error(), "cannot carry") {
		t.Errorf("a webhook is no notification channel: %v", err)
	}
	sk.err = context.Canceled
	if err := n.Test(context.Background(), "push"); err == nil {
		t.Error("Test reports a failure")
	}
	if err := n.Test(context.Background(), "nope"); err == nil || !strings.Contains(err.Error(), "no such connector") {
		t.Errorf("Test unknown: %v", err)
	}
	// Links.
	if n.Link("") != "https://kitties.example.com" || n.Link("c1") != "https://kitties.example.com/#chat/c1" {
		t.Error("Link")
	}
	bare := New(connector.NewRegistry(), func() Config { return Config{} }, nil, nil)
	if bare.Link("c1") != "" {
		t.Error("no public address, no link")
	}
}
