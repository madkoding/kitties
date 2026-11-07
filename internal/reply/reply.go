// Package reply lets the person answer the kitties from the notification channel. With
// notify.replies on, the gateway listens to the ntfy topic it notifies through: what the person
// writes there goes to the kitty that notified them last, to the kitty they name ("luna: …",
// "@luna …"), to all of them ("all: …", "todos: …"), or to the lead kitty when there is one, and
// the kitty's answer comes back as a notification.
//
// The topic is the password: on a public server whoever knows it can write to it, so replies need
// a topic hard to guess or an access token (notify.CheckReplies). The gateway's own messages
// carry connector.NtfyMark and are never taken as the person's.
package reply

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/notify"
)

// Chats is what replies need of the chats.
type Chats interface {
	List() []*chat.Session
	Get(id string) (*chat.Session, error)
	Create(title string, members []string) (*chat.Session, error)
	Send(ctx context.Context, id, text string) ([]chat.Turn, error)
	AnswerHelp(id string, index int, a chat.HelpAnswer) error
	Subscribe(id string) (<-chan chat.Event, func())
}

// Fleet lists the kitties.
type Fleet interface {
	List() []*kitty.Kitty
}

// Deliverer sends a notification now.
type Deliverer interface {
	Deliver(ctx context.Context, n notify.Notification) error
}

// Limits on what the channel takes.
const (
	// MaxText is the longest message a kitty is given.
	MaxText = 2000
	// recent is how long a notification still says who a reply without a name is for.
	recent = 24 * time.Hour
	// perHour is how many messages an hour are taken; the rest are dropped.
	perHour = 30
	// answerFor is how long a kitty may take to answer a message.
	answerFor = 10 * time.Minute
)

// follow is how long a kitty's desktop task is followed to send its result.
var follow = 6 * time.Hour

// allWords name every kitty at once.
var allWords = map[string]bool{"all": true, "everyone": true, "todos": true, "todas": true, "everybody": true, "kitties": true, "todos los kitties": true}

// seen is the last notification about a kitty.
type seen struct {
	chat string
	at   time.Time
}

// Service routes what the person writes to the kitties.
type Service struct {
	chats Chats
	fleet Fleet
	out   Deliverer
	// Lead is the kitty that answers what names nobody, when there is one (may be nil).
	Lead func() *kitty.Kitty
	logf func(string, ...any)
	now  func() time.Time

	mu    sync.Mutex
	last  map[string]seen // by kitty name
	whom  string          // the kitty of the last notification
	whoAt time.Time
	taken []time.Time
	wg    sync.WaitGroup
}

// New creates the service.
func New(chats Chats, fleet Fleet, out Deliverer, logf func(string, ...any)) *Service {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Service{chats: chats, fleet: fleet, out: out, logf: logf, now: time.Now, last: map[string]seen{}}
}

// Saw records a notification that went out: a reply that names nobody answers its kitty.
func (s *Service) Saw(n notify.Notification) {
	if n.Kitty == "" || n.Event == notify.Digest {
		return
	}
	// A notification the lead signs for another kitty is about that kitty's chat: its members
	// are the ones a "name: …" reply goes on with there.
	var members []string
	if n.Chat != "" {
		if sess, err := s.chats.Get(n.Chat); err == nil {
			for _, k := range s.fleet.List() {
				if member(sess, k.ID) {
					members = append(members, strings.ToLower(k.Name))
				}
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	name := strings.ToLower(n.Kitty)
	if n.Chat != "" {
		s.last[name] = seen{chat: n.Chat, at: s.now()}
		for _, m := range members {
			s.last[m] = seen{chat: n.Chat, at: s.now()}
		}
	}
	s.whom, s.whoAt = name, s.now()
}

// Wait blocks until the replies in flight are answered.
func (s *Service) Wait() { s.wg.Wait() }

// allow takes one message from the hour's allowance.
func (s *Service) allow() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	kept := s.taken[:0]
	for _, t := range s.taken {
		if now.Sub(t) < time.Hour {
			kept = append(kept, t)
		}
	}
	s.taken = kept
	if len(s.taken) >= perHour {
		return false
	}
	s.taken = append(s.taken, now)
	return true
}

// Handle takes one message the person wrote (title may be empty) and has the kitties answer it.
// It returns once every kitty answered; their answers go out as notifications.
func (s *Service) Handle(ctx context.Context, title, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if !s.allow() {
		return errors.New("too many messages this hour: this one was dropped")
	}
	if len([]rune(text)) > MaxText {
		s.say(ctx, "", fmt.Sprintf("That message is too long for the kitties (%d characters at most).", MaxText), "")
		return fmt.Errorf("the message is too long")
	}
	targets, body := s.targets(title, text)
	if len(targets) == 0 {
		targets = s.fallback()
	}
	if len(targets) == 0 {
		s.say(ctx, "", "Who is it for? Start with a kitty's name, like "+s.example()+", or all: to tell every kitty.", "")
		return errors.New("the message names no kitty")
	}
	if body == "" {
		return nil
	}
	var wg sync.WaitGroup
	for _, k := range targets {
		wg.Add(1)
		go func(k *kitty.Kitty) {
			defer wg.Done()
			s.deliver(ctx, k, body)
		}(k)
	}
	wg.Wait()
	return nil
}

// example is how to name a kitty, with one of theirs.
func (s *Service) example() string {
	for _, k := range s.sorted() {
		return k.Name + ": …"
	}
	return "luna: …"
}

func (s *Service) sorted() []*kitty.Kitty {
	ks := s.fleet.List()
	sort.Slice(ks, func(i, j int) bool { return ks[i].Name < ks[j].Name })
	return ks
}

// The ways a message names its kitties: "luna: …", "luna, bo: …", "luna y bo: …", "@luna …".
var (
	named    = regexp.MustCompile(`(?s)^\s*([^:\n]{1,80}?)\s*:\s*(.+)$`)
	mentions = regexp.MustCompile(`(?s)^\s*((?:@[a-z0-9][a-z0-9-]*[\s,]*)+)(.*)$`)
	splitter = regexp.MustCompile(`\s*(?:,|&|\s+y\s+|\s+and\s+|\s+e\s+)\s*`)
)

// targets are the kitties a message names, and what is left of it for them. A title that names
// kitties wins; then a "name: …" start; then "@name" mentions. A start that is not a kitty's
// name ("Nota: …") is part of the message.
func (s *Service) targets(title, text string) ([]*kitty.Kitty, string) {
	if ks := s.resolve(title); len(ks) > 0 {
		return ks, text
	}
	if m := named.FindStringSubmatch(text); m != nil {
		if ks := s.resolve(m[1]); len(ks) > 0 {
			return ks, strings.TrimSpace(m[2])
		}
	}
	if m := mentions.FindStringSubmatch(strings.ToLower(text)); m != nil {
		var names []string
		for _, f := range strings.FieldsFunc(m[1], func(r rune) bool { return r == '@' || r == ',' || r == ' ' || r == '\n' || r == '\t' }) {
			names = append(names, f)
		}
		if ks := s.resolve(strings.Join(names, ",")); len(ks) > 0 {
			return ks, strings.TrimSpace(text[len(text)-len(m[2]):])
		}
	}
	return nil, text
}

// resolve turns a list of names into kitties; nil unless every name is a kitty (or "all").
func (s *Service) resolve(list string) []*kitty.Kitty {
	list = strings.ToLower(strings.TrimSpace(list))
	if list == "" {
		return nil
	}
	ks := s.sorted()
	if allWords[list] {
		return ks
	}
	var out []*kitty.Kitty
	for _, name := range splitter.Split(list, -1) {
		name = strings.TrimPrefix(strings.TrimSpace(name), "@")
		if name == "" {
			continue
		}
		var found *kitty.Kitty
		for _, k := range ks {
			if strings.ToLower(k.Name) == name {
				found = k
			}
		}
		if found == nil {
			return nil
		}
		dup := false
		for _, o := range out {
			dup = dup || o.ID == found.ID
		}
		if !dup {
			out = append(out, found)
		}
	}
	return out
}

// fallback is who answers a message that names nobody: the lead kitty, else the kitty that
// notified last (in the last day), else the only kitty.
func (s *Service) fallback() []*kitty.Kitty {
	if s.Lead != nil {
		if k := s.Lead(); k != nil {
			return []*kitty.Kitty{k}
		}
	}
	ks := s.sorted()
	s.mu.Lock()
	whom, at := s.whom, s.whoAt
	s.mu.Unlock()
	if whom != "" && s.now().Sub(at) < recent {
		for _, k := range ks {
			if strings.ToLower(k.Name) == whom {
				return []*kitty.Kitty{k}
			}
		}
	}
	if len(ks) == 1 {
		return ks
	}
	return nil
}

// chatFor is where a kitty is told: the chat it last notified about (in the last day), else its
// latest chat with the person, else a new one. group says the chat has other kitties too.
func (s *Service) chatFor(k *kitty.Kitty) (id string, group bool, err error) {
	s.mu.Lock()
	sn, ok := s.last[strings.ToLower(k.Name)]
	s.mu.Unlock()
	if ok && s.now().Sub(sn.at) < recent {
		if sess, err := s.chats.Get(sn.chat); err == nil && member(sess, k.ID) {
			return sess.ID, sess.Group(), nil
		}
	}
	for _, sess := range s.chats.List() { // newest first
		if len(sess.Members) == 1 && sess.Members[0] == k.ID && sess.Goal == "" && sess.Owner == "" {
			return sess.ID, false, nil
		}
	}
	sess, err := s.chats.Create("", []string{k.ID})
	if err != nil {
		return "", false, err
	}
	return sess.ID, false, nil
}

func member(sess *chat.Session, id string) bool {
	for _, m := range sess.Members {
		if m == id {
			return true
		}
	}
	return false
}

// deliver gives one kitty the message and sends its answer back.
func (s *Service) deliver(ctx context.Context, k *kitty.Kitty, text string) {
	id, group, err := s.chatFor(k)
	if err != nil {
		s.say(ctx, k.Name, "could not open a chat: "+err.Error(), "")
		return
	}
	// A kitty waiting for the person's help gets the message as its answer.
	if sess, err := s.chats.Get(id); err == nil && len(sess.Turns) > 0 {
		i := len(sess.Turns) - 1
		if t := sess.Turns[i]; t.Working && t.Kitty == k.ID {
			if err := s.chats.AnswerHelp(id, i, chat.HelpAnswer{Text: text}); err == nil {
				s.say(ctx, k.Name, "got your answer and carries on.", id)
				return
			}
		}
	}
	if group {
		text = "@" + k.Name + " " + text
	}
	ctx, cancel := context.WithTimeout(ctx, answerFor)
	defer cancel()
	turns, err := s.chats.Send(ctx, id, text)
	if errors.Is(err, chat.ErrBusy) {
		s.say(context.Background(), k.Name, "is busy with a task on its desktop: write again when it tells you it is done.", id)
		return
	}
	if err != nil {
		s.say(context.Background(), k.Name, "did not get your message: "+chat.Plain(err.Error()), id)
		return
	}
	// The turns that came back are the chat's last ones.
	first := sessLen(s.chats, id) - len(turns)
	for i, t := range turns {
		if t.Kitty != k.ID {
			continue
		}
		body := t.Text
		if t.Working {
			if body != "" {
				body += "\n\n"
			}
			body += "(Working on it on its desktop: you will get the result here.)"
			s.wg.Add(1)
			go func(index int, said string) {
				defer s.wg.Done()
				s.followTask(k, id, index, said)
			}(first+i, t.Text)
		}
		s.say(context.Background(), k.Name, body, id)
	}
}

// sessLen is how many turns a chat has now.
func sessLen(c Chats, id string) int {
	if sess, err := c.Get(id); err == nil {
		return len(sess.Turns)
	}
	return 0
}

// followTask waits for a kitty's desktop task to end and sends what it said after said.
func (s *Service) followTask(k *kitty.Kitty, id string, index int, said string) {
	events, stop := s.chats.Subscribe(id)
	defer stop()
	done := func() (string, bool) {
		sess, err := s.chats.Get(id)
		if err != nil || index < 0 || index >= len(sess.Turns) {
			return "", true
		}
		t := sess.Turns[index]
		if t.Working {
			return "", false
		}
		return strings.TrimSpace(strings.TrimPrefix(t.Text, said)), true
	}
	timer := time.NewTimer(follow)
	defer timer.Stop()
	for {
		if text, ok := done(); ok {
			if text != "" {
				s.say(context.Background(), k.Name, text, id)
			}
			return
		}
		select {
		case <-events:
		case <-timer.C:
			return
		}
	}
}

// say sends a notification back to the person.
func (s *Service) say(ctx context.Context, name, text, chatID string) {
	if s.out == nil {
		return
	}
	title := "answered"
	if name == "" {
		name, title = "kitties", "about your message"
	}
	if len(text) > 3500 {
		text = text[:3500] + "…"
	}
	if err := s.out.Deliver(ctx, notify.Notification{Event: notify.Reply, Kitty: name, Title: title, Body: text, Chat: chatID}); err != nil {
		s.logf("reply: %v", err)
	}
}
