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

	"github.com/madkoding/kitties/internal/artifacts"
	"github.com/madkoding/kitties/internal/connector"
	"github.com/madkoding/kitties/internal/desktop"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/provider"
	"github.com/madkoding/kitties/internal/skills"
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
	// Steps are the actions the kitty took on its desktop in this turn, with their screenshots.
	Steps []Step `json:"steps,omitempty"`
	// Artifacts names the files the kitty saved to the library in this turn.
	Artifacts []string `json:"artifacts,omitempty"`
	// Skills names the skills it read or proposed in this turn.
	Skills []string `json:"skills,omitempty"`
	// Working is true while the kitty is still acting on its desktop for this turn.
	Working bool `json:"working,omitempty"`
}

// Step is one action on the desktop.
type Step struct {
	At     time.Time      `json:"at"`
	Action string         `json:"action"`
	Args   map[string]any `json:"args,omitempty"`
	Result string         `json:"result,omitempty"`
	Error  string         `json:"error,omitempty"`
	// Shot is the screenshot taken after this step, as a file name under the chat's shots
	// (served by the gateway); empty when the step took none.
	Shot string `json:"shot,omitempty"`
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

// Working reports whether a kitty is still acting on its desktop in this session.
func (s *Session) Working() bool {
	for i := range s.Turns {
		if s.Turns[i].Working {
			return true
		}
	}
	return false
}

// ErrBusy is returned when a message arrives while a kitty is still working on the thread.
var ErrBusy = errors.New("the kitties are still working on this chat")

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
	// MaxSteps bounds how many desktop rounds a task may take in one turn.
	MaxSteps int
	// skills and arts are the libraries, nil when the deployment has none.
	skills *skills.Library
	arts   *artifacts.Store
	// tasks are the background desktop tasks in flight, by session id.
	tasks map[string]*task
	// watchers are the subscribers of each session.
	watchers map[string]map[chan Event]struct{}
	// nativeTools lets a kitty call tools in its provider's own format when it can.
	nativeTools bool
	// wg counts the background tasks, so a test (or a shutdown) can wait for them.
	wg sync.WaitGroup
}

// task is one desktop task running in the background.
type task struct {
	cancel context.CancelFunc
}

// Event is what a watcher of a session is told.
type Event struct {
	Type    string `json:"type"` // turn | update | done | deleted
	Session string `json:"session"`
	Index   int    `json:"index"`
	Turn    *Turn  `json:"turn,omitempty"`
}

// Subscribe watches a session: every new or changed turn arrives on the channel until unsubscribe.
func (s *Service) Subscribe(id string) (<-chan Event, func()) {
	ch := make(chan Event, 64)
	s.mu.Lock()
	if s.watchers == nil {
		s.watchers = map[string]map[chan Event]struct{}{}
	}
	if s.watchers[id] == nil {
		s.watchers[id] = map[chan Event]struct{}{}
	}
	s.watchers[id][ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		if set := s.watchers[id]; set != nil {
			delete(set, ch)
		}
		s.mu.Unlock()
	}
}

// emitLocked tells the watchers of a session; the caller holds the lock. A slow watcher loses
// the event rather than holding everyone else.
func (s *Service) emitLocked(ev Event) {
	for ch := range s.watchers[ev.Session] {
		select {
		case ch <- ev:
		default:
		}
	}
}

// SetNativeTools is whether kitties call tools natively when their provider can (on by default).
func (s *Service) SetNativeTools(on bool) { s.nativeTools = on }

// New creates the service, loading the sessions kept in dir.
func New(fleet Fleet, dir string) (*Service, error) {
	s := &Service{fleet: fleet, dir: dir, sessions: map[string]*Session{}, running: map[string]*sync.Mutex{},
		now: time.Now, newID: randomID, MaxRounds: 4, MaxTurns: 60, MaxSteps: 25, tasks: map[string]*task{}, nativeTools: true}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// SetUser sets how kitties refer to the person.
func (s *Service) SetUser(name string) { s.user = name }

// SetLibrary gives the kitties the skills and the artifacts.
func (s *Service) SetLibrary(sk *skills.Library, arts *artifacts.Store) {
	s.skills, s.arts = sk, arts
}

// libraryPrompt tells a kitty what it can look up and keep.
func (s *Service) libraryPrompt() string {
	var b strings.Builder
	if s.skills != nil {
		b.WriteString("You have a library of skills (procedures). Read one with a line `@skill read <name>` " +
			"before a task it covers; when you learn a procedure worth keeping, propose it with " +
			"`@skill save {\"name\": \"…\", \"body\": \"# Title\\n\\nHow to…\"}` (a person accepts it).\n")
		if idx := s.skills.Index(); idx != "" {
			b.WriteString("Skills:\n" + idx)
		}
	}
	if s.arts != nil {
		b.WriteString("You have a library of artifacts (files) shared by every kitty. Keep a result for later " +
			"with `@artifact save {\"name\": \"report.md\", \"content\": \"…\"}`, read one with " +
			"`@artifact read <name>`, list them with `@artifact list`.\n")
		if idx := s.arts.Index(12); idx != "" {
			b.WriteString("Artifacts:\n" + idx)
		}
	}
	return b.String()
}

// Directive is one `@skill` or `@artifact` line of a reply.
type Directive struct {
	Kind string // skill | artifact
	Verb string // read | save | list
	Args map[string]any
	Raw  string
}

// parseDirectives finds the library lines of a reply and returns them with the remaining text.
func parseDirectives(text string) ([]Directive, string) {
	var out []Directive
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(strings.Trim(strings.TrimSpace(line), "`"))
		kind := ""
		switch {
		case strings.HasPrefix(trimmed, "@skill "):
			kind = "skill"
		case strings.HasPrefix(trimmed, "@artifact "):
			kind = "artifact"
		default:
			kept = append(kept, line)
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "@"+kind+" "))
		verb, argsText, _ := strings.Cut(rest, " ")
		d := Directive{Kind: kind, Verb: strings.ToLower(strings.TrimSpace(verb)), Raw: trimmed, Args: map[string]any{}}
		if argsText = strings.TrimSpace(argsText); argsText != "" {
			if err := json.Unmarshal([]byte(argsText), &d.Args); err != nil {
				d.Args["name"] = strings.Trim(argsText, "\"' ")
			}
		}
		out = append(out, d)
	}
	return out, strings.TrimSpace(strings.Join(kept, "\n"))
}

func argText(args map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := args[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// runDirective executes one library line for a kitty and says what happened.
func (s *Service) runDirective(d Directive, k *kitty.Kitty, turn *Turn) string {
	name := argText(d.Args, "name", "skill", "file")
	switch d.Kind {
	case "skill":
		if s.skills == nil {
			return "[skill] there is no skills library here"
		}
		switch d.Verb {
		case "read", "get", "show":
			sk, err := s.skills.Get(name)
			if err != nil {
				return "[skill " + name + "] " + err.Error()
			}
			turn.Skills = append(turn.Skills, sk.Name)
			return "[skill " + sk.Name + "]\n" + sk.Body
		case "save", "propose", "write":
			body := argText(d.Args, "body", "content", "text")
			sk, err := s.skills.Propose(name, body)
			if err != nil {
				return "[skill save] " + err.Error()
			}
			turn.Skills = append(turn.Skills, sk.Name)
			return "[skill " + sk.Name + "] proposed; a person will accept it from the library"
		case "list", "search":
			found, err := s.skills.Search(argText(d.Args, "query", "name"), 20)
			if err != nil {
				return "[skill list] " + err.Error()
			}
			var b strings.Builder
			for _, sk := range found {
				fmt.Fprintf(&b, "- %s: %s\n", sk.Name, sk.Summary)
			}
			return "[skills]\n" + strings.TrimSpace(b.String())
		}
	case "artifact":
		if s.arts == nil {
			return "[artifact] there is no artifacts library here"
		}
		switch d.Verb {
		case "save", "write", "put":
			content := argText(d.Args, "content", "text", "body")
			a, err := s.arts.Put(name, []byte(content), k.Name)
			if err != nil {
				return "[artifact save] " + err.Error()
			}
			turn.Artifacts = append(turn.Artifacts, a.Name)
			return "[artifact " + a.Name + "] saved (" + a.Type + ", " + fmt.Sprint(a.Size) + " bytes)"
		case "read", "get", "open":
			data, err := s.arts.Get(name)
			if err != nil {
				return "[artifact " + name + "] " + err.Error()
			}
			text := string(data)
			if len(text) > 6000 {
				text = text[:6000] + "…"
			}
			return "[artifact " + name + "]\n" + text
		case "list", "ls":
			idx := s.arts.Index(50)
			if idx == "" {
				idx = "(none yet)"
			}
			return "[artifacts]\n" + strings.TrimSpace(idx)
		}
	}
	return "[" + d.Kind + "] unknown verb " + d.Verb + " (read, save or list)"
}

// keepFiles saves the files connectors answered with into the artifacts library.
func (s *Service) keepFiles(results []connector.Result, k *kitty.Kitty, turn *Turn) {
	for i := range results {
		if results[i].File == nil || s.arts == nil {
			continue
		}
		a, err := s.arts.Put(results[i].File.Name, results[i].File.Data, k.Name)
		if err != nil {
			results[i].Err = err.Error()
			continue
		}
		results[i].Artifact = a.Name
		turn.Artifacts = append(turn.Artifacts, a.Name)
	}
}

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
	if t, ok := s.tasks[id]; ok {
		t.cancel()
	}
	delete(s.sessions, id)
	s.emitLocked(Event{Type: "deleted", Session: id})
	_ = os.RemoveAll(filepath.Join(s.dir, id))
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
	if sess.Working() {
		s.mu.Unlock()
		return nil, ErrBusy
	}
	sess.Turns = append(sess.Turns, Turn{At: s.now(), From: "user", Text: text})
	sess.UpdatedAt = s.now()
	members := append([]string(nil), sess.Members...)
	err := s.save(sess)
	s.emitLocked(Event{Type: "turn", Session: id, Index: len(sess.Turns) - 1, Turn: &sess.Turns[len(sess.Turns)-1]})
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
		turn, pending, err := s.answer(ctx, id, k, kitties)
		if err != nil {
			turn = Turn{At: s.now(), From: k.Name, Kitty: k.ID, Text: "(" + k.Name + " could not answer: " + err.Error() + ")"}
		}
		s.mu.Lock()
		sess.Turns = append(sess.Turns, turn)
		index := len(sess.Turns) - 1
		sess.UpdatedAt = s.now()
		saveErr := s.save(sess)
		s.emitLocked(Event{Type: "turn", Session: id, Index: index, Turn: &sess.Turns[index]})
		s.mu.Unlock()
		if saveErr != nil {
			return replies, saveErr
		}
		replies = append(replies, turn)
		if pending != nil {
			s.startTask(id, index, k, pending)
			// The kitties after this one in a group wait for the task: they answer on the
			// person's next message, with the task's result in the thread.
			break
		}
	}
	return replies, nil
}

// Cancel stops the desktop task running in a session, if any.
func (s *Service) Cancel(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return false
	}
	t.cancel()
	return true
}

// Wait blocks until every background task has ended.
func (s *Service) Wait() { s.wg.Wait() }

// Shot reads one screenshot of a session.
func (s *Service) Shot(id, name string) ([]byte, error) {
	if !shotName.MatchString(name) {
		return nil, fmt.Errorf("no such screenshot %q", name)
	}
	if _, err := s.Get(id); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(s.dir, id, name))
	if err != nil {
		return nil, fmt.Errorf("no such screenshot %q", name)
	}
	return data, nil
}

var shotName = regexp.MustCompile(`^[0-9]+-[0-9]+\.png$`)

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

// pendingTask is a turn whose kitty asked to act on its desktop: the loop continues in the
// background from these messages and actions.
type pendingTask struct {
	brain    provider.Client
	messages []provider.Message
	tools    []provider.Tool
	native   bool
	reply    provider.Reply
	text     string
	actions  []action
	allowed  map[string]string
	// pre are the outcomes of the batch's connector and library actions, already run in the
	// call that answered the person, by their index in actions.
	pre map[int]outcome
}

// ask asks the brain, with the tools when native.
func ask(ctx context.Context, brain provider.Client, native bool, messages []provider.Message, tools []provider.Tool) (provider.Reply, error) {
	if native {
		if tc, ok := brain.(provider.ToolClient); ok {
			return tc.CompleteTools(ctx, messages, tools)
		}
	}
	return brain.Complete(ctx, messages)
}

// answer asks one kitty for its turn, running the connectors it calls. When the kitty asks to
// act on its desktop, the turn is returned as it stands with the pending task to continue.
func (s *Service) answer(ctx context.Context, id string, k *kitty.Kitty, members []*kitty.Kitty) (Turn, *pendingTask, error) {
	brain, err := s.fleet.Brain(k)
	if err != nil {
		return Turn{}, nil, err
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
	if k.Desktop {
		system += "\n" + desktop.Catalogue()
	}
	if lib := s.libraryPrompt(); lib != "" {
		system += "\n" + lib
	}
	sess, err := s.Get(id)
	if err != nil {
		return Turn{}, nil, err
	}
	messages := s.transcript(system, sess.Turns, k)

	native := false
	var tools []provider.Tool
	if tc, ok := brain.(provider.ToolClient); ok && s.nativeTools && tc.SupportsTools() {
		tools = s.toolSet(k, allowed)
		native = len(tools) > 0
	}
	turn := Turn{From: k.Name, Kitty: k.ID}
	var parts []string
	for round := 0; round < s.MaxRounds; round++ {
		reply, err := ask(ctx, brain, native, messages, tools)
		if err != nil {
			return Turn{}, nil, err
		}
		turn.Usage.Input += reply.Usage.Input
		turn.Usage.Output += reply.Usage.Output
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
		// Without a display a desktop tool is just words, and without connectors a connector
		// call is too: both are dropped rather than answered.
		kept := actions[:0]
		for _, a := range actions {
			if (a.kind == "tool" && !k.Desktop) || (a.kind == "connector" && len(allowed) == 0) {
				continue
			}
			kept = append(kept, a)
		}
		actions = kept
		if len(actions) == 0 {
			break
		}
		if hasDesktopAction(actions) {
			// Connector and library actions run first, in this call; the display's go on in
			// the background with everything the model needs to continue.
			pre := map[int]outcome{}
			for i, a := range actions {
				if a.kind != "tool" {
					pre[i] = s.runNonDesktop(ctx, []action{a}, k, allowed, &turn)[0]
				}
			}
			turn.At = s.now()
			turn.Text = strings.TrimSpace(strings.Join(parts, "\n\n"))
			turn.Working = true
			return turn, &pendingTask{brain: brain, messages: messages, tools: tools, native: native, reply: reply, text: reply.Content, actions: actions, allowed: allowed, pre: pre}, nil
		}
		outs := s.runNonDesktop(ctx, actions, k, allowed, &turn)
		messages = append(messages, feedback(native, reply, reply.Content, outs, nil)...)
	}
	turn.At = s.now()
	turn.Text = strings.TrimSpace(strings.Join(parts, "\n\n"))
	if turn.Text == "" {
		turn.Text = "(" + k.Name + " had nothing to add)"
	}
	return turn, nil, nil
}

// startTask continues a turn in the background: the kitty acts on its desktop, sees the
// screenshot, and goes on until it says done or the steps run out.
func (s *Service) startTask(id string, index int, k *kitty.Kitty, p *pendingTask) {
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.tasks[id] = &task{cancel: cancel}
	s.mu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		s.runTask(ctx, id, index, k, p)
		s.mu.Lock()
		delete(s.tasks, id)
		s.mu.Unlock()
	}()
}

// runner is the fleet as a desktop runner, when it is one.
func (s *Service) runner() desktop.Runner {
	r, _ := s.fleet.(desktop.Runner)
	return r
}

func (s *Service) runTask(ctx context.Context, id string, index int, k *kitty.Kitty, p *pendingTask) {
	runner := s.runner()
	messages := p.messages
	actions := p.actions
	reply := p.reply
	replyText := p.text
	var parts []string
	final := ""
	fail := ""
	for round := 0; ; round++ {
		if runner == nil {
			fail = "this kitty has no desktop runner"
			break
		}
		if round >= s.MaxSteps {
			fail = fmt.Sprintf("stopped after %d rounds without finishing", s.MaxSteps)
			break
		}
		var outs []outcome
		done := false
		for i, a := range actions {
			if a.kind != "tool" {
				if o, ok := p.pre[i]; ok && round == 0 {
					outs = append(outs, o)
					continue
				}
				// A library or connector action in a later round runs here.
				var turn Turn
				o := s.runNonDesktop(ctx, []action{a}, k, p.allowed, &turn)
				s.edit(id, index, func(t *Turn) {
					t.Calls = append(t.Calls, turn.Calls...)
					t.Artifacts = append(t.Artifacts, turn.Artifacts...)
					t.Skills = append(t.Skills, turn.Skills...)
				})
				outs = append(outs, o...)
				continue
			}
			da := desktop.Action{Name: a.name, Args: a.args, Raw: a.raw}
			if summary, ok := da.Done(); ok {
				final, done = summary, true
				s.appendStep(id, index, Step{At: s.now(), Action: a.name, Args: a.args, Result: summary})
				outs = append(outs, outcome{action: a, text: "finished"})
				break
			}
			out, err := desktop.Run(ctx, runner, k.ID, da)
			step := Step{At: s.now(), Action: a.name, Args: a.args, Result: out}
			o := outcome{action: a, text: out}
			if err != nil {
				step.Error = err.Error()
				o.err = err.Error()
			}
			outs = append(outs, o)
			s.appendStep(id, index, step)
			if ctx.Err() != nil {
				break
			}
		}
		if ctx.Err() != nil {
			fail = "the task was cancelled"
			break
		}
		if done {
			break
		}
		png, err := runner.Screenshot(ctx, k.ID)
		extra := provider.Message{Role: "user", Content: "Here is the screenshot of your display now. Continue with your next actions, or call done when the task is finished."}
		if err != nil {
			extra.Content = "The screenshot failed: " + err.Error() + ". Continue with your next actions, or call done when the task is finished."
		} else if name, err := s.saveShot(id, index, png); err == nil {
			s.markShot(id, index, name)
			extra.Images = []provider.Image{{MIME: "image/png", Data: png}}
		}
		messages = append(messages, feedback(p.native, reply, replyText, outs, &extra)...)
		if len(messages) > s.MaxTurns*2 {
			messages = append(messages[:1], messages[len(messages)-s.MaxTurns*2:]...)
		}
		answer, err := ask(ctx, p.brain, p.native, messages, p.tools)
		if err != nil {
			fail = err.Error()
			break
		}
		s.addUsage(id, index, answer.Usage)
		reply = answer
		replyText = answer.Content
		var text string
		if p.native && len(answer.Calls) > 0 {
			actions, text = fromCalls(answer.Calls), answer.Content
		} else {
			actions, text = fromText(answer.Content)
		}
		if text = strings.TrimSpace(text); text != "" {
			parts = append(parts, text)
		}
		if len(actions) == 0 {
			break
		}
	}
	if final != "" {
		parts = append(parts, final)
	}
	if fail != "" {
		parts = append(parts, "("+k.Name+" could not finish: "+fail+")")
	}
	s.finishTurn(id, index, strings.TrimSpace(strings.Join(parts, "\n\n")))
}

// edit changes one turn of a session under the lock and saves it.
func (s *Service) edit(id string, index int, fn func(*Turn)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok || index >= len(sess.Turns) {
		return
	}
	fn(&sess.Turns[index])
	sess.UpdatedAt = s.now()
	_ = s.save(sess)
	kind := "update"
	if !sess.Turns[index].Working {
		kind = "done"
	}
	s.emitLocked(Event{Type: kind, Session: id, Index: index, Turn: &sess.Turns[index]})
}

func (s *Service) appendStep(id string, index int, step Step) {
	s.edit(id, index, func(t *Turn) { t.Steps = append(t.Steps, step) })
}

func (s *Service) markShot(id string, index int, name string) {
	s.edit(id, index, func(t *Turn) {
		if len(t.Steps) > 0 {
			t.Steps[len(t.Steps)-1].Shot = name
		} else {
			t.Steps = append(t.Steps, Step{At: s.now(), Action: "screenshot", Shot: name})
		}
	})
}

func (s *Service) addUsage(id string, index int, u provider.Usage) {
	s.edit(id, index, func(t *Turn) {
		t.Usage.Input += u.Input
		t.Usage.Output += u.Output
	})
}

func (s *Service) finishTurn(id string, index int, text string) {
	s.edit(id, index, func(t *Turn) {
		t.Working = false
		if text != "" {
			if t.Text != "" {
				t.Text += "\n\n"
			}
			t.Text += text
		}
		if t.Text == "" {
			t.Text = "(" + t.From + " finished without a word)"
		}
	})
}

// saveShot writes a screenshot under the session's folder and returns its name.
func (s *Service) saveShot(id string, index int, png []byte) (string, error) {
	dir := filepath.Join(s.dir, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	n := 0
	s.mu.RLock()
	if sess, ok := s.sessions[id]; ok && index < len(sess.Turns) {
		n = len(sess.Turns[index].Steps)
	}
	s.mu.RUnlock()
	name := fmt.Sprintf("%d-%d.png", index, n)
	return name, os.WriteFile(filepath.Join(dir, name), png, 0o600)
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
