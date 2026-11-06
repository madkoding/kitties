package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/agenda"
	"github.com/madkoding/kitties/internal/kitty"
)

func TestFollowUpBringsTheKittysTodo(t *testing.T) {
	s, d, sa := deskService(t, "on it")
	d.cat = "- [x] group 1\n- [ ] group 2\n"
	ana, _ := d.Get("ana")
	g := agenda.Goal{ID: "g1", Kitty: ana.ID, Title: "Groups", Text: "check every group"}
	id, err := s.RunGoal(context.Background(), g, agenda.Trigger{Kind: "follow_up", Previous: "blocked"})
	if err != nil {
		t.Fatal(err)
	}
	s.Wait()
	got, _ := s.Get(id)
	call := got.Turns[0].Text
	if !strings.Contains(call, "a follow-up") || !strings.Contains(call, "Your TODO.md") || !strings.Contains(call, "- [ ] group 2") {
		t.Errorf("follow-up call = %q", call)
	}
	if !strings.Contains(sa.seen[0][len(sa.seen[0])-1].Content, "group 2") {
		t.Error("the kitty is told its TODO.md")
	}
	// A scheduled run does not read it; nor is anything added when there is no file.
	id2, _ := s.RunGoal(context.Background(), agenda.Goal{ID: "g2", Kitty: ana.ID, Title: "Other", Text: "x"}, agenda.Trigger{Kind: "schedule"})
	s.Wait()
	if got, _ := s.Get(id2); strings.Contains(got.Turns[0].Text, "TODO.md") {
		t.Error("only follow-ups bring it")
	}
	d.cat = ""
	if note := s.todoNote(context.Background(), ana); note != "" {
		t.Errorf("no file: %q", note)
	}
	// A kitty without a desktop, or a fleet without a runner, keeps none.
	if note := s.todoNote(context.Background(), &kitty.Kitty{ID: "b1"}); note != "" {
		t.Errorf("no desktop: %q", note)
	}
	f, _, _, _ := fleet(t)
	if note := service(t, f).todoNote(context.Background(), ana); note != "" {
		t.Errorf("no runner: %q", note)
	}
	// A long list is clipped.
	d.cat = strings.Repeat("x", maxTodo+100)
	if note := s.todoNote(context.Background(), ana); !strings.Contains(note, "…") || len(note) > maxTodo+300 {
		t.Errorf("long list: %d", len(note))
	}
}
