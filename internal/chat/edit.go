package chat

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
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
