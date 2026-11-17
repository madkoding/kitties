package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/connector"
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

// A connector whose kind lists its actions is offered one tool per action, with that action's
// arguments, and a call to one runs the connector with the action set.
func TestActionTools(t *testing.T) {
	f, _, _, _ := fleet(t)
	jira, err := connector.Build("jira", connector.Settings{"project": "KIT", "url": "https://acme.atlassian.net", "email": "a@b.c", "token": "t"}, "work tickets")
	if err != nil {
		t.Fatal(err)
	}
	f.conns.Add("jira", jira)
	f.conns.Add("my memory", jira) // a name that is not a tool word, and would clash once cleaned
	s := service(t, f)
	allowed := f.conns.Descriptions([]string{"jira", "echo", "my memory"})
	tools, routes := s.connectorTools(allowed)
	byName := map[string]provider.Tool{}
	for _, tl := range tools {
		byName[tl.Name] = tl
	}
	req, ok := byName["jira_request"]
	if !ok {
		t.Fatalf("no jira_request tool: %v", routes)
	}
	props, _ := req.Parameters["properties"].(map[string]any)
	if q, _ := props["query"].(map[string]any); q["type"] != "object" {
		t.Errorf("query is an object: %v", props)
	}
	if p, _ := props["path"].(map[string]any); p["type"] != "string" || !strings.Contains(p["description"].(string), "developer.atlassian.com") {
		t.Errorf("path: %v", props)
	}
	if _, ok := props["action"]; ok {
		t.Error("an action tool has no action argument")
	}
	if !strings.Contains(req.Description, "jira connection") || !strings.Contains(req.Description, "work tickets") {
		t.Errorf("description = %q", req.Description)
	}
	if _, ok := byName["jira_create_issue"]; !ok {
		t.Error("no jira_create_issue tool")
	}
	if _, ok := byName["connector_echo"]; !ok {
		t.Error("a connector without listed actions keeps its one tool")
	}
	if _, ok := byName["my_memory_request"]; !ok {
		t.Errorf("cleaned name: %v", routes)
	}
	acts := fromCalls([]provider.ToolCall{
		{ID: "1", Name: "jira_request", Args: map[string]any{"path": "/rest/api/3/issue/SMDFS-1243", "action": "other"}},
		{ID: "2", Name: "connector_echo", Args: map[string]any{"text": "hi"}},
	}, routes)
	if acts[0].kind != "connector" || acts[0].name != "jira" || acts[0].args["action"] != "request" || acts[0].args["path"] != "/rest/api/3/issue/SMDFS-1243" {
		t.Errorf("action tool call = %+v", acts[0])
	}
	if acts[1].kind != "connector" || acts[1].name != "echo" {
		t.Errorf("connector tool call = %+v", acts[1])
	}
	// A clash with another tool's name moves the action tool aside.
	if got := actionToolName("memory", "list"); got != "connector_memory__list" {
		t.Errorf("clash = %q", got)
	}
	if got := actionToolName("page", "x"); got != "connector_page__x" {
		t.Errorf("clash = %q", got)
	}
}

// Every tool that is not a connector is reserved, so an action tool never takes its name.
func TestReservedToolsCoverTheToolSet(t *testing.T) {
	f, _, _, _ := fleet(t)
	s := service(t, f)
	k := f.kitties["a1"]
	for _, tl := range s.toolSet(k, nil) {
		reserved := reservedTools[tl.Name]
		for _, p := range reservedPrefixes {
			reserved = reserved || strings.HasPrefix(tl.Name, p)
		}
		if !reserved {
			t.Errorf("%s is not reserved", tl.Name)
		}
	}
}

// slowConn answers when it is let go, so a test can look at the chat while the call runs.
type slowConn struct {
	started chan struct{}
	release chan struct{}
}

func (c *slowConn) Describe() string { return "slow" }
func (c *slowConn) Kind() string     { return "jira" }
func (c *slowConn) Call(context.Context, map[string]any) (string, error) {
	close(c.started)
	<-c.release
	return `{"key":"SMDFS-1243"}`, nil
}

// While a connector runs the chat says so ("calling"), and the turn keeps what was asked.
func TestCallingWhileTheConnectorRuns(t *testing.T) {
	f, sa, _, _ := fleet(t)
	slow := &slowConn{started: make(chan struct{}), release: make(chan struct{})}
	f.conns.Add("jira", slow)
	f.kitties["a1"].Connectors = []string{"jira"}
	s := service(t, f)
	sess, _ := s.Create("", []string{"ana"})
	sa.replies = []string{"@jira {\"action\": \"request\", \"path\": \"/rest/api/3/issue/SMDFS-1243\"}", "Está en curso."}
	done := make(chan []Turn, 1)
	go func() {
		replies, _ := s.Send(context.Background(), sess.ID, "mira SMDFS-1243")
		done <- replies
	}()
	<-slow.started
	got, _ := s.Get(sess.ID)
	if len(got.Calling) != 1 || got.Calling[0].Connector != "jira" || got.Calling[0].Kind != "jira" || got.Calling[0].Action != "request" || got.Calling[0].Detail != "GET /rest/api/3/issue/SMDFS-1243" || got.Calling[0].Kitty != "ana" {
		t.Errorf("calling = %+v", got.Calling)
	}
	close(slow.release)
	replies := <-done
	if len(replies) != 1 || len(replies[0].Calls) != 1 || replies[0].Calls[0].Detail != "GET /rest/api/3/issue/SMDFS-1243" || replies[0].Calls[0].Action != "request" {
		t.Fatalf("replies = %+v", replies)
	}
	if got, _ := s.Get(sess.ID); len(got.Calling) != 0 {
		t.Errorf("still calling: %+v", got.Calling)
	}
}
