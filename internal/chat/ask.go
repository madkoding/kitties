package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/madkoding/kitties/internal/agenda"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/notify"
)

// THE OTHERS ASK THE LEAD, THE LEAD ASKS THE PERSON.
//
// With a lead kitty, the person talks with the lead alone. A kitty that doubts something in work
// it does on its own (a goal, a scheduled task) asks the lead (ask_lead), and a goal it gives up on
// goes to the lead too, instead of the person. The question lands in the lead's Reports thread
// and wakes it: when it knows, it answers (answer_kitty) and the answer goes back to the thread
// that asked, where the kitty carries on; when only the person can decide, the lead asks them
// there, in its own name, and passes their answer on the same way.
//
// A hand-over from the lead is not asked back (the lead waits for its answer and reads the doubt
// in it), an exposed chat never reaches the lead, and a thread asks at most maxLeadAsks times
// between two messages of the person or the scheduler: past that, the person is asked.

// maxLeadAsks is how many questions a thread may put to the lead between two messages of the
// person (or of the scheduler).
const maxLeadAsks = 3

// leadAnswerWait is how long a question waits for the lead's thread to be free, and an answer
// for the asking thread to be.
var leadAnswerWait = 30 * time.Minute

// LeadAsk is a question a kitty put to the lead, kept on the lead's Reports thread.
type LeadAsk struct {
	// Kitty is the id of the kitty that asked, and Name its name.
	Kitty string `json:"kitty"`
	Name  string `json:"name"`
	// Chat is the thread it asked from, where the answer goes.
	Chat     string    `json:"chat"`
	Question string    `json:"question"`
	At       time.Time `json:"at"`
	// Answered is when the lead passed an answer on; zero while it is open.
	Answered time.Time `json:"answered,omitempty"`
}

// canAskLead is the lead k may ask from a thread: nil when there is none, k is it, or the
// thread is a hand-over, an exposed chat, the Reports thread, or has asked enough already.
func (s *Service) canAskLead(k *kitty.Kitty, id string) *kitty.Kitty {
	lead := s.leadFor(k)
	if lead == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	if !ok || sess.Owner != "" || sess.Exposed || sess.Reports {
		return nil
	}
	asked := 0
	for i := len(sess.Turns) - 1; i >= 0; i-- {
		t := sess.Turns[i]
		if t.From == "user" || t.From == "scheduler" {
			break
		}
		if t.AskedLead != "" {
			asked++
		}
	}
	if asked >= maxLeadAsks {
		return nil
	}
	return lead
}

// askLead puts k's question to the lead and wakes it in its Reports thread, in the background.
// It returns whether the question went to the lead (false: nobody to ask, or asked enough).
func (s *Service) askLead(k *kitty.Kitty, id, question string) (*kitty.Kitty, bool) {
	question = strings.TrimSpace(question)
	lead := s.canAskLead(k, id)
	if lead == nil || question == "" {
		return nil, false
	}
	reports, err := s.ReportsChat(lead)
	if err != nil || reports == id {
		return nil, false
	}
	about := ""
	if sess, err := s.Get(id); err == nil && sess.Title != "" {
		about = " (working on \"" + sess.Title + "\")"
	}
	s.mu.Lock()
	if sess, ok := s.sessions[reports]; ok {
		sess.Asks = append(sess.Asks, LeadAsk{Kitty: k.ID, Name: k.Name, Chat: id, Question: clip(question, 2000), At: s.now()})
		if len(sess.Asks) > 50 {
			sess.Asks = sess.Asks[len(sess.Asks)-50:]
		}
		_ = s.save(sess)
	}
	s.mu.Unlock()
	text := fmt.Sprintf("Question from %s%s: %s\n\nIf you know, answer it: @answer_kitty %s {\"text\": \"...\"} "+
		"(it goes back to %s, who carries on). If only the person can decide, ask them in one short message; "+
		"once they answer, pass it on the same way.", k.Name, about, question, k.Name, k.Name)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.wakeLead(lead, k, reports, text, question, id)
	}()
	return lead, true
}

// wakeLead has the lead read a question in its Reports thread. When its turn ends with the
// question still open, the lead could not answer it: the person is asked, in the lead's name.
func (s *Service) wakeLead(lead, k *kitty.Kitty, reports, text, question, chatID string) {
	if err := s.sendWhenFree(reports, k.Name, k.ID, text); err != nil {
		// The lead could not be reached: the person hears the question, still in its name.
		s.tellPerson(lead, k, question, chatID)
		return
	}
	s.waitIdle(reports)
	if !s.askOpen(reports, k.ID, chatID) {
		return
	}
	sess, err := s.Get(reports)
	if err != nil || len(sess.Turns) == 0 {
		return
	}
	last := len(sess.Turns) - 1
	line := sess.Turns[last].Text
	if sess.Turns[last].Kitty == lead.ID {
		s.edit(reports, last, func(t *Turn) { t.NeedsHelp = true })
	} else {
		line = k.Name + " asks: " + question
	}
	s.notifyHelp(notify.Notification{Event: notify.Help, Kitty: lead.Name, Title: "needs your help", Body: summaryLine(line), Chat: reports})
}

// tellPerson is a question the lead could not take: it goes to the person as before, in the
// lead's name.
func (s *Service) tellPerson(lead, k *kitty.Kitty, question, chatID string) {
	name, title := signed(lead, k, "needs your help")
	s.notifyHelp(notify.Notification{Event: notify.Help, Kitty: name, Title: title, Body: summaryLine(question), Chat: chatID})
}

func (s *Service) notifyHelp(n notify.Notification) {
	if s.notifier != nil {
		s.notifier.Notify(n)
	}
}

// askOpen says whether the kitty's question from a thread still waits for the lead's answer.
func (s *Service) askOpen(reports, kittyID, chatID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[reports]
	if !ok {
		return false
	}
	for _, a := range sess.Asks {
		if a.Kitty == kittyID && a.Chat == chatID && a.Answered.IsZero() {
			return true
		}
	}
	return false
}

// sendWhenFree posts a message in a thread as soon as it is not at work, waiting up to
// leadAnswerWait, and waits for the answer it starts.
func (s *Service) sendWhenFree(id, from, fromID, text string) error {
	deadline := time.Now().Add(leadAnswerWait)
	for {
		_, err := s.send(context.Background(), id, from, fromID, text)
		if !errors.Is(err, ErrBusy) {
			return err
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(s.pollEvery)
	}
}

// waitIdle waits while a thread's last turn is still at work on its desktop.
func (s *Service) waitIdle(id string) {
	deadline := time.Now().Add(leadAnswerWait)
	for time.Now().Before(deadline) {
		sess, err := s.Get(id)
		if err != nil || !sess.Working() {
			return
		}
		time.Sleep(s.pollEvery)
	}
}

// askLeadHelp is what ask_lead says about itself.
const askLeadHelp = "Ask the lead kitty something you doubt in work you do on your own (a goal, a scheduled task), " +
	"instead of the person. Its answer comes back in this thread, and you go on from it: after asking, say in one line what you wait for and stop."

// runAskLead is a kitty asking the lead.
func (s *Service) runAskLead(id string, a action, k *kitty.Kitty, turn *Turn) outcome {
	question := strings.TrimSpace(argText(a.args, "question", "text", "name"))
	if question == "" {
		return outcome{action: a, err: "say what you ask: @ask_lead {\"question\": \"...\"}"}
	}
	lead, ok := s.askLead(k, id, question)
	if !ok {
		return outcome{action: a, err: "there is no lead to ask here (or you asked it enough already): ask the person in your answer"}
	}
	turn.AskedLead = lead.Name
	return outcome{action: a, text: "asked " + lead.Name + ". Its answer comes back in this thread: say in one line what you wait for and stop here."}
}

// answerKittyHelp is what the lead's answer_kitty tool says about itself.
const answerKittyHelp = "Answer a question another kitty asked you (it reached you in your Reports thread): the answer goes " +
	"back to the thread it asked from, and it carries on. If only the person can decide, ask them first and pass on what they say."

// runAnswerKitty is the lead answering a kitty's question: the answer goes, in the background, to
// the thread that asked.
func (s *Service) runAnswerKitty(a action, lead *kitty.Kitty) outcome {
	if !lead.Lead {
		return outcome{action: a, err: "only the lead kitty answers the others' questions"}
	}
	text := strings.TrimSpace(argText(a.args, "text", "answer"))
	if a.name == "" || text == "" {
		return outcome{action: a, err: "say whom and what: @answer_kitty <name> {\"text\": \"...\"}"}
	}
	target, err := s.fleet.Get(a.name)
	if err != nil {
		return outcome{action: a, err: "no such kitty " + a.name}
	}
	reports, err := s.ReportsChat(lead)
	if err != nil {
		return outcome{action: a, err: err.Error()}
	}
	var ask *LeadAsk
	s.mu.Lock()
	if sess, ok := s.sessions[reports]; ok {
		for i := len(sess.Asks) - 1; i >= 0; i-- {
			if sess.Asks[i].Kitty == target.ID && sess.Asks[i].Answered.IsZero() {
				sess.Asks[i].Answered = s.now()
				q := sess.Asks[i]
				ask = &q
				break
			}
		}
		if ask != nil {
			// Once answered, the help the thread waited for is no longer the person's.
			if n := len(sess.Turns); n > 0 && sess.Turns[n-1].NeedsHelp {
				sess.Turns[n-1].NeedsHelp = false
			}
			_ = s.save(sess)
		}
	}
	s.mu.Unlock()
	if ask == nil {
		return outcome{action: a, err: target.Name + " has no question waiting for you; to give it work, use delegate"}
	}
	msg := fmt.Sprintf("%s answers your question (%s):\n%s\n\nGo on with the task from here.", lead.Name, clip(ask.Question, 300), text)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.sendWhenFree(ask.Chat, lead.Name, lead.ID, msg); err != nil {
			s.note(lead, agenda.KindBlocked, "", ask.Chat, "could not pass the answer to "+target.Name+": "+err.Error())
		}
	}()
	return outcome{action: a, text: "sent to " + target.Name + ", who goes on in its own thread"}
}

// askedLead says whether a turn put a question to the lead.
func (s *Service) askedLead(id string, index int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	return ok && index >= 0 && index < len(sess.Turns) && sess.Turns[index].AskedLead != ""
}

// goldfishFrom is where a kitty's view of a hand-over from the lead starts: the lead's last
// task, so each task is read fresh, without the ones before it (the lead keeps the long memory).
// -1 for any other thread, or for the lead itself.
func (s *Service) goldfishFrom(sess *Session, k *kitty.Kitty) int {
	if sess.Owner == "" || sess.Owner == k.ID {
		return -1
	}
	lead := s.Lead()
	if lead == nil || lead.ID != sess.Owner {
		return -1
	}
	for i := len(sess.Turns) - 1; i >= 0; i-- {
		if sess.Turns[i].Kitty == lead.ID {
			return i
		}
	}
	return -1
}

// leadWork says whether k is at work on a task the lead handed it.
func (s *Service) leadWork(k *kitty.Kitty) bool {
	lead := s.leadFor(k)
	if lead == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sess := range s.sessions {
		if sess.Owner == lead.ID && len(sess.Members) == 1 && sess.Members[0] == k.ID && sess.Working() {
			return true
		}
	}
	return false
}

// waitForLeadWork holds k's own goal while it works on a task from the lead: the lead's work goes
// first. It waits up to leadAnswerWait, and says whether it waited.
func (s *Service) waitForLeadWork(ctx context.Context, k *kitty.Kitty) bool {
	waited := false
	deadline := time.Now().Add(leadAnswerWait)
	for s.leadWork(k) && time.Now().Before(deadline) {
		waited = true
		select {
		case <-ctx.Done():
			return waited
		case <-time.After(s.pollEvery):
		}
	}
	return waited
}
