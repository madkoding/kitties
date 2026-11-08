package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/madkoding/kitties/internal/agenda"
	"github.com/madkoding/kitties/internal/desktop"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/notify"
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
	if result == "asked" {
		// It waits for the lead's answer: the run goes on when it comes, and reports then.
		if lead := s.leadFor(k); lead != nil {
			s.note(k, agenda.KindEscalated, goal, chatID, "asked "+lead.Name+": "+firstLine(text))
		}
		return
	}
	kind := agenda.KindDone
	switch result {
	case "blocked":
		kind = agenda.KindBlocked
		s.tell(notify.GoalBlocked, k, "could not finish a goal", summaryLine(text), chatID)
	case "escalated":
		kind = agenda.KindEscalated
	case "delegated":
		kind = agenda.KindDelegated
		s.tell(notify.GoalDone, k, "finished a goal (through "+"another kitty)", summaryLine(text), chatID)
	default:
		s.tell(notify.GoalDone, k, "finished a goal", summaryLine(text), chatID)
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
	// A task from the lead goes first: the kitty's own goal waits for it.
	if s.waitForLeadWork(ctx, k) {
		s.note(k, agenda.KindSkipped, g.ID, id, "waited for "+s.Lead().Name+"'s task before "+g.Title)
	}
	call := goalPrefix + g.Title + "\n" + g.Text + "\n\nWhy now: " + t.Text()
	text := call + "\n\nWork on it now. When you are done, say what you did and what is left. If you are stuck, say what you need."
	if g.Flow != "" && k.Desktop {
		out, ok := s.playFlow(ctx, id, g, k, call)
		if ok {
			return id, nil
		}
		// The name is quoted as JSON: whatever it holds stays inside its quotes.
		name, _ := json.Marshal(g.Flow)
		text += fmt.Sprintf("\n\nYour saved page flow %s ran first and did not get through:\n%s\n"+
			"Finish the goal yourself; once you know the right steps, save them again with page_flow and {\"save\": %s}.",
			name, clip(out, maxFlowOut), name)
	}
	if t.Kind == "follow_up" {
		text += s.todoNote(ctx, k)
	}
	if _, err := s.send(ctx, id, "scheduler", "", text); err != nil {
		return id, err
	}
	return id, nil
}

// maxFlowOut is how much of a saved flow's output goes into a turn or to the kitty.
const maxFlowOut = 6000

// playFlow plays a goal's saved page flow on the kitty's desktop, without the model: a run of a
// routine the kitty already knows costs no tokens. The thread gets a turn with what it did; a
// flow that went through reports the goal done. It returns the flow's output and whether it went
// through; one that did not (or a thread at work) leaves the goal to the kitty.
func (s *Service) playFlow(ctx context.Context, id string, g agenda.Goal, k *kitty.Kitty, call string) (string, bool) {
	runner := s.runner()
	if runner == nil {
		return "this gateway has no desktops", false
	}
	if sess, err := s.Get(id); err == nil && sess.Working() {
		return "the thread was at work", false
	}
	if m := s.machine(); m != nil {
		if p, err := m.Power(k.ID); err == nil && p.State != kitty.PowerOn {
			if err := m.PowerOnWait(ctx, k.ID, machineWait); err != nil {
				return "the machine could not be turned on: " + err.Error(), false
			}
		}
	}
	a := desktop.Action{Name: "page_flow", Args: map[string]any{"name": g.Flow}}
	ctx, cancel := context.WithTimeout(ctx, flowTimeout)
	defer cancel()
	out, err := desktop.Run(ctx, runner, k.ID, a)
	if err != nil {
		if out == "" {
			out = err.Error()
		}
		return out, false
	}
	text := fmt.Sprintf("I played my saved steps \u201c%s\u201d for this goal, without thinking them over again.\n\n%s", g.Flow, lastRead(out))
	// The scheduler's call opens the run, as when the kitty works the goal itself: the task's
	// history counts its runs by it (kinds.go).
	s.appendTurn(id, Turn{At: s.now(), From: "scheduler", Text: call})
	s.appendTurn(id, Turn{At: s.now(), From: k.Name, Kitty: k.ID, Text: text, Steps: []Step{{At: s.now(), Action: "page_flow", Args: a.Args, Result: clip(out, maxFlowOut)}}, Looks: &Looks{Reads: 1, ReadTokens: (len(out) + 3) / 4}})
	s.reportGoal(k, g.ID, id, "done", text)
	return out, true
}

// flowTimeout bounds a saved flow played for a goal: 30 steps of at most 30 seconds each.
const flowTimeout = 16 * time.Minute

// lastRead is the end of a flow's output, where it says how it finished and what the page
// showed, for the turn's text.
func lastRead(out string) string {
	if i := strings.Index(out, "the flow finished"); i >= 0 {
		return clip(strings.TrimSpace(out[i:]), 1500)
	}
	return clip(out, 1500)
}

// maxTodo is how much of a kitty's TODO.md goes with a follow-up.
const maxTodo = 4000

// todoNote is the kitty's own list of what is done and left (TODO.md in its workspace), for a
// run that goes on with unfinished work; empty when it keeps none or its desktop cannot be read.
func (s *Service) todoNote(ctx context.Context, k *kitty.Kitty) string {
	runner := s.runner()
	if runner == nil || !k.Desktop {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := runner.Exec(ctx, k.ID, "cat", workspaceOf(k)+"/TODO.md")
	if out = strings.TrimSpace(out); err != nil || out == "" {
		return ""
	}
	return "\n\nYour TODO.md (what you wrote down as done and left) says:\n" + clip(out, maxTodo) +
		"\nStart from the first item left; keep the file up to date as you go."
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
	// The kitty joins the chat it helps in at once, so the chat reads as a group while it works.
	s.addDelegate(id, target.ID)
	d := Delegation{Kitty: target.Name, KittyID: target.ID, Chat: did, Task: clip(task, 300)}
	s.note(k, agenda.KindDelegated, "", id, "to "+target.Name+": "+firstLine(task))
	// The lead may give the kitty more (or less) for this task alone: it ends with the task.
	if t := taskLimitsArg(a.args); k.Lead && !t.Empty() {
		if err := t.Validate(); err != nil {
			return outcome{action: a, err: err.Error()}
		}
		defer s.setTaskOverride(did, t)()
		line := "for this task only, " + target.Name + "'s " + t.Describe(target.Limits)
		d.Limits = line
		s.note(target, agenda.KindLimit, "", did, k.Name+" gave "+line)
	}
	done := s.helping(id, Helper{Kitty: target.Name, KittyID: target.ID, From: k.Name, Task: d.Task, Chat: did, Since: s.now()})
	s.handOverStarted(did)
	answer, err := s.askKitty(ctx, did, k, target, task)
	// The thread between the two is done with: it goes after a while (kinds.go), the answer
	// stays in this chat.
	s.handOverEnded(did)
	done()
	if err != nil {
		d.Error = err.Error()
		turn.Delegations = append(turn.Delegations, d)
		// The lead (its Reports thread) and the person, when they asked for it, hear how it went,
		// with secrets masked as in the sidebar.
		s.tellDelegation(k, target, "could not do "+k.Name+"'s subtask", Gist(task)+" — "+Gist(err.Error()), id)
		s.handOverFailed(target, err, id)
		return outcome{action: a, err: target.Name + ": " + err.Error()}
	}
	d.Result = clip(answer, 300)
	d.Answer = wholeAnswer(answer, 300)
	turn.Delegations = append(turn.Delegations, d)
	s.tellDelegation(k, target, "finished "+k.Name+"'s subtask", Gist(task)+" — "+Gist(answer), id)
	s.handOverDone(target)
	return outcome{action: a, text: answeredBy(target.Name) + answer}
}

// answeredBy starts what a kitty is told another answered. The person sees that answer already,
// under the turn that asked, so the kitty is asked to build on it instead of saying it again:
// the same words twice in a row is what made hand-overs read as duplicated chats.
func answeredBy(name string) string {
	return name + " answered (the person already sees this answer under your message: do not repeat it, say only what it means for the task):\n"
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
	limit := s.limitsFor(to, did).MaxSeconds
	if limit > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(limit)*time.Second)
		defer cancel()
	}
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
// there is one (unattended without one, a free kitty of the fleet: standIn) and the thread is
// not too deep already, else the person is asked. It returns
// the goal result, whether the person is needed, and what to add to the turn.
func (s *Service) giveUp(id string, index int, k *kitty.Kitty, fail string) (result string, needsHelp bool, text string) {
	request := s.lastRequest(id, index)
	backup := k.Backup
	if backup == "" && !k.Limits.Or(s.DefaultLimits).Attended {
		// No backup named: a kitty of the fleet that is free and can do the same takes it.
		if other := s.standIn(k); other != nil {
			backup = other.Name
		}
	}
	if backup != "" && s.depthOf(id) < s.MaxDepth {
		if target, err := s.fleet.Get(backup); err == nil && target.ID != k.ID {
			task := fmt.Sprintf("I am %s and I could not finish a task: %s.\nThe task: %s\nPlease take it over and tell me what you achieve.", k.Name, fail, request)
			var turn Turn
			o := s.delegate(context.Background(), id, action{kind: "delegate", name: target.Name, args: map[string]any{"task": task}, raw: "@delegate " + target.Name}, k, &turn)
			s.edit(id, index, func(t *Turn) { t.Delegations = append(t.Delegations, turn.Delegations...) })
			if o.err == "" {
				return "delegated", false, "Handed over to " + target.Name + ", who answered:\n" + strings.TrimPrefix(o.text, answeredBy(target.Name))
			}
			fail += "; " + target.Name + " could not take over: " + o.err
		}
	}
	if s.goalOf(id) != "" {
		// Work of its own (a goal): with a lead, the lead is asked first, not the person.
		question := fmt.Sprintf("I could not finish a task: %s.\nThe task: %s\nWhat should I do?", fail, clip(request, 1000))
		if lead, ok := s.askLead(k, id, question); ok {
			s.edit(id, index, func(t *Turn) { t.AskedLead = lead.Name })
			return "asked", false, "(" + k.Name + " asked " + lead.Name + " for help: " + Plain(fail) + ")"
		}
	}
	s.note(k, agenda.KindEscalated, "", id, fail)
	return "escalated", true, "(" + k.Name + " needs help: " + Plain(fail) + ")"
}

// standIn is the kitty that takes over a task k gave up on when k names no backup: another one
// with a desktop as k has, not in error and not at work on something else, the most like k
// (the connectors k uses, the skills of its soul); nil when none is free, or when k has handed
// over as many tasks today as its stand-ins allow.
func (s *Service) standIn(k *kitty.Kitty) *kitty.Kitty {
	var best *kitty.Kitty
	bestScore := -1
	for _, other := range s.fleet.List() {
		if other.ID == k.ID || other.Desktop != k.Desktop || other.Status == kitty.StatusError || s.KittyBusy(other.ID) {
			continue
		}
		if score := likeness(k, other); score > bestScore {
			best, bestScore = other, score
		}
	}
	if best == nil || !s.standInLeft(k.ID) {
		return nil
	}
	return best
}

// likeness is how well other can do k's work: two points for each connector of k it may use
// too (a kitty with no list may use every one), one for each skill their souls share, and one
// for a machine that is on.
func likeness(k, other *kitty.Kitty) int {
	score := 0
	for _, c := range k.Connectors {
		if len(other.Connectors) == 0 || contains(other.Connectors, c) {
			score += 2
		}
	}
	for _, a := range k.Soul.Skills {
		for _, b := range other.Soul.Skills {
			if strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) {
				score++
				break
			}
		}
	}
	if other.Status == kitty.StatusRunning {
		score++
	}
	return score
}

// DefaultStandIns is how many tasks a day a kitty without a backup may hand to a stand-in.
const DefaultStandIns = 3

// SetStandIns has the stand-ins a kitty may use a day read from fn (the settings); zero: none.
func (s *Service) SetStandIns(fn func() int) { s.standInsFrom = fn }

// standInLeft takes one of the kitty's stand-ins for today, and reports whether there was one.
// The count starts again each day, so a task nobody can do does not go round the whole fleet.
func (s *Service) standInLeft(kittyID string) bool {
	limit := DefaultStandIns
	if s.standInsFrom != nil {
		limit = s.standInsFrom()
	}
	day := s.now().Format("2006-01-02")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.standIns == nil || s.standInsDay != day {
		s.standIns, s.standInsDay = map[string]int{}, day
	}
	if s.standIns[kittyID] >= limit {
		return false
	}
	s.standIns[kittyID]++
	return true
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
