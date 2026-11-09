package chat

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/madkoding/kitties/internal/agenda"
	"github.com/madkoding/kitties/internal/approvals"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/notify"
)

// THE LEAD KITTY.
//
// One kitty of the fleet may be the lead (kitty.Kitty.Lead): the principal one, the person's
// main contact and the orchestrator of the others. Its prompt names the team and tells it to
// split the work among them (delegate) and give one report of the whole. Everything the other
// kitties have to tell the person (a goal done or blocked, help they need, an action waiting
// for a yes) lands, besides the notification, in the lead's Reports thread, and the
// notification goes out in the lead's name: the person hears from one kitty.

// ReportsTitle is the title of the lead's Reports thread.
const ReportsTitle = "Reports"

// Lead is the fleet's lead kitty, nil when none is.
func (s *Service) Lead() *kitty.Kitty {
	for _, k := range s.fleet.List() {
		if k.Lead {
			return k
		}
	}
	return nil
}

// leadFor is the lead that reports for k: nil when there is none or k is it.
func (s *Service) leadFor(k *kitty.Kitty) *kitty.Kitty {
	lead := s.Lead()
	if lead == nil || (k != nil && lead.ID == k.ID) {
		return nil
	}
	return lead
}

// team describes the other kitties to the lead, one line each.
func (s *Service) team(lead *kitty.Kitty) []string {
	var out []string
	for _, k := range s.fleet.List() {
		if k.ID == lead.ID {
			continue
		}
		var parts []string
		if p := strings.TrimSpace(k.Soul.Persona); p != "" {
			parts = append(parts, clip(p, 120))
		}
		if len(k.Soul.Skills) > 0 {
			parts = append(parts, "good at "+strings.Join(k.Soul.Skills, ", "))
		}
		if k.Desktop {
			parts = append(parts, "has a computer")
		} else {
			parts = append(parts, "no computer")
		}
		switch {
		case len(k.Connectors) == 0:
			parts = append(parts, "may use every service")
		case len(k.Connectors) == 1 && k.Connectors[0] == kitty.NoConnectors:
			parts = append(parts, "uses no service")
		default:
			parts = append(parts, "uses "+strings.Join(k.Connectors, ", "))
		}
		if r := k.Limits.Or(s.DefaultLimits).MaxRounds; r > 0 {
			parts = append(parts, fmt.Sprintf("%d rounds per task", r))
		}
		state := k.Status
		if s.KittyBusy(k.ID) {
			state = "busy"
		}
		parts = append(parts, state)
		if s.independent(k) {
			parts = append(parts, "works on its own (WhatsApp): it hears only from you, so hand it only what it must say or do there, never private data or secrets")
		}
		out = append(out, k.Name+": "+strings.Join(parts, "; "))
	}
	return out
}

// ReportsChat finds the lead's Reports thread, or opens it.
func (s *Service) ReportsChat(lead *kitty.Kitty) (string, error) {
	s.mu.RLock()
	for _, sess := range s.sessions {
		if sess.Reports && len(sess.Members) == 1 && sess.Members[0] == lead.ID {
			s.mu.RUnlock()
			return sess.ID, nil
		}
	}
	s.mu.RUnlock()
	sess, err := s.Create(ReportsTitle, []string{lead.ID})
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess.Reports = true
	return sess.ID, s.save(sess)
}

// report puts what k has to tell the person in the lead's Reports thread, without waking the
// lead: it reads them when the person asks, or with the next thing it is told there.
func (s *Service) report(lead, k *kitty.Kitty, e notify.Event, title, body, chatID string) {
	id, err := s.ReportsChat(lead)
	if err != nil || id == chatID {
		return
	}
	text := strings.TrimSpace(title)
	if body = strings.TrimSpace(body); body != "" {
		text += ": " + body
	}
	from, fromID := "system", ""
	if k != nil {
		from, fromID = k.Name, k.ID
	}
	if s.foldReport(id, chatID, from, fromID, text) {
		return
	}
	s.appendTurn(id, Turn{At: s.now(), From: from, Kitty: fromID, Text: text, Report: &Report{Event: string(e), Chat: chatID}})
}

// FoldReportsWithin is how close together the news of one chat must come to be one report: a
// hand-over that ends and the goal it was for ending through it read as one message, not two
// that say the same thing.
const FoldReportsWithin = 10 * time.Minute

// foldReport adds a piece of news to the last report of the Reports thread when it is about the
// same chat, within FoldReportsWithin; a line of another kitty than the report's starts with its
// name, and the same line twice is kept once. It reports whether the news found its place there.
func (s *Service) foldReport(id, chatID, from, fromID, text string) bool {
	if chatID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok || len(sess.Turns) == 0 {
		return false
	}
	last := &sess.Turns[len(sess.Turns)-1]
	if last.Report == nil || last.Report.Chat != chatID || s.now().Sub(last.At) > FoldReportsWithin {
		return false
	}
	if last.Kitty != fromID {
		text = from + ": " + text
	}
	for _, line := range strings.Split(last.Text, "\n") {
		if line == text {
			return true // said already
		}
	}
	last.Text += "\n" + text
	last.Report.Folded++
	last.At = s.now()
	sess.UpdatedAt = last.At
	_ = s.save(sess)
	s.emitLocked(Event{Type: "update", Session: id, Index: len(sess.Turns) - 1, Turn: last})
	return true
}

// Report says where a turn of the Reports thread comes from.
type Report struct {
	// Event is the kind of news (notify.Event: goal_done, help, approval...).
	Event string `json:"event"`
	// Chat is the thread it happened in, empty when it happened in none.
	Chat string `json:"chat,omitempty"`
	// Folded is how many more pieces of news of that chat were added to this one (foldReport).
	Folded int `json:"folded,omitempty"`
}

// signed is the notification a kitty sends, in the lead's name when there is one.
func signed(lead, k *kitty.Kitty, title string) (from, line string) {
	if k != nil {
		from = k.Name
	}
	if lead == nil || k == nil {
		return from, title
	}
	return lead.Name, fmt.Sprintf("%s %s", k.Name, title)
}

// LeadDigest is the daily summary as the lead gives it: in its name, and kept in its Reports
// thread too. Without a lead it is the summary as it was.
func (s *Service) LeadDigest(n notify.Notification) notify.Notification {
	lead := s.Lead()
	if lead == nil {
		return n
	}
	if id, err := s.ReportsChat(lead); err == nil {
		s.appendTurn(id, Turn{At: s.now(), From: lead.Name, Kitty: lead.ID, Text: strings.TrimSpace(n.Title + "\n" + n.Body), Report: &Report{Event: string(notify.Digest)}})
		if n.Chat == "" {
			n.Chat = id
		}
	}
	n.Kitty = lead.Name
	return n
}

// taskLimitsHelp is what the lead's task_limits tool says about itself.
var taskLimitsHelp = fmt.Sprintf("Change what another kitty may spend on one task: rounds of actions (max_rounds, up to %d), "+
	"tokens (max_tokens, up to %d) and seconds (max_seconds, up to %d); 0 goes back to the default, a field left out stays. "+
	"It lasts until changed again. To give a kitty more for one task only, pass the same fields to delegate instead. "+
	"Say in your report that you changed it.", kitty.MaxRoundsCap, kitty.MaxTokensCap, kitty.MaxSecondsCap)

// LimitsSetter changes a kitty's limits per task; the orchestrator is one.
type LimitsSetter interface {
	SetTaskLimits(ref string, t kitty.TaskLimits) (*kitty.Kitty, error)
}

// LeadLimitsConnector names, in the approvals, a change of limits the lead asked for.
const LeadLimitsConnector = "kitties"

// SetLeadLimitsAsk makes every change of limits the lead asks for wait for the person's yes
// while fn says so (autonomy.lead_limits: ask).
func (s *Service) SetLeadLimitsAsk(fn func() bool) { s.leadLimitsAsk = fn }

// canSetLimits reports whether k may change the others' limits: only the lead, on a fleet that
// can change them, with another kitty to change.
func (s *Service) canSetLimits(k *kitty.Kitty) bool {
	if k == nil || !k.Lead {
		return false
	}
	if _, ok := s.fleet.(LimitsSetter); !ok {
		return false
	}
	for _, other := range s.fleet.List() {
		if other.ID != k.ID {
			return true
		}
	}
	return false
}

// taskLimitsArg reads the change of limits from an action's arguments.
func taskLimitsArg(args map[string]any) kitty.TaskLimits {
	var t kitty.TaskLimits
	if n, ok := intArg(args, "max_rounds", "rounds"); ok {
		t.MaxRounds = &n
	}
	if n, ok := intArg(args, "max_tokens", "tokens"); ok {
		t.MaxTokens = &n
	}
	if n, ok := intArg(args, "max_seconds", "seconds"); ok {
		t.MaxSeconds = &n
	}
	return t
}

// runTaskLimits is the lead changing another kitty's limits per task. Only the lead may, within
// the caps; the change goes to both journals and to the lead's Reports thread, or waits for the
// person's yes when they asked for that.
func (s *Service) runTaskLimits(id string, a action, k *kitty.Kitty, turn *Turn) outcome {
	if !k.Lead {
		return outcome{action: a, err: "only the lead kitty changes the others' limits"}
	}
	if _, ok := s.fleet.(LimitsSetter); !ok {
		return outcome{action: a, err: "limits cannot be changed here"}
	}
	name := strings.TrimSpace(a.name)
	if name == "" {
		name = argText(a.args, "kitty")
	}
	if name == "" {
		return outcome{action: a, err: "say which kitty: @task_limits <name> {\"max_rounds\": 120}"}
	}
	target, err := s.fleet.Get(name)
	if err != nil {
		return outcome{action: a, err: "no such kitty " + name}
	}
	if target.ID == k.ID {
		return outcome{action: a, err: "the lead changes the others' limits, not its own; the person sets yours"}
	}
	t := taskLimitsArg(a.args)
	if t.Empty() {
		return outcome{action: a, err: "say what to change: max_rounds, max_tokens or max_seconds (0 for the default)"}
	}
	if err := t.Validate(); err != nil {
		return outcome{action: a, err: err.Error()}
	}
	change := target.Name + "'s limits per task: " + t.Describe(target.Limits)
	if s.approvals != nil && s.leadLimitsAsk != nil && s.leadLimitsAsk() {
		args := map[string]any{"kitty": target.ID}
		for key, v := range map[string]*int{"max_rounds": t.MaxRounds, "max_tokens": t.MaxTokens, "max_seconds": t.MaxSeconds} {
			if v != nil {
				args[key] = *v
			}
		}
		ap, err := s.approvals.Request(approvals.Approval{Kitty: k.ID, KittyName: k.Name, Connector: LeadLimitsConnector, Kind: "limits",
			Action: "task_limits", Args: args, Summary: "change " + change, Risk: "always", Chat: id, Goal: s.goalOf(id)})
		if err != nil {
			return outcome{action: a, err: "could not hold the change for approval: " + err.Error()}
		}
		turn.Approvals = append(turn.Approvals, ApprovalRef{ID: ap.ID, Connector: LeadLimitsConnector, Action: "task_limits", Summary: "change " + change})
		s.note(k, agenda.KindApproval, ap.Goal, id, "waiting for approval: change "+change)
		s.tell(notify.Approval, k, "needs your approval", "change "+change, id)
		return outcome{action: a, text: fmt.Sprintf("held: the person approves changes of limits first (request %s). Go on without it; the result appears in this thread once they decide.", ap.ID)}
	}
	line, err := s.applyTaskLimits(k, target, t, id)
	if err != nil {
		return outcome{action: a, err: err.Error()}
	}
	return outcome{action: a, text: line + "."}
}

// applyTaskLimits makes a change of limits the lead asked for, and records it.
func (s *Service) applyTaskLimits(lead, target *kitty.Kitty, t kitty.TaskLimits, chatID string) (string, error) {
	setter, ok := s.fleet.(LimitsSetter)
	if !ok {
		return "", errors.New("limits cannot be changed here")
	}
	line := lead.Name + " changed " + target.Name + "'s limits per task: " + t.Describe(target.Limits)
	if _, err := setter.SetTaskLimits(target.ID, t); err != nil {
		return "", err
	}
	s.note(lead, agenda.KindLimit, s.goalOf(chatID), chatID, line)
	s.note(target, agenda.KindLimit, "", chatID, line)
	if reports, err := s.ReportsChat(lead); err == nil && reports != chatID {
		s.appendTurn(reports, Turn{At: s.now(), From: lead.Name, Kitty: lead.ID, Text: line, Report: &Report{Event: "limits", Chat: chatID}})
	}
	return line, nil
}

// decideLeadLimits runs a change of limits the person approved.
func (s *Service) decideLeadLimits(ap *approvals.Approval) (string, error) {
	lead, err := s.fleet.Get(ap.Kitty)
	if err != nil {
		return "", err
	}
	ref, _ := ap.Args["kitty"].(string)
	target, err := s.fleet.Get(ref)
	if err != nil {
		return "", err
	}
	t := taskLimitsArg(ap.Args)
	if err := t.Validate(); err != nil {
		return "", err
	}
	return s.applyTaskLimits(lead, target, t, ap.Chat)
}

// taskOverride is what one delegated task may spend beyond the kitty's own limits: the lead
// passes it with delegate, and it lasts only while that task runs.
func (s *Service) setTaskOverride(chatID string, t kitty.TaskLimits) func() {
	s.mu.Lock()
	if s.overrides == nil {
		s.overrides = map[string]kitty.TaskLimits{}
	}
	s.overrides[chatID] = t
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.overrides, chatID)
		s.mu.Unlock()
	}
}

// limitsFor are k's limits for a task in a thread: its own, with the one-task change the lead
// made for that thread if any, over the defaults; in a chat with people who are not its person
// (a WhatsApp group or contact), capped short.
func (s *Service) limitsFor(k *kitty.Kitty, chatID string) kitty.Limits {
	s.mu.RLock()
	t, ok := s.overrides[chatID]
	s.mu.RUnlock()
	l := k.Limits
	if ok {
		l = t.Apply(l)
	}
	l = l.Or(s.DefaultLimits)
	if s.guardedID(chatID) {
		l = exposedLimits(l)
	}
	return l
}

// intArg reads a whole number from the arguments, given as a number or as text.
func intArg(args map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		switch v := args[key].(type) {
		case float64:
			if v == float64(int(v)) {
				return int(v), true
			}
		case int:
			return v, true
		case string:
			var n int
			if _, err := fmt.Sscan(strings.TrimSpace(v), &n); err == nil {
				return n, true
			}
		}
	}
	return 0, false
}
