package chat

import (
	"fmt"
	"strings"

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
