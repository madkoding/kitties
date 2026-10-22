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
