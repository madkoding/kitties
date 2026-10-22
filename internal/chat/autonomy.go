package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/madkoding/kitties/internal/agenda"
	"github.com/madkoding/kitties/internal/kitty"
)

// HOW A KITTY WORKS ON ITS OWN.
//
// The agenda wakes a kitty up (RunGoal) in a chat of the goal's own; the task loop keeps it
// within its limits and tries another way when an attempt fails; when it gives up, the backup
// kitty takes the task over or the person is asked (giveUp); and at any point a kitty may hand
// a subtask to another (delegate). Everything of note lands in the kitty's journal.

// SetAgenda gives the chats the journal to write and the line to report a goal's result on.
func (s *Service) SetAgenda(j *agenda.Journal, r Reporter) {
	s.journal, s.goals = j, r
}

// note writes a line in the kitty's journal, when there is one.
func (s *Service) note(k *kitty.Kitty, kind, goal, chatID, text string) {
	if s.journal == nil || k == nil {
		return
	}
	_ = s.journal.Add(agenda.Entry{Kitty: k.ID, Kind: kind, Goal: goal, Chat: chatID, Text: text})
}

// reportGoal closes a goal's run: the result goes to the agenda and the journal.
func (s *Service) reportGoal(k *kitty.Kitty, goal, chatID, result, text string) {
	if s.goals != nil {
		s.goals.Report(goal, result)
	}
	kind := agenda.KindDone
	switch result {
	case "blocked":
		kind = agenda.KindBlocked
	case "escalated":
		kind = agenda.KindEscalated
	case "delegated":
		kind = agenda.KindDelegated
	}
	s.note(k, kind, goal, chatID, firstLine(text))
}

// RunGoal implements agenda.Runner: the kitty is told its goal in the goal's own thread and
// works on it as it would on a person's message. The thread is returned; the result arrives
// through the Reporter when the work ends.
func (s *Service) RunGoal(ctx context.Context, g agenda.Goal, t agenda.Trigger) (string, error) {
	k, err := s.fleet.Get(g.Kitty)
	if err != nil {
		return "", err
	}
	id, err := s.goalChat(g, k)
	if err != nil {
		return "", err
	}
	text := "Goal: " + g.Title + "\n" + g.Text + "\n\nWhy now: " + t.Text() +
		"\n\nWork on it now. When you are done, say what you did and what is left. If you are stuck, say what you need."
	if _, err := s.send(ctx, id, "scheduler", "", text); err != nil {
		return id, err
	}
	return id, nil
}

// goalChat finds the thread a goal is worked in, or opens one.
func (s *Service) goalChat(g agenda.Goal, k *kitty.Kitty) (string, error) {
	s.mu.RLock()
	if sess, ok := s.sessions[g.Chat]; ok && contains(sess.Members, k.ID) {
		s.mu.RUnlock()
		return sess.ID, nil
	}
	for _, sess := range s.sessions {
		if sess.Goal == g.ID && contains(sess.Members, k.ID) {
			s.mu.RUnlock()
			return sess.ID, nil
		}
	}
	s.mu.RUnlock()
	sess, err := s.Create(g.Title, []string{k.ID})
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess.Goal = g.ID
	return sess.ID, s.save(sess)
}

// canDelegate reports whether there is another kitty to hand work to and room to do so.
func (s *Service) canDelegate(k *kitty.Kitty) bool {
	if s.MaxDepth <= 0 {
		return false
	}
	for _, other := range s.fleet.List() {
		if other.ID != k.ID {
			return true
		}
	}
	return false
}

// peers names the other kitties this one may delegate to in this thread: none when the
// thread is already as deep in hand-overs as allowed.
func (s *Service) peers(k *kitty.Kitty, id string) []string {
	if s.depthOf(id) >= s.MaxDepth {
		return nil
	}
	var names []string
	for _, other := range s.fleet.List() {
		if other.ID != k.ID {
			names = append(names, other.Name)
		}
	}
	return names
}

// depthOf is how many hand-overs deep a thread is.
func (s *Service) depthOf(id string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sess, ok := s.sessions[id]; ok {
		return sess.Depth
	}
	return 0
}

// delegate runs a delegate action: the other kitty gets the task in a thread between the two,
// and its answer is the outcome. The delegating kitty waits for the work, within the other's
// time limit.
func (s *Service) delegate(ctx context.Context, id string, a action, k *kitty.Kitty, turn *Turn) outcome {
	task := strings.TrimSpace(argText(a.args, "task", "text"))
	if a.name == "" {
		return outcome{action: a, err: "say which kitty: @delegate <name> {\"task\": \"...\"}"}
	}
	if task == "" {
		return outcome{action: a, err: "say what to do: {\"task\": \"...\"}"}
	}
	target, err := s.fleet.Get(a.name)
	if err != nil {
		return outcome{action: a, err: "no such kitty " + a.name}
	}
	if target.ID == k.ID {
		return outcome{action: a, err: "a kitty cannot delegate to itself"}
	}
	depth := s.depthOf(id)
	if depth >= s.MaxDepth {
		return outcome{action: a, err: fmt.Sprintf("the task has been handed over %d times already; do it yourself or ask the person", depth)}
	}
	did, err := s.delegationChat(k, target, depth+1)
	if err != nil {
		return outcome{action: a, err: err.Error()}
	}
	d := Delegation{Kitty: target.Name, KittyID: target.ID, Chat: did, Task: clip(task, 300)}
	s.note(k, agenda.KindDelegated, "", id, "to "+target.Name+": "+firstLine(task))
	answer, err := s.askKitty(ctx, did, k, target, task)
	if err != nil {
		d.Error = err.Error()
		turn.Delegations = append(turn.Delegations, d)
		return outcome{action: a, err: target.Name + ": " + err.Error()}
	}
	d.Result = clip(answer, 300)
	turn.Delegations = append(turn.Delegations, d)
	return outcome{action: a, text: target.Name + " answered:\n" + answer}
}

// delegationChat is the thread between a kitty and the one it delegates to, at that depth.
func (s *Service) delegationChat(from, to *kitty.Kitty, depth int) (string, error) {
	s.mu.RLock()
	for _, sess := range s.sessions {
		if sess.Owner == from.ID && sess.Depth == depth && len(sess.Members) == 1 && sess.Members[0] == to.ID {
			s.mu.RUnlock()
			return sess.ID, nil
		}
	}
	s.mu.RUnlock()
	sess, err := s.Create(from.Name+" → "+to.Name, []string{to.ID})
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess.Owner, sess.Depth = from.ID, depth
	return sess.ID, s.save(sess)
}

// askKitty sends a task to another kitty in a thread and waits for its answer, including the
// background work it may start, within its time limit.
func (s *Service) askKitty(ctx context.Context, did string, from, to *kitty.Kitty, task string) (string, error) {
	limit := to.Limits.Or(s.DefaultLimits).MaxSeconds
	if limit <= 0 {
		limit = 600
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(limit)*time.Second)
	defer cancel()
	replies, err := s.send(ctx, did, from.Name, from.ID, task)
	if err != nil {
		if errors.Is(err, ErrBusy) {
			return "", errors.New("is busy with something else; try later")
		}
		return "", err
	}
	if len(replies) == 0 {
		return "", errors.New("did not answer")
	}
	// The other kitty may have gone to its desktop: wait for that turn to end.
	for {
		sess, err := s.Get(did)
		if err != nil {
			return "", err
		}
		if !sess.Working() {
			last := sess.Turns[len(sess.Turns)-1]
			if last.NeedsHelp {
				return "", errors.New("could not do it: " + firstLine(last.Text))
			}
			if strings.HasPrefix(last.Text, "("+to.Name+" could not answer: ") {
				return "", errors.New(strings.TrimSuffix(strings.TrimPrefix(last.Text, "("+to.Name+" "), ")"))
			}
			return last.Text, nil
		}
		select {
		case <-ctx.Done():
			s.Cancel(did)
			return "", errors.New("did not finish in time")
		case <-time.After(s.pollEvery):
		}
	}
}

// giveUp is what happens when a task is given up on: the backup kitty takes it over when
// there is one and the thread is not too deep already, else the person is asked. It returns
// the goal result, whether the person is needed, and what to add to the turn.
func (s *Service) giveUp(id string, index int, k *kitty.Kitty, fail string) (result string, needsHelp bool, text string) {
	request := s.lastRequest(id, index)
	if k.Backup != "" && s.depthOf(id) < s.MaxDepth {
		if target, err := s.fleet.Get(k.Backup); err == nil && target.ID != k.ID {
			task := fmt.Sprintf("I am %s and I could not finish a task: %s.\nThe task: %s\nPlease take it over and tell me what you achieve.", k.Name, fail, request)
			var turn Turn
			o := s.delegate(context.Background(), id, action{kind: "delegate", name: target.Name, args: map[string]any{"task": task}, raw: "@delegate " + target.Name}, k, &turn)
			s.edit(id, index, func(t *Turn) { t.Delegations = append(t.Delegations, turn.Delegations...) })
			if o.err == "" {
				return "delegated", false, "Handed over to " + target.Name + ", who answered:\n" + strings.TrimPrefix(o.text, target.Name+" answered:\n")
			}
			fail += "; " + target.Name + " could not take over: " + o.err
		}
	}
	s.note(k, agenda.KindEscalated, "", id, fail)
	return "escalated", true, "(" + k.Name + " needs help: " + fail + ")"
}

// lastRequest is what the thread asked for before the turn at index: the last message that
// is not the kitty's own.
func (s *Service) lastRequest(id string, index int) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	if !ok {
		return ""
	}
	for i := index - 1; i >= 0; i-- {
		if i < len(sess.Turns) && sess.Turns[i].Kitty != sess.Turns[index].Kitty {
			return clip(sess.Turns[i].Text, 1500)
		}
	}
	return "(no request found in the thread)"
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return clip(s, 200)
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
