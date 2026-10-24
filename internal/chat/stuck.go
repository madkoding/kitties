package chat

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/madkoding/kitties/internal/desktop"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/provider"
)

// WHEN A KITTY IS STUCK.
//
// A kitty that acts and sees the very same screen again and again is blocked by something it
// does not see through: a check that a human is there, a code sent to the person, a window that
// ignores it. After stuckAfter screenshots in a row that did not change, it is told so; and when
// the screen reads like something only a person can pass, it asks for the person's hands at once
// instead of trying again until its budget is gone.

// stuckAfter is how many screenshots in a row, each equal to the one before, count as stuck.
const stuckAfter = 3

// stuckNudge is what a stuck kitty is told when its screen does not read like a check.
const stuckNudge = "Your last actions left the display exactly as it was. Something is not working: try a different way. " +
	"If only the person can get past it (a login, a code they receive, a check that a human is there, a permission), " +
	"call ask_help instead of repeating yourself."

// watchScreen compares a screenshot with the last one of the task and says whether the kitty
// has now seen the same screen stuckAfter times in a row; it starts counting again after that.
func (st *taskState) watchScreen(png []byte) bool {
	sum := sha256.Sum256(png)
	if sum == st.lastShot {
		st.same++
	} else {
		st.same = 0
	}
	st.lastShot = sum
	if st.same >= stuckAfter {
		st.same = 0
		return true
	}
	return false
}

// unstick deals with a kitty that sees the same screen again. When the screen reads like
// something for the person, the person is asked, and the screen is looked at again afterwards;
// else the kitty is told to try another way, or to ask. It returns the message that goes with
// the next screenshot.
func (s *Service) unstick(ctx context.Context, runner desktop.Runner, id string, index int, k *kitty.Kitty, png []byte) provider.Message {
	ocr, _ := runner.Exec(ctx, k.ID, "kitty-ocr")
	reason, ok := desktop.NeedsPerson(ocr)
	if !ok {
		return s.screenMessageWith(id, index, png, nil, stuckNudge)
	}
	reason = "the display is stopped by " + reason
	back := s.askHelp(ctx, id, index, k, reason)
	if ctx.Err() != nil {
		return provider.Message{Role: "user", Content: back}
	}
	fresh, err := runner.Screenshot(ctx, k.ID)
	return s.screenMessageWith(id, index, fresh, err, fmt.Sprintf("You were stuck: %s. %s", reason, back))
}
