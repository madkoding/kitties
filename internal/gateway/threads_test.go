package gateway

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/agenda"
	"github.com/madkoding/kitties/internal/chat"
)

// The fake chats tell their threads apart as the real ones do (internal/chat/kinds.go).
func (c *fakeChats) KeepChat(id string, keep bool) (*chat.Session, error) {
	sess, err := c.Get(id)
	if err != nil {
		return nil, err
	}
	if chat.KindOf(sess) != chat.KindHandOver {
		return nil, chat.ErrNotHandOver
	}
	sess.Keep = keep
	return sess, nil
}

func (c *fakeChats) Runs(goal string, n int) (string, []chat.Run, error) {
	for _, s := range c.sessions {
		if s.Goal == goal {
			return s.ID, []chat.Run{{Turn: 0, Turns: 2, Why: "the schedule", Result: "done", Answer: "Nothing new.", Kitty: "ana"}}, nil
		}
	}
	return "", []chat.Run{}, nil
}

func (c *fakeChats) TidyGoals(exists func(string) bool) int {
	n := 0
	for _, s := range c.sessions {
		if s.Goal != "" && !exists(s.Goal) {
			s.Goal = ""
			n++
		}
	}
	return n
}

func TestThreadKinds(t *testing.T) {
	c := &fakeChats{sessions: map[string]*chat.Session{
		"c1": {ID: "c1", Title: "Plan", Members: []string{"a1"}},
		"c2": {ID: "c2", Title: "Inbox", Members: []string{"a1"}, Goal: "g1"},
		"c3": {ID: "c3", Title: "ana → bo", Members: []string{"b1"}, Owner: "a1", Depth: 1},
	}}
	f := &fakeFleet{}
	a := newFakeAgenda()
	a.goals["g1"] = &agenda.Goal{ID: "g1", Kitty: "a1", Title: "Inbox", Every: "@hourly", Enabled: true, Chat: "c2"}
	s := New(f, c, "tok", "test").WithAgenda(a)
	auth := func(r *http.Request) { r.Header.Set("Authorization", "Bearer tok") }
	titles := func(kind string) string {
		rec := do(s, "GET", "/v1/chats?kind="+kind, "", auth)
		var out struct {
			Chats []chat.Session `json:"chats"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		var got []string
		for _, x := range out.Chats {
			got = append(got, x.Title)
		}
		return strings.Join(got, ",")
	}
	// The sidebar's chats leave the goal's thread and the hand-over out; each kind lists its own.
	if got := titles("chat"); got != "Plan" {
		t.Errorf("chats = %q", got)
	}
	if got := titles("goal"); got != "Inbox" {
		t.Errorf("goals = %q", got)
	}
	if got := titles("handover"); got != "ana → bo" {
		t.Errorf("hand-overs = %q", got)
	}
	if got := titles(""); strings.Count(got, ",") != 2 {
		t.Errorf("all = %q", got)
	}
	// A hand-over kept shows among the chats; a chat cannot be kept.
	if rec := do(s, "PUT", "/v1/chats/c3/keep", `{"keep": true}`, auth); rec.Code != 200 {
		t.Fatalf("keep: %d %s", rec.Code, rec.Body)
	}
	if got := titles("chat"); !strings.Contains(got, "ana → bo") {
		t.Errorf("chats after keep = %q", got)
	}
	if rec := do(s, "PUT", "/v1/chats/c1/keep", `{"keep": true}`, auth); rec.Code != http.StatusBadRequest {
		t.Errorf("keep a chat: %d", rec.Code)
	}
	if rec := do(s, "PUT", "/v1/chats/nope/keep", `{"keep": true}`, auth); rec.Code != http.StatusNotFound {
		t.Errorf("keep a missing chat: %d", rec.Code)
	}
	// A goal's runs, with the goal and its thread.
	rec := do(s, "GET", "/v1/goals/g1/runs", "", auth)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"chat":"c2"`) || !strings.Contains(rec.Body.String(), "Nothing new.") {
		t.Errorf("runs: %d %s", rec.Code, rec.Body)
	}
	if rec := do(s, "GET", "/v1/goals/nope/runs", "", auth); rec.Code != http.StatusNotFound {
		t.Errorf("runs of a missing goal: %d", rec.Code)
	}
	// MCP: chat_list by kind, goal_runs and chat_keep.
	s.WithMCP(mcpToken, false)
	if text, _, _ := tool(t, s, "chat_list", map[string]any{}); strings.Contains(text, "Inbox") || !strings.Contains(text, `"kind": "chat"`) {
		t.Errorf("chat_list: %q", text)
	}
	if text, _, _ := tool(t, s, "chat_list", map[string]any{"kind": "goal"}); !strings.Contains(text, "Inbox") {
		t.Errorf("chat_list goal: %q", text)
	}
	if text, _, _ := tool(t, s, "goal_runs", map[string]any{"id": "g1"}); !strings.Contains(text, "Nothing new.") || !strings.Contains(text, `"chat": "c2"`) {
		t.Errorf("goal_runs: %q", text)
	}
	if text, isErr, _ := tool(t, s, "chat_keep", map[string]any{"id": "c3", "keep": false}); isErr || !strings.Contains(text, `"keep": false`) {
		t.Errorf("chat_keep: %q", text)
	}
	// A goal removed gives its thread back to the chats.
	if rec := do(s, "DELETE", "/v1/goals/g1", "", auth); rec.Code != 200 {
		t.Fatalf("remove: %d", rec.Code)
	}
	if got := titles("chat"); !strings.Contains(got, "Inbox") {
		t.Errorf("chats after the goal went = %q", got)
	}
}
