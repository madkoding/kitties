package chat

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/notify"
)

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
		if id != "" && !contains(sess.Members, id) && !contains(delegates, id) && !contains(sess.Dismissed, id) {
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
	// Handed work again, a delegate the person took off is back.
	sess.Dismissed = without(sess.Dismissed, kittyID)
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
	// Queued says it waits its turn: the reply handed out more pieces than run at once, or
	// another piece for the same kitty goes first. It has no thread yet.
	Queued bool `json:"queued,omitempty"`
}

// helping marks a kitty at work for a chat, and returns what ends it: the watchers of the chat
// are told both times ("helping"), so the page shows the kitty working while it does.
func (s *Service) helping(id string, h Helper) func() {
	s.setHelping(id, func(list []Helper) []Helper { return append(list, h) })
	return func() {
		s.setHelping(id, func(list []Helper) []Helper {
			kept := list[:0]
			for _, x := range list {
				if x.Queued || x.KittyID != h.KittyID || x.Chat != h.Chat {
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

// tellDelegation says how a hand-over went: in the lead's Reports thread, unless the lead itself
// asked (it has the answer already), and to the person when they chose to hear of hand-overs.
// The notices of one chat that end within HandOverWait go out as one message.
func (s *Service) tellDelegation(from, to *kitty.Kitty, title, body, chatID string) {
	lead := s.leadFor(to)
	if lead != nil && lead.ID != from.ID {
		s.report(lead, to, notify.Delegated, title, body, chatID)
	}
	if s.notifier == nil {
		return
	}
	name, line := signed(lead, to, title)
	s.queueHandOver(notify.Notification{Event: notify.Delegated, Kitty: name, Title: line, Body: body, Chat: chatID})
}

// queueHandOver keeps a hand-over's notice for HandOverWait, the first of a chat starting the
// wait; with no wait it goes out at once.
func (s *Service) queueHandOver(n notify.Notification) { s.queueNotice(n.Chat, n) }

// queueNotice keeps a notice under a key for HandOverWait, the first of the key starting the
// wait; with no wait it goes out at once. The notices of a key go out together.
func (s *Service) queueNotice(key string, n notify.Notification) {
	if s.HandOverWait <= 0 {
		s.notifier.Notify(n)
		return
	}
	s.handOverMu.Lock()
	defer s.handOverMu.Unlock()
	if s.handOvers == nil {
		s.handOvers = map[string][]notify.Notification{}
	}
	if len(s.handOvers[key]) == 0 {
		time.AfterFunc(s.HandOverWait, func() { s.flushHandOvers(key) })
	}
	s.handOvers[key] = append(s.handOvers[key], n)
}

// flushHandOvers sends a chat's waiting hand-over notices: one as it is, several as one message
// that lists them.
func (s *Service) flushHandOvers(chat string) {
	s.handOverMu.Lock()
	list := s.handOvers[chat]
	delete(s.handOvers, chat)
	s.handOverMu.Unlock()
	if len(list) == 0 {
		return
	}
	s.notifier.Notify(combineHandOvers(list))
}

// combineHandOvers is one notice for several hand-overs of a chat (or several goals the team
// finished, for the lead), in the name of the first.
func combineHandOvers(list []notify.Notification) notify.Notification {
	if len(list) == 1 {
		return list[0]
	}
	lines := make([]string, len(list))
	for i, n := range list {
		lines[i] = "• " + n.Title
		if n.Body != "" {
			lines[i] += ": " + n.Body
		}
	}
	first := list[0]
	title := fmt.Sprintf("%d hand-overs ended", len(list))
	if first.Event == notify.GoalDone {
		title = fmt.Sprintf("%d goals finished", len(list))
	}
	return notify.Notification{Event: first.Event, Kitty: first.Kitty, Chat: first.Chat,
		Title: title, Body: strings.Join(lines, "\n")}
}

// dismissDelegate takes a delegate off a chat for good: its old hand-overs no longer bring it back.
func dismissDelegate(sess *Session, kittyID string) {
	sess.Delegates = without(sess.Delegates, kittyID)
	if !contains(sess.Dismissed, kittyID) {
		sess.Dismissed = append(sess.Dismissed, kittyID)
	}
}

// without is list less id, nil when nothing is left.
func without(list []string, id string) []string {
	var out []string
	for _, x := range list {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

// FlushHandOvers sends every hand-over notice still waiting, at once: the gateway calls it when
// it stops, so the last minute's are not lost.
func (s *Service) FlushHandOvers() {
	s.handOverMu.Lock()
	chats := make([]string, 0, len(s.handOvers))
	for chat := range s.handOvers {
		chats = append(chats, chat)
	}
	s.handOverMu.Unlock()
	for _, chat := range chats {
		s.flushHandOvers(chat)
	}
}

// pruneHelpers takes the kitties that no longer exist off a chat's delegates (for good: its old
// hand-overs no longer bring them back) and its owner; it reports whether anything changed.
func pruneHelpers(sess *Session, exists func(id string) bool) bool {
	changed := false
	for _, id := range append([]string(nil), sess.Delegates...) {
		if !exists(id) {
			dismissDelegate(sess, id)
			changed = true
		}
	}
	if sess.Owner != "" && !exists(sess.Owner) {
		sess.Owner = ""
		changed = true
	}
	return changed
}

// RestoreDelegate brings back a delegate the person took off a chat: it counts among the chat's
// kitties again, as its hand-overs say.
func (s *Service) RestoreDelegate(id, ref string) (*Session, error) {
	k, err := s.fleet.Get(ref)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("no such chat %q", id)
	}
	if !contains(sess.Dismissed, k.ID) {
		return nil, fmt.Errorf("%s was not taken off this chat", k.Name)
	}
	sess.Dismissed = without(sess.Dismissed, k.ID)
	if !contains(sess.Members, k.ID) && !contains(sess.Delegates, k.ID) {
		sess.Delegates = append(sess.Delegates, k.ID)
	}
	sess.UpdatedAt = s.now()
	if err := s.save(sess); err != nil {
		return nil, err
	}
	c := *sess
	return &c, nil
}

// HandOverStat is how one kitty has handed work out and taken it on, over every chat kept.
type HandOverStat struct {
	KittyID string `json:"kitty_id"`
	Kitty   string `json:"kitty"`
	// Asked is how many subtasks it handed to others, AskedFailed how many of those were not done.
	Asked       int `json:"asked"`
	AskedFailed int `json:"asked_failed"`
	// Took is how many subtasks others handed it, TookFailed how many of those it could not do.
	Took       int `json:"took"`
	TookFailed int `json:"took_failed"`
	// Threads are the hand-over threads it took part in, the latest first (at most statThreads).
	Threads []StatThread `json:"threads,omitempty"`
}

// StatThread is a hand-over thread, to open from the stats.
type StatThread struct {
	ID    string    `json:"id"`
	Title string    `json:"title"`
	At    time.Time `json:"at"`
}

// statThreads is how many hand-over threads the stats list per kitty.
const statThreads = 5

// FailStreakAlert is how many subtasks in a row a kitty may fail before the lead (and the
// person, when they hear of hand-overs) is told something is wrong with it.
const FailStreakAlert = 3

// HandOverStats counts the hand-overs of every chat kept, per kitty of the fleet, busiest first;
// kitties that never handed nor took work are left out.
func (s *Service) HandOverStats() []HandOverStat {
	byID := map[string]*HandOverStat{}
	get := func(id string) *HandOverStat {
		if st, ok := byID[id]; ok {
			return st
		}
		st := &HandOverStat{KittyID: id}
		byID[id] = st
		return st
	}
	threads := map[string]map[string]time.Time{} // kitty -> thread -> last hand-over
	seen := func(kitty, chat string, at time.Time) {
		if kitty == "" || chat == "" {
			return
		}
		if threads[kitty] == nil {
			threads[kitty] = map[string]time.Time{}
		}
		if at.After(threads[kitty][chat]) {
			threads[kitty][chat] = at
		}
	}
	s.mu.RLock()
	titles := map[string]string{}
	for _, sess := range s.sessions {
		titles[sess.ID] = sess.Title
		for i := range sess.Turns {
			t := &sess.Turns[i]
			for _, d := range t.Delegations {
				failed := d.Error != ""
				if t.Kitty != "" {
					st := get(t.Kitty)
					st.Asked++
					if failed {
						st.AskedFailed++
					}
					seen(t.Kitty, d.Chat, t.At)
				}
				if d.KittyID != "" {
					st := get(d.KittyID)
					st.Took++
					if failed {
						st.TookFailed++
					}
					seen(d.KittyID, d.Chat, t.At)
				}
			}
		}
	}
	s.mu.RUnlock()
	for id, st := range byID {
		for chat, at := range threads[id] {
			st.Threads = append(st.Threads, StatThread{ID: chat, Title: titles[chat], At: at})
		}
		sort.Slice(st.Threads, func(i, j int) bool {
			if !st.Threads[i].At.Equal(st.Threads[j].At) {
				return st.Threads[i].At.After(st.Threads[j].At)
			}
			return st.Threads[i].ID < st.Threads[j].ID
		})
		if len(st.Threads) > statThreads {
			st.Threads = st.Threads[:statThreads]
		}
	}
	out := make([]HandOverStat, 0, len(byID))
	for _, k := range s.fleet.List() {
		if st, ok := byID[k.ID]; ok {
			st.Kitty = k.Name
			out = append(out, *st)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Asked+out[i].Took > out[j].Asked+out[j].Took
	})
	return out
}

// HandOverDay is how many subtasks the team handed around on one day, and how many of them
// failed, for the lead's chart.
type HandOverDay struct {
	// Day is the date, YYYY-MM-DD, in the gateway's time zone.
	Day    string `json:"day"`
	Count  int    `json:"count"`
	Failed int    `json:"failed"`
}

// HandOverDays counts the hand-overs of every chat kept over the n days up to now, oldest first;
// a day without any is there with zeros, so the chart keeps its days apart.
func (s *Service) HandOverDays(n int, now time.Time) []HandOverDay {
	if n < 1 {
		return nil
	}
	out := make([]HandOverDay, n)
	index := map[string]int{}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for i := 0; i < n; i++ {
		day := today.AddDate(0, 0, i-n+1).Format("2006-01-02")
		out[i].Day = day
		index[day] = i
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sess := range s.sessions {
		for i := range sess.Turns {
			t := &sess.Turns[i]
			if len(t.Delegations) == 0 {
				continue
			}
			j, ok := index[t.At.In(now.Location()).Format("2006-01-02")]
			if !ok {
				continue
			}
			for _, d := range t.Delegations {
				out[j].Count++
				if d.Error != "" {
					out[j].Failed++
				}
			}
		}
	}
	return out
}

// handOverFailed counts a subtask a kitty could not do; every FailStreakAlert in a row, the lead
// (its Reports thread, even when it asked) and the person hear that something is wrong with it.
func (s *Service) handOverFailed(k *kitty.Kitty, err error, chatID string) {
	s.handOverMu.Lock()
	if s.failStreak == nil {
		s.failStreak = map[string]int{}
	}
	s.failStreak[k.ID]++
	n := s.failStreak[k.ID]
	s.handOverMu.Unlock()
	if n%FailStreakAlert != 0 {
		return
	}
	s.tell(notify.Delegated, k, fmt.Sprintf("failed %d subtasks in a row", n),
		"The last one: "+Gist(err.Error())+". Check its model, its limits or its desktop.", chatID)
}

// handOverDone ends a kitty's streak of failed subtasks.
func (s *Service) handOverDone(k *kitty.Kitty) {
	s.handOverMu.Lock()
	delete(s.failStreak, k.ID)
	s.handOverMu.Unlock()
}
