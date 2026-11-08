package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
		sess.Description, changed = d, true
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
		label := t.From
		if label == "user" {
			label = s.userName()
		}
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
