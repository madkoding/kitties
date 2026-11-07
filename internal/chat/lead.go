package chat

import (
	"fmt"
	"strings"

	"github.com/madkoding/kitties/internal/agenda"
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
	s.appendTurn(id, Turn{At: s.now(), From: from, Kitty: fromID, Text: text, Report: &Report{Event: string(e), Chat: chatID}})
}

// Report says where a turn of the Reports thread comes from.
type Report struct {
	// Event is the kind of news (notify.Event: goal_done, help, approval...).
	Event string `json:"event"`
	// Chat is the thread it happened in, empty when it happened in none.
	Chat string `json:"chat,omitempty"`
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

// roundsHelp is what the lead's rounds tool says about itself.
const roundsHelp = "Change how many rounds of actions another kitty may take on one task (its max_rounds): more for a long task " +
	"you hand it, fewer to keep one short. 0 goes back to the default. The change lasts until it is changed again; " +
	"say in your report that you made it."

// RoundsSetter changes a kitty's rounds per task; the orchestrator is one.
type RoundsSetter interface {
	SetRounds(ref string, rounds int) (*kitty.Kitty, error)
}

// canSetRounds reports whether k may change the others' rounds: only the lead, on a fleet that
// can change them, with another kitty to change.
func (s *Service) canSetRounds(k *kitty.Kitty) bool {
	if k == nil || !k.Lead {
		return false
	}
	if _, ok := s.fleet.(RoundsSetter); !ok {
		return false
	}
	for _, other := range s.fleet.List() {
		if other.ID != k.ID {
			return true
		}
	}
	return false
}

// runRounds is the lead changing another kitty's rounds per task. Only the lead may, within
// kitty.MaxRoundsCap; the change goes to both journals and to the lead's Reports thread.
func (s *Service) runRounds(id string, a action, k *kitty.Kitty) outcome {
	if !k.Lead {
		return outcome{action: a, err: "only the lead kitty changes the others' rounds"}
	}
	setter, ok := s.fleet.(RoundsSetter)
	if !ok {
		return outcome{action: a, err: "rounds cannot be changed here"}
	}
	name := strings.TrimSpace(a.name)
	if name == "" {
		name = argText(a.args, "kitty")
	}
	if name == "" {
		return outcome{action: a, err: "say which kitty: @rounds <name> {\"max_rounds\": 40}"}
	}
	target, err := s.fleet.Get(name)
	if err != nil {
		return outcome{action: a, err: "no such kitty " + name}
	}
	if target.ID == k.ID {
		return outcome{action: a, err: "the lead changes the others' rounds, not its own; the person sets yours"}
	}
	n, ok := intArg(a.args, "max_rounds", "rounds")
	if !ok {
		return outcome{action: a, err: fmt.Sprintf("say how many: {\"max_rounds\": 1 to %d, or 0 for the default}", kitty.MaxRoundsCap)}
	}
	if n < 0 || n > kitty.MaxRoundsCap {
		return outcome{action: a, err: fmt.Sprintf("rounds go from 1 to %d (0 for the default)", kitty.MaxRoundsCap)}
	}
	before := target.Limits.MaxRounds
	if _, err := setter.SetRounds(target.ID, n); err != nil {
		return outcome{action: a, err: err.Error()}
	}
	say := func(v int) string {
		if v == 0 {
			return "the default"
		}
		return fmt.Sprint(v)
	}
	line := fmt.Sprintf("%s set %s's rounds per task from %s to %s", k.Name, target.Name, say(before), say(n))
	s.note(k, agenda.KindLimit, s.goalOf(id), id, line)
	s.note(target, agenda.KindLimit, "", id, line)
	if reports, err := s.ReportsChat(k); err == nil && reports != id {
		s.appendTurn(reports, Turn{At: s.now(), From: k.Name, Kitty: k.ID, Text: line, Report: &Report{Event: "rounds", Chat: id}})
	}
	return outcome{action: a, text: line + "."}
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
