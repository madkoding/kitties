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
