package agenda

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixed() time.Time { return time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC) }

func store(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(filepath.Join(t.TempDir(), "goals.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.now = fixed
	n := 0
	s.newID = func() string { n++; return "g" + string(rune('0'+n)) }
	return s
}

func TestStoreCRUD(t *testing.T) {
	s := store(t)
	g, err := s.Add(Goal{Kitty: "k1", Title: " Weekly report ", Every: "@weekly", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if g.ID != "g1" || g.Title != "Weekly report" || g.Text != "Weekly report" || g.Every != "0 9 * * 1" || g.NextRun.Format("2006-01-02 15:04") != "2026-03-16 09:00" || g.Runs != 0 {
		t.Errorf("added = %+v", g)
	}
	if _, err := s.Add(Goal{Kitty: "k1", Title: "no trigger"}); err == nil {
		t.Error("a goal needs a schedule or an event")
	}
	if _, err := s.Add(Goal{Title: "no kitty", Every: "@daily"}); err == nil {
		t.Error("a goal needs a kitty")
	}
	if _, err := s.Add(Goal{Kitty: "k1", Every: "@daily"}); err == nil {
		t.Error("a goal needs a title")
	}
	if _, err := s.Add(Goal{Kitty: "k1", Title: "x", Every: "bad"}); err == nil {
		t.Error("a bad schedule is refused")
	}
	if _, err := s.Add(Goal{Kitty: "k1", Title: "x", On: []string{" "}}); err == nil {
		t.Error("an empty connector is refused")
	}
	ev, _ := s.Add(Goal{Kitty: "k2", Title: "Triage", Text: "Look at the issue", On: []string{" github "}, Enabled: true})
	if ev.On[0] != "github" || !ev.NextRun.IsZero() {
		t.Errorf("event goal = %+v", ev)
	}
	disabled, _ := s.Add(Goal{Kitty: "k1", Title: "Paused", Every: "@daily"})
	if !disabled.NextRun.IsZero() {
		t.Error("a disabled goal has no next run")
	}
	if got, _ := s.Get("g1"); got.Title != "Weekly report" {
		t.Error("Get")
	}
	if _, err := s.Get("zz"); err == nil {
		t.Error("Get unknown")
	}
	if l := s.List(""); len(l) != 3 || l[0].Kitty != "k1" || l[2].Kitty != "k2" {
		t.Errorf("List = %d", len(l))
	}
	if l := s.List("k2"); len(l) != 1 {
		t.Errorf("List k2 = %d", len(l))
	}
	// Update changes what the page edits, keeps the rest, and refreshes the next run.
	up, err := s.Update("g1", Goal{Title: "Daily report", Text: "Write it", Every: "@daily", Enabled: true})
	if err != nil || up.Title != "Daily report" || up.Text != "Write it" || up.NextRun.Format("15:04") != "09:00" || up.NextRun.Day() != 11 || up.Kitty != "k1" || up.ID != "g1" {
		t.Errorf("Update = %+v %v", up, err)
	}
	if _, err := s.Update("g1", Goal{Title: ""}); err == nil {
		t.Error("Update validates")
	}
	if _, err := s.Update("zz", Goal{Title: "x", Every: "@daily"}); err == nil {
		t.Error("Update unknown")
	}
	// Reopening the file finds the same goals.
	again, err := NewStore(s.path)
	if err != nil || len(again.List("")) != 3 {
		t.Fatalf("reopen: %v %d", err, len(again.List("")))
	}
	if err := s.Remove("g2"); err != nil || len(s.List("")) != 2 {
		t.Errorf("Remove: %v", err)
	}
	if err := s.Remove("zz"); err == nil {
		t.Error("Remove unknown")
	}
	if err := s.RemoveOf("k1"); err != nil || len(s.List("")) != 0 {
		t.Errorf("RemoveOf: %v %d", err, len(s.List("")))
	}
	if err := s.RemoveOf("nobody"); err != nil {
		t.Error("RemoveOf nothing is fine")
	}
}

func TestStoreFiles(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(bad, []byte("{not json"), 0o600)
	if _, err := NewStore(bad); err == nil {
		t.Error("a damaged file is an error")
	}
	if _, err := NewStore(filepath.Join(dir, "sub")); err != nil {
		t.Error("a missing file is an empty store")
	}
	_ = os.Mkdir(filepath.Join(dir, "dir"), 0o700)
	if _, err := NewStore(filepath.Join(dir, "dir")); err == nil {
		t.Error("a directory is an error")
	}
	// A store whose file cannot be written refuses the add and keeps nothing.
	s, _ := NewStore(filepath.Join(dir, "dir", "nested", "goals.json"))
	_ = os.Chmod(filepath.Join(dir, "dir"), 0o500)
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "dir"), 0o700) })
	if os.Getuid() != 0 {
		if _, err := s.Add(Goal{Kitty: "k", Title: "x", Every: "@daily"}); err == nil || len(s.List("")) != 0 {
			t.Error("a failed save keeps nothing")
		}
	}
	if randomID() == randomID() || len(randomID()) != 12 {
		t.Error("ids are random")
	}
}

func TestSyncFromFile(t *testing.T) {
	s := store(t)
	own, _ := s.Add(Goal{Kitty: "k1", Title: "Mine", Every: "@daily", Enabled: true})
	if err := s.Sync([]Goal{{Kitty: "k1", Title: "Report", Every: "@weekly"}, {Kitty: "k2", Title: "Triage", On: []string{"github"}}}); err != nil {
		t.Fatal(err)
	}
	list := s.List("")
	if len(list) != 3 {
		t.Fatalf("after sync: %d", len(list))
	}
	var report *Goal
	for _, g := range list {
		if g.Title == "Report" {
			report = g
		}
	}
	if report == nil || !report.FromFile || !report.Enabled || report.NextRun.IsZero() {
		t.Fatalf("report = %+v", report)
	}
	// File goals cannot be edited or removed from the page.
	if _, err := s.Update(report.ID, Goal{Title: "x", Every: "@daily"}); err == nil || !strings.Contains(err.Error(), "configuration") {
		t.Error("file goals are read-only")
	}
	if err := s.Remove(report.ID); err == nil {
		t.Error("file goals stay")
	}
	// A run is remembered across a sync of the same title; a title that is gone is dropped.
	s.started(report.ID, fixed())
	s.attach(report.ID, "chat-1")
	s.Report(report.ID, "done")
	if err := s.Sync([]Goal{{Kitty: "k1", Title: "Report", Every: "@daily"}}); err != nil {
		t.Fatal(err)
	}
	list = s.List("")
	if len(list) != 2 {
		t.Fatalf("after second sync: %d", len(list))
	}
	for _, g := range list {
		switch g.Title {
		case "Report":
			if g.ID != report.ID || g.Runs != 1 || g.Chat != "chat-1" || g.LastResult != "done" || g.Every != "0 9 * * *" {
				t.Errorf("history kept: %+v", g)
			}
		case "Mine":
			if g.ID != own.ID || g.FromFile {
				t.Errorf("own goal untouched: %+v", g)
			}
		default:
			t.Errorf("unexpected %s", g.Title)
		}
	}
	if err := s.Sync([]Goal{{Kitty: "k1", Title: "Bad", Every: "nope"}}); err == nil {
		t.Error("a bad file goal is refused")
	}
}

func TestJournal(t *testing.T) {
	j := NewJournal(filepath.Join(t.TempDir(), "journal"))
	now := fixed()
	j.now = func() time.Time { now = now.Add(time.Minute); return now }
	if j.Recent("k1", 10) != nil {
		t.Error("no journal yet")
	}
	for i := 0; i < 5; i++ {
		if err := j.Add(Entry{Kitty: "k1", Kind: KindRun, Text: "run " + string(rune('a'+i))}); err != nil {
			t.Fatal(err)
		}
	}
	_ = j.Add(Entry{Kitty: "other/../x", Kind: KindDone, Text: "elsewhere", At: fixed()})
	got := j.Recent("k1", 3)
	if len(got) != 3 || got[0].Text != "run e" || got[2].Text != "run c" || got[0].At.Before(got[2].At) {
		t.Errorf("recent = %+v", got)
	}
	if all := j.Recent("k1", 0); len(all) != 5 {
		t.Errorf("all = %d", len(all))
	}
	if got := j.Recent("other/../x", 1); len(got) != 1 || !got[0].At.Equal(fixed()) {
		t.Errorf("odd names are files too: %+v", got)
	}
	if entries, _ := os.ReadDir(j.dir); len(entries) != 2 || strings.Contains(entries[0].Name()+entries[1].Name(), "/") {
		t.Errorf("files = %v", entries)
	}
	// A damaged line is skipped.
	f, _ := os.OpenFile(j.file("k1"), os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString("{broken\n")
	f.Close()
	if all := j.Recent("k1", 0); len(all) != 5 {
		t.Errorf("after damage = %d", len(all))
	}
	if err := j.Forget("k1"); err != nil || j.Recent("k1", 1) != nil {
		t.Error("Forget")
	}
	if err := j.Forget("k1"); err != nil {
		t.Error("Forget twice is fine")
	}
	// A journal that cannot be written says so.
	ro := NewJournal(filepath.Join(j.file("other/../x"), "sub"))
	if err := ro.Add(Entry{Kitty: "k", Text: "x"}); err == nil {
		t.Error("a file is not a directory")
	}
}

func TestSummarise(t *testing.T) {
	gh := `{"action":"opened","issue":{"number":12,"title":"Crash on start","body":"It crashes","html_url":"https://github.com/a/b/issues/12","user":{"login":"pat"}},"repository":{"full_name":"a/b"}}`
	ev := Summarise("github", "issues", []byte(gh))
	for _, want := range []string{"action: opened", "issue #12: Crash on start (by pat)", "It crashes", "https://github.com/a/b/issues/12", "repository: a/b"} {
		if !strings.Contains(ev.Text, want) {
			t.Errorf("github: missing %q in %q", want, ev.Text)
		}
	}
	push := `{"ref":"refs/heads/main","commits":[{},{}],"head_commit":{"message":"fix: the thing"},"repository":{"full_name":"a/b"}}`
	if ev := Summarise("github", "push", []byte(push)); !strings.Contains(ev.Text, "push to refs/heads/main: 2 commits") || !strings.Contains(ev.Text, "fix: the thing") {
		t.Errorf("push: %q", ev.Text)
	}
	slack := `{"event":{"type":"message","text":"deploy please","user":"U1","channel":"C1"}}`
	if ev := Summarise("team-slack", "", []byte(slack)); ev.Kind != "message" || !strings.Contains(ev.Text, "message from U1 in C1: deploy please") {
		t.Errorf("slack: %+v", ev)
	}
	if ev := Summarise("hook", "", []byte(`{"text":"hi","user_name":"ana"}`)); ev.Text != "message from ana: hi" {
		t.Errorf("generic message: %q", ev.Text)
	}
	if ev := Summarise("hook", "", []byte(`{"z":1,"a":{"b":2}}`)); ev.Text != "a: {\"b\":2}\nz: 1" {
		t.Errorf("flat: %q", ev.Text)
	}
	if ev := Summarise("hook", "", []byte("plain text")); ev.Text != "plain text" {
		t.Errorf("plain: %q", ev.Text)
	}
	if ev := Summarise("hook", "", nil); ev.Text != "(an empty event)" {
		t.Errorf("empty: %q", ev.Text)
	}
	if ev := Summarise("hook", "", []byte(`{}`)); ev.Text != "{}" {
		t.Errorf("empty object: %q", ev.Text)
	}
	long := Summarise("hook", "", []byte(strings.Repeat("x", 5000)))
	if len(long.Text) > maxEventText+4 {
		t.Error("long bodies are clipped")
	}
	if ev := Summarise("github", "release", []byte(`{"release":{"tag_name":"v1.0","html_url":"u"}}`)); !strings.Contains(ev.Text, "release: v1.0") {
		t.Errorf("release: %q", ev.Text)
	}
	stripe := `{"type":"invoice.payment_failed","data":{"object":{"id":"in_1","amount_due":25000,"currency":"eur","customer_email":"c@x","status":"open"}}}`
	ev = Summarise("billing", "", []byte(stripe))
	for _, want := range []string{"invoice.payment_failed in_1", "amount due: 250.00 EUR", "customer: c@x", "status: open"} {
		if !strings.Contains(ev.Text, want) {
			t.Errorf("stripe: missing %q in %q", want, ev.Text)
		}
	}
	if ev.Kind != "invoice.payment_failed" {
		t.Errorf("stripe kind = %q", ev.Kind)
	}
	if ev := Summarise("billing", "x", []byte(`{"type":"charge.succeeded","data":{"object":{"amount":999,"currency":"usd"}}}`)); ev.Kind != "x" || !strings.Contains(ev.Text, "amount: 9.99 USD") {
		t.Errorf("stripe payment: %+v", ev)
	}
}

// fakeRunner records the goals it was asked to run.
type fakeRunner struct {
	mu    sync.Mutex
	runs  []Trigger
	goals []Goal
	fail  error
	slow  chan struct{}
}

func (r *fakeRunner) RunGoal(ctx context.Context, g Goal, t Trigger) (string, error) {
	r.mu.Lock()
	r.runs = append(r.runs, t)
	r.goals = append(r.goals, g)
	r.mu.Unlock()
	if r.slow != nil {
		<-r.slow
	}
	if r.fail != nil {
		return "", r.fail
	}
	return "chat-" + g.ID, nil
}

func (r *fakeRunner) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.runs)
}

func TestSchedulerTickFireAndRunNow(t *testing.T) {
	s := store(t)
	j := NewJournal(filepath.Join(t.TempDir(), "j"))
	r := &fakeRunner{}
	sched := New(s, j, r)
	now := fixed()
	sched.now = func() time.Time { return now }
	s.now = sched.now
	daily, _ := s.Add(Goal{Kitty: "k1", Title: "Daily", Every: "0 9 * * *", Enabled: true})
	_, _ = s.Add(Goal{Kitty: "k1", Title: "Issues", On: []string{"github"}, Enabled: true})
	_, _ = s.Add(Goal{Kitty: "k2", Title: "Off", On: []string{"github"}, Enabled: false})
	// Not due yet (next run is tomorrow at nine): nothing happens.
	sched.Tick(context.Background())
	sched.Wait()
	if r.count() != 0 {
		t.Fatal("nothing is due")
	}
	now = now.Add(24 * time.Hour)
	sched.Tick(context.Background())
	sched.Wait()
	if r.count() != 1 || r.runs[0].Kind != "schedule" || r.goals[0].ID != daily.ID {
		t.Fatalf("runs = %+v", r.runs)
	}
	g, _ := s.Get(daily.ID)
	if g.Runs != 1 || g.Chat != "chat-"+daily.ID || !g.LastRun.Equal(now) || g.NextRun.Day() != 12 || g.LastResult != "running" {
		t.Errorf("after the run: %+v", g)
	}
	// The same tick again does not run it twice.
	sched.Tick(context.Background())
	sched.Wait()
	if r.count() != 1 {
		t.Error("a goal runs once per schedule")
	}
	// An event runs the goals listening to its source, and only the enabled ones.
	ids := sched.Fire(context.Background(), Event{Source: "github", Kind: "issues", Text: "action: opened\nissue #1"})
	sched.Wait()
	if len(ids) != 1 || r.count() != 2 || r.runs[1].Kind != "event" || r.runs[1].Event.Text != "action: opened\nissue #1" || r.runs[1].Text() != "an event from github (issues):\naction: opened\nissue #1" {
		t.Errorf("fire = %v runs=%+v", ids, r.runs)
	}
	if sched.Fire(context.Background(), Event{Source: "nobody"}) != nil {
		t.Error("no listener, no run")
	}
	// The journal has the story.
	entries := j.Recent("k1", 0)
	kinds := []string{}
	for _, e := range entries {
		kinds = append(kinds, e.Kind)
	}
	if strings.Join(kinds, ",") != "run,event,run" {
		t.Errorf("journal = %v", kinds)
	}
	// Report closes the run.
	sched.Report(daily.ID, "done")
	if g, _ := s.Get(daily.ID); g.LastResult != "done" {
		t.Error("Report")
	}
	// RunNow runs at once and returns the chat; an unknown goal is an error.
	if chatID, err := sched.RunNow(context.Background(), daily.ID); err != nil || chatID != "chat-"+daily.ID || r.count() != 3 || r.runs[2].Kind != "manual" || r.runs[2].Text() != "a person asked for it now" {
		t.Errorf("RunNow = %q %v", chatID, err)
	}
	if _, err := sched.RunNow(context.Background(), "zz"); err == nil {
		t.Error("RunNow unknown")
	}
	if (Trigger{Kind: "event"}).Text() != "something arrived from a service" || (Trigger{}).Text() != "the schedule" {
		t.Error("trigger texts")
	}
}

func TestSchedulerBusyAndFailures(t *testing.T) {
	s := store(t)
	j := NewJournal(filepath.Join(t.TempDir(), "j"))
	r := &fakeRunner{slow: make(chan struct{})}
	sched := New(s, j, r)
	now := fixed()
	sched.now = func() time.Time { return now }
	s.now = sched.now
	g, _ := s.Add(Goal{Kitty: "k1", Title: "Slow", Every: "* * * * *", On: []string{"hook"}, Enabled: true})
	now = now.Add(time.Minute)
	sched.Tick(context.Background())
	for r.count() == 0 {
		time.Sleep(time.Millisecond)
	}
	// While it runs, the clock and an event skip it (the schedule moves on), and RunNow refuses.
	now = now.Add(time.Minute)
	sched.Tick(context.Background())
	sched.Fire(context.Background(), Event{Source: "hook"})
	if _, err := sched.RunNow(context.Background(), g.ID); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Errorf("RunNow while busy: %v", err)
	}
	if r.count() != 1 {
		t.Errorf("a busy goal is not stacked: %d", r.count())
	}
	if got, _ := s.Get(g.ID); got.Runs != 1 || !got.NextRun.After(now) || got.LastResult != "running" {
		t.Errorf("the schedule moved on without counting a run: %+v", got)
	}
	close(r.slow)
	sched.Wait()
	kinds := []string{}
	for _, e := range j.Recent("k1", 0) {
		kinds = append(kinds, e.Kind)
	}
	if strings.Join(kinds, ",") != "skipped,event,skipped,run" {
		t.Errorf("journal = %v", kinds)
	}
	// A runner that fails: the goal is marked failed and the problem is logged.
	var logged []string
	fail := &fakeRunner{fail: errors.New("no brain")}
	sched2 := New(s, j, fail)
	sched2.SetLog(func(f string, a ...any) { logged = append(logged, f) })
	sched2.now = sched.now
	now = now.Add(time.Minute)
	sched2.Tick(context.Background())
	sched2.Wait()
	if got, _ := s.Get(g.ID); got.LastResult != "failed" || len(logged) != 1 {
		t.Errorf("failure: %+v logged=%v", got, logged)
	}
	if _, err := sched2.RunNow(context.Background(), g.ID); err == nil || !strings.Contains(err.Error(), "no brain") {
		t.Errorf("RunNow failure: %v", err)
	}
	// Run stops when the context ends.
	ctx, cancel := context.WithCancel(context.Background())
	sched.Every = time.Hour
	done := make(chan struct{})
	go func() { sched.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("Run did not stop")
	}
	// Reporting an unknown goal or recording a run of one is harmless.
	s.Report("zz", "done")
	s.started("zz", now)
	s.skip("zz", now)
	s.attach("zz", "c")
	if firstLine("  one\ntwo ") != "one" {
		t.Error("firstLine")
	}
}

func TestAgendaFacade(t *testing.T) {
	dir := t.TempDir()
	r := &fakeRunner{}
	a, err := Open(dir, r)
	if err != nil {
		t.Fatal(err)
	}
	g, err := a.AddGoal(Goal{Kitty: "k1", Title: "Inbox", Every: "@hourly", On: []string{"mail"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := a.Goal(g.ID); got.Title != "Inbox" || len(a.Goals("k1")) != 1 || len(a.Goals("k2")) != 0 {
		t.Error("goals")
	}
	if _, err := a.UpdateGoal(g.ID, Goal{Title: "Mail", Every: "@daily", On: []string{"mail"}, Enabled: true}); err != nil {
		t.Error(err)
	}
	if chatID, err := a.RunNow(context.Background(), g.ID); err != nil || chatID != "chat-"+g.ID {
		t.Errorf("RunNow = %q %v", chatID, err)
	}
	a.Report(g.ID, "done")
	if ids := a.Fire(context.Background(), Event{Source: "mail", Text: "new"}); len(ids) != 1 {
		t.Errorf("Fire = %v", ids)
	}
	a.Scheduler.Wait()
	_ = a.Note(Entry{Kitty: "k1", Kind: KindRecovered, Text: "back"})
	if got := a.Recent("k1", 1); len(got) != 1 || got[0].Kind != KindRecovered {
		t.Errorf("recent = %+v", got)
	}
	if err := a.RemoveGoal(g.ID); err != nil || len(a.Goals("")) != 0 {
		t.Error("RemoveGoal")
	}
	_, _ = a.AddGoal(Goal{Kitty: "k1", Title: "x", Every: "@daily"})
	var forgot []string
	a.OnForget = []func(string) error{func(k string) error { forgot = append(forgot, k); return nil }}
	if err := a.Forget("k1"); err != nil || len(a.Goals("")) != 0 || a.Recent("k1", 1) != nil || len(forgot) != 1 || forgot[0] != "k1" {
		t.Error("Forget")
	}
	a.OnForget = []func(string) error{func(string) error { return errors.New("disk") }}
	if err := a.Forget("k1"); err == nil {
		t.Error("a store that cannot forget is an error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { a.Run(ctx); close(done) }()
	cancel()
	<-done
	if _, err := Open(filepath.Join(dir, "journal"), r); err == nil {
		// journal/ exists as a directory; goals.json under it is fine. A file in the way is not.
		_ = os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o600)
		if _, err := Open(filepath.Join(dir, "file"), r); err == nil {
			t.Error("a file in the way")
		}
	}
}
