package chat

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/kitty"
)

// memoryFleet is a machine fleet that tells the memory of its machines.
type memoryFleet struct {
	*machineFleet
	mem kitty.Memory
	err error
}

func (m *memoryFleet) Memory(context.Context, string) (kitty.Memory, error) { return m.mem, m.err }

func memoryService(t *testing.T, mem kitty.Memory, replies ...string) (*Service, *memoryFleet, *scripted) {
	t.Helper()
	f, sa, _, _ := fleet(t)
	m := &memoryFleet{machineFleet: &machineFleet{deskFleet: &deskFleet{fakeFleet: f}, state: kitty.PowerOn}, mem: mem}
	s := service(t, m)
	sa.replies = replies
	return s, m, sa
}

var nearlyFull = kitty.Memory{TotalMB: 2048, UsedMB: 1900, AvailableMB: 148, SwapTotalMB: 1024, SwapUsedMB: 900,
	Top: []kitty.Process{{Name: "chromium", MB: 1500, Count: 12}}}

// The kitty is told the memory of its machine with its state, and what to do when it is full.
func TestKittyIsToldItsMemory(t *testing.T) {
	s, _, sa := memoryService(t, nearlyFull, "hello")
	sess, _ := s.Create("", []string{"ana"})
	if _, err := s.Send(context.Background(), sess.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	last := sa.seen[0][len(sa.seen[0])-1].Content
	if !strings.Contains(last, "148 MB free") || !strings.Contains(last, "nearly full") {
		t.Errorf("note = %q", last)
	}
	s, _, sa = memoryService(t, kitty.Memory{TotalMB: 4096, UsedMB: 1024, AvailableMB: 3072}, "hello")
	sess, _ = s.Create("", []string{"ana"})
	_, _ = s.Send(context.Background(), sess.ID, "hi")
	if last := sa.seen[0][len(sa.seen[0])-1].Content; !strings.Contains(last, "3.0 GB free; no swap") || !strings.Contains(last, "open only what fits") {
		t.Errorf("roomy note = %q", last)
	}
}

// open and launch are refused when what is left does not fit them; the kitty learns what to close.
func TestOpenIsRefusedWhenMemoryIsFull(t *testing.T) {
	s, m, sa := memoryService(t, nearlyFull,
		"@tool open {\"url\": \"https://example.com\"}\n@tool click {\"x\": 1, \"y\": 2}",
		"@tool done {\"summary\": \"no room\"}")
	sess, _ := s.Create("", []string{"ana"})
	if _, err := s.Send(context.Background(), sess.ID, "open it"); err != nil {
		t.Fatal(err)
	}
	waitDone(t, s, sess.ID)
	execs := strings.Join(m.execs, "\n")
	if strings.Contains(execs, "kitty-open") || !strings.Contains(execs, "xdotool") {
		t.Errorf("execs = %v", m.execs)
	}
	var feedback string
	for _, msgs := range sa.seen[1:] {
		feedback += msgs[len(msgs)-1].Content
	}
	if !strings.Contains(feedback, "not enough memory to open another page") || !strings.Contains(feedback, "ctrl+w") {
		t.Errorf("feedback = %q", feedback)
	}
	// A machine that cannot tell its memory lets the tool run.
	m.err = errors.New("cannot tell")
	if err := s.memoryGate(context.Background(), &kitty.Kitty{ID: "a1", Desktop: true}, "launch"); err != nil {
		t.Errorf("unknown memory: %v", err)
	}
	m.err = nil
	if err := s.memoryGate(context.Background(), &kitty.Kitty{ID: "a1", Desktop: true}, "launch"); err == nil || !strings.Contains(err.Error(), "start another program") {
		t.Errorf("launch = %v", err)
	}
	if err := s.memoryGate(context.Background(), &kitty.Kitty{ID: "a1", Desktop: true}, "click"); err != nil {
		t.Errorf("a click needs no memory: %v", err)
	}
}

// The machine tool's status tells the memory and what holds the most.
func TestMachineStatusTellsMemory(t *testing.T) {
	s, _, sa := memoryService(t, nearlyFull, "@tool machine {\"action\": \"status\"}", "ok")
	sess, _ := s.Create("", []string{"ana"})
	if _, err := s.Send(context.Background(), sess.ID, "how is your machine"); err != nil {
		t.Fatal(err)
	}
	got := sa.seen[1][len(sa.seen[1])-2].Content
	if !strings.Contains(got, "chromium 1.5 GB (12 processes)") || !strings.Contains(got, "swap 900 MB of 1.0 GB") {
		t.Errorf("status = %q", got)
	}
}
