package gateway

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/madkoding/kitties/internal/agenda"
	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/kitty"
)

// fakeAgenda is an agenda in memory.
type fakeAgenda struct {
	mu      sync.Mutex
	goals   map[string]*agenda.Goal
	fired   []agenda.Event
	runs    []string
	n       int
	busy    bool
	entries map[string][]agenda.Entry
}

func newFakeAgenda() *fakeAgenda {
	return &fakeAgenda{goals: map[string]*agenda.Goal{}, entries: map[string][]agenda.Entry{}}
}

func (a *fakeAgenda) Goals(kitty string) []*agenda.Goal {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []*agenda.Goal
	for _, g := range a.goals {
		if kitty == "" || g.Kitty == kitty {
			out = append(out, g)
		}
	}
	return out
}
func (a *fakeAgenda) Goal(id string) (*agenda.Goal, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if g, ok := a.goals[id]; ok {
		return g, nil
	}
	return nil, errors.New("no such goal " + id)
}
func (a *fakeAgenda) AddGoal(g agenda.Goal) (*agenda.Goal, error) {
	if err := g.Validate(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.n++
	g.ID = "g" + string(rune('0'+a.n))
	a.goals[g.ID] = &g
	return &g, nil
}
func (a *fakeAgenda) UpdateGoal(id string, g agenda.Goal) (*agenda.Goal, error) {
	old, err := a.Goal(id)
	if err != nil {
		return nil, err
	}
	if old.FromFile {
		return nil, errors.New("this goal comes from the configuration file; edit it there")
	}
	g.ID, g.Kitty = id, old.Kitty
	if err := g.Validate(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.goals[id] = &g
	return &g, nil
}
func (a *fakeAgenda) RemoveGoal(id string) error {
	if _, err := a.Goal(id); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.goals, id)
	return nil
}
func (a *fakeAgenda) RunNow(_ context.Context, id string) (string, error) {
	if _, err := a.Goal(id); err != nil {
		return "", err
	}
	if a.busy {
		return "", errors.New("the goal is already running")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.runs = append(a.runs, id)
	return "chat-" + id, nil
}
func (a *fakeAgenda) Fire(_ context.Context, ev agenda.Event) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.fired = append(a.fired, ev)
	var ids []string
	for id, g := range a.goals {
		for _, on := range g.On {
			if on == ev.Source {
				ids = append(ids, id)
			}
		}
	}
	return ids
}
func (a *fakeAgenda) Forget(kitty string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, g := range a.goals {
		if g.Kitty == kitty {
			delete(a.goals, id)
		}
	}
	delete(a.entries, kitty)
	return nil
}
func (a *fakeAgenda) Recent(kitty string, n int) []agenda.Entry {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.entries[kitty]
	if n < len(out) {
		out = out[:n]
	}
	return out
}

func agendaServer(t *testing.T) (*httptest.Server, *fakeAgenda) {
	t.Helper()
	f := &fakeFleet{kitties: map[string]*kitty.Kitty{"a1": {ID: "a1", Name: "ana", Status: kitty.StatusRunning}}}
	a := newFakeAgenda()
	srv := httptest.NewServer(New(f, &fakeChats{sessions: map[string]*chat.Session{}}, "tok", "test").WithAgenda(a))
	t.Cleanup(srv.Close)
	return srv, a
}

func TestGoalsAPI(t *testing.T) {
	srv, a := agendaServer(t)
	if code, m, _ := call(t, srv, "GET", "/v1/goals", "tok", nil); code != 200 || len(m["goals"].([]any)) != 0 {
		t.Errorf("empty = %d %v", code, m)
	}
	code, m, _ := call(t, srv, "POST", "/v1/goals", "tok", map[string]any{"kitty": "ana", "title": "Inbox", "every": "@hourly", "on": []string{"w"}})
	if code != 201 || m["goal"].(map[string]any)["kitty"] != "a1" || m["goal"].(map[string]any)["enabled"] != true {
		t.Fatalf("add = %d %v", code, m)
	}
	id := m["goal"].(map[string]any)["id"].(string)
	for name, tc := range map[string]struct {
		body map[string]any
		code int
	}{
		"unknown kitty":     {map[string]any{"kitty": "zz", "title": "x", "every": "@daily"}, 404},
		"unknown connector": {map[string]any{"kitty": "ana", "title": "x", "on": []string{"nope"}}, 400},
		"no title":          {map[string]any{"kitty": "ana", "every": "@daily"}, 400},
		"bad schedule":      {map[string]any{"kitty": "ana", "title": "x", "every": "nope"}, 400},
		"no trigger":        {map[string]any{"kitty": "ana", "title": "x"}, 400},
	} {
		if code, _, data := call(t, srv, "POST", "/v1/goals", "tok", tc.body); code != tc.code {
			t.Errorf("%s = %d %s", name, code, data)
		}
	}
	if code, _, _ := call(t, srv, "POST", "/v1/goals", "tok", "{bad"); code != 400 {
		t.Error("bad json")
	}
	if code, m, _ := call(t, srv, "GET", "/v1/goals?kitty=ana", "tok", nil); code != 200 || len(m["goals"].([]any)) != 1 {
		t.Errorf("by kitty = %d %v", code, m)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/goals?kitty=zz", "tok", nil); code != 404 {
		t.Error("unknown kitty filter")
	}
	if code, m, _ := call(t, srv, "GET", "/v1/goals/"+id, "tok", nil); code != 200 || m["goal"].(map[string]any)["title"] != "Inbox" {
		t.Errorf("get = %d %v", code, m)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/goals/zz", "tok", nil); code != 404 {
		t.Error("get unknown")
	}
	off := false
	if code, m, _ := call(t, srv, "PUT", "/v1/goals/"+id, "tok", map[string]any{"title": "Mail", "every": "@daily", "enabled": off}); code != 200 || m["goal"].(map[string]any)["title"] != "Mail" || m["goal"].(map[string]any)["enabled"] != false {
		t.Errorf("update = %d %v", code, m)
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/goals/"+id, "tok", map[string]any{"title": "", "every": "@daily"}); code != 400 {
		t.Error("update validates")
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/goals/"+id, "tok", map[string]any{"title": "x", "on": []string{"nope"}}); code != 400 {
		t.Error("update checks connectors")
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/goals/"+id, "tok", "nope"); code != 400 {
		t.Error("update bad json")
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/goals/zz", "tok", map[string]any{"title": "x", "every": "@daily"}); code != 404 {
		t.Error("update unknown")
	}
	a.goals[id].FromFile = true
	if code, _, _ := call(t, srv, "PUT", "/v1/goals/"+id, "tok", map[string]any{"title": "x", "every": "@daily"}); code != 403 {
		t.Error("a file goal is read-only")
	}
	a.goals[id].FromFile = false
	if code, m, _ := call(t, srv, "POST", "/v1/goals/"+id+"/run", "tok", nil); code != 200 || m["chat"] != "chat-"+id || len(a.runs) != 1 {
		t.Errorf("run = %d %v", code, m)
	}
	a.busy = true
	if code, _, _ := call(t, srv, "POST", "/v1/goals/"+id+"/run", "tok", nil); code != 409 {
		t.Error("run while running")
	}
	if code, _, _ := call(t, srv, "POST", "/v1/goals/zz/run", "tok", nil); code != 404 {
		t.Error("run unknown")
	}
	if code, _, _ := call(t, srv, "DELETE", "/v1/goals/"+id, "tok", nil); code != 200 || len(a.goals) != 0 {
		t.Error("remove")
	}
	if code, _, _ := call(t, srv, "DELETE", "/v1/goals/"+id, "tok", nil); code != 404 {
		t.Error("remove unknown")
	}
	// The journal.
	a.entries["a1"] = []agenda.Entry{{Kitty: "a1", Kind: "run", Text: "one"}, {Kitty: "a1", Kind: "done", Text: "two"}}
	if code, m, _ := call(t, srv, "GET", "/v1/kitties/ana/journal?n=1", "tok", nil); code != 200 || len(m["journal"].([]any)) != 1 {
		t.Errorf("journal = %d %v", code, m)
	}
	if code, m, _ := call(t, srv, "GET", "/v1/kitties/ana/journal?n=nope", "tok", nil); code != 200 || len(m["journal"].([]any)) != 2 {
		t.Errorf("journal default = %d %v", code, m)
	}
	if code, m, _ := call(t, srv, "GET", "/v1/kitties/ana/journal", "tok", nil); code != 200 || m["journal"] == nil {
		t.Errorf("journal = %d %v", code, m)
	}
	delete(a.entries, "a1")
	if code, m, _ := call(t, srv, "GET", "/v1/kitties/ana/journal", "tok", nil); code != 200 || len(m["journal"].([]any)) != 0 {
		t.Errorf("empty journal = %d %v", code, m)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/kitties/zz/journal", "tok", nil); code != 404 {
		t.Error("journal of nobody")
	}
	// Everything needs the token.
	if code, _, _ := call(t, srv, "GET", "/v1/goals", "", nil); code != 401 {
		t.Error("auth")
	}
	// Removing the kitty takes its goals and journal along.
	_, _ = a.AddGoal(agenda.Goal{Kitty: "a1", Title: "Bye", Every: "@daily"})
	a.entries["a1"] = []agenda.Entry{{Kitty: "a1", Kind: "run"}}
	if code, _, data := call(t, srv, "DELETE", "/v1/kitties/ana", "tok", nil); code != 200 || len(a.goals) != 0 || len(a.entries) != 0 {
		t.Errorf("remove kitty = %d %s goals=%d", code, data, len(a.goals))
	}
}

func TestHooks(t *testing.T) {
	srv, a := agendaServer(t)
	_, _ = a.AddGoal(agenda.Goal{Kitty: "a1", Title: "Triage", On: []string{"gh"}, Enabled: true})
	body := `{"action":"opened","issue":{"number":1,"title":"Bug"}}`
	post := func(path string, hdr map[string]string) (int, map[string]any) {
		req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(body))
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var m map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&m)
		return resp.StatusCode, m
	}
	// GitHub's signature.
	mac := hmac.New(sha256.New, []byte("shh"))
	mac.Write([]byte(body))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	code, m := post("/v1/hooks/gh", map[string]string{"X-Hub-Signature-256": sig, "X-GitHub-Event": "issues"})
	if code != 200 || len(m["goals"].([]any)) != 1 || len(a.fired) != 1 || a.fired[0].Kind != "issues" || !strings.Contains(a.fired[0].Text, "issue #1: Bug") {
		t.Errorf("github hook = %d %v fired=%+v", code, m, a.fired)
	}
	// The plain header and the query, with the event name from our own header.
	if code, m := post("/v1/hooks/gh", map[string]string{"X-Kitties-Secret": "shh", "X-Kitties-Event": "ping"}); code != 200 || a.fired[len(a.fired)-1].Kind != "ping" {
		t.Errorf("header hook = %d %v", code, m)
	}
	if code, _ := post("/v1/hooks/gh?secret=shh", nil); code != 200 {
		t.Errorf("query hook = %d", code)
	}
	// Wrong or missing secrets, a bad signature, a connector without one, an unknown one.
	for name, tc := range map[string]struct {
		path string
		hdr  map[string]string
		code int
	}{
		"wrong header":  {"/v1/hooks/gh", map[string]string{"X-Kitties-Secret": "nope"}, 403},
		"nothing":       {"/v1/hooks/gh", nil, 403},
		"bad signature": {"/v1/hooks/gh", map[string]string{"X-Hub-Signature-256": "sha256=00"}, 403},
		"no secret set": {"/v1/hooks/sl", map[string]string{"X-Kitties-Secret": "x"}, 403},
		"unknown":       {"/v1/hooks/zz", map[string]string{"X-Kitties-Secret": "x"}, 404},
	} {
		if code, _ := post(tc.path, tc.hdr); code != tc.code {
			t.Errorf("%s = %d, want %d", name, code, tc.code)
		}
	}
	fired := len(a.fired)
	// Wrong secrets are counted: after ten the client is refused for a while.
	for i := 0; i < 12; i++ {
		post("/v1/hooks/gh", map[string]string{"X-Kitties-Secret": "nope"})
	}
	if code, _ := post("/v1/hooks/gh?secret=shh", nil); code != 429 {
		t.Errorf("after many wrong secrets = %d", code)
	}
	if len(a.fired) != fired {
		t.Error("refused hooks fire nothing")
	}
	// Too big a body.
	big := httptest.NewServer(New(&fakeFleet{kitties: map[string]*kitty.Kitty{}}, &fakeChats{}, "tok", "").WithAgenda(a))
	defer big.Close()
	req, _ := http.NewRequest("POST", big.URL+"/v1/hooks/gh?secret=shh", strings.NewReader(strings.Repeat("x", maxHookBody+10)))
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 413 {
		t.Errorf("big = %d", resp.StatusCode)
	}
	// Without an agenda every route says so.
	none := httptest.NewServer(New(&fakeFleet{kitties: map[string]*kitty.Kitty{"a1": {ID: "a1", Name: "ana"}}}, &fakeChats{}, "tok", ""))
	defer none.Close()
	for _, tc := range []struct{ method, path string }{{"GET", "/v1/goals"}, {"POST", "/v1/goals"}, {"GET", "/v1/goals/x"}, {"PUT", "/v1/goals/x"}, {"DELETE", "/v1/goals/x"}, {"POST", "/v1/goals/x/run"}, {"GET", "/v1/kitties/ana/journal"}} {
		if code, _, data := call(t, none, tc.method, tc.path, "tok", nil); code != 404 || !strings.Contains(string(data), "no agenda") {
			t.Errorf("%s %s without an agenda = %d", tc.method, tc.path, code)
		}
	}
	req, _ = http.NewRequest("POST", none.URL+"/v1/hooks/gh", nil)
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 404 {
		t.Error("hook without an agenda")
	}
}
