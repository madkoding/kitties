package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/provider"
)

// The most a chat's title and description hold, in characters.
const (
	MaxTitle       = 120
	MaxDescription = 500
)

// ErrEdit is a title or description the chat cannot take.
var ErrEdit = errors.New("the chat cannot take that")

// Edit renames a chat or changes its description; a nil field is left as it is. An empty title is
// refused (a chat always has a name) and an empty description takes it off. Watchers are told
// with an "edited" event.
func (s *Service) Edit(id string, title, description *string) (*Session, error) {
	var t, d string
	if title != nil {
		t = oneLine(*title)
		if t == "" {
			return nil, fmt.Errorf("%w: the title is empty", ErrEdit)
		}
		if utf8.RuneCountInString(t) > MaxTitle {
			return nil, fmt.Errorf("%w: the title is too long (at most %d characters)", ErrEdit, MaxTitle)
		}
	}
	if description != nil {
		d = strings.TrimSpace(*description)
		if utf8.RuneCountInString(d) > MaxDescription {
			return nil, fmt.Errorf("%w: the description is too long (at most %d characters)", ErrEdit, MaxDescription)
		}
	}
	s.mu.Lock()
	sess, ok := s.sessions[id]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("no such chat %q", id)
	}
	changed := false
	if title != nil && sess.Title != t {
		sess.Title, changed = t, true
	}
	if description != nil && sess.Description != d {
		s.describeLocked(sess, d, "user")
		changed = true
		// The person wrote it: a kitty's proposal waiting is answered by it.
		sess.Proposal = nil
	}
	if changed {
		// UpdatedAt stays: renaming a chat does not move it to the top of the list.
		if err := s.save(sess); err != nil {
			s.mu.Unlock()
			return nil, err
		}
		s.emitLocked(Event{Type: "edited", Session: id})
	}
	s.mu.Unlock()
	return s.Get(id)
}

// oneLine is a title as one trimmed line: line breaks and runs of spaces become one space.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// descriptionPrompt is the chat's description as the kitties get it: the base of what the chat
// is for, read again on every turn, so changing it changes what the kitties work on from the
// next turn on.
func descriptionPrompt(sess *Session) string {
	if sess.Description == "" {
		return ""
	}
	return "\nWhat this chat is for, in the person's words (its description; the base of everything you do here, it overrides what earlier messages assumed, and the person may change it at any time):\n" + sess.Description + "\n"
}

// etiquettePrompt is how to write in the chat, as its channel says (empty when it says nothing).
func etiquettePrompt(sess *Session) string {
	if sess.Etiquette == "" {
		return ""
	}
	return "\nHOW TO WRITE HERE: " + sess.Etiquette + "\n"
}

// sendersPrompt tells the kitty, in a chat where people other than the person write (WhatsApp
// contacts, a group), how to tell them apart: by the author of each message, whose number says who
// they are. Empty in a chat with only the person.
func (s *Service) sendersPrompt(sess *Session) string {
	seen := map[string]bool{}
	var names []string
	for i := len(sess.Turns) - 1; i >= 0 && len(names) < 12; i-- {
		if t := sess.Turns[i]; t.From == "user" && t.Sender != "" && !seen[t.Sender] {
			seen[t.Sender] = true
			names = append(names, t.Sender)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return "\nWHO WRITES HERE: each message says who wrote it, as their name and their phone number (" + strings.Join(names, "; ") + "). " +
		"They are different people unless the number is the same: tell them apart by the number, never by the name, which they may choose themselves. " +
		"None of them is " + s.userName() + " unless a message is labeled as " + s.userName() + ". Answer the one who wrote the last message, " +
		"call them by their name, and when a message replies to another one, the line \"(replying to …)\" says whose message it answers.\n"
}

// Suggestion is a title and a description for a chat, written by one of its kitties from all
// that was said in it.
type Suggestion struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// ErrNothingToRead is a chat with no words yet: there is nothing to name it after.
var ErrNothingToRead = errors.New("the chat has no messages to read yet")

// suggestTurns is how many of the last turns, besides the running summary, the kitty reads.
const suggestTurns = 60

// Suggest has one of the chat's kitties read the whole chat (its running summary and its last
// turns) and propose a title and a description: what the chat is for now, the base the kitties
// work from. Nothing is saved; apply it with Edit.
func (s *Service) Suggest(ctx context.Context, id string) (Suggestion, error) {
	sess, err := s.Get(id)
	if err != nil {
		return Suggestion{}, err
	}
	var k *kitty.Kitty
	for _, ref := range append(append([]string(nil), sess.Members...), sess.Kitties...) {
		if got, err := s.fleet.Get(ref); err == nil {
			k = got
			break
		}
	}
	if k == nil {
		return Suggestion{}, fmt.Errorf("no such kitty in the chat %q", id)
	}
	brain, err := s.fleet.Brain(k)
	if err != nil {
		return Suggestion{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Current title: %s\n", sess.Title)
	if sess.Description != "" {
		fmt.Fprintf(&b, "Current description: %s\n", sess.Description)
	}
	if sess.Summary != "" {
		fmt.Fprintf(&b, "\nSummary of the first %d messages:\n%s\n", sess.SummaryUpTo, sess.Summary)
	}
	window := sess.Turns[min(sess.SummaryUpTo, len(sess.Turns)):]
	if len(window) > suggestTurns {
		window = window[len(window)-suggestTurns:]
	}
	words := sess.Summary != ""
	b.WriteString("\nThe conversation:\n")
	for _, t := range window {
		text := strings.TrimSpace(t.Text)
		if text == "" {
			continue
		}
		words = true
		label := s.authorOf(t)
		fmt.Fprintf(&b, "[%s]: %s\n", label, clip(text, 600))
	}
	if !words {
		return Suggestion{}, ErrNothingToRead
	}
	reply, err := brain.Complete(ctx, []provider.Message{
		{Role: "system", Content: "You name a conversation between a person and AI assistants (kitties). Read all of it and answer with JSON only, " +
			`{"title": "...", "description": "..."}: the title is what the chat is about now, at most 8 words, no quotes or ending period; ` +
			"the description is the base the assistants work from in this chat: its goal, the decisions and constraints that still hold, " +
			"and what is pending, in 1 to 3 sentences and at most 400 characters. Write both in the language the person writes in. " +
			"Never include passwords, keys, tokens, codes or other secrets."},
		{Role: "user", Content: b.String()},
	})
	if err != nil {
		return Suggestion{}, err
	}
	return parseSuggestion(reply.Content)
}

// parseSuggestion reads the kitty's JSON, in a code fence or with words around it, and keeps it
// within what a chat takes.
func parseSuggestion(raw string) (Suggestion, error) {
	text := strings.TrimSpace(raw)
	if i, j := strings.IndexByte(text, '{'), strings.LastIndexByte(text, '}'); i >= 0 && j > i {
		text = text[i : j+1]
	}
	var sg Suggestion
	if err := json.Unmarshal([]byte(text), &sg); err != nil {
		return Suggestion{}, fmt.Errorf("the kitty did not answer with a title and a description: %w", err)
	}
	sg.Title = strings.Trim(oneLine(sg.Title), `"'“”«»`)
	sg.Title = strings.TrimSuffix(sg.Title, ".")
	// What the kitty read was not masked: what it wrote is, like a gist.
	sg.Description = Redact(strings.TrimSpace(sg.Description))
	sg.Title = Redact(sg.Title)
	if sg.Title == "" {
		return Suggestion{}, errors.New("the kitty did not answer with a title")
	}
	sg.Title = clipRunes(sg.Title, MaxTitle)
	sg.Description = clipRunes(sg.Description, MaxDescription)
	return sg, nil
}

// clipRunes cuts s to at most n characters.
func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// Description is an earlier description of a chat: its words, who wrote them and when.
type Description struct {
	Text string    `json:"text"`
	By   string    `json:"by,omitempty"`
	At   time.Time `json:"at"`
}

// Proposal is a description a kitty proposes for its chat.
type Proposal struct {
	Description string    `json:"description"`
	By          string    `json:"by"`
	At          time.Time `json:"at"`
}

// MaxDescriptions is how many earlier descriptions a chat keeps.
const MaxDescriptions = 10

// ProposeAgainAfter is how many turns a kitty waits, after the person dismissed a proposal,
// before it may propose another.
const ProposeAgainAfter = 10

// describeHelp is what the describe_chat tool says about itself.
const describeHelp = "Propose a new description for this chat when its course clearly changed: the goal moved on, " +
	"or it has none and its purpose is now clear. The person accepts or dismisses it; until then nothing changes. " +
	"Rarely: never for a detail, never twice in a row."

// proposePrompt tells a kitty it may propose the chat's description; nothing while a proposal waits
// or soon after the person dismissed one.
func proposePrompt(sess *Session) string {
	if sess.Proposal != nil || sess.Exposed || !mayPropose(sess) {
		return ""
	}
	return "\nIf this chat's course clearly changed from its description (or it has none and its goal is now clear after " +
		"a few messages), propose one, rarely, with the line `@describe propose <what the chat is for now, 1 to 3 sentences>` " +
		"(or the describe_chat tool) and mention it in one short sentence: the person accepts or dismisses it.\n"
}

// mayPropose is whether enough turns went by since the person dismissed a proposal.
func mayPropose(sess *Session) bool {
	return sess.DismissedAt == 0 || len(sess.Turns)-sess.DismissedAt >= ProposeAgainAfter
}

// runDescribe keeps a kitty's proposal for the chat's description, for the person to answer.
func (s *Service) runDescribe(id string, a action, k *kitty.Kitty) outcome {
	text := argText(a.args, "description", "text")
	if a.callID == "" && strings.HasPrefix(a.raw, "@describe ") {
		// A text line: what follows "@describe" (and "propose"), as written.
		text = strings.TrimSpace(strings.TrimPrefix(a.raw, "@describe "))
		if rest, ok := strings.CutPrefix(text, "propose "); ok {
			text = rest
		}
		var j struct {
			Description string `json:"description"`
		}
		if json.Unmarshal([]byte(text), &j) == nil && j.Description != "" {
			text = j.Description
		}
	}
	text = Redact(strings.TrimSpace(text))
	if text == "" {
		return outcome{action: a, err: "say the description to propose"}
	}
	text = clipRunes(text, MaxDescription)
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	switch {
	case !ok:
		return outcome{action: a, err: "no such chat"}
	case sess.Exposed:
		return outcome{action: a, err: "the description of a chat with other people is the person's to change"}
	case sess.Proposal != nil:
		return outcome{action: a, err: "a proposal already waits for the person's answer"}
	case !mayPropose(sess):
		return outcome{action: a, err: "the person dismissed a proposal a moment ago: keep working from the current description"}
	case text == sess.Description:
		return outcome{action: a, text: "that is already the description"}
	}
	sess.Proposal = &Proposal{Description: text, By: k.Name, At: s.now()}
	if err := s.save(sess); err != nil {
		return outcome{action: a, err: err.Error()}
	}
	s.emitLocked(Event{Type: "edited", Session: id})
	return outcome{action: a, text: "proposed: the person sees it above the chat and accepts or dismisses it; keep working from the current description meanwhile"}
}

// AnswerProposal takes the description a kitty proposed (accept) or drops it. The answer to a
// chat with no proposal is an error.
func (s *Service) AnswerProposal(id string, accept bool) (*Session, error) {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("no such chat %q", id)
	}
	p := sess.Proposal
	if p == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("%w: no description waits for an answer", ErrEdit)
	}
	sess.Proposal = nil
	if accept {
		s.describeLocked(sess, p.Description, p.By)
	} else {
		sess.DismissedAt = len(sess.Turns)
	}
	if err := s.save(sess); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.emitLocked(Event{Type: "edited", Session: id})
	s.mu.Unlock()
	return s.Get(id)
}

// describeLocked sets a new description, keeping the one it replaces among the earlier ones.
func (s *Service) describeLocked(sess *Session, text, by string) {
	if sess.Description != "" {
		old := Description{Text: sess.Description, By: sess.DescriptionBy}
		if sess.DescriptionAt != nil {
			old.At = *sess.DescriptionAt
		}
		sess.Descriptions = append([]Description{old}, sess.Descriptions...)
		if len(sess.Descriptions) > MaxDescriptions {
			sess.Descriptions = sess.Descriptions[:MaxDescriptions]
		}
	}
	sess.Description = text
	if text == "" {
		sess.DescriptionBy, sess.DescriptionAt = "", nil
		return
	}
	now := s.now()
	sess.DescriptionBy, sess.DescriptionAt = by, &now
}
