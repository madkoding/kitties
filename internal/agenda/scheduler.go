package agenda

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Trigger says why a goal runs.
type Trigger struct {
	Kind string `json:"kind"` // schedule | event | manual
	// Event is set for an event trigger.
	Event *Event `json:"event,omitempty"`
}

// Text is what the kitty is told about the trigger.
func (t Trigger) Text() string {
	switch t.Kind {
	case "event":
		if t.Event == nil {
			return "something arrived from a service"
		}
		head := "an event from " + t.Event.Source
		if t.Event.Kind != "" {
			head += " (" + t.Event.Kind + ")"
		}
		return head + ":\n" + t.Event.Text
	case "manual":
		return "a person asked for it now"
	default:
		return "the schedule"
	}
}

// Runner does the work of a goal: it starts the kitty on it and returns the chat it happens
// in. The result arrives later, through Report.
type Runner interface {
	RunGoal(ctx context.Context, g Goal, t Trigger) (chatID string, err error)
}

// Scheduler wakes kitties up: on the clock for scheduled goals, on Fire for events.
type Scheduler struct {
	store   *Store
	journal *Journal
	runner  Runner
	now     func() time.Time
	// Every is how often the clock is checked.
	Every time.Duration
	mu    sync.Mutex
	// busy are the goals with a run in flight, so a slow one is not stacked.
	busy map[string]bool
	logf func(string, ...any)
	wg   sync.WaitGroup
}

// New creates the scheduler over the store and the journal.
func New(store *Store, journal *Journal, runner Runner) *Scheduler {
	return &Scheduler{store: store, journal: journal, runner: runner, now: time.Now, Every: time.Minute, busy: map[string]bool{}, logf: func(string, ...any) {}}
}

// SetLog sets where problems are reported.
func (s *Scheduler) SetLog(logf func(string, ...any)) { s.logf = logf }

// Run checks the clock until ctx ends: every goal that is due runs once.
func (s *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(s.Every)
	defer t.Stop()
	for {
		s.Tick(ctx)
		select {
		case <-ctx.Done():
			s.wg.Wait()
			return
		case <-t.C:
		}
	}
}

// Tick runs the goals that are due now.
func (s *Scheduler) Tick(ctx context.Context) {
	for _, g := range s.store.due(s.now()) {
		s.start(ctx, g, Trigger{Kind: "schedule"})
	}
}

// Fire runs the goals listening to the event's source and returns their ids.
func (s *Scheduler) Fire(ctx context.Context, ev Event) []string {
	if ev.At.IsZero() {
		ev.At = s.now()
	}
	var ids []string
	for _, g := range s.store.listening(ev.Source) {
		_ = s.journal.Add(Entry{Kitty: g.Kitty, Kind: KindEvent, Goal: g.ID, Text: "from " + ev.Source + ": " + firstLine(ev.Text)})
		e := ev
		s.start(ctx, g, Trigger{Kind: "event", Event: &e})
		ids = append(ids, g.ID)
	}
	return ids
}

// RunNow runs a goal at a person's request and returns the chat it works in.
func (s *Scheduler) RunNow(ctx context.Context, id string) (string, error) {
	g, err := s.store.Get(id)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	if s.busy[id] {
		s.mu.Unlock()
		return g.Chat, errors.New("the goal is already running")
	}
	s.busy[id] = true
	s.mu.Unlock()
	chatID, err := s.run(ctx, g, Trigger{Kind: "manual"})
	s.mu.Lock()
	delete(s.busy, id)
	s.mu.Unlock()
	return chatID, err
}

// start runs a goal in the background, unless a run of it is still in flight.
func (s *Scheduler) start(ctx context.Context, g *Goal, t Trigger) {
	s.mu.Lock()
	if s.busy[g.ID] {
		s.mu.Unlock()
		_ = s.journal.Add(Entry{Kitty: g.Kitty, Kind: KindSkipped, Goal: g.ID, Chat: g.Chat, Text: g.Title + ": still working on the previous run"})
		if t.Kind == "schedule" {
			s.store.skip(g.ID, s.now())
		}
		return
	}
	s.busy[g.ID] = true
	s.mu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		_, err := s.run(ctx, g, t)
		if err != nil {
			s.logf("goal %q of %s: %v", g.Title, g.Kitty, err)
		}
		s.mu.Lock()
		delete(s.busy, g.ID)
		s.mu.Unlock()
	}()
}

// run records the run, asks the runner, and journals the outcome of the start.
func (s *Scheduler) run(ctx context.Context, g *Goal, t Trigger) (string, error) {
	s.store.started(g.ID, s.now())
	_ = s.journal.Add(Entry{Kitty: g.Kitty, Kind: KindRun, Goal: g.ID, Chat: g.Chat, Text: g.Title + " (" + t.Kind + ")"})
	chatID, err := s.runner.RunGoal(ctx, *g, t)
	s.store.attach(g.ID, chatID)
	if err != nil {
		s.store.Report(g.ID, "failed")
		_ = s.journal.Add(Entry{Kitty: g.Kitty, Kind: KindBlocked, Goal: g.ID, Chat: chatID, Text: g.Title + ": could not start: " + err.Error()})
		return chatID, fmt.Errorf("%s: %w", g.Title, err)
	}
	return chatID, nil
}

// Report is how the runner says a run ended (done, blocked, escalated, delegated).
func (s *Scheduler) Report(goalID, result string) {
	s.store.Report(goalID, result)
}

// Wait blocks until every run in flight has started its work.
func (s *Scheduler) Wait() { s.wg.Wait() }

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return clip(s, 200)
}
