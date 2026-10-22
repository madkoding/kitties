package chat

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/artifacts"
	"github.com/madkoding/kitties/internal/provider"
	"github.com/madkoding/kitties/internal/skills"
)

// toolBrain answers with native calls, remembering the tools and messages it was given.
type toolBrain struct {
	replies []provider.Reply
	seen    [][]provider.Message
	tools   [][]provider.Tool
	text    bool
	err     error
}

func (b *toolBrain) Complete(ctx context.Context, m []provider.Message) (provider.Reply, error) {
	return b.CompleteTools(ctx, m, nil)
}
func (b *toolBrain) CompleteTools(_ context.Context, m []provider.Message, tools []provider.Tool) (provider.Reply, error) {
	b.seen = append(b.seen, m)
	b.tools = append(b.tools, tools)
	if b.err != nil {
		return provider.Reply{}, b.err
	}
	r := b.replies[0]
	if len(b.replies) > 1 {
		b.replies = b.replies[1:]
	}
	return r, nil
}
func (b *toolBrain) Describe() string    { return "tools" }
func (b *toolBrain) SupportsTools() bool { return !b.text }

func TestNativeToolsOnTheDesktop(t *testing.T) {
	f, _, _, e := fleet(t)
	d := &deskFleet{fakeFleet: f}
	s := service(t, d)
	dir := t.TempDir()
	s.SetLibrary(skills.New(filepath.Join(dir, "skills")), artifacts.New(filepath.Join(dir, "artifacts")))
	brain := &toolBrain{replies: []provider.Reply{
		{Content: "Opening.", Calls: []provider.ToolCall{{ID: "c1", Name: "open", Args: map[string]any{"url": "https://example.com"}}, {ID: "c2", Name: "connector_echo", Args: map[string]any{"text": "hi"}}}},
		{Calls: []provider.ToolCall{{ID: "c3", Name: "artifact_save", Args: map[string]any{"name": "seen.md", "content": "a page"}}, {ID: "c4", Name: "click", Args: map[string]any{"x": 1.0, "y": 2.0}}}},
		{Content: "All good.", Calls: []provider.ToolCall{{ID: "c5", Name: "done", Args: map[string]any{"summary": "Opened it."}}}},
	}}
	f.brains["a1"] = brain
	sess, _ := s.Create("", []string{"ana"})
	replies, err := s.Send(context.Background(), sess.ID, "open example.com")
	if err != nil || !replies[0].Working || replies[0].Text != "Opening." || len(replies[0].Calls) != 1 || len(e.calls) != 1 {
		t.Fatalf("Send = %+v %v", replies, err)
	}
	got := waitDone(t, s, sess.ID)
	turn := got.Turns[1]
	if turn.Text != "Opening.\n\nAll good.\n\nOpened it." || len(turn.Artifacts) != 1 || turn.Artifacts[0] != "seen.md" {
		t.Errorf("turn = %+v", turn)
	}
	names := []string{}
	for _, st := range turn.Steps {
		names = append(names, st.Action)
	}
	if strings.Join(names, ",") != "open,click,done" {
		t.Errorf("steps = %v", names)
	}
	// The first call carried the tool set: desktop, connector, skills and artifacts.
	tools := brain.tools[0]
	have := map[string]bool{}
	for _, tl := range tools {
		have[tl.Name] = true
	}
	for _, want := range []string{"click", "done", "connector_echo", "skill_read", "artifact_save"} {
		if !have[want] {
			t.Errorf("tool set lacks %s", want)
		}
	}
	// The second call replays the assistant's calls and answers each with a tool message, then
	// the screenshot as a user message.
	second := brain.seen[1]
	n := len(second)
	if second[n-4].Role != "assistant" || len(second[n-4].ToolCalls) != 2 || second[n-3].Role != "tool" || second[n-3].ToolCallID != "c1" ||
		second[n-2].ToolCallID != "c2" || !strings.Contains(second[n-2].Content, "echoed") || second[n-1].Role != "user" || len(second[n-1].Images) != 1 {
		t.Errorf("native feedback = %+v", second[n-4:])
	}
	// The third call answers the artifact save and the click.
	third := brain.seen[2]
	if m := third[len(third)-3]; m.Role != "tool" || m.ToolCallID != "c3" || !strings.Contains(m.Content, "saved") {
		t.Errorf("artifact answer = %+v", m)
	}
}

func TestNativeToolsWithoutDesktop(t *testing.T) {
	f, _, _, _ := fleet(t)
	s := service(t, f)
	s.SetLibrary(skills.New(filepath.Join(t.TempDir(), "skills")), nil)
	brain := &toolBrain{replies: []provider.Reply{
		{Calls: []provider.ToolCall{{ID: "k1", Name: "skill_read", Args: map[string]any{"name": "report-findings"}}, {ID: "k2", Name: "click", Args: map[string]any{"x": 1.0, "y": 1.0}}, {ID: "k3", Name: "skill_fly"}}},
		{Content: "Read it."},
	}}
	f.brains["b1"] = brain
	sess, _ := s.Create("", []string{"bo"})
	replies, err := s.Send(context.Background(), sess.ID, "read the skill")
	if err != nil || replies[0].Text != "Read it." || replies[0].Working || strings.Join(replies[0].Skills, ",") != "report-findings" {
		t.Fatalf("Send = %+v %v", replies, err)
	}
	second := brain.seen[1]
	if second[len(second)-1].Role != "tool" || second[len(second)-1].ToolCallID != "k3" || !strings.HasPrefix(second[len(second)-1].Content, "error:") {
		t.Errorf("an unknown verb answers with an error: %+v", second[len(second)-1])
	}
	for _, tl := range brain.tools[0] {
		if tl.Name == "click" || strings.HasPrefix(tl.Name, "artifact_") {
			t.Errorf("a brain-only kitty without artifacts is not offered %s", tl.Name)
		}
	}
	// A brain in text mode gets no tools and uses the lines.
	brain.text = true
	brain.replies = []provider.Reply{{Content: "@skill read report-findings\nok"}, {Content: "fine"}}
	if _, err := s.Send(context.Background(), sess.ID, "again"); err != nil {
		t.Fatal(err)
	}
	if brain.tools[len(brain.tools)-1] != nil {
		t.Error("text mode sends no tools")
	}
	// Native tools can be switched off for the whole service.
	brain.text = false
	s.SetNativeTools(false)
	brain.replies = []provider.Reply{{Content: "plain"}}
	s.Send(context.Background(), sess.ID, "and now")
	if brain.tools[len(brain.tools)-1] != nil {
		t.Error("native tools off")
	}
	// A failing brain in a native task.
	brain.err = errors.New("down")
	if replies, _ := s.Send(context.Background(), sess.ID, "x"); !strings.Contains(replies[0].Text, "could not answer") {
		t.Error("failure is reported")
	}
}

func TestSubscribe(t *testing.T) {
	f, sa, _, _ := fleet(t)
	s := service(t, f)
	sess, _ := s.Create("", []string{"ana"})
	ch, cancel := s.Subscribe(sess.ID)
	defer cancel()
	sa.replies = []string{"hello"}
	if _, err := s.Send(context.Background(), sess.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	first := <-ch
	second := <-ch
	if first.Type != "turn" || first.Index != 0 || first.Turn.From != "user" || second.Type != "turn" || second.Index != 1 || second.Turn.Text != "hello" {
		t.Errorf("events = %+v %+v", first, second)
	}
	// A full channel drops events instead of blocking.
	for i := 0; i < 70; i++ {
		s.Send(context.Background(), sess.ID, "more")
	}
	if err := s.Delete(sess.ID); err != nil {
		t.Fatal(err)
	}
	deleted := false
	for len(ch) > 0 {
		if ev := <-ch; ev.Type == "deleted" {
			deleted = true
		}
	}
	if deleted {
		t.Log("the delete event arrived (the channel had room)")
	}
	cancel()
	cancel()
	ch2, cancel2 := s.Subscribe("other")
	cancel2()
	s.emitLocked(Event{Type: "turn", Session: "other"})
	if len(ch2) != 0 {
		t.Error("an unsubscribed watcher gets nothing")
	}
}
