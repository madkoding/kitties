package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/kitty"
)

// A task stopped on its budget can go on: the turn says so, and the kitty's next answer gets its
// last steps, so it continues rather than starting again.
func TestResumeAfterBudget(t *testing.T) {
	s, d, sa := deskService(t, "@tool click {\"x\": 1, \"y\": 2}", "@tool click {\"x\": 3, \"y\": 4}")
	d.kitties["a1"].Limits = kitty.Limits{MaxRounds: 1}
	sess, _ := s.Create("", []string{"ana"})
	if _, err := s.Send(context.Background(), sess.ID, "fill the form"); err != nil {
		t.Fatal(err)
	}
	got := waitDone(t, s, sess.ID)
	turn := got.Turns[1]
	if !turn.Resumable || !strings.Contains(turn.Detail, "stopped after 1 rounds") {
		t.Fatalf("turn = resumable %v, detail %q", turn.Resumable, turn.Detail)
	}
	sa.replies = []string{"Going on from where I was."}
	if _, err := s.Send(context.Background(), sess.ID, "Continue where you left off."); err != nil {
		t.Fatal(err)
	}
	waitDone(t, s, sess.ID)
	seen := sa.seen[len(sa.seen)-1]
	var mine string
	for _, m := range seen {
		if m.Role == "assistant" && strings.Contains(m.Content, "stopped before it was finished") {
			mine = m.Content
		}
	}
	if !strings.Contains(mine, `click {"x":1,"y":2}`) || !strings.Contains(mine, "look at the screen first") {
		t.Errorf("the kitty was not reminded of its steps: %q", mine)
	}
	// A task that finishes is not resumable.
	if r := recap(Turn{Detail: "x", Steps: make([]Step, recapSteps+5)}); strings.Count(r, "\n- ") != recapSteps {
		t.Errorf("recap keeps the last %d steps: %d", recapSteps, strings.Count(r, "\n- "))
	}
}
