package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/provider"
)

// A kitty that writes the short "@<connector> {...}" (as it did with Jira) gets the call run and
// its result back, inside a code fence or not, and the fence does not stay in the answer.
func TestShortConnectorLineRuns(t *testing.T) {
	f, sa, _, e := fleet(t)
	s := service(t, f)
	sess, _ := s.Create("", []string{"ana"})
	sa.replies = []string{"Lo busco.\n```json\n@echo {\"action\": \"request\", \"method\": \"GET\", \"path\": \"/rest/api/3/issue/SMDFS-1243\", \"query\": {\"fields\": \"summary\"}}\n```", "Listo."}
	replies, err := s.Send(context.Background(), sess.ID, "mira SMDFS-1243")
	if err != nil || len(replies) != 1 {
		t.Fatalf("Send = %+v, %v", replies, err)
	}
	if replies[0].Text != "Lo busco.\n\nListo." || len(replies[0].Calls) != 1 || replies[0].Calls[0].Output != "echoed" {
		t.Errorf("reply = %+v", replies[0])
	}
	if len(e.calls) != 1 || e.calls[0]["action"] != "request" || e.calls[0]["path"] != "/rest/api/3/issue/SMDFS-1243" {
		t.Errorf("connector calls = %v", e.calls)
	}
	second := sa.seen[1]
	if last := second[len(second)-1]; last.Role != "user" || !strings.Contains(last.Content, "echoed") {
		t.Errorf("the result goes back: %+v", last)
	}
	if sys := sa.seen[0][0].Content; !strings.Contains(sys, "@connector echo {") {
		t.Errorf("the text prompt shows a call: %s", sys)
	}
}

// A model offered tools that still writes its call as a line gets it run, and answered as a
// line: a tool result without the call it answers is refused by the providers.
func TestNativeBrainWritingALine(t *testing.T) {
	f, _, _, e := fleet(t)
	s := service(t, f)
	brain := &toolBrain{replies: []provider.Reply{
		{Content: "@connector_echo {\"text\": \"hi\"}"},
		{Content: "Hecho."},
	}}
	f.brains["a1"] = brain
	sess, _ := s.Create("", []string{"ana"})
	replies, err := s.Send(context.Background(), sess.ID, "go")
	if err != nil || replies[0].Text != "Hecho." || len(e.calls) != 1 || e.calls[0]["text"] != "hi" {
		t.Fatalf("Send = %+v %v %v", replies, err, e.calls)
	}
	for _, m := range brain.seen[1] {
		if m.Role == "tool" {
			t.Errorf("a line is not answered as a tool result: %+v", m)
		}
	}
	if last := brain.seen[1][len(brain.seen[1])-1]; last.Role != "user" || !strings.Contains(last.Content, "echoed") {
		t.Errorf("the result goes back as a message: %+v", last)
	}
	sys := brain.seen[0][0].Content
	if !strings.Contains(sys, "connector_<name>") || strings.Contains(sys, "@connector") {
		t.Errorf("a kitty with tools is told to call them: %s", sys)
	}
	var echo *provider.Tool
	for i, tl := range brain.tools[0] {
		if tl.Name == "connector_echo" {
			echo = &brain.tools[0][i]
		}
	}
	if echo == nil {
		t.Fatal("no connector_echo tool")
	}
	props, _ := echo.Parameters["properties"].(map[string]any)
	for _, p := range []string{"action", "text", "method", "path", "query", "body"} {
		if _, ok := props[p]; !ok {
			t.Errorf("connector tool lacks %q: %v", p, props)
		}
	}
}
