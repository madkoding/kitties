// Package chat runs the conversations: one person with one kitty, or a group where several
// kitties work together on the same thread.
//
// In a group, every message from the person goes to the kitties it mentions (@name), or to every
// member when it mentions none. Each kitty answers in turn and sees what the others said before
// it, labelled with their name, so the second one builds on the first instead of repeating it.
package chat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/madkoding/kitties/internal/connector"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/provider"
	"github.com/madkoding/kitties/internal/soul"
)

// Turn is one message of a thread.
type Turn struct {
	At   time.Time `json:"at"`
	From string    `json:"from"` // "user", a kitty's name, or "system" for connector results
	// Kitty is the kitty id when From is a kitty.
	Kitty string `json:"kitty,omitempty"`
	Text  string `json:"text"`
	// Calls are the connectors the kitty used in this turn, if any.
	Calls []connector.Result `json:"calls,omitempty"`
	Usage provider.Usage     `json:"usage,omitempty"`
}

// Session is one thread.
type Session struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Members   []string  `json:"members"` // kitty ids
	Turns     []Turn    `json:"turns"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Group reports whether the session has more than one kitty.
func (s *Session) Group() bool { return len(s.Members) > 1 }

// Fleet is what the chat needs from the orchestrator.
type Fleet interface {
	Get(ref string) (*kitty.Kitty, error)
	Brain(k *kitty.Kitty) (provider.Client, error)
	Connectors() *connector.Registry
}

// Service runs and stores the sessions.
type Service struct {
	fleet Fleet
	dir   string
	user  string

	mu       sync.RWMutex
	sessions map[string]*Session
	// running serialises the turns of one session.
	running map[string]*sync.Mutex
	now     func() time.Time
	newID   func() string
	// MaxRounds bounds how many times a kitty may call connectors in one answer.
	MaxRounds int
	// MaxTurns is how much of the thread a kitty is shown.
	MaxTurns int
}

// New creates the service, loading the sessions kept in dir.
func New(fleet Fleet, dir string) (*Service, error) {
	s := &Service{fleet: fleet, dir: dir, sessions: map[string]*Session{}, running: map[string]*sync.Mutex{},
		now: time.Now, newID: randomID, MaxRounds: 4, MaxTurns: 60}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// SetUser sets how kitties refer to the person.
func (s *Service) SetUser(name string) { s.user = name }

func randomID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (s *Service) load() error {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read the chats: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			return err
		}
		var sess Session
		if err := json.Unmarshal(data, &sess); err != nil {
			return fmt.Errorf("the chat %s is damaged: %w", e.Name(), err)
		}
		s.sessions[sess.ID] = &sess
	}
	return nil
}

func (s *Service) save(sess *Session) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create the chats folder: %w", err)
	}
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, sess.ID+".json")
	if err := os.WriteFile(path+".tmp", data, 0o600); err != nil {
		return fmt.Errorf("save the chat: %w", err)
	}
	return os.Rename(path+".tmp", path)
}

// Create opens a session with the given kitties (ids or names).
func (s *Service) Create(title string, members []string) (*Session, error) {
	if len(members) == 0 {
		return nil, errors.New("a chat needs at least one kitty")
	}
	ids := make([]string, 0, len(members))
	names := make([]string, 0, len(members))
	seen := map[string]bool{}
	for _, ref := range members {
		k, err := s.fleet.Get(ref)
		if err != nil {
			return nil, err
		}
		if seen[k.ID] {
			continue
		}
		seen[k.ID] = true
		ids = append(ids, k.ID)
		names = append(names, k.Name)
	}
	if title == "" {
		title = strings.Join(names, ", ")
	}
	now := s.now()
	sess := &Session{ID: s.newID(), Title: title, Members: ids, Turns: []Turn{}, CreatedAt: now, UpdatedAt: now}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess.ID] = sess
	if err := s.save(sess); err != nil {
		delete(s.sessions, sess.ID)
		return nil, err
	}
	return sess, nil
}

// AddMembers adds kitties to a session.
func (s *Service) AddMembers(id string, members []string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("no such chat %q", id)
	}
	for _, ref := range members {
		k, err := s.fleet.Get(ref)
		if err != nil {
			return nil, err
		}
		if !contains(sess.Members, k.ID) {
			sess.Members = append(sess.Members, k.ID)
		}
	}
	sess.UpdatedAt = s.now()
	return sess, s.save(sess)
}

// RemoveMember takes a kitty out of a session.
func (s *Service) RemoveMember(id, ref string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("no such chat %q", id)
	}
	k, err := s.fleet.Get(ref)
	if err != nil {
		return nil, err
	}
	kept := sess.Members[:0]
	for _, m := range sess.Members {
		if m != k.ID {
			kept = append(kept, m)
		}
	}
	sess.Members = kept
	sess.UpdatedAt = s.now()
	return sess, s.save(sess)
}

// Get returns a copy of the session.
func (s *Service) Get(id string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("no such chat %q", id)
	}
	c := *sess
	c.Members = append([]string(nil), sess.Members...)
	c.Turns = append([]Turn(nil), sess.Turns...)
	return &c, nil
}

// List returns every session, newest first, without the turns.
func (s *Service) List() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		c := *sess
		c.Turns = nil
		out = append(out, &c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

// Delete removes a session.
func (s *Service) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[id]; !ok {
		return fmt.Errorf("no such chat %q", id)
	}
	delete(s.sessions, id)
	err := os.Remove(filepath.Join(s.dir, id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

var mention = regexp.MustCompile(`@([a-z0-9][a-z0-9-]*)`)

// Send posts the person's message and returns the kitties' replies.
func (s *Service) Send(ctx context.Context, id, text string) ([]Turn, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("the message is empty")
	}
	lock := s.lockFor(id)
	lock.Lock()
	defer lock.Unlock()

	s.mu.Lock()
	sess, ok := s.sessions[id]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("no such chat %q", id)
	}
	sess.Turns = append(sess.Turns, Turn{At: s.now(), From: "user", Text: text})
	sess.UpdatedAt = s.now()
	members := append([]string(nil), sess.Members...)
	err := s.save(sess)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}

	kitties := make([]*kitty.Kitty, 0, len(members))
	for _, m := range members {
		k, err := s.fleet.Get(m)
		if err != nil {
			continue
		}
		kitties = append(kitties, k)
	}
	if len(kitties) == 0 {
		return nil, errors.New("the chat has no kitties left")
	}
	responders := s.responders(kitties, text)
	var replies []Turn
	for _, k := range responders {
		turn, err := s.answer(ctx, id, k, kitties)
		if err != nil {
			turn = Turn{At: s.now(), From: k.Name, Kitty: k.ID, Text: "(" + k.Name + " could not answer: " + err.Error() + ")"}
		}
		s.mu.Lock()
		sess.Turns = append(sess.Turns, turn)
		sess.UpdatedAt = s.now()
		saveErr := s.save(sess)
		s.mu.Unlock()
		if saveErr != nil {
			return replies, saveErr
		}
		replies = append(replies, turn)
	}
	return replies, nil
}

func (s *Service) lockFor(id string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.running[id]
	if !ok {
		l = &sync.Mutex{}
		s.running[id] = l
	}
	return l
}

// responders picks who answers: the kitties the message mentions, or every member.
func (s *Service) responders(kitties []*kitty.Kitty, text string) []*kitty.Kitty {
	if len(kitties) == 1 {
		return kitties
	}
	var picked []*kitty.Kitty
	for _, m := range mention.FindAllStringSubmatch(strings.ToLower(text), -1) {
		for _, k := range kitties {
			if k.Name == m[1] && !containsKitty(picked, k) {
				picked = append(picked, k)
			}
		}
	}
	if len(picked) == 0 {
		return kitties
	}
	return picked
}

func containsKitty(list []*kitty.Kitty, k *kitty.Kitty) bool {
	for _, x := range list {
		if x.ID == k.ID {
			return true
		}
	}
	return false
}

// answer asks one kitty for its turn, running the connectors it calls.
func (s *Service) answer(ctx context.Context, id string, k *kitty.Kitty, members []*kitty.Kitty) (Turn, error) {
	brain, err := s.fleet.Brain(k)
	if err != nil {
		return Turn{}, err
	}
	allowed := s.fleet.Connectors().Descriptions(k.Connectors)
	var group []string
	for _, m := range members {
		if m.ID != k.ID {
			group = append(group, m.Name)
		}
	}
	system := k.Soul.SystemPrompt(soul.Context{
		KittyName: k.Name, Desktop: k.Desktop, Workspace: workspaceOf(k), Group: group, Connectors: allowed, User: s.user,
	})
	sess, err := s.Get(id)
	if err != nil {
		return Turn{}, err
	}
	messages := s.transcript(system, sess.Turns, k)

	turn := Turn{From: k.Name, Kitty: k.ID}
	var parts []string
	for round := 0; round < s.MaxRounds; round++ {
		reply, err := brain.Complete(ctx, messages)
		if err != nil {
			return Turn{}, err
		}
		turn.Usage.Input += reply.Usage.Input
		turn.Usage.Output += reply.Usage.Output
		calls, text := connector.Parse(reply.Content)
		if text != "" {
			parts = append(parts, text)
		}
		if len(calls) == 0 || len(allowed) == 0 {
			break
		}
		var results []connector.Result
		for _, c := range calls {
			results = append(results, s.fleet.Connectors().Call(ctx, c, k.Connectors))
		}
		turn.Calls = append(turn.Calls, results...)
		messages = append(messages,
			provider.Message{Role: "assistant", Content: reply.Content},
			provider.Message{Role: "user", Content: connector.Format(results) + "\n\nContinue, and answer the person."})
	}
	turn.At = s.now()
	turn.Text = strings.TrimSpace(strings.Join(parts, "\n\n"))
	if turn.Text == "" {
		turn.Text = "(" + k.Name + " had nothing to add)"
	}
	return turn, nil
}

func workspaceOf(k *kitty.Kitty) string {
	if k.Desktop {
		return "/home/kitty/work"
	}
	return ""
}

// transcript renders the thread as the kitty sees it: its own turns as the assistant, everyone
// else's as the user, labelled by name in a group.
func (s *Service) transcript(system string, turns []Turn, k *kitty.Kitty) []provider.Message {
	if len(turns) > s.MaxTurns {
		turns = turns[len(turns)-s.MaxTurns:]
	}
	messages := []provider.Message{{Role: "system", Content: system}}
	var pending []string
	flush := func() {
		if len(pending) > 0 {
			messages = append(messages, provider.Message{Role: "user", Content: strings.Join(pending, "\n\n")})
			pending = nil
		}
	}
	for _, t := range turns {
		if t.Kitty == k.ID {
			flush()
			messages = append(messages, provider.Message{Role: "assistant", Content: t.Text})
			continue
		}
		label := t.From
		if t.From == "user" {
			label = s.user
			if label == "" {
				label = "User"
			}
		}
		pending = append(pending, "["+label+"]: "+t.Text)
	}
	flush()
	if last := messages[len(messages)-1]; last.Role != "user" {
		messages = append(messages, provider.Message{Role: "user", Content: "(continue)"})
	}
	return messages
}
