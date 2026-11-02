package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/kitty"
)

// machineFleet is a desktop fleet whose kitties have a machine that can be off.
type machineFleet struct {
	*deskFleet
	state  string
	reason string
	onErr  error
	ons    int
	offs   []kitty.OffOptions
}

func (m *machineFleet) Power(string) (kitty.Power, error) {
	return kitty.Power{State: m.state, OffReason: m.reason, AutoOffMinutes: 15}, nil
}

func (m *machineFleet) PowerOnWait(context.Context, string, time.Duration) error {
	m.ons++
	if m.onErr != nil {
		return m.onErr
	}
	m.state, m.reason = kitty.PowerOn, ""
	return nil
}

func (m *machineFleet) PowerOff(_ context.Context, _ string, o kitty.OffOptions) error {
	m.offs = append(m.offs, o)
	m.state, m.reason = kitty.PowerOff, o.Reason
	return nil
}

func machineService(t *testing.T, state string, replies ...string) (*Service, *machineFleet, *scripted) {
	t.Helper()
	f, sa, _, _ := fleet(t)
	m := &machineFleet{deskFleet: &deskFleet{fakeFleet: f}, state: state}
	s := service(t, m)
	sa.replies = replies
	return s, m, sa
}

// The kitty is told the state of its machine in a volatile message at the end of the request, and
// nowhere in the stable prefix (the system prompt and the conversation), so the prompt cache
// holds whatever the state is.
func TestKittyIsToldItsMachineStateOutsideTheStablePrefix(t *testing.T) {
	var prefixes []string
	for _, state := range []string{kitty.PowerOff, kitty.PowerOn} {
		s, m, sa := machineService(t, state, "hello")
		m.reason = kitty.OffIdle
		sess, _ := s.Create("", []string{"ana"})
		if _, err := s.Send(context.Background(), sess.ID, "hi"); err != nil {
			t.Fatal(err)
		}
		msgs := sa.seen[0]
		last := msgs[len(msgs)-1]
		if !last.Volatile || last.Role != "user" || !strings.Contains(last.Content, "Your machine") {
			t.Fatalf("%s: last message = %+v", state, last)
		}
		if state == kitty.PowerOff && !strings.Contains(last.Content, "turned itself off") {
			t.Errorf("the note does not say why it is off: %q", last.Content)
		}
		var stable []string
		for _, msg := range msgs[:len(msgs)-1] {
			if msg.Volatile || strings.Contains(msg.Content, "Your machine, now") {
				t.Errorf("the state leaked into the stable part: %+v", msg)
			}
			stable = append(stable, msg.Role+msg.Content)
		}
		prefixes = append(prefixes, strings.Join(stable, "\x00"))
	}
	if prefixes[0] != prefixes[1] {
		t.Error("the stable prefix changes with the state of the machine")
	}
}

// A kitty without a machine (or a fleet that has none) is told nothing.
func TestNoMachineNoteWithoutDesktop(t *testing.T) {
	f, _, sb, _ := fleet(t)
	m := &machineFleet{deskFleet: &deskFleet{fakeFleet: f}, state: kitty.PowerOff}
	s := service(t, m)
	sess, _ := s.Create("", []string{"bo"})
	if _, err := s.Send(context.Background(), sess.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	for _, msg := range sb.seen[0] {
		if msg.Volatile {
			t.Errorf("a kitty without a desktop got a machine note: %+v", msg)
		}
	}
}

// A desktop tool used while the machine is off turns it on first, and the turn shows the step.
func TestDesktopToolTurnsTheMachineOn(t *testing.T) {
	s, m, _ := machineService(t, kitty.PowerOff,
		"@tool click {\"x\": 1, \"y\": 2}",
		"@tool done {\"summary\": \"clicked\"}")
	sess, _ := s.Create("", []string{"ana"})
	if _, err := s.Send(context.Background(), sess.ID, "click"); err != nil {
		t.Fatal(err)
	}
	got := waitDone(t, s, sess.ID)
	if m.ons != 1 {
		t.Errorf("the machine was turned on %d times", m.ons)
	}
	var names []string
	for _, st := range got.Turns[1].Steps {
		names = append(names, st.Action)
	}
	if strings.Join(names, ",") != "machine,click,done" {
		t.Errorf("steps = %v", names)
	}
	if !strings.Contains(strings.Join(m.execs, "\n"), "xdotool") {
		t.Errorf("the click did not run: %v", m.execs)
	}
}

// A machine that cannot be turned on keeps the tool from running, and the kitty is told plainly.
func TestDesktopToolSaysWhenTheMachineStaysOff(t *testing.T) {
	s, m, sa := machineService(t, kitty.PowerOff,
		"@tool click {\"x\": 1, \"y\": 2}",
		"@tool done {\"summary\": \"could not\"}")
	m.onErr = errors.New("the container engine is not running")
	sess, _ := s.Create("", []string{"ana"})
	if _, err := s.Send(context.Background(), sess.ID, "click"); err != nil {
		t.Fatal(err)
	}
	waitDone(t, s, sess.ID)
	if strings.Contains(strings.Join(m.execs, "\n"), "xdotool") {
		t.Errorf("a tool ran on a machine that is off: %v", m.execs)
	}
	var feedback string
	for _, msgs := range sa.seen[1:] {
		feedback += msgs[len(msgs)-1].Content
	}
	if !strings.Contains(feedback, "could not be turned on") || !strings.Contains(feedback, "container engine") {
		t.Errorf("the kitty was not told why: %q", feedback)
	}
}

// The machine tool tells the state, turns it on and turns it off (the kitty's own task does not
// keep it from turning off its machine).
func TestMachineTool(t *testing.T) {
	s, m, sa := machineService(t, kitty.PowerOff,
		"@tool machine {\"action\": \"status\"}\n@tool machine {\"action\": \"on\"}",
		"@tool machine {\"action\": \"off\"}",
		"The machine is off now.")
	sess, _ := s.Create("", []string{"ana"})
	replies, err := s.Send(context.Background(), sess.ID, "check your machine")
	if err != nil {
		t.Fatal(err)
	}
	if replies[0].Working {
		t.Error("the machine tool started a desktop task")
	}
	if m.ons != 1 || len(m.offs) != 1 || !m.offs[0].Self || m.offs[0].Reason != kitty.OffKitty || m.offs[0].Force {
		t.Errorf("ons = %d, offs = %+v", m.ons, m.offs)
	}
	first := sa.seen[1][len(sa.seen[1])-2].Content
	if !strings.Contains(first, "your machine is off") || !strings.Contains(first, "your machine is on") {
		t.Errorf("the results = %q", first)
	}
	if got := replies[0].Text; !strings.Contains(got, "off now") {
		t.Errorf("text = %q", got)
	}
}
