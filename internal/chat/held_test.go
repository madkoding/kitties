package chat

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/vault"
)

// heldFleet is a desktop whose display the person can hold.
type heldFleet struct {
	*deskFleet
	mu   sync.Mutex
	held bool
}

func (h *heldFleet) Held(string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.held
}

func (h *heldFleet) give() {
	h.mu.Lock()
	h.held = false
	h.mu.Unlock()
}

// While the person uses the display the kitty's actions wait; when it is given back they run.
func TestDesktopWaitsWhileHeld(t *testing.T) {
	old := heldEvery
	heldEvery = time.Millisecond
	t.Cleanup(func() { heldEvery = old })
	f, sa, _, _ := fleet(t)
	h := &heldFleet{deskFleet: &deskFleet{fakeFleet: f}, held: true}
	s := service(t, h)
	sa.replies = []string{"@tool click {\"x\": 1, \"y\": 2}", "@tool done {\"summary\": \"clicked\"}"}
	sess, _ := s.Create("", []string{"ana"})
	if _, err := s.Send(context.Background(), sess.ID, "click it"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, _ := s.Get(sess.ID)
		if len(got.Turns) > 1 && len(got.Turns[1].Steps) > 0 {
			if got.Turns[1].Steps[0].Action != "held" {
				t.Fatalf("first step = %+v", got.Turns[1].Steps[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no waiting step")
		}
		time.Sleep(time.Millisecond)
	}
	if len(h.execs) != 0 {
		t.Errorf("acted while held: %v", h.execs)
	}
	h.give()
	got := waitDone(t, s, sess.ID)
	if len(h.execs) != 1 || !strings.Contains(h.execs[0], "mousemove 1 2 click") {
		t.Errorf("execs = %v", h.execs)
	}
	if steps := got.Turns[1].Steps; len(steps) != 3 || steps[1].Action != "click" {
		t.Errorf("steps = %+v", steps)
	}
}

// A kitty asks for help: the step carries the reason, it waits for the person to take the
// desktop and give it back, then goes on; when nobody comes it hears so and stops.
func TestAskHelp(t *testing.T) {
	oldEvery, oldWait := heldEvery, helpWait
	heldEvery = time.Millisecond
	t.Cleanup(func() { heldEvery, helpWait = oldEvery, oldWait })
	for _, comes := range []bool{true, false} {
		helpWait = time.Hour
		if !comes {
			helpWait = 20 * time.Millisecond
		}
		f, sa, _, _ := fleet(t)
		h := &heldFleet{deskFleet: &deskFleet{fakeFleet: f}}
		s := service(t, h)
		sa.replies = []string{"I need you for a captcha.\n@tool ask_help {\"reason\": \"a captcha\"}", "@tool done {\"summary\": \"in\"}"}
		sess, _ := s.Create("", []string{"ana"})
		if _, err := s.Send(context.Background(), sess.ID, "sign me in"); err != nil {
			t.Fatal(err)
		}
		if comes {
			// The step shows, the person takes the desktop, then gives it back.
			deadline := time.Now().Add(2 * time.Second)
			for {
				got, _ := s.Get(sess.ID)
				if len(got.Turns) > 1 && len(got.Turns[1].Steps) > 0 {
					st := got.Turns[1].Steps[0]
					if st.Action != "ask_help" || st.Args["reason"] != "a captcha" {
						t.Fatalf("step = %+v", st)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("no help step")
				}
				time.Sleep(time.Millisecond)
			}
			h.mu.Lock()
			h.held = true
			h.mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			h.give()
		}
		waitDone(t, s, sess.ID)
		seen := sa.seen[len(sa.seen)-1]
		last := seen[len(seen)-1].Content
		want := "gave it back"
		if !comes {
			want = "nobody came"
		}
		if !strings.Contains(last, want) {
			t.Errorf("comes=%v: the kitty heard %q", comes, last)
		}
	}
}

// quickFleet is a person who took the desktop and gave it back between two looks of the kitty.
type quickFleet struct {
	*deskFleet
	mu   sync.Mutex
	took time.Time
}

func (q *quickFleet) Held(string) bool { return false }
func (q *quickFleet) HoldStarted(string) time.Time {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.took
}

func TestAskHelpQuickGiveBack(t *testing.T) {
	oldEvery, oldWait := heldEvery, helpWait
	heldEvery, helpWait = 5*time.Millisecond, time.Hour
	t.Cleanup(func() { heldEvery, helpWait = oldEvery, oldWait })
	f, sa, _, _ := fleet(t)
	q := &quickFleet{deskFleet: &deskFleet{fakeFleet: f}}
	s := service(t, q)
	sa.replies = []string{"@tool ask_help {\"reason\": \"a code\"}", "@tool done {\"summary\": \"in\"}"}
	sess, _ := s.Create("", []string{"ana"})
	if _, err := s.Send(context.Background(), sess.ID, "sign me in"); err != nil {
		t.Fatal(err)
	}
	// The person took the desktop after the kitty asked: a moment ahead, so a slow machine that has
	// not yet got the kitty as far as asking cannot put it before the request.
	q.mu.Lock()
	q.took = time.Now().Add(time.Minute)
	q.mu.Unlock()
	waitDone(t, s, sess.ID)
	seen := sa.seen[len(sa.seen)-1]
	if last := seen[len(seen)-1].Content; !strings.Contains(last, "gave it back") {
		t.Errorf("the kitty heard %q", last)
	}
}

// The person can answer a kitty that asked: told to it, or typed on its desktop unseen.
func TestAnswerHelp(t *testing.T) {
	oldEvery, oldWait := heldEvery, helpWait
	heldEvery, helpWait = time.Millisecond, time.Hour
	t.Cleanup(func() { heldEvery, helpWait = oldEvery, oldWait })
	for _, c := range []struct {
		name   string
		answer HelpAnswer
		told   string // what the kitty hears
		execs  []string
	}{
		{"told", HelpAnswer{Text: "the code is 4821"}, "the person answered: the code is 4821", nil},
		{"secret", HelpAnswer{Text: "hunter2", Secret: true, Enter: true}, "you are not shown it", []string{"xdotool type --delay 20 --file -", "xdotool key -- Return"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, sa, _, _ := fleet(t)
			h := &heldFleet{deskFleet: &deskFleet{fakeFleet: f}}
			s := service(t, h)
			sa.replies = []string{"@tool ask_help {\"reason\": \"the code from your phone\"}", "@tool done {\"summary\": \"in\"}"}
			sess, _ := s.Create("", []string{"ana"})
			if _, err := s.Send(context.Background(), sess.ID, "sign me in"); err != nil {
				t.Fatal(err)
			}
			// The request carries what the kitty sees, and waits for the person.
			var shot string
			deadline := time.Now().Add(2 * time.Second)
			for shot == "" {
				got, _ := s.Get(sess.ID)
				if len(got.Turns) > 1 {
					for _, st := range got.Turns[1].Steps {
						if st.Action == "ask_help" {
							shot = st.Shot
						}
					}
				}
				if time.Now().After(deadline) {
					t.Fatal("the request for help took no screenshot")
				}
				time.Sleep(time.Millisecond)
			}
			if err := s.AnswerHelp(sess.ID, 1, c.answer); err != nil {
				t.Fatal(err)
			}
			got := waitDone(t, s, sess.ID)
			seen := sa.seen[len(sa.seen)-1]
			if last := seen[len(seen)-1].Content; !strings.Contains(last, c.told) {
				t.Errorf("the kitty heard %q", last)
			}
			if c.answer.Secret && strings.Contains(seen[len(seen)-1].Content, c.answer.Text) {
				t.Error("the secret reached the kitty")
			}
			if c.answer.Secret && (len(h.inputs) != 1 || h.inputs[0] != "a1:hunter2" || strings.Contains(strings.Join(h.execs, " "), "hunter2")) {
				t.Errorf("the secret was not typed from the input alone: inputs %v execs %v", h.inputs, h.execs)
			}
			for _, want := range c.execs {
				if !strings.Contains(strings.Join(h.execs, "\n"), want) {
					t.Errorf("execs = %v, want %q", h.execs, want)
				}
			}
			var helped bool
			for _, st := range got.Turns[1].Steps {
				if st.Action == "helped" {
					helped = true
					if c.answer.Secret && strings.Contains(st.Result, c.answer.Text) {
						t.Error("the step shows the secret")
					}
				}
			}
			if !helped {
				t.Errorf("no helped step: %+v", got.Turns[1].Steps)
			}
			// Nobody waits any more.
			if err := s.AnswerHelp(sess.ID, 1, c.answer); !errors.Is(err, ErrNoHelp) {
				t.Errorf("a second answer = %v", err)
			}
		})
	}
}

func TestAnswerHelpChecks(t *testing.T) {
	s, _, _ := deskService(t, "ok")
	for _, c := range []struct {
		a    HelpAnswer
		want string
	}{{HelpAnswer{Text: "  "}, "is empty"}, {HelpAnswer{Text: strings.Repeat("x", 2001)}, "too long"}} {
		if err := s.AnswerHelp("c", 0, c.a); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("AnswerHelp(%d chars) = %v", len(c.a.Text), err)
		}
	}
	if err := s.AnswerHelp("nobody", 0, HelpAnswer{Text: "x"}); !errors.Is(err, ErrNoHelp) {
		t.Errorf("no one is waiting: %v", err)
	}
	// A second answer to the same request, before the kitty reads the first.
	wait, stop := s.expectHelp("c", 0)
	defer stop()
	if err := s.AnswerHelp("c", 0, HelpAnswer{Text: "one"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AnswerHelp("c", 0, HelpAnswer{Text: "two"}); err == nil {
		t.Error("two answers fit")
	}
	if a := <-wait; a.Text != "one" {
		t.Errorf("answer = %+v", a)
	}
}

// Without anything to type on, a secret cannot be delivered, and a failing keyboard is told.
func TestAnswerHelpTypingFails(t *testing.T) {
	s, d, _ := deskService(t, "ok")
	ana, _ := s.fleet.Get("ana")
	d.execErr = errors.New("no display")
	if got := s.applyHelp(context.Background(), "c", 0, ana, HelpAnswer{Text: "x", Secret: true}); !strings.Contains(got, "failed: typing failed") {
		t.Errorf("got %q", got)
	}
	plain := service(t, &fakeFleet{})
	if got := plain.applyHelp(context.Background(), "c", 0, ana, HelpAnswer{Text: "x", Secret: true}); !strings.Contains(got, "cannot be typed on") {
		t.Errorf("got %q", got)
	}
}

// A kitty types a secret the person saved for it by name: it goes from the vault to the
// keyboard, and nothing the kitty, its model or the chat holds ever has it.
func TestTypeSecretFromTheVault(t *testing.T) {
	v, err := vault.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = v.Put("Shop password", "hunter2", []string{"a1"})
	_ = v.Put("Bank code", "999", []string{"someone-else"})
	f, sa, _, _ := fleet(t)
	h := &heldFleet{deskFleet: &deskFleet{fakeFleet: f}}
	s := service(t, h)
	s.SetVault(v)
	sa.replies = []string{
		"@tool type_secret {\"name\": \"bank code\"}\n@tool type_secret {\"name\": \"shop PASSWORD\", \"enter\": true}",
		"@tool done {\"summary\": \"in\"}"}
	sess, _ := s.Create("", []string{"ana"})
	if _, err := s.Send(context.Background(), sess.ID, "sign me in"); err != nil {
		t.Fatal(err)
	}
	got := waitDone(t, s, sess.ID)
	if len(h.inputs) != 1 || h.inputs[0] != "a1:hunter2" || !strings.Contains(strings.Join(h.execs, "\n"), "xdotool key -- Return") {
		t.Errorf("inputs %v execs %v", h.inputs, h.execs)
	}
	// What the model was told: its system prompt lists the names it may use (and only those), the
	// one it may not use is refused with them, and no message holds a secret.
	first := sa.seen[0]
	if sys := first[0].Content; !strings.Contains(sys, "Shop password") || strings.Contains(sys, "Bank code") {
		t.Errorf("the prompt does not list the right secrets: %q", sys)
	}
	var all strings.Builder
	for _, msgs := range sa.seen {
		for _, m := range msgs {
			all.WriteString(m.Content)
		}
	}
	if strings.Contains(all.String(), "hunter2") || strings.Contains(all.String(), "999") {
		t.Error("a secret reached the model")
	}
	last := sa.seen[1][len(sa.seen[1])-1].Content
	if !strings.Contains(last, "no saved secret called") || !strings.Contains(last, "Shop password") || !strings.Contains(last, "typed the secret shop PASSWORD") {
		t.Errorf("the kitty heard %q", last)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "hunter2") {
		t.Error("a secret is in the chat")
	}
	var step *Step
	for i, st := range got.Turns[1].Steps {
		if st.Action == "type_secret" && st.Error == "" {
			step = &got.Turns[1].Steps[i]
		}
	}
	if step == nil || step.Args["name"] != "shop PASSWORD" || step.Result != "typed" {
		t.Errorf("steps = %+v", got.Turns[1].Steps)
	}
}

func TestTypeSecretWithoutAVaultOrWithoutKeys(t *testing.T) {
	s, d, _ := deskService(t, "ok")
	ana, _ := s.fleet.Get("ana")
	a := action{kind: "tool", name: "type_secret", args: map[string]any{"name": "x"}}
	if o := s.typeSecret(context.Background(), "c", 0, ana, a); !strings.Contains(o.err, "no saved secrets") {
		t.Errorf("no vault: %+v", o)
	}
	v, _ := vault.Open(t.TempDir())
	s.SetVault(v)
	if o := s.typeSecret(context.Background(), "c", 0, ana, a); !strings.Contains(o.err, "none is saved for you") {
		t.Errorf("empty vault: %+v", o)
	}
	_ = v.Put("x", "1", []string{ana.ID})
	d.execErr = errors.New("no display")
	if o := s.typeSecret(context.Background(), "c", 0, ana, a); !strings.Contains(o.err, "typing failed") {
		t.Errorf("a failing keyboard: %+v", o)
	}
}

// A secret the person types for a kitty can be kept for next time, for that kitty alone.
func TestAnswerHelpSavesTheSecret(t *testing.T) {
	oldEvery, oldWait := heldEvery, helpWait
	heldEvery, helpWait = time.Millisecond, time.Hour
	t.Cleanup(func() { heldEvery, helpWait = oldEvery, oldWait })
	v, _ := vault.Open(t.TempDir())
	f, sa, _, _ := fleet(t)
	h := &heldFleet{deskFleet: &deskFleet{fakeFleet: f}}
	s := service(t, h)
	s.SetVault(v)
	sa.replies = []string{"@tool ask_help {\"reason\": \"the password\"}", "@tool done {\"summary\": \"in\"}"}
	sess, _ := s.Create("", []string{"ana"})
	if _, err := s.Send(context.Background(), sess.ID, "sign me in"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.RLock()
		_, waiting := s.helps[helpKey(sess.ID, 1)]
		s.mu.RUnlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("nobody waits")
		}
		time.Sleep(time.Millisecond)
	}
	if err := s.AnswerHelp(sess.ID, 1, HelpAnswer{Text: "hunter2", Secret: true, Save: "Shop password"}); err != nil {
		t.Fatal(err)
	}
	got := waitDone(t, s, sess.ID)
	if pw, err := v.Get("shop password", "a1"); err != nil || pw != "hunter2" {
		t.Errorf("saved = %q, %v", pw, err)
	}
	if _, err := v.Get("shop password", "b1"); err == nil {
		t.Error("another kitty may use it")
	}
	for _, st := range got.Turns[1].Steps {
		if st.Action == "helped" && st.Args["saved"] != "Shop password" {
			t.Errorf("the step does not say it was saved: %+v", st)
		}
	}
	// A name that cannot be kept is said, and the typing still happens.
	f2, sa2, _, _ := fleet(t)
	h2 := &heldFleet{deskFleet: &deskFleet{fakeFleet: f2}}
	s2 := service(t, h2)
	s2.SetVault(v)
	sa2.replies = []string{"@tool ask_help {\"reason\": \"the password\"}", "@tool done {\"summary\": \"in\"}"}
	sess2, _ := s2.Create("", []string{"ana"})
	if _, err := s2.Send(context.Background(), sess2.ID, "again"); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(time.Millisecond) {
		s2.mu.RLock()
		_, waiting := s2.helps[helpKey(sess2.ID, 1)]
		s2.mu.RUnlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("nobody waits")
		}
	}
	_ = s2.AnswerHelp(sess2.ID, 1, HelpAnswer{Text: "x", Secret: true, Save: strings.Repeat("n", 99)})
	got2 := waitDone(t, s2, sess2.ID)
	var said bool
	for _, st := range got2.Turns[1].Steps {
		said = said || (st.Action == "helped" && strings.Contains(st.Error, "was not saved"))
	}
	if !said || len(h2.inputs) != 1 {
		t.Errorf("a bad name: said %v, inputs %v", said, h2.inputs)
	}
}
