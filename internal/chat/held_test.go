package chat

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
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
	time.Sleep(30 * time.Millisecond)
	q.mu.Lock()
	q.took = time.Now()
	q.mu.Unlock()
	waitDone(t, s, sess.ID)
	seen := sa.seen[len(sa.seen)-1]
	if last := seen[len(seen)-1].Content; !strings.Contains(last, "gave it back") {
		t.Errorf("the kitty heard %q", last)
	}
}
