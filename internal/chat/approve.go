package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/madkoding/kitties/internal/agenda"
	"github.com/madkoding/kitties/internal/approvals"
	"github.com/madkoding/kitties/internal/connector"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/notify"
)

// ACTIONS THAT WAIT FOR A YES.
//
// A connector action that reaches other people or moves money is not run when the kitty asks:
// it is held, the kitty is told so and goes on, and the person decides from the interface (or
// the notification). A yes runs the action and puts what happened in the thread; a no puts the
// reason there, where the kitty reads it and learns. A kitty that keeps getting yes for the
// same kind of action does it on its own from then on, sooner for a relaxed person, later for a
// demanding one, and never for what is always asked.

// ErrNoApprovals is returned when nothing is held back in this deployment.
var ErrNoApprovals = errors.New("this gateway holds no actions for approval")

// SetApprovals gives the chats the store that holds actions for a yes.
func (s *Service) SetApprovals(a *approvals.Store) { s.approvals = a }

// SetNotifier gives the chats the way to tell the person outside the interface.
func (s *Service) SetNotifier(n notify.Sender) { s.notifier = n }

// SetDemand tells the chats how demanding a kitty's person is, 1 (relaxed) to 5 (strict).
func (s *Service) SetDemand(fn func(kittyID string) int) { s.demand = fn }

// demandOf is the person's level for a kitty, 3 when nobody knows.
func (s *Service) demandOf(k *kitty.Kitty) int {
	if s.demand == nil {
		return 3
	}
	if d := s.demand(k.ID); d >= 1 && d <= 5 {
		return d
	}
	return 3
}

// tell sends a notification when there is a notifier.
func (s *Service) tell(e notify.Event, k *kitty.Kitty, title, body, chatID string) {
	if s.notifier == nil {
		return
	}
	name := ""
	if k != nil {
		name = k.Name
	}
	s.notifier.Notify(notify.Notification{Event: e, Kitty: name, Title: title, Body: body, Chat: chatID})
}

// goalOf is the goal a thread works on, if it works one.
func (s *Service) goalOf(id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sess, ok := s.sessions[id]; ok {
		return sess.Goal
	}
	return ""
}

// gate decides what happens to a connector action: it runs (ok false), or it is held for the
// person, or refused, and the outcome says so (ok true). Nothing is limited unless the person
// set it (or asked the kitty to): a rule, the kitty's "ask" mode, or its actions for the day.
func (s *Service) gate(id string, k *kitty.Kitty, a action, turn *Turn) (outcome, bool) {
	if s.approvals == nil {
		return outcome{}, false
	}
	c, found := s.fleet.Connectors().Get(a.name)
	if !found {
		return outcome{}, false
	}
	kinder, isKind := c.(connector.Kinder)
	if !isKind {
		return outcome{}, false
	}
	kind := kinder.Kind()
	risk := connector.RiskOfCall(kind, a.args)
	action := connector.ActionOf(kind, a.args)
	summary := approvals.Summarise(a.name, action, a.args)
	verdict := s.approvals.Check(k.ID, a.name, action, risk.String(), k.Approvals, s.demandOf(k))
	if verdict.Blocked {
		s.note(k, agenda.KindApproval, s.goalOf(id), id, "did not do it ("+verdict.Why+"): "+summary)
		return outcome{action: a, err: "not done: the person asked you never to do this (" + summary + "). Tell them plainly; " +
			"if they want it after all, they can lift the limit, or ask you to."}, true
	}
	// What reaches other people counts against the actions of the day, when the person set some.
	if verdict.Run && risk != connector.RiskNone && s.overActions(k) {
		verdict = approvals.Verdict{Why: "over the day's actions"}
		summary = fmt.Sprintf("(over today's %d actions) %s", k.Limits.Or(s.DefaultLimits).DayActions, summary)
	}
	if verdict.Run {
		if risk != connector.RiskNone {
			s.spendAction(k)
		}
		if verdict.Why != approvals.WhyFree {
			s.note(k, agenda.KindApproval, s.goalOf(id), id, "went ahead on its own ("+verdict.Why+"): "+summary)
		}
		return outcome{}, false
	}
	ap, err := s.approvals.Request(approvals.Approval{Kitty: k.ID, KittyName: k.Name, Connector: a.name, Kind: kind, Action: action, Args: a.args, Summary: summary, Risk: risk.String(), Chat: id, Goal: s.goalOf(id)})
	if err != nil {
		return outcome{action: a, err: "could not hold the action for approval: " + err.Error()}, true
	}
	turn.Approvals = append(turn.Approvals, ApprovalRef{ID: ap.ID, Connector: a.name, Action: action, Summary: summary})
	s.note(k, agenda.KindApproval, ap.Goal, id, "waiting for approval: "+summary)
	s.tell(notify.Approval, k, "needs your approval", summary, id)
	return outcome{action: a, text: fmt.Sprintf("held: the person has to approve this first (request %s). Do not repeat it. The result will appear in this thread once they decide; go on with other work, or finish and say it is waiting.", ap.ID)}, true
}

// ListApprovals returns the requests in a status ("" for all), newest first.
func (s *Service) ListApprovals(status string) ([]approvals.Approval, error) {
	if s.approvals == nil {
		return nil, ErrNoApprovals
	}
	return s.approvals.List(status, 200), nil
}

// ApprovalRules returns what the person allowed for good.
func (s *Service) ApprovalRules() ([]approvals.Rule, error) {
	if s.approvals == nil {
		return nil, ErrNoApprovals
	}
	return s.approvals.Rules(), nil
}

// SetApprovalRule limits an action of a kitty (ask, never or auto), or lifts the limit with an
// empty mode. The kitty is named by its name or id.
func (s *Service) SetApprovalRule(ref, conn, action, mode string) (*approvals.Rule, error) {
	if s.approvals == nil {
		return nil, ErrNoApprovals
	}
	k, err := s.fleet.Get(ref)
	if err != nil {
		return nil, err
	}
	return s.approvals.SetRule(approvals.Rule{Kitty: k.ID, KittyName: k.Name, Connector: conn, Action: action, Mode: mode})
}

// RemoveApprovalRule takes a decision back.
func (s *Service) RemoveApprovalRule(id string) error {
	if s.approvals == nil {
		return ErrNoApprovals
	}
	return s.approvals.RemoveRule(id)
}

// DecideApproval records the person's answer. A yes runs the action and tells the thread what
// happened; a no tells it why. Either way the kitty learns from it.
func (s *Service) DecideApproval(ctx context.Context, id string, approve bool, reason string, always bool) (*approvals.Approval, error) {
	if s.approvals == nil {
		return nil, ErrNoApprovals
	}
	ap, err := s.approvals.Decide(id, approve, reason, always)
	if err != nil {
		return nil, err
	}
	k, _ := s.fleet.Get(ap.Kitty)
	turn := Turn{At: s.now(), From: "approval", Kitty: ""}
	if approve {
		var allow []string
		if k != nil {
			allow = k.Connectors
		}
		res := s.fleet.Connectors().Call(ctx, connector.Call{Name: ap.Connector, Args: ap.Args}, allow)
		if k != nil {
			s.spendAction(k)
			s.keepFiles([]connector.Result{res}, k, &turn)
		}
		s.approvals.Done(id, res.Output, res.Err)
		ap.Result, ap.Error = res.Output, res.Err
		turn.Calls = append(turn.Calls, res)
		if res.Err != "" {
			turn.Text = "Approved, but it failed: " + ap.Summary + "\n" + res.Err
		} else {
			turn.Text = "Approved and done: " + ap.Summary + "\n" + firstLine(res.Output)
		}
		if always {
			turn.Text += "\n(From now on this kind of action goes ahead without asking.)"
		}
		s.note(k, agenda.KindApproval, ap.Goal, ap.Chat, "approved: "+ap.Summary)
	} else {
		turn.Text = "Rejected: " + ap.Summary
		if ap.Reason != "" {
			turn.Text += "\nReason: " + ap.Reason
		}
		s.note(k, agenda.KindApproval, ap.Goal, ap.Chat, "rejected: "+ap.Summary)
	}
	s.appendTurn(ap.Chat, turn)
	if s.learnFromApproval != nil {
		s.learnFromApproval(*ap)
	}
	return ap, nil
}

// appendTurn adds a turn to a thread, when it still exists.
func (s *Service) appendTurn(id string, t Turn) {
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return
	}
	sess.Turns = append(sess.Turns, t)
	sess.UpdatedAt = s.now()
	_ = s.save(sess)
	s.emitLocked(Event{Type: "turn", Session: id, Index: len(sess.Turns) - 1, Turn: &sess.Turns[len(sess.Turns)-1]})
}

// summaryLine keeps a long text to a line for a notification.
func summaryLine(s string) string { return clip(strings.Join(strings.Fields(s), " "), 200) }
