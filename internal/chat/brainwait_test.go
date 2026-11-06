package chat

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/provider"
)

// downFor is a brain that starts a task, then is down (with err) for n asks, then finishes it.
func downFor(n int, err error) provider.Client {
	var mu sync.Mutex
	asks := 0
	return provider.Func(func(context.Context, []provider.Message) (provider.Reply, error) {
		mu.Lock()
		defer mu.Unlock()
		asks++
		switch {
		case asks == 1:
			return provider.Reply{Content: "@tool click {\"x\": 1, \"y\": 1}"}, nil
		case asks <= 1+n:
			return provider.Reply{}, err
		}
		return provider.Reply{Content: "@tool done {\"summary\": \"finished after the outage\"}"}, nil
	})
}

func runWithBrain(t *testing.T, brain provider.Client, setup func(*Service)) Turn {
	t.Helper()
	s, d, _ := deskService(t)
	s.brainEvery, s.BrainWait = time.Millisecond, time.Second
	d.brains["a1"] = brain
	if setup != nil {
		setup(s)
	}
	sess, _ := s.Create("", []string{"ana"})
	if _, err := s.Send(context.Background(), sess.ID, "go"); err != nil {
		t.Fatal(err)
	}
	return waitDone(t, s, sess.ID).Turns[1]
}

func waits(turn Turn) []Step {
	var out []Step
	for _, st := range turn.Steps {
		if st.Action == "wait" {
			out = append(out, st)
		}
	}
	return out
}

func TestUnattendedTaskWaitsOutABrainOutage(t *testing.T) {
	// Three 503s in a row: the kitty waits, ever longer, and finishes without a failed attempt.
	turn := runWithBrain(t, downFor(3, &provider.HTTPError{Code: 503, Body: "overloaded"}), nil)
	w := waits(turn)
	if !strings.Contains(turn.Text, "finished after the outage") || turn.NeedsHelp || turn.Detail != "" || len(w) != 3 {
		t.Fatalf("turn = %+v", turn)
	}
	if !strings.Contains(w[0].Error, "HTTP 503") || !strings.Contains(w[0].Result, "not answering") {
		t.Errorf("wait step = %+v", w[0])
	}
}

func TestBrainOutageLimits(t *testing.T) {
	outage := &provider.HTTPError{Code: 429}
	// Longer than BrainWait: the attempt fails as before.
	turn := runWithBrain(t, downFor(1000, outage), func(s *Service) { s.BrainWait = 3 * time.Millisecond })
	if !strings.Contains(turn.Text, "could not finish") || len(waits(turn)) == 0 {
		t.Errorf("past the wait: %+v", turn)
	}
	// An error that waiting does not fix fails at once.
	turn = runWithBrain(t, downFor(1, errors.New("invalid model")), nil)
	if !strings.Contains(turn.Text, "could not finish: invalid model") || len(waits(turn)) != 0 {
		t.Errorf("not transient: %+v", turn)
	}
	// Attended, the person is there: no waiting.
	turn = runWithBrain(t, downFor(1, outage), func(s *Service) { s.DefaultLimits.Attended = true })
	if !strings.Contains(turn.Text, "could not finish") || len(waits(turn)) != 0 {
		t.Errorf("attended: %+v", turn)
	}
	// BrainWait zero: no waiting.
	turn = runWithBrain(t, downFor(1, outage), func(s *Service) { s.BrainWait = 0 })
	if len(waits(turn)) != 0 {
		t.Errorf("no wait configured: %+v", turn)
	}
}

func TestBrainOutageWaitIsCancelled(t *testing.T) {
	s, d, _ := deskService(t)
	s.brainEvery, s.BrainWait = time.Hour, 10*time.Hour
	d.brains["a1"] = downFor(1000, &provider.HTTPError{Code: 502})
	sess, _ := s.Create("", []string{"ana"})
	if _, err := s.Send(context.Background(), sess.ID, "go"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, _ := s.Get(sess.ID)
		if len(got.Turns) > 1 && len(waits(got.Turns[1])) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the kitty never waited")
		}
		time.Sleep(time.Millisecond)
	}
	s.Cancel(sess.ID)
	turn := waitDone(t, s, sess.ID).Turns[1]
	if !strings.Contains(turn.Text, "cancelled") {
		t.Errorf("cancel: %+v", turn)
	}
	if d := service(t, d.fakeFleet); d.BrainWait != 30*time.Minute || d.brainEvery != 15*time.Second {
		t.Errorf("defaults: %v %v", d.BrainWait, d.brainEvery)
	}
}
