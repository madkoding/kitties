package demo

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/container"
	"github.com/madkoding/kitties/internal/provider"
)

func ask(t *testing.T, b *Brain, system, user string) string {
	t.Helper()
	r, err := b.Complete(context.Background(), []provider.Message{{Role: "system", Content: system}, {Role: "user", Content: user}})
	if err != nil {
		t.Fatal(err)
	}
	return r.Content
}

func TestBrain(t *testing.T) {
	b := NewBrain()
	desk := "You are Mimi, a kitty.\n@tool done {}"
	if b.Describe() == "" {
		t.Error("Describe")
	}
	if got := ask(t, b, desk, "[Pat]: hello"); !strings.Contains(got, "I am Mimi") {
		t.Error(got)
	}
	if got := ask(t, b, desk, "[Pat]: open https://example.org please"); !strings.Contains(got, `@tool open {"url": "https://example.org"}`) {
		t.Error(got)
	}
	if got := ask(t, b, "You are Tom", "[Pat]: open the site"); strings.Contains(got, "@tool") {
		t.Error("no desktop, no tools: " + got)
	}
	// With a connector listed, a request to post goes through it; the answer reads the result.
	withSlack := "You are Mimi, a kitty.\nYou can use these external services by writing a line of the form @connector <name> <json arguments> on its own; the result comes back to you as a message:\n- team-slack: posts to #launch\n- wiki: the team wiki\n"
	if got := ask(t, b, withSlack, "[Pat]: please post the announcement"); !strings.Contains(got, `@connector team-slack {"text":`) {
		t.Error(got)
	}
	if got := ask(t, b, desk, "[Pat]: please post the announcement"); strings.Contains(got, "@connector") {
		t.Error("no connector listed: " + got)
	}
	if got := ask(t, b, withSlack, "Results of your actions:\n- @connector team-slack {\"text\": \"x\"}: ok\n  ok\n\nContinue, and answer the person."); !strings.Contains(got, "Posted!") {
		t.Error(got)
	}
	if got := ask(t, b, withSlack, "Results of your actions:\n- @connector team-slack {\"text\": \"x\"}: failed: boom\n\nContinue, and answer the person."); !strings.Contains(got, "refused") {
		t.Error(got)
	}
	if got := ask(t, b, desk, "Results of your actions:\n- @skill read report-findings: ok\n  # Report\n- @artifact save {\"name\": \"launch-plan.md\"}: ok\n\nContinue, and answer the person."); !strings.Contains(got, "launch-plan.md") || strings.Contains(got, "@tool") {
		t.Error("library results end the turn: " + got)
	}
	peers := desk + "\nOther kitties you can hand a subtask to: luna, tom. Write @delegate <name> on its own line.\n"
	if got := ask(t, b, peers, "[Pat]: ask luna to summarise the plan"); !strings.Contains(got, `@delegate luna {"task": "summarise the plan"}`) {
		t.Error(got)
	}
	if got := ask(t, b, peers, "[Pat]: ask tom"); !strings.Contains(got, `@delegate tom {"task": "help with this"}`) {
		t.Error(got)
	}
	if got := ask(t, b, desk, "[Pat]: ask luna to summarise"); strings.Contains(got, "@delegate") {
		t.Error("no peers, no delegation: " + got)
	}
	if got := ask(t, b, peers, "[Pat]: ask zed to do it"); strings.Contains(got, "@delegate") {
		t.Error("not a peer: " + got)
	}
	if got := ask(t, b, peers, "Results of your actions:\n- @delegate luna {\"task\": \"x\"}: ok\n  luna answered:\n  done\n\nContinue, and answer the person."); !strings.Contains(got, "luna took care of it") {
		t.Error(got)
	}
	// Memory: the kitty keeps what it is told, thanks for it, and consolidates the feedback it got.
	// Asked to hold back, it limits itself, and says so once it is set.
	if got := ask(t, b, desk, "[Pat]: please ask me before posting"); !strings.Contains(got, `@limit set {"connector": "team-slack", "action": "send", "mode": "ask"}`) {
		t.Errorf("limit = %q", got)
	}
	if got := ask(t, b, desk, "Results of your actions:\n- @limit set {}: ok\n  from now on, send on team-slack waits for the person's yes\n\nContinue, and answer the person."); !strings.Contains(got, "ask you before posting") {
		t.Errorf("limit set = %q", got)
	}
	if got := ask(t, b, desk, "[Pat]: please remember that I like short answers"); !strings.Contains(got, `@memory remember {"text": "I like short answers"}`) {
		t.Error(got)
	}
	if got := ask(t, b, desk, "Results of your actions:\n- @memory remember {}: ok\n  noted (user, id a1)\n\nContinue, and answer the person."); !strings.Contains(got, "remember that") {
		t.Error(got)
	}
	if got := ask(t, b, desk, "Results of your actions:\n- @connector team-slack {}: ok\n  held: the person has to approve this first (request x).\n\nContinue"); !strings.Contains(got, "approval") {
		t.Error(got)
	}
	prompt := "Current notes:\n[user] Answer in Spanish\n[work] Dates twice\n[user] Answer in Spanish\n\nThe person's latest feedback, oldest first:\n- down | asked: x | the kitty did: y | the person said: too long\n- up | asked: x\n- down | asked: x | the person said:  \n"
	if got := ask(t, b, "You keep the memory of a kitty, an AI assistant", prompt); got != `[{"scope": "user", "text": "Answer in Spanish"}, {"scope": "work", "text": "Dates twice"}, {"scope": "user", "text": "Correction: too long"}]` {
		t.Errorf("distilled = %s", got)
	}
	if got := ask(t, b, "You keep the memory of a kitty", "Current notes:\n(none)\n"); got != "[]" {
		t.Errorf("nothing to keep = %s", got)
	}
	if got := ask(t, b, "You keep the running summary of a long conversation", "Summary so far"); !strings.Contains(got, "planning the launch") {
		t.Error(got)
	}
	if got := ask(t, b, desk, "[scheduler]: Goal: Inbox\nLook at it.\n\nWhy now: the schedule"); !strings.Contains(got, "Checked **Inbox**") {
		t.Error(got)
	}
	if got := ask(t, b, desk, "[scheduler]: Goal: Triage\nLook.\n\nWhy now: an event from github (issues):\nissue #1"); !strings.Contains(got, "Something arrived for **Triage**") {
		t.Error(got)
	}
	if firstConnector("x @connector <name> y\n\nno list") != "" || firstConnector("nothing") != "" {
		t.Error("firstConnector without a list")
	}
	if firstConnector("@connector <name> y\n- coder: runs code\n- team-slack: posts to #launch\n") != "team-slack" || firstConnector("@connector <name> y\n- coder: runs code\n- wiki: notes\n") != "coder" {
		t.Error("firstConnector prefers a messaging service, else the first")
	}
	if got := ask(t, b, desk, "[Pat]: plan the launch"); !strings.Contains(got, "1. Research") || !strings.Contains(got, "@artifact save") {
		t.Error(got)
	}
	if got := ask(t, b, desk, "[artifact launch-plan.md] saved"); !strings.Contains(got, "launch-plan.md") {
		t.Error(got)
	}
	if got := ask(t, b, desk, "[Pat]: @mimi what do you think?"); !strings.Contains(got, "Mimi here") {
		t.Error(got)
	}
	if got := ask(t, b, desk, "[Pat]: "+strings.Repeat("x", 80)); !strings.Contains(got, "…") {
		t.Error(got)
	}
	seen := []string{}
	for i := 0; i < 3; i++ {
		seen = append(seen, ask(t, b, desk, "Results of your actions:\n- ok"))
	}
	if !strings.Contains(seen[0], "@tool click") || !strings.Contains(seen[1], "@tool scroll") || !strings.Contains(seen[2], "@tool done") {
		t.Errorf("rounds = %v", seen)
	}
}

func TestDesktop(t *testing.T) {
	d := NewDesktop()
	ctx := context.Background()
	if id, err := d.Up(ctx, container.Spec{Name: "kitty-a"}); err != nil || id != "demo-kitty-a" {
		t.Error("Up")
	}
	if err := d.Stop(ctx, "kitty-a"); err != nil {
		t.Error(err)
	}
	if list, _ := d.List(ctx); len(list) != 1 || list[0].Running || list[0].Status != "Exited" {
		t.Errorf("List = %+v", list)
	}
	_ = d.Start(ctx, "kitty-a")
	if list, _ := d.List(ctx); !list[0].Running {
		t.Error("Start")
	}
	empty, _ := d.Screenshot(ctx, "kitty-a")
	if _, err := d.Exec(ctx, "kitty-a"); err != nil {
		t.Error(err)
	}
	if out, _ := d.Exec(ctx, "kitty-a", "kitty-open", "https://example.com"); out != "opened https://example.com" {
		t.Error(out)
	}
	opened, _ := d.Screenshot(ctx, "kitty-a")
	_, _ = d.Exec(ctx, "kitty-a", "xdotool", "mousemove", "1", "1", "click", "1")
	clicked, _ := d.Screenshot(ctx, "kitty-a")
	if out, _ := d.Exec(ctx, "kitty-a", "ls"); !strings.HasPrefix(out, "(demo) ls") {
		t.Error(out)
	}
	for _, png := range [][]byte{empty, opened, clicked} {
		if !bytes.HasPrefix(png, []byte("\x89PNG")) {
			t.Error("not a PNG")
		}
	}
	if bytes.Equal(empty, opened) || bytes.Equal(opened, clicked) {
		t.Error("the screenshots change with the state")
	}
	_ = d.Remove(ctx, "kitty-a")
	if list, _ := d.List(ctx); len(list) != 0 {
		t.Error("Remove")
	}
}
