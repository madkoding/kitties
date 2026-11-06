package agenda

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFollowUps(t *testing.T) {
	s := store(t)
	j := NewJournal(filepath.Join(t.TempDir(), "j"))
	r := &fakeRunner{}
	sched := New(s, j, r)
	sched.FollowUps = []time.Duration{10 * time.Minute, time.Hour}
	now := fixed()
	sched.now = func() time.Time { return now }
	s.now = sched.now
	g, _ := s.Add(Goal{Kitty: "k1", Title: "Issues", On: []string{"github"}, Enabled: true})
	tick := func() { sched.Tick(context.Background()); sched.Wait() }

	// An event run that ends blocked is due again after the first wait, and only then.
	sched.Fire(context.Background(), Event{Source: "github"})
	sched.Wait()
	sched.Report(g.ID, "blocked")
	got, _ := s.Get(g.ID)
	if got.FollowUps != 1 || !got.FollowUpAt.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("after blocked: %+v", got)
	}
	now = now.Add(9 * time.Minute)
	tick()
	if r.count() != 1 {
		t.Fatal("the follow-up is not due yet")
	}
	now = now.Add(time.Minute)
	tick()
	if r.count() != 2 || r.runs[1].Kind != "follow_up" || r.runs[1].Previous != "blocked" {
		t.Fatalf("follow-up run: %+v", r.runs)
	}
	if !strings.Contains(r.runs[1].Text(), "stopped before it was finished") || !strings.Contains(r.runs[1].Text(), "go on from where it stopped") {
		t.Errorf("follow-up text: %q", r.runs[1].Text())
	}
	if got, _ := s.Get(g.ID); !got.FollowUpAt.IsZero() || got.LastResult != "running" {
		t.Errorf("a run clears the pending follow-up: %+v", got)
	}
	tick()
	if r.count() != 2 {
		t.Error("a follow-up runs once")
	}

	// The second one waits longer; past the last wait there is none, and the journal says so.
	sched.Report(g.ID, "escalated")
	if got, _ := s.Get(g.ID); got.FollowUps != 2 || !got.FollowUpAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("second follow-up: %+v", got)
	}
	now = now.Add(time.Hour)
	tick()
	if r.count() != 3 || !strings.Contains(r.runs[2].Text(), "waiting for the person") {
		t.Fatalf("second run: %+v", r.runs)
	}
	sched.Report(g.ID, "blocked")
	if got, _ := s.Get(g.ID); got.FollowUps != 2 || !got.FollowUpAt.IsZero() {
		t.Errorf("no follow-up left: %+v", got)
	}
	var texts []string
	for _, e := range j.Recent("k1", 0) {
		if e.Kind == KindRetry {
			texts = append(texts, e.Text)
		}
	}
	if len(texts) != 3 || !strings.Contains(texts[0], "no follow-up left") || !strings.Contains(texts[2], "follow-up 1 of 2 at 09:10") {
		t.Errorf("journal: %q", texts)
	}

	// A run that finishes starts the count again.
	sched.Report(g.ID, "done")
	if got, _ := s.Get(g.ID); got.FollowUps != 0 || !got.FollowUpAt.IsZero() || got.LastResult != "done" {
		t.Errorf("after done: %+v", got)
	}

	// A disabled goal gets no follow-up and no line; nor does any goal when they are off.
	off, _ := s.Add(Goal{Kitty: "k2", Title: "Off", On: []string{"x"}, Enabled: false})
	sched.Report(off.ID, "failed")
	if got, _ := s.Get(off.ID); !got.FollowUpAt.IsZero() || len(j.Recent("k2", 0)) != 0 {
		t.Errorf("disabled: %+v", got)
	}
	sched.FollowUps = nil
	sched.Report(g.ID, "blocked")
	if got, _ := s.Get(g.ID); !got.FollowUpAt.IsZero() {
		t.Error("no follow-ups configured")
	}
	sched.Report("zz", "blocked") // unknown goal: harmless
}

func TestFollowUpWhileBusyAndOnFailure(t *testing.T) {
	s := store(t)
	j := NewJournal(filepath.Join(t.TempDir(), "j"))
	r := &fakeRunner{slow: make(chan struct{})}
	sched := New(s, j, r)
	now := fixed()
	sched.now = func() time.Time { return now }
	s.now = sched.now
	g, _ := s.Add(Goal{Kitty: "k1", Title: "Slow", On: []string{"hook"}, Enabled: true})
	sched.Report(g.ID, "failed")
	if got, _ := s.Get(g.ID); got.FollowUps != 1 || !got.FollowUpAt.Equal(now.Add(DefaultFollowUps[0])) {
		t.Fatalf("default waits: %+v", got)
	}
	// A run by hand is the follow-up: none stays pending after it starts.
	go func() { _, _ = sched.RunNow(context.Background(), g.ID) }()
	for r.count() == 0 {
		time.Sleep(time.Millisecond)
	}
	if got, _ := s.Get(g.ID); !got.FollowUpAt.IsZero() {
		t.Error("a manual run clears the follow-up")
	}
	// A follow-up that comes due while the goal is still at work waits, without a journal line.
	sched.Report(g.ID, "blocked")
	now = now.Add(time.Hour)
	before := len(j.Recent("k1", 0))
	sched.Tick(context.Background())
	if r.count() != 1 || len(j.Recent("k1", 0)) != before {
		t.Errorf("busy: runs=%d journal=%v", r.count(), j.Recent("k1", 0))
	}
	close(r.slow)
	for {
		sched.mu.Lock()
		busy := sched.busy[g.ID]
		sched.mu.Unlock()
		if !busy {
			break
		}
		time.Sleep(time.Millisecond)
	}
	sched.Tick(context.Background())
	sched.Wait()
	if r.count() != 2 || r.runs[1].Kind != "follow_up" {
		t.Errorf("after the run ends: %+v", r.runs)
	}
	// A follow-up after a start that failed, and one with an unknown result, have their own words.
	if !strings.Contains((Trigger{Kind: "follow_up", Previous: "failed"}).Text(), "could not start") ||
		!strings.Contains((Trigger{Kind: "follow_up"}).Text(), "did not finish") {
		t.Error("follow-up texts")
	}
	// The runner failing to start plans a follow-up too.
	fail := &fakeRunner{fail: context.DeadlineExceeded}
	s2 := store(t)
	s2.now = sched.now
	g2, _ := s2.Add(Goal{Kitty: "k3", Title: "Broken", On: []string{"x"}, Enabled: true})
	sched2 := New(s2, j, fail)
	sched2.now = sched.now
	if _, err := sched2.RunNow(context.Background(), g2.ID); err == nil {
		t.Fatal("RunNow should fail")
	}
	if got, _ := s2.Get(g2.ID); got.LastResult != "failed" || got.FollowUps != 1 {
		t.Errorf("failed start: %+v", got)
	}
}

func TestFollowUpsFromTheSettings(t *testing.T) {
	s := store(t)
	j := NewJournal(filepath.Join(t.TempDir(), "j"))
	sched := New(s, j, &fakeRunner{})
	sched.now = fixed
	waits := []time.Duration{time.Hour}
	sched.SetFollowUps(func() []time.Duration { return waits })
	g, _ := s.Add(Goal{Kitty: "k1", Title: "G", On: []string{"x"}, Enabled: true})
	sched.Report(g.ID, "blocked")
	if got, _ := s.Get(g.ID); !got.FollowUpAt.Equal(fixed().Add(time.Hour)) {
		t.Errorf("from the settings: %+v", got)
	}
	waits = nil
	sched.Report(g.ID, "blocked")
	if got, _ := s.Get(g.ID); !got.FollowUpAt.IsZero() {
		t.Error("turned off in the settings")
	}
}
