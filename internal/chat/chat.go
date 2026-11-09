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
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/madkoding/kitties/internal/agenda"
	"github.com/madkoding/kitties/internal/approvals"
	"github.com/madkoding/kitties/internal/artifacts"
	"github.com/madkoding/kitties/internal/attach"
	"github.com/madkoding/kitties/internal/budget"
	"github.com/madkoding/kitties/internal/connector"
	"github.com/madkoding/kitties/internal/desktop"
	"github.com/madkoding/kitties/internal/guard"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/memory"
	"github.com/madkoding/kitties/internal/notify"
	"github.com/madkoding/kitties/internal/provider"
	"github.com/madkoding/kitties/internal/replay"
	"github.com/madkoding/kitties/internal/skills"
	"github.com/madkoding/kitties/internal/soul"
	"github.com/madkoding/kitties/internal/vault"
)

// ShotTokens is about what one screenshot of the 1280x800 display weighs for a model
// (width*height/750, the way Anthropic counts an image; others are near it): an estimate, for
// comparing pictures with text, not a bill.
const ShotTokens = 1280 * 800 / 750

// Looks is how a turn saw its work: by picture or by text.
type Looks struct {
	Shots int `json:"shots,omitempty"`
	// ShotTokens is the estimate of what the screenshots weighed (ShotTokens each).
	ShotTokens int `json:"shot_tokens,omitempty"`
	// Reads are the text answers that took the place of a picture; ReadTokens what they
	// weighed, at about four characters a token.
	Reads      int `json:"reads,omitempty"`
	ReadTokens int `json:"read_tokens,omitempty"`
}

// Turn is one message of a thread.
type Turn struct {
	At   time.Time `json:"at"`
	From string    `json:"from"` // "user", a kitty's name, or "system" for connector results
	// Kitty is the kitty id when From is a kitty.
	Kitty string `json:"kitty,omitempty"`
	// Sender names who wrote a "user" turn that is not the person: a WhatsApp contact as
	// "Ana (+56912345678)". The kitty reads it as the turn's author, so the people of a chat are
	// not all taken for the person.
	Sender string `json:"sender,omitempty"`
	Text   string `json:"text"`
	// Calls are the connectors the kitty used in this turn, if any.
	Calls []connector.Result `json:"calls,omitempty"`
	Usage provider.Usage     `json:"usage,omitempty"`
	// Looks counts how the kitty saw its work in this turn: screenshots sent to the model, and
	// answers it read as text instead (a page, a file, a command).
	Looks *Looks `json:"looks,omitempty"`
	// Steps are the actions the kitty took on its desktop in this turn, with their screenshots.
	Steps []Step `json:"steps,omitempty"`
	// Artifacts names the files the kitty saved to the library in this turn.
	Artifacts []string `json:"artifacts,omitempty"`
	// Skills names the skills it read or proposed in this turn.
	Skills []string `json:"skills,omitempty"`
	// Attachments are the files the person attached to this message (attach.go).
	Attachments []attach.Attachment `json:"attachments,omitempty"`
	// Working is true while the kitty is still acting on its desktop for this turn.
	Working bool `json:"working,omitempty"`
	// NeedsHelp is true when the kitty gave up on this turn and asked the person.
	NeedsHelp bool `json:"needs_help,omitempty"`
	// Resumable is true when the task stopped on its budget (time, tokens or rounds) and can go
	// on from where it was: the kitty's next answer gets the steps it had taken.
	Resumable bool `json:"resumable,omitempty"`
	// Parked is true when the kitty stopped on its day's tokens: it goes on by itself once
	// they are back (a new day, or a higher limit), see resume.go.
	Parked bool `json:"parked,omitempty"`
	// Delegations are the subtasks the kitty handed to other kitties in this turn.
	Delegations []Delegation `json:"delegations,omitempty"`
	// Approvals are the actions this turn held for the person's yes.
	Approvals []ApprovalRef `json:"approvals,omitempty"`
	// Feedback is what the person said about the turn: "up", "down" or empty.
	Feedback string `json:"feedback,omitempty"`
	// Limits are the limits the kitty set on its own actions in this turn, because the person
	// asked: the page shows them, and can take each back.
	Limits []LimitRef `json:"limits,omitempty"`
	// Report, in the lead's Reports thread, says what news this turn brings from another kitty.
	Report *Report `json:"report,omitempty"`
	// AskedLead names the lead kitty this turn asked a question of (ask.go): its answer comes
	// back in the thread.
	AskedLead string `json:"asked_lead,omitempty"`
	// Detail is the technical form of what the text says in plain words (an error as the
	// service wrote it), for whoever asks to see technical details.
	Detail string `json:"detail,omitempty"`
}

// LimitRef is a limit a kitty set on itself: the mode it set (empty for a limit lifted) and the
// one there was before, which undoing it puts back.
type LimitRef struct {
	Connector string `json:"connector"`
	Action    string `json:"action"`
	Mode      string `json:"mode"`
	Previous  string `json:"previous,omitempty"`
}

// ApprovalRef points at an action held for approval.
type ApprovalRef struct {
	ID        string `json:"id"`
	Connector string `json:"connector"`
	Action    string `json:"action"`
	Summary   string `json:"summary"`
}

// Delegation is a subtask one kitty handed to another.
type Delegation struct {
	Kitty   string `json:"kitty"` // the other kitty's name
	KittyID string `json:"kitty_id"`
	Chat    string `json:"chat"` // where it happened
	Task    string `json:"task"`
	Result  string `json:"result,omitempty"`
	// Answer is the whole answer, up to answerMax, when Result had to be cut.
	Answer string `json:"answer,omitempty"`
	Error  string `json:"error,omitempty"`
	// Limits says what the lead gave the kitty for this task alone, when it did.
	Limits string `json:"limits,omitempty"`
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

// StepPlan is the action of a step that is no action: what the kitty said it was about to do
// (its words next to a batch of desktop actions), kept in the steps as the line the person
// follows the task by, instead of in the message, which keeps the answer only.
const StepPlan = "plan"

// planStep is the kitty's words next to a batch of actions, as a step; ok is false when it said
// nothing or the batch ends the task (then the words belong to the answer).
func planStep(at time.Time, text string, actions []action) (Step, bool) {
	text = strings.TrimSpace(text)
	if text == "" || len(actions) == 0 || endsTask(actions) {
		return Step{}, false
	}
	return Step{At: at, Action: StepPlan, Result: text}, true
}

// Session is one thread.
type Session struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Description is what the person says the chat is about (Edit): the sidebar's tooltip, the
	// header's second line and a line of the kitties' prompt.
	Description string `json:"description,omitempty"`
	// DescriptionBy wrote the description ("user", or a kitty's name when the person accepted its
	// proposal) and DescriptionAt is when; Descriptions are the earlier ones, newest first.
	DescriptionBy string        `json:"description_by,omitempty"`
	DescriptionAt *time.Time    `json:"description_at,omitempty"`
	Descriptions  []Description `json:"descriptions,omitempty"`
	// Proposal is a new description a kitty proposes because the chat changed course: it waits
	// for the person to accept or dismiss it. DismissedAt is how many turns the chat had when the
	// person last dismissed one, so the kitties do not ask again at once.
	Proposal    *Proposal `json:"proposal,omitempty"`
	DismissedAt int       `json:"proposal_dismissed_at,omitempty"`
	Members     []string  `json:"members"` // kitty ids
	Turns       []Turn    `json:"turns"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	// Goal is the goal this thread works on, when the scheduler opened it.
	Goal string `json:"goal,omitempty"`
	// Owner is the kitty that opened this thread to delegate, and Depth how many hand-overs
	// deep it is (a person's chat is 0).
	Owner string `json:"owner,omitempty"`
	Depth int    `json:"depth,omitempty"`
	// Help says the last turn is a kitty asking for help; set on the copies Get and List hand out.
	Help bool `json:"needs_help,omitempty"`
	// Summary stands for the first SummaryUpTo turns, which the kitties no longer see one by
	// one: a long thread keeps its early context in a few lines instead of dropping it.
	Summary     string `json:"summary,omitempty"`
	SummaryUpTo int    `json:"summary_up_to,omitempty"`
	// Reports marks the lead kitty's Reports thread, where the other kitties' news land.
	Reports bool `json:"reports,omitempty"`
	// Asks, on the Reports thread, are the questions the other kitties put to the lead (ask.go).
	Asks []LeadAsk `json:"asks,omitempty"`
	// Gist is the chat's last words, up to two lines in the sidebar, with secrets and personal details masked, and
	// GistBy who said them ("user" or a kitty's name): what the sidebar shows under the title.
	Gist   string `json:"gist,omitempty"`
	GistBy string `json:"gist_by,omitempty"`
	// Exposed marks a chat where whoever writes is not the person (a WhatsApp contact, a group, a
	// public chat): the guardrails apply (guard.go). Audience says who writes there.
	Exposed  bool   `json:"exposed,omitempty"`
	Audience string `json:"audience,omitempty"`
	// Etiquette is how to write in this chat, as the channel it is bound to says (a WhatsApp
	// group: whom an answer reaches, how to @mention someone and when everyone): a line of the
	// kitties' prompt.
	Etiquette string `json:"etiquette,omitempty"`
	// Delegates are the kitties that took part through a hand-over without being members (they
	// do not answer what the person writes), and Kitties every kitty of the chat: members,
	// owner and delegates (crew.go). More than one makes the chat a group of kitties.
	Delegates []string `json:"delegates,omitempty"`
	Kitties   []string `json:"kitties,omitempty"`
	// Dismissed are delegates the person took off the chat: its old hand-overs no longer count.
	Dismissed []string `json:"dismissed,omitempty"`
	// Helping are the kitties at work on a subtask handed over from this chat right now: the page
	// shows them working until they answer. Cleared when the gateway starts.
	Helping []Helper `json:"helping,omitempty"`
	// Kind is what the thread is: a chat, a goal's (a scheduled task's) or a hand-over between
	// two kitties (kinds.go); worked out from the fields above on every save and load.
	Kind string `json:"kind"`
	// Keep marks a hand-over thread the person kept: it stays, and shows among the chats.
	// EndedAt is when its last hand-over ended, and ExpiresAt when it goes (zero: it stays);
	// ExpiresAt is set on the copies Get and List hand out.
	Keep      bool      `json:"keep,omitempty"`
	EndedAt   time.Time `json:"ended_at,omitzero"`
	ExpiresAt time.Time `json:"expires_at,omitzero"`
	// Shown says the chat goes in the list of chats (Listed), set on the copies Get and List
	// hand out: a goal's thread and a hand-over not kept do not.
	Shown bool `json:"listed"`
}

// NeedsHelp reports whether the last turn is a kitty asking the person for help.
func (s *Session) NeedsHelp() bool {
	return len(s.Turns) > 0 && s.Turns[len(s.Turns)-1].NeedsHelp
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
	List() []*kitty.Kitty
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
	// handOvers holds the notices of hand-overs that ended, per chat, for HandOverWait so the
	// ones that end close together go out as one message (crew.go).
	handOverMu   sync.Mutex
	handOvers    map[string][]notify.Notification
	HandOverWait time.Duration
	// failStreak counts, per kitty, the subtasks handed to it that failed in a row (crew.go).
	failStreak map[string]int
	// vault holds the secrets a kitty may type without reading them, nil when there is none.
	vault *vault.Store
	// helps are the kitties waiting for the person's answer, by turn (help.go).
	helps map[string]chan HelpAnswer
	// standIns counts, for the day standInsDay, the tasks each kitty handed to a stand-in;
	// standInsFrom gives how many it may (autonomy.go).
	standIns     map[string]int
	standInsDay  string
	standInsFrom func() int
	// resuming are the parked chats being woken up (resume.go).
	resuming map[string]bool
	now      func() time.Time
	newID    func() string
	// MaxRounds bounds how many times a kitty may call connectors in one answer.
	MaxRounds int
	// UnattendedRounds is how many rounds of connector calls an unattended kitty may take in one
	// answer (MaxRounds is for an attended one); zero, the default, sets no cap.
	UnattendedRounds int
	// UnattendedWindows is how many windows of a task's rounds an unattended kitty goes through by
	// itself on the desktop before it stops at the safety cap and waits to be told to go on; zero,
	// the default, sets no cap.
	UnattendedWindows int
	// BrainWait is how long, in all, an unattended task waits for a brain that is down (busy,
	// failing or unreachable) before the attempt fails; zero does not wait.
	BrainWait time.Duration
	// brainWaitFrom, when set, gives BrainWait, read at each outage so a change in the settings
	// applies at once.
	brainWaitFrom func() time.Duration
	// brainEvery is the first wait before trying a brain that is down again; it doubles.
	brainEvery time.Duration
	// MaxTurns is how many turns of the thread a kitty is shown; zero (the default) shows all of
	// it, fitted to the model's context window.
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
	// DefaultLimits bound a task of a kitty that sets none of its own.
	DefaultLimits kitty.Limits
	// MaxDepth is how many hand-overs deep a delegation may go.
	MaxDepth int
	// journal and goals are the agenda's record and its report line, nil without an agenda.
	journal *agenda.Journal
	goals   Reporter
	// approvals holds the actions waiting for a yes, nil when nothing is held back.
	approvals *approvals.Store
	budget    *budget.Store
	// demand is how demanding a kitty's person is (1 relaxed to 5 strict); 3 when unset.
	demand func(kittyID string) int
	// notifier tells the person what happened outside the interface, nil when it does not.
	notifier notify.Sender
	// learnFromApproval hears every decision of the person, to learn from it; nil without memory.
	learnFromApproval func(approvals.Approval)
	// memory is what the kitties know about the person and the work, nil without it.
	memory *memory.Store
	// distilling marks the kitties whose memory is being consolidated, so it runs once at a time.
	distilling map[string]bool
	// CompactAfter is how many turns past the last summary a thread may grow before the early
	// ones are summarised; CompactKeep is how many of the latest stay as they were said.
	CompactAfter int
	CompactKeep  int
	// pollEvery is how often a delegating kitty looks whether the other one finished.
	pollEvery time.Duration
	// overrides are the one-task changes of limits the lead passed with a delegation, by thread;
	// leadLimitsAsk says whether the lead's changes of limits wait for the person (lead.go).
	overrides     map[string]kitty.TaskLimits
	leadLimitsAsk func() bool
	// parallel is how many hand-overs of one reply run at the same time (actions.go).
	parallel func() int
	// handOverKeep says how long a hand-over thread stays after it ended (kinds.go).
	handOverKeep func() time.Duration
	// guard keeps the exposed chats from giving the person away, nil when there is none.
	guard Guard
	// isIndependent says which kitties work on their own (independent.go); nil: none.
	isIndependent func(id string) bool
	// relay says something in the WhatsApp chat a chat is bound to (independent.go); nil: none.
	relay func(ctx context.Context, chatID, text string) error
	// exposedMinutes is how long a task asked in a WhatsApp chat may run (SetExposedMinutes);
	// nil or 0: ExposedMaxSeconds.
	exposedMinutes func(chatID string) int
}

// Reporter hears how a goal's run ended.
type Reporter interface {
	Report(goalID, result string)
}

// task is one desktop task running in the background.
type task struct {
	cancel context.CancelFunc
}

// Event is what a watcher of a session is told.
type Event struct {
	Type    string `json:"type"` // turn | update | done | deleted | helping | edited
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
		HandOverWait: time.Minute,
		now:          time.Now, newID: randomID, MaxRounds: 4, MaxSteps: 25, tasks: map[string]*task{}, nativeTools: true,
		DefaultLimits: kitty.Limits{MaxTokens: 0, Retries: 1}, MaxDepth: 2, pollEvery: 200 * time.Millisecond,
		BrainWait: DefaultBrainWait, brainEvery: 15 * time.Second,
		distilling: map[string]bool{}, CompactAfter: 40, CompactKeep: 16}
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
func (s *Service) libraryPrompt(k *kitty.Kitty) string {
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
		shared := "shared by every kitty"
		if s.independent(k) {
			shared = "of your own (you work apart from the other kitties: you see only the files you saved)"
		}
		b.WriteString("You have a library of artifacts (files) " + shared + ". Keep a result for later " +
			"with `@artifact save {\"name\": \"report.md\", \"content\": \"…\"}`, read one with " +
			"`@artifact read <name>`, list them with `@artifact list`. To point a person at one, link it as " +
			"`[report.md](/v1/artifacts/report.md)`: the app opens it from that path; never write a host " +
			"(localhost, wails.localhost) in front of it.\n")
		if idx := s.arts.IndexOf(12, s.artifactKeep(k)); idx != "" {
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
		case strings.HasPrefix(trimmed, "@delegate "):
			kind = "delegate"
		case strings.HasPrefix(trimmed, "@memory "):
			kind = "memory"
		case strings.HasPrefix(trimmed, "@limit "):
			kind = "limit"
		case strings.HasPrefix(trimmed, "@task_limits "):
			kind = "task_limits"
		case strings.HasPrefix(trimmed, "@describe "):
			kind = "describe"
		case strings.HasPrefix(trimmed, "@ask_lead "):
			kind = "ask_lead"
		case strings.HasPrefix(trimmed, "@answer_kitty "):
			kind = "answer_kitty"
		default:
			kept = append(kept, line)
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "@"+kind+" "))
		verb, argsText, _ := strings.Cut(rest, " ")
		if kind == "ask_lead" {
			// The question is all there is: no name before it.
			verb, argsText = "ask", rest
		}
		d := Directive{Kind: kind, Verb: strings.ToLower(strings.TrimSpace(verb)), Raw: trimmed, Args: map[string]any{}}
		if argsText = strings.TrimSpace(argsText); argsText != "" {
			if err := json.Unmarshal([]byte(argsText), &d.Args); err != nil {
				key := "name"
				switch kind {
				case "delegate":
					key = "task"
				case "memory":
					key = "text"
				case "task_limits":
					key = "max_rounds"
				case "describe":
					key = "description"
				case "ask_lead":
					key = "question"
				case "answer_kitty":
					key = "text"
				}
				d.Args[key] = strings.Trim(argsText, "\"' ")
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
			if by := s.arts.Owner(name); by != "" && by != k.Name && (s.independent(k) || s.independentNames()[by]) {
				return "[artifact save] " + name + " is another kitty's file: save yours under another name"
			}
			a, err := s.arts.Put(name, []byte(content), k.Name)
			if err != nil {
				return "[artifact save] " + err.Error()
			}
			turn.Artifacts = append(turn.Artifacts, a.Name)
			return "[artifact " + a.Name + "] saved (" + a.Type + ", " + fmt.Sprint(a.Size) + " bytes)"
		case "read", "get", "open":
			if !s.seesArtifact(k, s.arts.Owner(name), s.independentNames()) {
				return "[artifact " + name + "] no such artifact"
			}
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
			idx := s.arts.IndexOf(50, s.artifactKeep(k))
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
		// Recomputed rather than trusted: a chat saved before gists, or under older masking rules.
		refreshGist(&sess)
		refreshKitties(&sess)
		refreshKind(&sess)
		sess.ExpiresAt, sess.Shown = time.Time{}, false
		sess.Helping = nil // nobody is still at work after a restart
		s.sessions[sess.ID] = &sess
	}
	return nil
}

func (s *Service) save(sess *Session) error {
	refreshGist(sess)
	refreshKitties(sess)
	refreshKind(sess)
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
	if err := s.apart(ids); err != nil {
		return nil, err
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
	var add []string
	for _, ref := range members {
		k, err := s.fleet.Get(ref)
		if err != nil {
			return nil, err
		}
		add = append(add, k.ID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("no such chat %q", id)
	}
	all := append([]string(nil), sess.Members...)
	for _, kid := range add {
		if !contains(all, kid) {
			all = append(all, kid)
		}
	}
	if err := s.apart(all); err != nil {
		return nil, err
	}
	sess.Members = all
	sess.UpdatedAt = s.now()
	return sess, s.save(sess)
}

// RemoveMember takes a kitty out of a session: a member, or a delegate, which then stays out of
// the chat's kitties until it is handed work there again (crew.go).
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
	if !contains(sess.Members, k.ID) && contains(sess.Delegates, k.ID) {
		dismissDelegate(sess, k.ID)
		sess.UpdatedAt = s.now()
		return sess, s.save(sess)
	}
	if len(sess.Members) == 1 && sess.Members[0] == k.ID {
		return nil, errors.New("a chat needs at least one kitty: delete the chat instead")
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

// LeaveChats takes a removed kitty out of every chat, and deletes the chats it leaves with no
// kitty at all: a chat with nobody to answer is not a chat.
func (s *Service) LeaveChats(id string) error {
	return s.Prune(func(member string) bool { return member != id })
}

// Prune keeps, in every chat, only the members that still exist, and deletes the chats left
// with none (for instance the ones whose kitties were removed before chats were cleaned up).
func (s *Service) Prune(exists func(id string) bool) error {
	s.mu.Lock()
	var empty []string
	var firstErr error
	for id, sess := range s.sessions {
		kept := make([]string, 0, len(sess.Members))
		for _, m := range sess.Members {
			if exists(m) {
				kept = append(kept, m)
			}
		}
		// A kitty that is gone is no delegate, nor owner, of any chat either (crew.go).
		if pruneHelpers(sess, exists) && len(kept) == len(sess.Members) {
			if err := s.save(sess); err != nil && firstErr == nil {
				firstErr = err
			}
			continue
		}
		if len(kept) == len(sess.Members) {
			continue
		}
		if len(kept) == 0 {
			empty = append(empty, id)
			continue
		}
		sess.Members = kept
		if err := s.save(sess); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	s.mu.Unlock()
	for _, id := range empty {
		if err := s.Delete(id); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
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
	c.Delegates = append([]string(nil), sess.Delegates...)
	c.Kitties = append([]string(nil), sess.Kitties...)
	c.Helping = append([]Helper(nil), sess.Helping...)
	c.Turns = append([]Turn(nil), sess.Turns...)
	c.Help = sess.NeedsHelp()
	c.ExpiresAt = s.expiresAt(sess)
	c.Shown = sess.Listed()
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
		c.Help = sess.NeedsHelp()
		c.ExpiresAt = s.expiresAt(sess)
		c.Shown = sess.Listed()
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
	return s.send(ctx, id, "user", "", text)
}

// SendFiles is Send with files attached.
func (s *Service) SendFiles(ctx context.Context, id, text string, files []attach.Upload) ([]Turn, error) {
	return s.sendWith(ctx, id, "user", "", text, files)
}

// SendAs posts a message of someone who is not the person (a WhatsApp contact), named by sender,
// with the files they sent: the kitty reads it as theirs.
func (s *Service) SendAs(ctx context.Context, id, sender, text string, files []attach.Upload) ([]Turn, error) {
	return s.sendFrom(ctx, id, "user", "", sender, text, files)
}

// send posts a message from someone (the person, the scheduler, or a kitty delegating) and has
// the kitties answer it.
func (s *Service) send(ctx context.Context, id, from, fromID, text string) ([]Turn, error) {
	return s.sendWith(ctx, id, from, fromID, text, nil)
}

// sendWith is send with the files the person attached.
func (s *Service) sendWith(ctx context.Context, id, from, fromID, text string, files []attach.Upload) ([]Turn, error) {
	return s.sendFrom(ctx, id, from, fromID, "", text, files)
}

// sendFrom is sendWith with the name of who wrote, when it is not the person (Turn.Sender).
func (s *Service) sendFrom(ctx context.Context, id, from, fromID, sender, text string, files []attach.Upload) ([]Turn, error) {
	text = strings.TrimSpace(text)
	if text == "" && len(files) == 0 {
		return nil, errors.New("the message is empty")
	}
	// What a file is, and whether it may come in, is settled before anything is stored.
	metas, err := inspectAll(files)
	if err != nil {
		return nil, err
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
	if from == "user" && s.guarded(sess) {
		// A third party's message is weighed before anything of it is kept: one past the hour's
		// limit, or that tries to turn the kitty, is not answered.
		if err := s.guard.Admit(id, sess.Audience, text); err != nil {
			s.mu.Unlock()
			return nil, err
		}
	}
	if err := s.storeFiles(id, metas, files); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	sess.Turns = append(sess.Turns, Turn{At: s.now(), From: from, Kitty: fromID, Sender: sender, Text: text, Attachments: metas})
	sess.UpdatedAt = s.now()
	members := append([]string(nil), sess.Members...)
	goal := sess.Goal
	err = s.save(sess)
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
			turn = Turn{At: s.now(), From: k.Name, Kitty: k.ID, Text: "(" + k.Name + " could not answer: " + Plain(err.Error()) + ")", Detail: err.Error(), Parked: errors.Is(err, ErrDayTokens)}
			if s.guardedID(id) {
				// An error says how things are set up inside: the other side reads only that there
				// is no answer now; the person finds why in Detail.
				turn.Text = "(" + k.Name + " cannot answer right now)"
			}
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
			return replies, nil
		}
		if goal != "" {
			// A goal answered in words alone: the run is over, or waits for the lead's answer.
			result := "done"
			if err != nil {
				result = "blocked"
			} else if turn.AskedLead != "" {
				result = "asked"
			}
			s.reportGoal(k, goal, id, result, turn.Text)
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

// AnyBusy reports whether any kitty is in the middle of a task, in any chat.
func (s *Service) AnyBusy() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sess := range s.sessions {
		for _, t := range sess.Turns {
			if t.Working {
				return true
			}
		}
	}
	return false
}

// KittyBusy reports whether a kitty is in the middle of a task, in any chat.
func (s *Service) KittyBusy(kittyID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sess := range s.sessions {
		for _, t := range sess.Turns {
			if t.Working && t.Kitty == kittyID {
				return true
			}
		}
	}
	return false
}

// Replay is the film of a turn's desktop task: its screenshots, in order, as an animated GIF.
func (s *Service) Replay(id string, index int) ([]byte, error) {
	sess, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(sess.Turns) {
		return nil, fmt.Errorf("no such turn %d", index)
	}
	var shots [][]byte
	for _, st := range sess.Turns[index].Steps {
		if st.Shot == "" {
			continue
		}
		if data, err := s.Shot(id, st.Shot); err == nil {
			shots = append(shots, data)
		}
	}
	return replay.GIF(shots, replay.Default)
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
	// Where someone else writes, a kitty answers with the guard's rules; one that works on its own
	// keeps its own computer there (a browser, its files and commands: nothing of the person's).
	exposed := s.guardedID(id)
	if exposed && !s.independent(k) {
		return s.answerExposed(ctx, id, k, members, brain)
	}
	allowed := s.fleet.Connectors().Descriptions(k.Connectors)
	if exposed {
		allowed = s.guard.Connectors(allowed)
	}
	var group []string
	for _, m := range members {
		if m.ID != k.ID {
			group = append(group, m.Name)
		}
	}
	sc := soul.Context{
		KittyName: k.Name, Desktop: k.Desktop, Workspace: workspaceOf(k), Group: group, Connectors: allowed, User: s.user,
		Peers: s.peers(k, id), Unattended: !k.Limits.Or(s.DefaultLimits).Attended,
	}
	if k.Lead {
		sc.Lead, sc.Team = true, s.team(k)
	} else if lead := s.Lead(); lead != nil {
		sc.LeadName = lead.Name
		sc.AskLead = s.canAskLead(k, id) != nil
	}
	system := k.Soul.SystemPrompt(sc)
	if exposed {
		system += s.guard.Prompt(s.user) + s.ownComputerPrompt(k) + s.askFromExposedPrompt(k)
	}
	if k.Desktop {
		system += "\n" + desktop.Catalogue()
		if s.vault != nil && !exposed {
			if names := s.vault.Names(k.ID); len(names) > 0 {
				system += "\nSecrets the person saved for you to type with type_secret (you are never shown them): " + strings.Join(names, ", ") + ".\n"
			}
		}
	}
	if lib := s.libraryPrompt(k); lib != "" {
		system += "\n" + lib
	}
	s.compact(ctx, id, brain)
	sess, err := s.Get(id)
	if err != nil {
		return Turn{}, nil, err
	}
	if exposed {
		system += s.peoplePrompt(k)
	} else {
		system += s.memoryPrompt(k, sess)
	}
	system += s.limitPrompt(allowed)
	system += descriptionPrompt(sess) + s.sendersPrompt(sess) + etiquettePrompt(sess) + proposePrompt(sess)
	window := sess.Turns[min(sess.SummaryUpTo, len(sess.Turns)):]
	if from := s.goldfishFrom(sess, k); from >= 0 {
		// A task from the lead is all the kitty needs: it starts from it, without the earlier ones.
		window = sess.Turns[from:]
	} else if sess.Summary != "" {
		system += fmt.Sprintf("\nEarlier in this conversation (a summary of its first %d messages, which you no longer see one by one):\n%s\n", sess.SummaryUpTo, sess.Summary)
	}
	messages := s.transcriptFor(id, brain, s.deliver(ctx, id, k, window), system, window, k)

	native := false
	var tools []provider.Tool
	if tc, ok := brain.(provider.ToolClient); ok && s.nativeTools && tc.SupportsTools() {
		tools = s.toolSet(k, allowed)
		if exposed {
			tools = ownComputerTools(tools)
		}
		native = len(tools) > 0
	}
	turn := Turn{From: k.Name, Kitty: k.ID}
	var parts []string
	rounds, capped := s.MaxRounds, false
	if !k.Limits.Or(s.DefaultLimits).Attended {
		rounds = max(s.MaxRounds, s.UnattendedRounds)
		if s.UnattendedRounds == 0 {
			rounds = math.MaxInt
		}
	}
	for round := 0; round < rounds; round++ {
		capped = round == rounds-1
		if err := s.overTokens(k); err != nil {
			return Turn{}, nil, err
		}
		reply, err := ask(ctx, brain, native, s.withMachine(messages, k), tools)
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
		// Without a display a desktop tool is just words, and without connectors a connector
		// call is too: both are dropped rather than answered.
		kept := actions[:0]
		for _, a := range actions {
			if ((a.kind == "tool" || a.kind == "machine") && !k.Desktop) || (a.kind == "connector" && len(allowed) == 0) {
				continue
			}
			if a.kind == "delegate" && !s.canDelegate(k) {
				continue
			}
			if exposed && !s.ownComputerAllows(id, a, allowed) {
				continue
			}
			kept = append(kept, a)
		}
		actions = kept
		if len(actions) == 0 {
			capped = false
			break
		}
		if hasDesktopAction(actions) {
			// Connector and library actions run first, in this call; the display's go on in
			// the background with everything the model needs to continue.
			pre := map[int]outcome{}
			for i, a := range actions {
				if a.kind != "tool" {
					pre[i] = s.runNonDesktop(ctx, id, []action{a}, k, allowed, &turn)[0]
				}
			}
			turn.At = s.now()
			turn.Text = s.filterExposed(id, strings.TrimSpace(strings.Join(parts, "\n\n")))
			turn.Working = true
			return turn, &pendingTask{brain: brain, messages: messages, tools: tools, native: native, reply: reply, text: reply.Content, actions: actions, allowed: allowed, pre: pre}, nil
		}
		outs := s.runNonDesktop(ctx, id, actions, k, allowed, &turn)
		messages = append(messages, feedback(native, reply, reply.Content, outs, nil)...)
	}
	if capped {
		// The rounds ran out with work still asked for: say so rather than end as if it were done.
		parts = append(parts, fmt.Sprintf("(%s paused after %d rounds with work left: say \"continue\" and it goes on)", k.Name, rounds))
	}
	turn.At = s.now()
	turn.Text = strings.TrimSpace(strings.Join(parts, "\n\n"))
	if turn.Text == "" {
		turn.Text = "(" + k.Name + " had nothing to add)"
	}
	turn.Text = s.filterExposed(id, turn.Text)
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
// goOnWithout is what a kitty whose call for help went unanswered is told: the rest of the task
// does not wait for the person.
const goOnWithout = "do not stop here. Go on with every part of the task that does not need this, " +
	"and only at the end say in your answer exactly what you still need from the person."

// helpWait is how long a kitty that asked for help waits for the person to take its desktop.
var helpWait = 15 * time.Minute

// askHelp is the kitty asking the person to take its desktop over: the step shows a button to
// take control in the chat, a notification goes out, and the kitty waits until the person has
// taken the desktop and given it back (or does not come in time).
func (s *Service) askHelp(ctx context.Context, id string, index int, k *kitty.Kitty, reason string) string {
	// asked is taken before anything the person can see of the request: a desktop taken and given
	// back right after the step shows up still counts.
	asked := time.Now()
	reason = strings.TrimSpace(reason)
	if reason == "" || reason == "<nil>" {
		reason = "something on the desktop needs you"
	}
	s.appendStep(id, index, Step{At: s.now(), Action: "ask_help", Args: map[string]any{"reason": reason}, Result: reason})
	// The person sees what the kitty sees: its screen goes with the request.
	if runner := s.runner(); runner != nil {
		if png, err := runner.Screenshot(ctx, k.ID); err == nil {
			if name, err := s.saveShot(id, index, png); err == nil {
				s.markShot(id, index, name)
			}
		}
	}
	s.tell(notify.Help, k, "needs your hands on its desktop", reason, id)
	answers, stop := s.expectHelp(id, index)
	defer stop()
	h, ok := s.fleet.(holder)
	if !ok {
		return "nobody can take your desktop from here: " + goOnWithout
	}
	started, _ := s.fleet.(holdStarter)
	deadline := time.NewTimer(helpWait)
	defer deadline.Stop()
	t := time.NewTicker(heldEvery)
	defer t.Stop()
	came := false
	for {
		held := h.Held(k.ID)
		came = came || held || started != nil && started.HoldStarted(k.ID).After(asked)
		if came && !held {
			s.appendStep(id, index, Step{At: s.now(), Action: "helped", Result: "took the desktop and gave it back"})
			return "the person took your desktop and gave it back: look at the screenshot and continue"
		}
		select {
		case <-ctx.Done():
			return "stopped"
		case a := <-answers:
			return s.applyHelp(ctx, id, index, k, a)
		case <-deadline.C:
			if !came {
				return fmt.Sprintf("nobody came in %d minutes: %s", int(helpWait.Minutes()), goOnWithout)
			}
		case <-t.C:
		}
	}
}

// holder knows whether the person has taken a kitty's display over (the orchestrator does).
type holder interface{ Held(id string) bool }

// holdStarter knows when the person last took a kitty's display.
type holdStarter interface{ HoldStarted(id string) time.Time }

// heldEvery is how often a kitty waiting for its display looks whether it is back.
var heldEvery = time.Second

// waitWhileHeld waits, before an action on the display, while the person uses it: the two never
// fight over the mouse. The wait shows in the chat as one step.
func (s *Service) waitWhileHeld(ctx context.Context, id string, index int, kittyID string) {
	h, ok := s.fleet.(holder)
	if !ok || !h.Held(kittyID) {
		return
	}
	s.appendStep(id, index, Step{At: s.now(), Action: "held", Result: "the person is using the desktop: waiting until they give it back"})
	t := time.NewTicker(heldEvery)
	defer t.Stop()
	for h.Held(kittyID) {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) runner() desktop.Runner {
	r, _ := s.fleet.(desktop.Runner)
	return r
}

// taskState is what a background task carries between its rounds and attempts.
type taskState struct {
	messages  []provider.Message
	actions   []action
	reply     provider.Reply
	replyText string
	parts     []string
	final     string
	tokens    int
	rounds    int
	// lastShot and same follow the screen the kitty keeps seeing (stuck.go).
	lastShot [32]byte
	same     int
	// pre are the outcomes of the first batch's non-desktop actions, run before the task.
	pre map[int]outcome
	// detail is why the task failed, as the error said it.
	detail string
	// parked is whether it stopped on the kitty's day's tokens.
	parked bool
}

// runTask drives a desktop task to its end within the kitty's limits: rounds of actions, a
// screenshot after each, and the brain's next move; a failed attempt is tried another way as
// many times as the limits allow, and a task given up on is handed to the backup kitty or
// left with the person.
func (s *Service) runTask(ctx context.Context, id string, index int, k *kitty.Kitty, p *pendingTask) {
	limits := s.limitsFor(k, id)
	if limits.MaxSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(limits.MaxSeconds)*time.Second)
		defer cancel()
	}
	st := &taskState{messages: p.messages, actions: p.actions, reply: p.reply, replyText: p.text, pre: p.pre, tokens: p.reply.Usage.Counted()}
	fail, retryable := "", false
	for attempt := 0; ; attempt++ {
		fail, retryable = s.attempt(ctx, id, index, k, p, st, limits)
		if fail == "" || !retryable || attempt >= limits.Retries {
			break
		}
		s.note(k, agenda.KindRetry, "", id, fmt.Sprintf("attempt %d failed (%s); trying another way", attempt+1, fail))
		st.parts = append(st.parts, fmt.Sprintf("(attempt %d failed: %s; trying another way)", attempt+1, Plain(fail)))
		st.messages = append(st.messages, provider.Message{Role: "user", Content: "That attempt failed: " + fail + ". Try a different approach: do not repeat the same actions, and call done when the task is finished."})
		st.actions, st.rounds = nil, 0
	}
	if st.final != "" {
		st.parts = append(st.parts, st.final)
	}
	result, needsHelp := "done", false
	if fail != "" {
		result = "blocked"
		st.parts = append(st.parts, "("+k.Name+" could not finish: "+Plain(fail)+")")
		st.detail = fail
		if !errors.Is(ctx.Err(), context.Canceled) {
			// Given up, not stopped by a person: the backup takes over, or the person is asked.
			var handed string
			result, needsHelp, handed = s.giveUp(id, index, k, fail)
			if handed != "" {
				st.parts = append(st.parts, handed)
			}
		}
	}
	s.closeTask(id, index, k, s.filterExposed(id, strings.TrimSpace(strings.Join(st.parts, "\n\n"))), needsHelp, result)
	if st.detail != "" {
		s.edit(id, index, func(t *Turn) {
			t.Detail = st.detail
			t.Resumable = strings.HasPrefix(fail, "stopped")
			t.Parked = st.parked
		})
	}
}

// recapSteps is how many of its last steps a kitty is reminded of when it goes on with a task.
const recapSteps = 30

// recap is what a kitty did in a task that stopped on its budget, for it to go on from there
// rather than start again: why it stopped, and its last steps with their results.
func recap(t Turn) string {
	var b strings.Builder
	fmt.Fprintf(&b, "(The task above stopped before it was finished: %s. The desktop is as I left it. My last steps were:", t.Detail)
	steps := t.Steps
	if len(steps) > recapSteps {
		steps = steps[len(steps)-recapSteps:]
	}
	for _, st := range steps {
		args, _ := json.Marshal(st.Args)
		line := "\n- " + st.Action + " " + string(args)
		if st.Error != "" {
			line += " -> failed: " + clip(st.Error, 160)
		} else if r := strings.TrimSpace(st.Result); r != "" {
			line += " -> " + clip(strings.ReplaceAll(r, "\n", " "), 160)
		}
		b.WriteString(line)
	}
	b.WriteString("\nWhen asked to go on, look at the screen first and continue from there, without repeating what worked.)")
	return b.String()
}

// attempt runs rounds until the task is done, the model stops asking for actions, or something
// gives: it returns why, and whether another attempt makes sense.
func (s *Service) attempt(ctx context.Context, id string, index int, k *kitty.Kitty, p *pendingTask, st *taskState, limits kitty.Limits) (fail string, retryable bool) {
	runner := s.runner()
	window := limits.MaxRounds
	if window == 0 {
		window = s.MaxSteps
	}
	// Unattended, a task goes through several windows of rounds by itself; the cap is the safety
	// net next to the token and time budgets.
	maxRounds := window
	if !limits.Attended {
		switch {
		case s.UnattendedWindows == 0:
			maxRounds = math.MaxInt // no cap: it goes on until done, stopped, or a limit the person set
		case s.UnattendedWindows > 1:
			maxRounds = window * s.UnattendedWindows
		}
	}
	for ; ; st.rounds++ {
		if runner == nil {
			return "this kitty has no desktop runner", false
		}
		if st.rounds >= maxRounds && !endsTask(st.actions) {
			return fmt.Sprintf("stopped after %d rounds without finishing", maxRounds), true
		}
		if ctx.Err() != nil {
			return stopReason(ctx, limits), false
		}
		var outs []outcome
		done := false
		// looked is whether something of this batch can have changed the display: only then a
		// new screenshot is worth its tokens.
		looked := false
		for i, a := range st.actions {
			if !s.ownComputerAllows(id, a, p.allowed) {
				outs = append(outs, outcome{action: a, err: notHere})
				continue
			}
			if why := s.linkRefused(id, a); why != "" {
				outs = append(outs, outcome{action: a, err: why})
				continue
			}
			if a.kind != "tool" {
				if o, ok := st.pre[i]; ok {
					outs = append(outs, o)
					continue
				}
				// A library, connector or delegation action in a later round runs here.
				var turn Turn
				o := s.runNonDesktop(ctx, id, []action{a}, k, p.allowed, &turn)
				s.edit(id, index, func(t *Turn) {
					t.Calls = append(t.Calls, turn.Calls...)
					t.Artifacts = append(t.Artifacts, turn.Artifacts...)
					t.Skills = append(t.Skills, turn.Skills...)
					t.Delegations = append(t.Delegations, turn.Delegations...)
					if turn.AskedLead != "" {
						t.AskedLead = turn.AskedLead
					}
				})
				outs = append(outs, o...)
				if a.kind == "machine" {
					looked = true
				}
				continue
			}
			da := desktop.Action{Name: a.name, Args: a.args, Raw: a.raw}
			if summary, ok := da.Done(); ok {
				st.final, done = summary, true
				s.appendStep(id, index, Step{At: s.now(), Action: a.name, Args: a.args, Result: summary})
				outs = append(outs, outcome{action: a, text: "finished"})
				break
			}
			if err := s.ensureMachine(ctx, id, index, k); err != nil {
				outs = append(outs, outcome{action: a, err: err.Error()})
				if ctx.Err() != nil {
					break
				}
				continue
			}
			if da.Name == "ask_help" {
				looked = true
				outs = append(outs, outcome{action: a, text: s.askHelp(ctx, id, index, k, fmt.Sprint(a.args["reason"]))})
				if ctx.Err() != nil {
					break
				}
				continue
			}
			if da.Name == "type_secret" {
				looked = true
				o := s.typeSecret(ctx, id, index, k, a)
				outs = append(outs, o)
				if ctx.Err() != nil {
					break
				}
				continue
			}
			s.waitWhileHeld(ctx, id, index, k.ID)
			if err := s.memoryGate(ctx, k, da.Name); err != nil {
				s.appendStep(id, index, Step{At: s.now(), Action: a.name, Args: a.args, Error: err.Error()})
				outs = append(outs, outcome{action: a, err: err.Error()})
				continue
			}
			looked = looked || da.Visual()
			out, err := desktop.Run(ctx, runner, k.ID, da)
			step := Step{At: s.now(), Action: a.name, Args: a.args, Result: out}
			o := outcome{action: a, text: out}
			if reason, ok := desktop.PageNeedsPerson(da.Name, out); ok {
				// No screenshot follows a page tool: the page's own words say it is for the person.
				o.text += "\n(This page seems to ask for " + reason + ": that is the person's to pass. Use ask_help now; never try to solve or get around it.)"
			}
			if err != nil {
				step.Error = err.Error()
				o.err = err.Error()
			}
			outs = append(outs, o)
			s.appendStep(id, index, step)
			if !da.Visual() && o.text != "" {
				s.addLooks(id, index, Looks{Reads: 1, ReadTokens: (len(o.text) + 3) / 4})
			}
			if ctx.Err() != nil {
				break
			}
		}
		st.pre = nil
		if done {
			return "", false
		}
		if ctx.Err() != nil {
			return stopReason(ctx, limits), false
		}
		if len(st.actions) > 0 {
			extra := provider.Message{Role: "user", Content: "Nothing on the display can have changed, so no screenshot comes with this: ask for screenshot or read_screen when you need to look. Continue with your next actions, or call done when the task is finished."}
			if looked {
				png, err := runner.Screenshot(ctx, k.ID)
				if err == nil && st.watchScreen(png) {
					extra = s.unstick(ctx, runner, id, index, k, png)
				} else {
					extra = s.screenMessage(id, index, png, err)
				}
			}
			if !limits.Attended && st.rounds > 0 && (st.rounds+1)%window == 0 && st.rounds+1 < maxRounds {
				// A window ends: no stop, a checkpoint. The kitty restates what is left and goes on.
				s.appendStep(id, index, Step{At: s.now(), Action: "continue", Result: fmt.Sprintf("round %d of at most %d: going on by itself", st.rounds+1, maxRounds)})
				extra.Content = checkpointNudge + "\n" + extra.Content
			}
			st.messages = append(st.messages, feedback(p.native, st.reply, st.replyText, outs, &extra)...)
		}
		st.messages = fitContext(trimHistory(st.messages, s.MaxTurns*2), provider.ContextWindow(p.brain))
		if err := s.overTokens(k); err != nil {
			st.parked = true
			return "stopped: " + err.Error(), false
		}
		answer, err := s.askPatiently(ctx, id, index, p, st.messages, limits)
		if err != nil {
			if ctx.Err() != nil {
				return stopReason(ctx, limits), false
			}
			return err.Error(), true
		}
		s.spendTokens(k, answer.Usage)
		s.addUsage(id, index, answer.Usage)
		st.tokens += answer.Usage.Counted()
		if limits.MaxTokens > 0 && st.tokens > limits.MaxTokens {
			return fmt.Sprintf("stopped: the token budget (%d) is spent", limits.MaxTokens), false
		}
		st.reply, st.replyText = answer, answer.Content
		var text string
		if p.native && len(answer.Calls) > 0 {
			st.actions, text = fromCalls(answer.Calls), answer.Content
		} else {
			st.actions, text = fromText(answer.Content)
		}
		if plan, ok := planStep(s.now(), text, st.actions); ok {
			// The kitty says what it goes on to do: a line the person follows, not the answer,
			// and, when they chose it, news of a long task on their phone.
			plan.Result = s.filterExposed(id, plan.Result)
			s.appendStep(id, index, plan)
			s.progress(k, plan.Result, id)
		} else if text = strings.TrimSpace(text); text != "" {
			st.parts = append(st.parts, text)
		}
		if len(st.actions) == 0 {
			return "", false
		}
	}
}

// askPatiently asks the brain for the task's next move. Unattended, a brain that is down for a
// while (busy, failing, unreachable) is waited for, ever longer up to brainWaitMax between tries
// and BrainWait in all, instead of spending one of the task's attempts and handing it to the
// person over a passing outage. Each wait shows in the chat as a step.
func (s *Service) askPatiently(ctx context.Context, id string, index int, p *pendingTask, messages []provider.Message, limits kitty.Limits) (provider.Reply, error) {
	wait, waited := s.brainEvery, time.Duration(0)
	for {
		answer, err := ask(ctx, p.brain, p.native, messages, p.tools)
		if err == nil || limits.Attended || ctx.Err() != nil || !provider.Transient(err) || waited+wait > s.brainWait() {
			return answer, err
		}
		s.appendStep(id, index, Step{At: s.now(), Action: "wait", Error: clip(err.Error(), 300),
			Result: fmt.Sprintf("the brain is not answering: trying again in %s", wait.Round(time.Second))})
		select {
		case <-ctx.Done():
			return provider.Reply{}, ctx.Err()
		case <-time.After(wait):
		}
		waited += wait
		wait = min(wait*2, brainWaitMax)
	}
}

// DefaultBrainWait is how long an unattended task waits for a brain that is down, by default.
const DefaultBrainWait = 30 * time.Minute

// SetBrainWait has the wait for a brain that is down read from fn (the settings).
func (s *Service) SetBrainWait(fn func() time.Duration) { s.brainWaitFrom = fn }

func (s *Service) brainWait() time.Duration {
	if s.brainWaitFrom != nil {
		return s.brainWaitFrom()
	}
	return s.BrainWait
}

// brainWaitMax is the longest wait between two tries of a brain that is down.
const brainWaitMax = 5 * time.Minute

// screenMessage is what the kitty is told after actions that can have changed its display: the
// screenshot, which is also kept for the chat's step, or why there is none.
func (s *Service) screenMessage(id string, index int, png []byte, err error) provider.Message {
	return s.screenMessageWith(id, index, png, err, "")
}

// screenMessageWith is screenMessage with something said before it.
func (s *Service) screenMessageWith(id string, index int, png []byte, err error, before string) provider.Message {
	m := provider.Message{Role: "user", Content: "Here is the screenshot of your display now. Continue with your next actions, or call done when the task is finished."}
	if err != nil {
		m.Content = "The screenshot failed: " + err.Error() + ". Continue with your next actions, or call done when the task is finished."
	} else if name, err := s.saveShot(id, index, png); err == nil {
		s.markShot(id, index, name)
		m.Images = []provider.Image{forModel(png)}
		s.addLooks(id, index, Looks{Shots: 1, ShotTokens: ShotTokens})
	}
	if before != "" {
		m.Content = before + "\n" + m.Content
	}
	return m
}

// checkpointNudge is said to an unattended kitty at the end of each window of rounds: the
// history may be trimmed, so it writes down where it is instead of stopping.
const checkpointNudge = "Checkpoint: nobody is waiting for you to ask, keep going. In your next reply, write the list of items " +
	"the task covers with what is done and what is left, then continue with the first one left."

// endsTask reports whether a batch calls done: that batch runs even past the round limit.
func endsTask(actions []action) bool {
	for _, a := range actions {
		if a.kind == "tool" {
			if _, ok := (desktop.Action{Name: a.name, Args: a.args, Raw: a.raw}).Done(); ok {
				return true
			}
		}
	}
	return false
}

// stopReason says why a context ended a task.
func stopReason(ctx context.Context, limits kitty.Limits) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Sprintf("stopped: the time budget (%ds) is spent", limits.MaxSeconds)
	}
	return "the task was cancelled"
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

// Plans are what a kitty said it went on to do on its desktop since a time (its plan lines in
// every chat), newest first, at most n; the daily summary repeats them.
func (s *Service) Plans(kittyID string, since time.Time, n int) []string {
	type said struct {
		at   time.Time
		text string
	}
	var all []said
	s.mu.RLock()
	for _, sess := range s.sessions {
		for _, t := range sess.Turns {
			if t.Kitty != kittyID {
				continue
			}
			for _, st := range t.Steps {
				if st.Action == StepPlan && !st.At.Before(since) {
					all = append(all, said{st.At, st.Result})
				}
			}
		}
	}
	s.mu.RUnlock()
	sort.Slice(all, func(i, j int) bool { return all[i].at.After(all[j].at) })
	var out []string
	for i := 0; i < len(all) && i < n; i++ {
		out = append(out, all[i].text)
	}
	return out
}

// progress tells the person, away from the page, what a kitty goes on to do in a long task. It
// goes straight to the notifier (the lead's Reports thread would only repeat the chat), which sends
// it when the person chose progress and holds back the next of the same chat for a while.
func (s *Service) progress(k *kitty.Kitty, line, chatID string) {
	if s.notifier == nil {
		return
	}
	name, title := signed(s.leadFor(k), k, "is working on it")
	s.notifier.Notify(notify.Notification{Event: notify.Progress, Kitty: name, Title: title, Body: summaryLine(line), Chat: chatID})
}

func (s *Service) appendStep(id string, index int, step Step) {
	s.edit(id, index, func(t *Turn) { t.Steps = append(t.Steps, step) })
}

func (s *Service) markShot(id string, index int, name string) {
	s.edit(id, index, func(t *Turn) {
		if len(t.Steps) > 0 && t.Steps[len(t.Steps)-1].Action != StepPlan {
			// A copy: whoever holds the turn's steps (a reader of the session, a subscriber)
			// never sees an element change under it.
			steps := append([]Step(nil), t.Steps...)
			steps[len(steps)-1].Shot = name
			t.Steps = steps
		} else {
			t.Steps = append(t.Steps, Step{At: s.now(), Action: "screenshot", Shot: name})
		}
	})
}

// addLooks adds to how a turn saw its work.
func (s *Service) addLooks(id string, index int, l Looks) {
	s.edit(id, index, func(t *Turn) {
		sum := Looks{}
		if t.Looks != nil {
			sum = *t.Looks
		}
		sum.Shots += l.Shots
		sum.ShotTokens += l.ShotTokens
		sum.Reads += l.Reads
		sum.ReadTokens += l.ReadTokens
		t.Looks = &sum
	})
}

func (s *Service) addUsage(id string, index int, u provider.Usage) {
	s.edit(id, index, func(t *Turn) {
		t.Usage = t.Usage.Add(u)
	})
}

// closeTask ends a turn's background work: the text, whether the person is needed, and the
// goal's result when the thread works a goal.
func (s *Service) closeTask(id string, index int, k *kitty.Kitty, text string, needsHelp bool, result string) {
	s.edit(id, index, func(t *Turn) {
		t.Working = false
		t.NeedsHelp = needsHelp
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
	if needsHelp {
		s.tell(notify.Help, k, "needs your help", summaryLine(text), id)
	}
	if result == "done" && s.askedLead(id, index) {
		result = "asked"
	}
	if goal := s.goalOf(id); goal != "" {
		s.reportGoal(k, goal, id, result, text)
	}
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
	return s.transcriptFor("", nil, nil, system, turns, k)
}

// transcriptFor is transcript with the files of the thread: id is the chat they are stored under,
// brain says what the model can be shown, and paths where each file is on the kitty's machine.
func (s *Service) transcriptFor(id string, brain provider.Client, paths map[string]string, system string, turns []Turn, k *kitty.Kitty) []provider.Message {
	if s.MaxTurns > 0 && len(turns) > s.MaxTurns {
		turns = turns[len(turns)-s.MaxTurns:]
	}
	// The files shown to the model as they are (pictures, documents) are those of the messages
	// this answer is for, the ones after the kitty's last turn. Earlier ones stay as a line, so a
	// long thread never carries the same picture again and again.
	fresh := 0
	for i, t := range turns {
		if t.Kitty == k.ID {
			fresh = i + 1
		}
	}
	messages := []provider.Message{{Role: "system", Content: system}}
	exposed := s.guardedID(id)
	var pending []string
	var images []provider.Image
	var docs []provider.File
	flush := func() {
		if len(pending) > 0 {
			messages = append(messages, provider.Message{Role: "user", Content: strings.Join(pending, "\n\n"), Images: images, Files: docs})
			pending, images, docs = nil, nil, nil
		}
	}
	for i, t := range turns {
		if t.Kitty == k.ID {
			flush()
			content := t.Text
			if t.Resumable {
				content += "\n\n" + recap(t)
			}
			messages = append(messages, provider.Message{Role: "assistant", Content: content})
			continue
		}
		label := t.From
		switch {
		case t.From == "user" && t.Sender != "":
			label = t.Sender
		case t.From == "user":
			label = s.user
			if label == "" {
				label = "User"
			}
		}
		entry := "[" + label + "]: " + t.Text
		if exposed && t.Kitty == "" {
			entry = guard.Wrap(label, t.Text)
		}
		if len(t.Attachments) > 0 {
			block, imgs, fs := s.describeFiles(id, brain, paths, k, t.Attachments, i >= fresh)
			entry += block
			images, docs = append(images, imgs...), append(docs, fs...)
		}
		pending = append(pending, entry)
	}
	flush()
	if last := messages[len(messages)-1]; last.Role != "user" {
		messages = append(messages, provider.Message{Role: "user", Content: "(continue)"})
	}
	return fitContext(messages, provider.ContextWindow(brain))
}

// fitContext keeps a conversation inside the model's context window by letting its oldest
// messages go (after the first, the system prompt), instead of sending what the provider would
// refuse. A fifth of the window is left for the answer. Nothing is dropped while it fits, and the
// last message is always kept.
// What documents weigh in fitContext: a document's bytes count a fiftieth of a token each. A
// picture weighs by its pixels (imageTokens).
const fileBytesPerChar = 50 / attach.CharsPerToken

func fitContext(messages []provider.Message, window int) []provider.Message {
	budget := window / 5 * 4 * attach.CharsPerToken
	size := func(m provider.Message) int {
		n := len(m.Content)
		for _, img := range m.Images {
			n += imageTokens(img) * attach.CharsPerToken
		}
		for _, f := range m.Files {
			n += len(f.Data) / fileBytesPerChar
		}
		for _, c := range m.ToolCalls {
			n += len(c.Name) + 16*len(c.Args)
		}
		return n
	}
	total := 0
	for _, m := range messages {
		total += size(m)
	}
	start := 1
	for total > budget && start < len(messages)-1 {
		total -= size(messages[start])
		start++
	}
	for start < len(messages)-1 && messages[start].Role == "tool" {
		start++
	}
	if start <= 1 {
		return messages
	}
	return append(messages[:1:1], messages[start:]...)
}

// trimHistory keeps the first message and the last keep ones, and never starts the kept tail in
// the middle of a tool exchange: a tool result whose call was cut away is rejected by the
// providers, so the tail moves forward to the next message that is not a tool result.
func trimHistory(messages []provider.Message, keep int) []provider.Message {
	if keep <= 0 || len(messages) <= keep {
		return messages
	}
	start := len(messages) - keep
	for start < len(messages) && messages[start].Role == "tool" {
		start++
	}
	return append(messages[:1:1], messages[start:]...)
}
