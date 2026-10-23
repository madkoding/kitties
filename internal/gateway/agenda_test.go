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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/agenda"
	"github.com/madkoding/kitties/internal/approvals"
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

func TestStripeSignature(t *testing.T) {
	body := []byte(`{"type":"invoice.payment_failed"}`)
	now := time.Unix(1_800_000_000, 0)
	sign := func(ts int64, secret string) string {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(strconv.FormatInt(ts, 10) + "."))
		mac.Write(body)
		return hex.EncodeToString(mac.Sum(nil))
	}
	ts := now.Unix()
	good := "t=" + strconv.FormatInt(ts, 10) + ",v1=" + sign(ts, "whsec")
	if !stripeSigned(good, body, "whsec", now) {
		t.Error("a valid signature")
	}
	// Rotated secrets send several v1 signatures: any one will do.
	if !stripeSigned("t="+strconv.FormatInt(ts, 10)+",v1=00,v1="+sign(ts, "whsec"), body, "whsec", now) {
		t.Error("one of several")
	}
	for name, header := range map[string]string{
		"wrong secret":    "t=" + strconv.FormatInt(ts, 10) + ",v1=" + sign(ts, "other"),
		"no timestamp":    "v1=" + sign(ts, "whsec"),
		"no signature":    "t=" + strconv.FormatInt(ts, 10),
		"bad timestamp":   "t=x,v1=" + sign(ts, "whsec"),
		"too old":         "t=" + strconv.FormatInt(ts-600, 10) + ",v1=" + sign(ts-600, "whsec"),
		"from the future": "t=" + strconv.FormatInt(ts+600, 10) + ",v1=" + sign(ts+600, "whsec"),
	} {
		if stripeSigned(header, body, "whsec", now) {
			t.Errorf("%s must be refused", name)
		}
	}
	if stripeSigned(good, []byte("tampered"), "whsec", now) {
		t.Error("a changed body")
	}
	// Through the hook: the header decides, with the real clock.
	srv, a := agendaServer(t)
	_, _ = a.AddGoal(agenda.Goal{Kitty: "a1", Title: "Overdue", On: []string{"gh"}, Enabled: true})
	nowTS := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte("shh"))
	mac.Write([]byte(strconv.FormatInt(nowTS, 10) + "."))
	mac.Write(body)
	req, _ := http.NewRequest("POST", srv.URL+"/v1/hooks/gh", strings.NewReader(string(body)))
	req.Header.Set("Stripe-Signature", "t="+strconv.FormatInt(nowTS, 10)+",v1="+hex.EncodeToString(mac.Sum(nil)))
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 200 {
		t.Errorf("a signed stripe hook = %v %v", resp, err)
	}
}

// fakeApprover holds a few approvals in memory.
type fakeApprover struct {
	list  []approvals.Approval
	rules []approvals.Rule
	none  bool
}

func (a *fakeApprover) ListApprovals(status string) ([]approvals.Approval, error) {
	if a.none {
		return nil, chat.ErrNoApprovals
	}
	var out []approvals.Approval
	for _, x := range a.list {
		if status == "" || x.Status == status {
			out = append(out, x)
		}
	}
	return out, nil
}

func (a *fakeApprover) DecideApproval(_ context.Context, id string, approve bool, reason string, always bool) (*approvals.Approval, error) {
	for i := range a.list {
		if a.list[i].ID != id {
			continue
		}
		if a.list[i].Status != approvals.Pending {
			return nil, errors.New("approval \"" + id + "\" is already " + a.list[i].Status)
		}
		if always && a.list[i].Risk == "always" {
			return nil, errors.New("this kind of action cannot be allowed for good: it is always asked")
		}
		a.list[i].Status, a.list[i].Reason, a.list[i].Always = approvals.Rejected, reason, always
		if approve {
			a.list[i].Status = approvals.Approved
		}
		return &a.list[i], nil
	}
	return nil, errors.New("no such approval " + id)
}

func (a *fakeApprover) ApprovalRules() ([]approvals.Rule, error) { return a.rules, nil }
func (a *fakeApprover) SetApprovalRule(kitty, conn, action, mode string) (*approvals.Rule, error) {
	if kitty != "mimi" {
		return nil, errors.New("no such kitty " + kitty)
	}
	if mode == "maybe" {
		return nil, errors.New("a limit is ask, never or auto, not \"maybe\"")
	}
	r := approvals.Rule{ID: "r" + conn, Kitty: "a1", KittyName: kitty, Connector: conn, Action: action, Mode: mode}
	a.rules = append(a.rules, r)
	return &r, nil
}
func (a *fakeApprover) RemoveApprovalRule(id string) error {
	for i, r := range a.rules {
		if r.ID == id {
			a.rules = append(a.rules[:i], a.rules[i+1:]...)
			return nil
		}
	}
	return errors.New("no such rule " + id)
}

func TestApprovalsAPI(t *testing.T) {
	ap := &fakeApprover{
		list: []approvals.Approval{
			{ID: "p1", Kitty: "a1", Connector: "slack", Action: "send", Status: approvals.Pending, Risk: "ask", Summary: "slack.send"},
			{ID: "p2", Kitty: "a1", Connector: "pay", Action: "refund", Status: approvals.Pending, Risk: "always"},
			{ID: "p3", Kitty: "a1", Status: approvals.Approved},
		},
		rules: []approvals.Rule{{ID: "r1", Kitty: "a1", Connector: "slack", Action: "send", Mode: "auto"}},
	}
	f := &fakeFleet{kitties: map[string]*kitty.Kitty{"a1": {ID: "a1", Name: "ana"}}}
	srv := httptest.NewServer(New(f, &fakeChats{}, "tok", "").WithApprovals(ap))
	defer srv.Close()
	if code, m, _ := call(t, srv, "GET", "/v1/approvals?status=pending", "tok", nil); code != 200 || len(m["approvals"].([]any)) != 2 {
		t.Errorf("pending = %d %v", code, m)
	}
	if code, m, _ := call(t, srv, "GET", "/v1/approvals", "tok", nil); code != 200 || len(m["approvals"].([]any)) != 3 {
		t.Errorf("all = %d %v", code, m)
	}
	if code, m, _ := call(t, srv, "GET", "/v1/approvals?status=rejected", "tok", nil); code != 200 || len(m["approvals"].([]any)) != 0 {
		t.Errorf("none = %d %v", code, m)
	}
	if code, m, _ := call(t, srv, "POST", "/v1/approvals/p1/approve", "tok", map[string]any{"always": true}); code != 200 || m["approval"].(map[string]any)["status"] != "approved" || m["approval"].(map[string]any)["always"] != true {
		t.Errorf("approve = %d %v", code, m)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/approvals/p1/approve", "tok", nil); code != 409 {
		t.Error("deciding twice is a conflict")
	}
	if code, _, _ := call(t, srv, "POST", "/v1/approvals/p2/approve", "tok", map[string]any{"always": true}); code != 400 {
		t.Error("an always-asked action cannot be for good")
	}
	if code, m, _ := call(t, srv, "POST", "/v1/approvals/p2/reject", "tok", map[string]any{"reason": "no refunds today"}); code != 200 || m["approval"].(map[string]any)["reason"] != "no refunds today" {
		t.Errorf("reject = %d %v", code, m)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/approvals/zz/reject", "tok", nil); code != 404 {
		t.Error("unknown")
	}
	if code, _, _ := call(t, srv, "POST", "/v1/approvals/p3/approve", "tok", "{bad"); code != 400 {
		t.Error("bad json")
	}
	if code, m, _ := call(t, srv, "GET", "/v1/approvals/rules", "tok", nil); code != 200 || len(m["rules"].([]any)) != 1 {
		t.Errorf("rules = %d %v", code, m)
	}
	// A limit set by hand: by the kitty's name, refused for an unknown kitty or a bad mode.
	if code, m, _ := call(t, srv, "POST", "/v1/approvals/rules", "tok", map[string]any{"kitty": "mimi", "connector": "x", "action": "post", "mode": "ask"}); code != 200 || m["rule"].(map[string]any)["mode"] != "ask" {
		t.Errorf("set rule = %d %v", code, m)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/approvals/rules", "tok", map[string]any{"kitty": "zz", "connector": "x", "mode": "ask"}); code != 404 {
		t.Errorf("unknown kitty = %d", code)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/approvals/rules", "tok", map[string]any{"kitty": "mimi", "connector": "x", "mode": "maybe"}); code != 400 {
		t.Errorf("bad mode = %d", code)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/approvals/rules", "tok", "{"); code != 400 {
		t.Errorf("bad json = %d", code)
	}
	ap.rules = ap.rules[:1]
	if code, _, _ := call(t, srv, "DELETE", "/v1/approvals/rules/r1", "tok", nil); code != 200 || len(ap.rules) != 0 {
		t.Error("remove rule")
	}
	if code, _, _ := call(t, srv, "DELETE", "/v1/approvals/rules/r1", "tok", nil); code != 404 {
		t.Error("remove unknown rule")
	}
	if code, m, _ := call(t, srv, "GET", "/v1/approvals/rules", "tok", nil); code != 200 || len(m["rules"].([]any)) != 0 {
		t.Errorf("no rules = %d %v", code, m)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/approvals", "", nil); code != 401 {
		t.Error("auth")
	}
	// A deployment where nothing is held says so everywhere.
	for _, tc := range []struct{ method, path string }{{"GET", "/v1/approvals"}, {"GET", "/v1/approvals/rules"}, {"DELETE", "/v1/approvals/rules/x"}, {"POST", "/v1/approvals/rules"}, {"POST", "/v1/approvals/x/approve"}, {"POST", "/v1/approvals/x/reject"}} {
		none := httptest.NewServer(New(f, &fakeChats{}, "tok", ""))
		if code, _, data := call(t, none, tc.method, tc.path, "tok", nil); code != 404 || !strings.Contains(string(data), "no actions for approval") {
			t.Errorf("%s %s without approvals = %d", tc.method, tc.path, code)
		}
		none.Close()
	}
	off := httptest.NewServer(New(f, &fakeChats{}, "tok", "").WithApprovals(&fakeApprover{none: true}))
	defer off.Close()
	if code, _, _ := call(t, off, "GET", "/v1/approvals", "tok", nil); code != 404 {
		t.Error("the service says none are held")
	}
}
