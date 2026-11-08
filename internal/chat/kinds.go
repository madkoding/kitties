package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/madkoding/kitties/internal/agenda"
)

// WHAT KIND OF THREAD A CHAT IS.
//
// Not every thread is a conversation with the person. A goal's thread is where a scheduled task
// runs, again and again: it belongs to the task, which keeps its history of runs (Runs), not to
// the chats. A hand-over thread is where one kitty asked another for a subtask: the answer
// already shows in the chat that asked, under the turn that asked, so the thread is a detail
// a click away, kept for a while after the work ends (HandOverKeep) and then dropped, unless
// the person keeps it (Keep). Only plain chats, and the hand-overs the person kept, go in the
// sidebar (Listed).

const (
	// KindChat is a conversation with the person (or a group of kitties, or a WhatsApp contact).
	KindChat = "chat"
	// KindGoal is the thread a scheduled task (a goal) runs in.
	KindGoal = "goal"
	// KindHandOver is the thread between a kitty and another it handed a subtask to.
	KindHandOver = "handover"
)

// DefaultHandOverKeep is how long a hand-over thread stays after its work ended.
const DefaultHandOverKeep = 72 * time.Hour

// KindOf is what kind of thread a session is.
func KindOf(s *Session) string {
	switch {
	case s.Goal != "":
		return KindGoal
	case s.Owner != "" || s.Depth > 0:
		return KindHandOver
	default:
		return KindChat
	}
}

// Listed reports whether the chat goes in the list of chats (the sidebar, chat_list): every
// plain chat, and the hand-over threads the person chose to keep.
func (s *Session) Listed() bool {
	switch KindOf(s) {
	case KindGoal:
		return false
	case KindHandOver:
		return s.Keep
	}
	return true
}

// refreshKind sets the fields worked out from the others, for whoever reads the chat.
func refreshKind(s *Session) {
	s.Kind = KindOf(s)
	if s.Kind != KindHandOver {
		s.EndedAt, s.Keep = time.Time{}, false
	}
}

// SetHandOverKeep has how long a hand-over thread stays after it ended read from fn (the
// settings); zero keeps them all.
func (s *Service) SetHandOverKeep(fn func() time.Duration) { s.handOverKeep = fn }

func (s *Service) keepFor() time.Duration {
	if s.handOverKeep != nil {
		return s.handOverKeep()
	}
	return DefaultHandOverKeep
}

// expiresAt is when a hand-over thread goes, zero when it stays: one at work, kept, or with
// hand-overs kept forever.
func (s *Service) expiresAt(sess *Session) time.Time {
	keep := s.keepFor()
	if sess.Kind != KindHandOver || sess.Keep || keep <= 0 || sess.Working() {
		return time.Time{}
	}
	end := sess.EndedAt
	if end.IsZero() {
		// A thread from before hand-overs ended: its last word is its end.
		end = sess.UpdatedAt
	}
	return end.Add(keep)
}

// handOverStarted marks a hand-over thread at work again: it does not go while it works.
func (s *Service) handOverStarted(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok && !sess.EndedAt.IsZero() {
		sess.EndedAt = time.Time{}
		_ = s.save(sess)
	}
}

// handOverEnded marks the end of a hand-over's work: the thread goes HandOverKeep later.
func (s *Service) handOverEnded(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		sess.EndedAt = s.now()
		_ = s.save(sess)
	}
}

// ErrNotHandOver is returned when a chat that is no hand-over is asked to be kept.
var ErrNotHandOver = errors.New("only a hand-over thread between two kitties can be kept or let go")

// KeepChat keeps a hand-over thread (it stays, and shows in the chats) or lets it go again (it
// leaves the chats, and goes HandOverKeep after it ended).
func (s *Service) KeepChat(id string, keep bool) (*Session, error) {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("no such chat %q", id)
	}
	if KindOf(sess) != KindHandOver {
		s.mu.Unlock()
		return nil, ErrNotHandOver
	}
	sess.Keep = keep
	if !keep && !sess.Working() {
		// Let go now: it stays its whole time from here, not from a word long ago.
		sess.EndedAt = s.now()
	}
	err := s.save(sess)
	s.emitLocked(Event{Type: "updated", Session: id})
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}

// ExpireHandOvers drops the hand-over threads whose time is up, and returns how many. The chat
// that asked keeps the answer (its Delegation), so nothing the person reads goes with them.
func (s *Service) ExpireHandOvers() (int, error) {
	now := s.now()
	var due []string
	s.mu.RLock()
	for id, sess := range s.sessions {
		if at := s.expiresAt(sess); !at.IsZero() && !at.After(now) && len(sess.Helping) == 0 {
			due = append(due, id)
		}
	}
	s.mu.RUnlock()
	var firstErr error
	n := 0
	for _, id := range due {
		if err := s.Delete(id); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		n++
	}
	return n, firstErr
}

// expireEvery is how often the hand-over threads whose time is up are dropped.
const expireEvery = 30 * time.Minute

// RunExpiry drops the hand-over threads whose time is up now and then, until ctx ends.
func (s *Service) RunExpiry(ctx context.Context, logf func(string, ...any)) {
	tick := time.NewTicker(expireEvery)
	defer tick.Stop()
	for {
		if n, err := s.ExpireHandOvers(); err != nil {
			logf("dropping old hand-over threads: %v", err)
		} else if n > 0 {
			logf("dropped %d hand-over thread(s) whose work ended", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// TidyGoals turns the threads of goals that no longer exist back into plain chats, so a removed
// goal leaves its thread among the chats instead of nowhere; exists says whether a goal does.
// It returns how many it turned.
func (s *Service) TidyGoals(exists func(goal string) bool) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, sess := range s.sessions {
		if sess.Goal != "" && !exists(sess.Goal) {
			sess.Goal = ""
			if s.save(sess) == nil {
				n++
			}
		}
	}
	return n
}

// Run is one run of a scheduled task in its thread: from the scheduler's call to what the
// kitty said last before the next one.
type Run struct {
	// Turn is the index of the scheduler's turn that started the run; Turns how many it took.
	Turn  int       `json:"turn"`
	Turns int       `json:"turns"`
	At    time.Time `json:"at"`
	End   time.Time `json:"end,omitempty"`
	// Why is what started it (the schedule, an event, the person, a follow-up).
	Why string `json:"why,omitempty"`
	// Result is how it ended: running, done, blocked, escalated, delegated or failed.
	Result string `json:"result"`
	// Answer is the kitty's last words in the run, Kitty its name.
	Answer string `json:"answer,omitempty"`
	Kitty  string `json:"kitty,omitempty"`
	// Actions is how many desktop actions the run took.
	Actions int `json:"actions,omitempty"`
}

// goalPrefix starts the scheduler's turn that runs a goal (RunGoal); a turn of the scheduler
// without it (a parked task woken up) is part of the run before.
const goalPrefix = "Goal: "

// Runs is the history of a goal's runs in its thread, newest first, at most n (0: all).
func (s *Service) Runs(goal string, n int) (chatID string, runs []Run, err error) {
	s.mu.RLock()
	var sess *Session
	for _, c := range s.sessions {
		if c.Goal == goal && (sess == nil || c.UpdatedAt.After(sess.UpdatedAt)) {
			sess = c
		}
	}
	if sess == nil {
		s.mu.RUnlock()
		return "", []Run{}, nil
	}
	chatID = sess.ID
	turns := append([]Turn(nil), sess.Turns...)
	owner := ""
	if len(sess.Members) > 0 {
		owner = sess.Members[0]
	}
	s.mu.RUnlock()
	var journal []agenda.Entry
	if s.journal != nil && owner != "" {
		journal = s.journal.Recent(owner, 2000)
	}
	runs = splitRuns(turns)
	for i := range runs {
		next := time.Time{}
		if i+1 < len(runs) {
			next = runs[i+1].At
		}
		if r := journalResult(journal, goal, chatID, runs[i].At, next); r != "" && runs[i].Result != "running" {
			runs[i].Result = r
		}
	}
	for i, j := 0, len(runs)-1; i < j; i, j = i+1, j-1 {
		runs[i], runs[j] = runs[j], runs[i]
	}
	if n > 0 && len(runs) > n {
		runs = runs[:n]
	}
	return chatID, runs, nil
}

// splitRuns cuts a goal's thread at each of the scheduler's calls.
func splitRuns(turns []Turn) []Run {
	var runs []Run
	for i, t := range turns {
		if t.From == "scheduler" && strings.HasPrefix(t.Text, goalPrefix) {
			runs = append(runs, Run{Turn: i, At: t.At, Why: whyOf(t.Text), Result: "done"})
		}
		if len(runs) == 0 {
			continue // the thread's words before its first run
		}
		r := &runs[len(runs)-1]
		r.Turns++
		if t.At.After(r.End) {
			r.End = t.At
		}
		if t.Kitty == "" {
			continue
		}
		r.Kitty = t.From
		r.Answer = Gist(t.Text)
		r.Actions += countActions(t.Steps)
		switch {
		case t.Working:
			r.Result = "running"
		case t.NeedsHelp:
			r.Result = "escalated"
		case strings.HasPrefix(t.Text, "("+t.From+" could not"):
			r.Result = "failed"
		default:
			r.Result = "done"
		}
	}
	return runs
}

// whyOf is the "Why now" of a goal's call.
func whyOf(text string) string {
	const why = "\n\nWhy now: "
	i := strings.Index(text, why)
	if i < 0 {
		return ""
	}
	rest := text[i+len(why):]
	if j := strings.Index(rest, "\n"); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest)
}

// countActions is how many of the steps are desktop actions (a plan line is none).
func countActions(steps []Step) int {
	n := 0
	for _, st := range steps {
		if st.Action != StepPlan {
			n++
		}
	}
	return n
}

// journalResult is how the journal says a run ended: the first result of the goal written in
// [from, to) (to zero: no end).
func journalResult(entries []agenda.Entry, goal, chatID string, from, to time.Time) string {
	result := ""
	for _, e := range entries { // newest first: the last match is the first result
		if e.Goal != goal || (e.Chat != "" && e.Chat != chatID) || e.At.Before(from) || (!to.IsZero() && !e.At.Before(to)) {
			continue
		}
		switch e.Kind {
		case agenda.KindDone, agenda.KindBlocked, agenda.KindEscalated, agenda.KindDelegated:
			result = e.Kind
		}
	}
	return result
}
