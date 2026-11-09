package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/madkoding/kitties/internal/connector"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/provider"
	"github.com/madkoding/kitties/internal/soul"
)

// EXPOSED CHATS.
//
// A chat is exposed when whoever writes in it is not the person: a WhatsApp contact or group, a
// chat the person made public. There the guard (internal/guard) decides: which messages are
// answered (Admit), what the kitty may use (only the connections picked for exposed kitties: no
// desktop, files, vault, library, notes, delegation), what it is told (Prompt), and what leaves
// (Filter). What a third party writes reaches the model marked as data.

// Guard is what the chats need of the guardrails.
type Guard interface {
	// On reports whether the exposed chats are guarded.
	On() bool
	// Admit says whether a message of an exposed chat is answered.
	Admit(chat, who, text string) error
	// Filter takes out of an answer what must not reach the other side; keep is what they wrote.
	Filter(chat, who, text, keep string) string
	// Outbound takes the secrets out of what a kitty sends to other people through a connection.
	Outbound(where, text string) string
	// Connectors keeps the connections an exposed kitty may still use.
	Connectors(allowed map[string]string) map[string]string
	// Blocked records a tool an exposed kitty asked for and did not get.
	Blocked(chat, who, what string)
	// Prompt is what an exposed kitty is told; person names the person.
	Prompt(person string) string
}

// SetGuard gives the chats their guardrails; nil leaves the exposed chats unguarded.
func (s *Service) SetGuard(g Guard) { s.guard = g }

// ErrNotFound is a chat that does not exist.
var ErrNotFound = errors.New("no such chat")

// Expose marks a chat as exposed (who writes is not the person) or takes the mark off; who says
// who writes there, for the record.
func (s *Service) Expose(id, who string, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return fmt.Errorf("%w %q", ErrNotFound, id)
	}
	if sess.Exposed == on && (!on || sess.Audience == who || who == "") {
		return nil
	}
	sess.Exposed = on
	if on && who != "" {
		sess.Audience = who
	}
	if !on {
		sess.Audience = ""
	}
	return s.save(sess)
}

// guarded reports whether a chat is exposed and the guard is on for it.
func (s *Service) guarded(sess *Session) bool {
	return sess != nil && sess.Exposed && s.guard != nil && s.guard.On()
}

// guardedID is guarded for a chat by its id.
func (s *Service) guardedID(id string) bool {
	if s.guard == nil || id == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.guarded(s.sessions[id])
}

// exposedView is the kitty as an exposed chat sees it: without its desktop, its workspace, the
// lead's role or the connections it may not use there.
func exposedView(k *kitty.Kitty) *kitty.Kitty {
	v := *k
	v.Desktop, v.Lead = false, false
	return &v
}

// exposedAllows reports whether an exposed kitty may run an action: only the connections it kept.
func exposedAllows(a action, allowed map[string]string) bool {
	if a.kind != "connector" {
		return false
	}
	_, ok := allowed[a.name]
	return ok
}

// theirWords is what the third parties wrote in a chat: the personal data in it are theirs.
func theirWords(sess *Session) string {
	var b strings.Builder
	for _, t := range sess.Turns {
		if t.Kitty == "" {
			b.WriteString(t.Text)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// scrubOutgoing takes the secrets out of the text of a connector call that reaches other people
// (a message, a post, a mail), in any chat.
func (s *Service) scrubOutgoing(a action) action {
	if s.guard == nil || a.kind != "connector" {
		return a
	}
	kind := a.name
	if c, ok := s.fleet.Connectors().Get(a.name); ok {
		if kinder, ok := c.(connector.Kinder); ok {
			kind = kinder.Kind()
		}
	}
	if connector.RiskOfCall(kind, a.args) == connector.RiskNone {
		return a
	}
	args := make(map[string]any, len(a.args))
	for key, v := range a.args {
		if str, ok := v.(string); ok {
			v = s.guard.Outbound(a.name, str)
		}
		args[key] = v
	}
	a.args = args
	return a
}

// answerExposed is answer in an exposed chat. The kitty is shown its soul, the rules of the
// guard and the thread, with the third parties' words marked as data; it gets nothing of the
// person (notes, vault, library, other kitties, workspace) and may only call the connections the
// person left it. Whatever else it asks for is refused and recorded, and its answer is filtered
// before it is kept, so the same text is what the chat shows and what goes out.
func (s *Service) answerExposed(ctx context.Context, id string, k *kitty.Kitty, members []*kitty.Kitty, brain provider.Client) (Turn, *pendingTask, error) {
	view := exposedView(k)
	allowed := s.guard.Connectors(s.fleet.Connectors().Descriptions(k.Connectors))
	var group []string
	for _, m := range members {
		if m.ID != k.ID {
			group = append(group, m.Name)
		}
	}
	system := k.Soul.SystemPrompt(soul.Context{KittyName: k.Name, Group: group, Connectors: allowed, User: s.user}) + s.guard.Prompt(s.user)
	s.compact(ctx, id, brain)
	sess, err := s.Get(id)
	if err != nil {
		return Turn{}, nil, err
	}
	system += descriptionPrompt(sess) + s.sendersPrompt(sess)
	if sess.Summary != "" {
		system += fmt.Sprintf("\nEarlier in this conversation (a summary of its first %d messages):\n%s\n", sess.SummaryUpTo, sess.Summary)
	}
	window := sess.Turns[min(sess.SummaryUpTo, len(sess.Turns)):]
	messages := s.transcriptFor(id, brain, nil, system, window, view)

	native := false
	var tools []provider.Tool
	if tc, ok := brain.(provider.ToolClient); ok && s.nativeTools && tc.SupportsTools() {
		for _, t := range s.toolSet(view, allowed) {
			if strings.HasPrefix(t.Name, prefixConnector) {
				tools = append(tools, t)
			}
		}
		native = len(tools) > 0
	}
	turn := Turn{From: k.Name, Kitty: k.ID}
	var parts []string
	for round := 0; round < max(s.MaxRounds, 1); round++ {
		if err := s.overTokens(k); err != nil {
			return Turn{}, nil, err
		}
		reply, err := ask(ctx, brain, native, messages, tools)
		if err != nil {
			return Turn{}, nil, err
		}
		s.spendTokens(k, reply.Usage)
		turn.Usage = turn.Usage.Add(reply.Usage)
		var actions []action
		text := reply.Content
		if native && len(reply.Calls) > 0 {
			actions = fromCalls(reply.Calls)
		} else {
			actions, text = fromText(reply.Content)
		}
		if text = strings.TrimSpace(text); text != "" {
			parts = append(parts, text)
		}
		if len(actions) == 0 {
			break
		}
		var kept []action
		var outs []outcome
		for _, a := range actions {
			if exposedAllows(a, allowed) {
				kept = append(kept, a)
				continue
			}
			s.guard.Blocked(id, sess.Audience, strings.TrimSpace(a.kind+" "+a.name))
			outs = append(outs, outcome{action: a, err: "not available in this chat: you are talking with someone who is not your person; answer in words"})
		}
		outs = append(outs, s.runNonDesktop(ctx, id, kept, k, allowed, &turn)...)
		messages = append(messages, feedback(native, reply, reply.Content, outs, nil)...)
	}
	turn.At = s.now()
	turn.Text = strings.TrimSpace(strings.Join(parts, "\n\n"))
	if turn.Text == "" {
		turn.Text = "(" + k.Name + " had nothing to add)"
	}
	turn.Text = s.guard.Filter(id, sess.Audience, turn.Text, theirWords(sess))
	return turn, nil, nil
}
