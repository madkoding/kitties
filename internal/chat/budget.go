package chat

import (
	"fmt"

	"github.com/madkoding/kitties/internal/agenda"
	"github.com/madkoding/kitties/internal/budget"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/notify"
	"github.com/madkoding/kitties/internal/provider"
)

// A DAY'S BUDGET.
//
// A kitty that works around the clock needs a ceiling. Per kitty and per day there are two:
// tokens (what it thinks with) and actions that reach other people. Out of tokens, it stops
// until tomorrow and the person is told once. Past its actions, each further one waits for a
// yes even if the kitty acts on its own; the yes is the person raising the ceiling for that one.

// SetBudget gives the chats the counts of what each kitty spent today.
func (s *Service) SetBudget(b *budget.Store) { s.budget = b }

// Spent is what a kitty used today against its limits (zero limit: none).
type Spent struct {
	Tokens     int `json:"tokens"`
	Actions    int `json:"actions"`
	DayTokens  int `json:"day_tokens"`
	DayActions int `json:"day_actions"`
}

// SpentBy returns today's counts for a kitty.
func (s *Service) SpentBy(k *kitty.Kitty) Spent {
	l := k.Limits.Or(s.DefaultLimits)
	sp := Spent{DayTokens: l.DayTokens, DayActions: l.DayActions}
	if s.budget != nil {
		d := s.budget.Today(k.ID)
		sp.Tokens, sp.Actions = d.Tokens, d.Actions
	}
	return sp
}

// spendTokens counts the tokens of one model reply.
func (s *Service) spendTokens(k *kitty.Kitty, u provider.Usage) {
	if s.budget != nil {
		s.budget.AddTokens(k.ID, u.Input+u.Output)
	}
}

// overTokens is an error when the kitty has spent its day's tokens; the first time in a day it
// also tells the person.
func (s *Service) overTokens(k *kitty.Kitty) error {
	sp := s.SpentBy(k)
	if s.budget == nil || sp.DayTokens == 0 || sp.Tokens < sp.DayTokens {
		return nil
	}
	if s.budget.Tell(k.ID) {
		s.note(k, agenda.KindBlocked, "", "", fmt.Sprintf("spent its day's tokens (%d); stopped until tomorrow", sp.DayTokens))
		s.tell(notify.Budget, k, "spent its day's tokens", fmt.Sprintf("%d of %d. It rests until tomorrow, or raise its daily limit.", sp.Tokens, sp.DayTokens), "")
	}
	return fmt.Errorf("the daily token budget (%d) is spent; it resumes tomorrow", sp.DayTokens)
}

// overActions reports whether the kitty has used its day's actions.
func (s *Service) overActions(k *kitty.Kitty) bool {
	sp := s.SpentBy(k)
	return sp.DayActions > 0 && sp.Actions >= sp.DayActions
}

// spendAction counts one action that reached other people.
func (s *Service) spendAction(k *kitty.Kitty) {
	if s.budget != nil {
		s.budget.AddActions(k.ID, 1)
	}
}
