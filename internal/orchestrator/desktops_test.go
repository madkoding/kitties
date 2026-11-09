package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/kitty"
)

// waitDesks waits until no look and no update of the desktops is running, and gives the last job.
func waitDesks(t *testing.T, m *Manager) *DesktopJob {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		m.desks.mu.Lock()
		running, job := m.desks.running(), m.desks.job
		m.desks.mu.Unlock()
		if !running {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatal("the desktops are still being updated")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func states(j *DesktopJob) map[string]string {
	out := map[string]string{}
	for _, k := range j.Kitties {
		out[k.Name] = k.State
		if k.Error != "" {
			out[k.Name] += ": " + k.Error
		}
	}
	return out
}

// Every desktop of an older image is moved in one go: the running ones made anew (sessions kept),
// a stopped one removed so it starts on the new image, the busy and the held left for later.
func TestUpdateDesktopsMovesEveryStaleDesktop(t *testing.T) {
	ctx := context.Background()
	m, f, _ := sessionsManager(t)
	for _, n := range []string{"mimi", "tom", "bo", "held", "work", "fresh"} {
		_, _ = m.Create(ctx, Spec{Name: n})
		if err := m.Start(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	no := false
	_, _ = m.Create(ctx, Spec{Name: "brain", Desktop: &no})
	_ = m.Stop(ctx, "bo")
	f.staleOf = map[string]bool{}
	for _, n := range []string{"mimi", "tom", "bo", "held", "work"} {
		k, _ := m.Get(n)
		f.staleOf[k.Container] = true
	}
	hk, _ := m.Get("held")
	wk, _ := m.Get("work")
	_, _ = m.Hold(hk.ID, true)
	m.SetBusy(func(id string) bool { return id == wk.ID })

	news := m.DesktopUpdates(ctx)
	if news.Available != 5 || len(news.Kitties) != 6 || news.Job != nil || news.CheckedAt != nil {
		t.Fatalf("news = %+v", news)
	}
	busy := map[string]string{}
	for _, d := range news.Kitties {
		busy[d.Name] = d.Busy
	}
	if busy["held"] != "held" || busy["work"] != "working" || busy["mimi"] != "" {
		t.Errorf("busy = %v", busy)
	}

	f.calls = nil
	job, err := m.UpdateDesktops(ctx, nil)
	if err != nil || job.Total != 5 || job.Phase != DesksDownload {
		t.Fatalf("UpdateDesktops = %+v, %v", job, err)
	}
	if _, err := m.UpdateDesktops(ctx, nil); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Errorf("a second update at once: %v", err)
	}
	if err := m.CheckDesktopUpdates(); err == nil {
		t.Error("a look while updating")
	}
	done := waitDesks(t, m)
	got := states(done)
	if got["mimi"] != DeskDone || got["tom"] != DeskDone || got["bo"] != DeskOnStart ||
		!strings.HasPrefix(got["held"], DeskSkipped+": you have its desktop") || !strings.HasPrefix(got["work"], DeskSkipped+": it is working") {
		t.Errorf("states = %v", got)
	}
	if done.Phase != DesksDone || done.Percent != 100 || done.Done != 5 || done.Finished == nil {
		t.Errorf("job = %+v", done)
	}
	if c := strings.Join(f.calls, "|"); !strings.Contains(c, "pull ") || strings.Index(c, "pull ") > strings.Index(c, "rm") {
		t.Errorf("the image was not pulled first: %s", c)
	}
	// The running ones run again; the stopped one stays off, its container gone until it starts.
	for _, n := range []string{"mimi", "tom"} {
		if k, _ := m.Get(n); k.Status != kitty.StatusRunning {
			t.Errorf("%s = %s", n, k.Status)
		}
	}
	bo, _ := m.Get("bo")
	if bo.Status != kitty.StatusStopped || f.exists[bo.Container] {
		t.Errorf("bo = %s (container kept: %v)", bo.Status, f.exists[bo.Container])
	}
	after := m.DesktopUpdates(ctx)
	if after.Available != 2 || after.Job == nil || after.CheckedAt == nil {
		t.Errorf("after = %+v", after)
	}
}

// The person can pick some; nothing to update, an unknown kitty and no engine are said so.
func TestUpdateDesktopsChoiceAndRefusals(t *testing.T) {
	ctx := context.Background()
	m, f, _ := sessionsManager(t)
	_, _ = m.Create(ctx, Spec{Name: "mimi"})
	_, _ = m.Create(ctx, Spec{Name: "tom"})
	_ = m.Start(ctx, "mimi")
	_ = m.Start(ctx, "tom")
	if _, err := m.UpdateDesktops(ctx, nil); err == nil || !strings.Contains(err.Error(), "no desktop") {
		t.Errorf("nothing stale: %v", err)
	}
	if _, err := m.UpdateDesktops(ctx, []string{"nobody"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
	f.stale = true
	job, err := m.UpdateDesktops(ctx, []string{"tom"})
	if err != nil || job.Total != 1 || job.Kitties[0].Name != "tom" {
		t.Fatalf("one = %+v, %v", job, err)
	}
	waitDesks(t, m)
	// A failure is the kitty's, and the others go on.
	f.fail = "rm"
	_, _ = m.UpdateDesktops(ctx, []string{"mimi", "tom"})
	got := states(waitDesks(t, m))
	if !strings.HasPrefix(got["mimi"], DeskFailed+": ") || !strings.HasPrefix(got["tom"], DeskFailed+": ") {
		t.Errorf("failed = %v", got)
	}
	none, _ := New(testConfig(t))
	if _, err := none.UpdateDesktops(ctx, nil); err == nil {
		t.Error("updated without an engine")
	}
	if err := none.CheckDesktopUpdates(); err == nil {
		t.Error("looked without an engine")
	}
	if news := none.DesktopUpdates(ctx); news.Available != 0 {
		t.Errorf("news without an engine = %+v", news)
	}
}

// A desktop that is up to date by now, or has no container yet, is not touched.
func TestUpdateOneLeavesWhatNeedsNothing(t *testing.T) {
	ctx := context.Background()
	m, f, _ := sessionsManager(t)
	k, _ := m.Create(ctx, Spec{Name: "mimi"})
	if s, err := m.updateOne(ctx, k.ID); s != DeskOnStart || err != nil {
		t.Errorf("no container = %s, %v", s, err)
	}
	_ = m.Start(ctx, "mimi")
	f.calls = nil
	if s, err := m.updateOne(ctx, k.ID); s != DeskDone || err != nil || strings.Contains(strings.Join(f.calls, "|"), "rm") {
		t.Errorf("up to date = %s, %v (%v)", s, err, f.calls)
	}
	if s, err := m.updateOne(ctx, "nobody"); s != DeskFailed || err == nil {
		t.Errorf("unknown = %s, %v", s, err)
	}
	f.stale, f.fail = true, "list"
	if s, err := m.updateOne(ctx, k.ID); s != DeskFailed || err == nil {
		t.Errorf("engine fails = %s, %v", s, err)
	}
	// A stopped desktop whose container cannot be removed failed.
	f.fail = ""
	_ = m.Stop(ctx, "mimi")
	f.fail = "rm"
	if s, err := m.updateOne(ctx, k.ID); s != DeskFailed || err == nil {
		t.Errorf("stopped, rm fails = %s, %v", s, err)
	}
}

// Looking for a new image is a pull in the background, noted with its time and its error.
func TestCheckDesktopUpdates(t *testing.T) {
	m, f, _ := sessionsManager(t)
	f.pullErr = errors.New("offline")
	if err := m.CheckDesktopUpdates(); err != nil {
		t.Fatal(err)
	}
	waitDesks(t, m)
	news := m.DesktopUpdates(context.Background())
	if news.CheckedAt == nil || news.CheckError != "offline" || news.Checking {
		t.Errorf("news = %+v", news)
	}
	if !strings.Contains(strings.Join(f.calls, "|"), "pull ") {
		t.Errorf("calls = %v", f.calls)
	}
	// The watcher looks too, even when it does not update by itself.
	f.pullErr = nil
	m.cfg.Containers.AutoUpdate = false
	m.CheckUpdates(context.Background(), func(*kitty.Kitty) {}, func(string, ...any) {})
	if news := m.DesktopUpdates(context.Background()); news.CheckError != "" {
		t.Errorf("after the watcher = %+v", news)
	}
}

// The watcher does not update by itself while the person updates the desktops.
func TestAutoUpdateGivesWayToTheUpdateOfAll(t *testing.T) {
	ctx := context.Background()
	m, f, _ := sessionsManager(t)
	_, _ = m.Create(ctx, Spec{Name: "mimi"})
	_ = m.Start(ctx, "mimi")
	f.stale = true
	m.desks.job = &DesktopJob{}
	var updated int
	m.CheckUpdates(ctx, func(*kitty.Kitty) { updated++ }, func(string, ...any) {})
	if updated != 0 {
		t.Errorf("updated %d while the person updates", updated)
	}
}
