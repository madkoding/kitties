package agenda

import (
	"context"
	"path/filepath"
)

// Agenda is the goals, the journal and the scheduler in one, as the gateway and the command
// use them.
type Agenda struct {
	Store     *Store
	Journal   *Journal
	Scheduler *Scheduler
	// OnForget are called with a removed kitty's id, so the other stores that keep things about
	// a kitty (its memory, the actions held for it) drop them too.
	OnForget []func(kitty string) error
}

// Open keeps the goals and the journals under dir and prepares the scheduler over runner.
func Open(dir string, runner Runner) (*Agenda, error) {
	store, err := NewStore(filepath.Join(dir, "goals.json"))
	if err != nil {
		return nil, err
	}
	journal := NewJournal(filepath.Join(dir, "journal"))
	return &Agenda{Store: store, Journal: journal, Scheduler: New(store, journal, runner)}, nil
}

// Goals lists the goals, of one kitty (by id) when given.
func (a *Agenda) Goals(kitty string) []*Goal { return a.Store.List(kitty) }

// Goal returns one goal.
func (a *Agenda) Goal(id string) (*Goal, error) { return a.Store.Get(id) }

// AddGoal keeps a new goal.
func (a *Agenda) AddGoal(g Goal) (*Goal, error) { return a.Store.Add(g) }

// UpdateGoal changes a goal.
func (a *Agenda) UpdateGoal(id string, g Goal) (*Goal, error) { return a.Store.Update(id, g) }

// RemoveGoal forgets a goal.
func (a *Agenda) RemoveGoal(id string) error { return a.Store.Remove(id) }

// RunNow runs a goal at once and returns its chat.
func (a *Agenda) RunNow(ctx context.Context, id string) (string, error) {
	return a.Scheduler.RunNow(ctx, id)
}

// Fire runs the goals listening to an event's source.
func (a *Agenda) Fire(ctx context.Context, ev Event) []string { return a.Scheduler.Fire(ctx, ev) }

// Recent is the last n lines of a kitty's journal, newest first.
func (a *Agenda) Recent(kitty string, n int) []Entry { return a.Journal.Recent(kitty, n) }

// Note writes a line in a kitty's journal.
func (a *Agenda) Note(e Entry) error { return a.Journal.Add(e) }

// Report implements the chats' reporter: how a goal's run ended.
func (a *Agenda) Report(goalID, result string) { a.Scheduler.Report(goalID, result) }

// Forget drops everything about a kitty that was removed.
func (a *Agenda) Forget(kitty string) error {
	if err := a.Store.RemoveOf(kitty); err != nil {
		return err
	}
	for _, fn := range a.OnForget {
		if err := fn(kitty); err != nil {
			return err
		}
	}
	return a.Journal.Forget(kitty)
}

// Run is the scheduler's clock, until ctx ends.
func (a *Agenda) Run(ctx context.Context) { a.Scheduler.Run(ctx) }
