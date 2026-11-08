package chat

import "time"

// THE KITTIES OF A CHAT.
//
// A chat's members are the kitties the person talks to there: they answer what is written. When
// one of them hands a subtask to another kitty (delegate), that kitty takes part in the chat too,
// without answering what the person writes: it is one of the chat's delegates. A thread a kitty
// opened to delegate has its owner as well. Kitties lists all of them, members first, and a chat
// with more than one reads as a group of kitties (the sidebar's huddled cats and count, the
// header's avatars), even when the person only wrote to one.

// refreshKitties keeps a session's delegates and kitties up to date from its members, owner and
// the hand-overs in its turns; save and load call it, so every change refreshes them. A delegate
// stays one when the turns that named it are trimmed away.
func refreshKitties(sess *Session) {
	delegates := make([]string, 0, len(sess.Delegates))
	add := func(id string) {
		if id != "" && !contains(sess.Members, id) && !contains(delegates, id) {
			delegates = append(delegates, id)
		}
	}
	for _, id := range sess.Delegates {
		add(id)
	}
	for i := range sess.Turns {
		for _, d := range sess.Turns[i].Delegations {
			add(d.KittyID)
		}
	}
	if len(delegates) == 0 {
		delegates = nil
	}
	sess.Delegates = delegates
	kitties := append([]string(nil), sess.Members...)
	if sess.Owner != "" && !contains(kitties, sess.Owner) {
		kitties = append(kitties, sess.Owner)
	}
	for _, id := range delegates {
		if !contains(kitties, id) {
			kitties = append(kitties, id)
		}
	}
	sess.Kitties = kitties
}

// Crew reports whether more than one kitty takes part in the session: several members, or one
// that delegated (or was delegated to). Group says only whether several answer the person.
func (s *Session) Crew() bool { return len(s.Kitties) > 1 }

// addDelegate records that a kitty took part in a chat through a hand-over, at once, so the chat
// reads as a group while the delegate is still at work.
func (s *Service) addDelegate(id, kittyID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok || contains(sess.Members, kittyID) || contains(sess.Delegates, kittyID) {
		return
	}
	sess.Delegates = append(sess.Delegates, kittyID)
	_ = s.save(sess)
}

// Helper is a kitty at work on a subtask handed over from a chat.
type Helper struct {
	Kitty   string    `json:"kitty"`
	KittyID string    `json:"kitty_id"`
	From    string    `json:"from"` // the kitty that asked
	Task    string    `json:"task"`
	Chat    string    `json:"chat"` // the thread between the two
	Since   time.Time `json:"since"`
}

// helping marks a kitty at work for a chat, and returns what ends it: the watchers of the chat
// are told both times ("helping"), so the page shows the kitty working while it does.
func (s *Service) helping(id string, h Helper) func() {
	s.setHelping(id, func(list []Helper) []Helper { return append(list, h) })
	return func() {
		s.setHelping(id, func(list []Helper) []Helper {
			kept := list[:0]
			for _, x := range list {
				if x.KittyID != h.KittyID || x.Chat != h.Chat {
					kept = append(kept, x)
				}
			}
			if len(kept) == 0 {
				return nil
			}
			return kept
		})
	}
}

func (s *Service) setHelping(id string, fn func([]Helper) []Helper) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return
	}
	sess.Helping = fn(append([]Helper(nil), sess.Helping...))
	_ = s.save(sess)
	s.emitLocked(Event{Type: "helping", Session: id, Index: -1})
}

// answerMax is the most of a delegate's answer kept whole in the chat that asked.
const answerMax = 20000

// wholeAnswer is the answer to keep beside the cut Result: empty when Result holds it all.
func wholeAnswer(answer string, cut int) string {
	if len(answer) <= cut {
		return ""
	}
	r := []rune(answer)
	if len(r) > answerMax {
		return string(r[:answerMax]) + "…"
	}
	return answer
}
