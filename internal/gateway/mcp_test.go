package gateway

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/agenda"
	"github.com/madkoding/kitties/internal/approvals"
	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/kitty"
)

const mcpToken = "mcp-secret-token"

func mcpServer(exec bool) (*Server, *fakeFleet, *fakeChats) {
	f := &fakeFleet{kitties: map[string]*kitty.Kitty{"a1": {ID: "a1", Name: "ana", Status: kitty.StatusRunning}}}
	c := &fakeChats{sessions: map[string]*chat.Session{}}
	return New(f, c, "gw-token", "test").WithMCP(mcpToken, exec), f, c
}

func rpc(s *Server, method string, params any) (map[string]any, int) {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	rec := do(s, "POST", "/mcp", string(body), func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+mcpToken) })
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out, rec.Code
}

// tool calls one tool and returns the first text of its answer and whether it failed.
func tool(t *testing.T, s *Server, name string, args any) (text string, isErr bool, resp map[string]any) {
	t.Helper()
	resp, code := rpc(s, "tools/call", map[string]any{"name": name, "arguments": args})
	if code != 200 {
		t.Fatalf("%s: status %d", name, code)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		return "", true, resp
	}
	isErr, _ = res["isError"].(bool)
	if c, _ := res["content"].([]any); len(c) > 0 {
		text, _ = c[0].(map[string]any)["text"].(string)
	}
	return text, isErr, resp
}

func TestMCPOffByDefault(t *testing.T) {
	s := newServer("gw-token")
	if rec := do(s, "POST", "/mcp", `{}`, nil); rec.Code == 200 {
		t.Fatalf("mcp answered while off: %d", rec.Code)
	}
	if New(nil, nil, "t", "v").WithMCP("", true).mux == nil {
		t.Fatal("unreachable")
	}
}

func TestMCPAuth(t *testing.T) {
	s, _, _ := mcpServer(false)
	body := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	for name, mod := range map[string]func(*http.Request){
		"no token":      nil,
		"gateway token": func(r *http.Request) { r.Header.Set("Authorization", "Bearer gw-token") },
		"query token":   func(r *http.Request) { r.URL.RawQuery = "token=" + mcpToken },
		"wrong":         func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") },
	} {
		if rec := do(s, "POST", "/mcp", body, mod); rec.Code != 401 {
			t.Errorf("%s: status %d, want 401", name, rec.Code)
		}
	}
	// A web page of another origin is refused even with the token; the same origin is let in.
	rec := do(s, "POST", "/mcp", body, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+mcpToken)
		r.Header.Set("Origin", "http://evil.example")
	})
	if rec.Code != 403 {
		t.Errorf("foreign origin: %d", rec.Code)
	}
	rec = do(s, "POST", "/mcp", body, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+mcpToken)
		r.Header.Set("Origin", "http://"+r.Host)
	})
	if rec.Code != 200 {
		t.Errorf("same origin: %d", rec.Code)
	}
}

func TestMCPRateLimit(t *testing.T) {
	s, _, _ := mcpServer(false)
	var last int
	for i := 0; i < 15; i++ {
		last = do(s, "POST", "/mcp", `{}`, func(r *http.Request) { r.Header.Set("Authorization", "Bearer bad") }).Code
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("guessing the token is not limited: %d", last)
	}
}

func TestMCPProtocol(t *testing.T) {
	s, _, _ := mcpServer(false)
	resp, _ := rpc(s, "initialize", map[string]any{"protocolVersion": "2025-03-26"})
	if resp["result"].(map[string]any)["protocolVersion"] != "2025-03-26" {
		t.Errorf("initialize: %v", resp)
	}
	resp, _ = rpc(s, "tools/list", nil)
	names := map[string]bool{}
	for _, x := range resp["result"].(map[string]any)["tools"].([]any) {
		names[x.(map[string]any)["name"].(string)] = true
	}
	for _, n := range []string{"kitty_list", "kitty_create", "chat_send", "kitty_screenshot", "fleet_status"} {
		if !names[n] {
			t.Errorf("tool %s missing", n)
		}
	}
	if names["kitty_exec"] {
		t.Error("exec is off by default")
	}
	if s, _, _ := mcpServer(true); true {
		resp, _ = rpc(s, "tools/list", nil)
		if !strings.Contains(string(mustJSON(resp)), "kitty_exec") {
			t.Error("exec missing when allowed")
		}
	}
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func TestMCPFleetTools(t *testing.T) {
	s, f, _ := mcpServer(false)
	if text, isErr, _ := tool(t, s, "kitty_list", map[string]any{}); isErr || !strings.Contains(text, "ana") {
		t.Errorf("kitty_list: %q", text)
	}
	if text, _, _ := tool(t, s, "fleet_status", map[string]any{}); !strings.Contains(text, `"version": "test"`) {
		t.Errorf("fleet_status: %q", text)
	}
	if _, isErr, _ := tool(t, s, "provider_list", map[string]any{}); isErr {
		t.Error("provider_list failed")
	}
	if text, _, _ := tool(t, s, "kitty_get", map[string]any{"ref": "ana"}); !strings.Contains(text, "a1") {
		t.Errorf("kitty_get: %q", text)
	}
	if _, isErr, _ := tool(t, s, "kitty_get", map[string]any{"ref": "ghost"}); !isErr {
		t.Error("a missing kitty is an error result")
	}
	text, _, _ := tool(t, s, "kitty_create", map[string]any{"name": "bob", "soul": map[string]any{"persona": "calm"}})
	if !strings.Contains(text, "id-bob") || !strings.Contains(text, kitty.StatusRunning) {
		t.Errorf("kitty_create: %q", text)
	}
	if text, _, _ = tool(t, s, "kitty_create", map[string]any{"name": "cy", "start": false}); !strings.Contains(text, kitty.StatusCreated) {
		t.Errorf("create without start: %q", text)
	}
	if _, isErr, _ := tool(t, s, "kitty_create", map[string]any{"name": "ana"}); !isErr {
		t.Error("a duplicate name fails")
	}
	if text, _, _ = tool(t, s, "kitty_stop", map[string]any{"ref": "ana"}); !strings.Contains(text, kitty.StatusStopped) {
		t.Errorf("stop: %q", text)
	}
	if text, _, _ = tool(t, s, "kitty_start", map[string]any{"ref": "ana"}); !strings.Contains(text, kitty.StatusRunning) {
		t.Errorf("start: %q", text)
	}
	if _, isErr, _ := tool(t, s, "kitty_start", map[string]any{"ref": "ghost"}); !isErr {
		t.Error("starting a missing kitty fails")
	}
	// Removing needs the confirmation.
	if _, isErr, _ := tool(t, s, "kitty_remove", map[string]any{"ref": "ana"}); !isErr || len(f.kitties) != 3 {
		t.Error("remove without confirm must refuse")
	}
	if _, isErr, _ := tool(t, s, "kitty_remove", map[string]any{"ref": "ana", "confirm": true}); isErr || len(f.kitties) != 2 {
		t.Error("remove with confirm")
	}
	if _, isErr, _ := tool(t, s, "kitty_remove", map[string]any{"ref": "ana", "confirm": true}); !isErr {
		t.Error("removing twice fails")
	}
	// A lifecycle that works but whose kitty is gone afterwards still says ok.
	f.kitties["z"] = &kitty.Kitty{ID: "z", Name: "zed"}
	f.fail = errors.New("boom")
	if _, isErr, _ := tool(t, s, "kitty_stop", map[string]any{"ref": "zed"}); !isErr {
		t.Error("a failing fleet is an error result")
	}
}

func TestMCPScreenshotAndExec(t *testing.T) {
	s, f, _ := mcpServer(true)
	_, _, resp := tool(t, s, "kitty_screenshot", map[string]any{"ref": "ana"})
	c := resp["result"].(map[string]any)["content"].([]any)[0].(map[string]any)
	if c["type"] != "image" || c["mimeType"] != "image/png" || c["data"] == "" {
		t.Errorf("screenshot: %v", c)
	}
	if _, isErr, _ := tool(t, s, "kitty_screenshot", map[string]any{"ref": "ghost"}); !isErr {
		t.Error("screenshot of a missing kitty fails")
	}
	if text, isErr, _ := tool(t, s, "kitty_exec", map[string]any{"ref": "ana", "argv": []string{"echo", "hi"}}); isErr || text != "echo hi" {
		t.Errorf("exec: %q %v", text, isErr)
	}
	if _, _, resp := tool(t, s, "kitty_exec", map[string]any{"ref": "ana"}); resp["error"] == nil {
		t.Error("exec without argv is a protocol error")
	}
	f.fail = errors.New("exit 1")
	if text, isErr, _ := tool(t, s, "kitty_exec", map[string]any{"ref": "ana", "argv": []string{"false"}}); !isErr || !strings.Contains(text, "exit 1") {
		t.Errorf("failing exec: %q", text)
	}
}

func TestMCPChatTools(t *testing.T) {
	s, _, c := mcpServer(false)
	if _, _, resp := tool(t, s, "chat_create", map[string]any{"title": "x"}); resp["error"] == nil {
		t.Error("chat_create needs members")
	}
	if text, _, _ := tool(t, s, "chat_create", map[string]any{"title": "plan", "members": []string{"ana"}}); !strings.Contains(text, `"id": "c1"`) {
		t.Errorf("chat_create: %q", text)
	}
	if _, isErr, _ := tool(t, s, "chat_send", map[string]any{"id": "c1", "text": "hello"}); isErr {
		t.Error("chat_send failed")
	}
	if text, _, _ := tool(t, s, "chat_send", map[string]any{"id": "c1", "text": "hello"}); !strings.Contains(text, "re: hello") {
		t.Errorf("chat_send: %q", text)
	}
	if _, _, resp := tool(t, s, "chat_send", map[string]any{"id": "c1", "text": " "}); resp["error"] == nil {
		t.Error("empty text is a protocol error")
	}
	if _, isErr, _ := tool(t, s, "chat_send", map[string]any{"id": "nope", "text": "x"}); !isErr {
		t.Error("send to a missing chat fails")
	}
	if text, _, _ := tool(t, s, "chat_list", map[string]any{}); !strings.Contains(text, "plan") {
		t.Errorf("chat_list: %q", text)
	}
	// chat_get trims to the last turns.
	sess := c.sessions["c1"]
	sess.Turns = nil
	for i := 0; i < 15; i++ {
		sess.Turns = append(sess.Turns, chat.Turn{From: "ana", Text: "t"})
	}
	text, _, _ := tool(t, s, "chat_get", map[string]any{"id": "c1"})
	if !strings.Contains(text, `"total_turns": 15`) || strings.Count(text, `"from": "ana"`) != defaultTurns {
		t.Errorf("chat_get default: %q", text)
	}
	text, _, _ = tool(t, s, "chat_get", map[string]any{"id": "c1", "last": 20})
	if strings.Count(text, `"from": "ana"`) != 15 {
		t.Errorf("chat_get last 20: %q", text)
	}
	if _, isErr, _ := tool(t, s, "chat_get", map[string]any{"id": "nope"}); !isErr {
		t.Error("missing chat fails")
	}
	if text, _, _ := tool(t, s, "chat_cancel", map[string]any{"id": "c1"}); !strings.Contains(text, "cancelled") {
		t.Errorf("chat_cancel: %q", text)
	}
	// Wrong argument shapes are protocol errors.
	if _, _, resp := tool(t, s, "kitty_get", "not an object"); resp["error"] == nil {
		t.Error("bad arguments")
	}
}

// computeFleet is a powerFleet that also says where desktops run.
type computeFleet struct{ *powerFleet }

func (computeFleet) Compute(ref string) (string, error) {
	if ref == "bo" {
		return "", kitty.ErrNoMachine
	}
	return "cloud", nil
}

func TestMCPMachineTools(t *testing.T) {
	f := &fakeFleet{kitties: map[string]*kitty.Kitty{"a1": {ID: "a1", Name: "ana", Desktop: true, Status: kitty.StatusRunning}}}
	chats := &fakeChats{sessions: map[string]*chat.Session{}}
	// A fleet that knows neither compute nor power says so.
	plain := New(f, chats, "gw", "test").WithMCP(mcpToken, false)
	if _, isErr, _ := tool(t, plain, "kitty_compute", map[string]any{"ref": "ana"}); !isErr {
		t.Error("compute without support")
	}
	if _, isErr, _ := tool(t, plain, "kitty_power", map[string]any{"ref": "ana", "on": true}); !isErr {
		t.Error("power without support")
	}
	p := &powerFleet{fakeFleet: f, power: kitty.Power{State: kitty.PowerOn}}
	s := New(computeFleet{p}, chats, "gw", "test").WithMCP(mcpToken, false)
	text, _, _ := tool(t, s, "kitty_compute", map[string]any{"ref": "ana"})
	if !strings.Contains(text, `"compute": "cloud"`) || !strings.Contains(text, `"state": "on"`) {
		t.Errorf("kitty_compute: %q", text)
	}
	if _, isErr, _ := tool(t, s, "kitty_compute", map[string]any{"ref": "bo"}); !isErr {
		t.Error("no machine")
	}
	if _, _, resp := tool(t, s, "kitty_power", map[string]any{"ref": "ana"}); resp["error"] == nil {
		t.Error("on is required")
	}
	if text, _, _ = tool(t, s, "kitty_power", map[string]any{"ref": "ana", "on": true}); !strings.Contains(text, "starting") || len(p.ons) != 1 {
		t.Errorf("power on: %q %v", text, p.ons)
	}
	p.inUse = true
	if _, isErr, _ := tool(t, s, "kitty_power", map[string]any{"ref": "ana", "on": false}); !isErr {
		t.Error("off while in use must fail")
	}
	if text, _, _ = tool(t, s, "kitty_power", map[string]any{"ref": "ana", "on": false, "force": true}); !strings.Contains(text, "off") || len(p.offs) != 1 || !p.offs[0].Force {
		t.Errorf("forced off: %q %v", text, p.offs)
	}
	if _, isErr, _ := tool(t, s, "kitty_power", map[string]any{"ref": "bo", "on": true}); !isErr {
		t.Error("power of a missing machine")
	}
}

func TestMCPApprovalsAndGoals(t *testing.T) {
	s, _, _ := mcpServer(false)
	// Nothing wired: the tools say so.
	for _, c := range []struct {
		name string
		args map[string]any
	}{
		{"approval_list", map[string]any{}}, {"approval_reject", map[string]any{"id": "x"}},
		{"goal_list", map[string]any{}}, {"goal_add", map[string]any{"kitty": "ana", "title": "t"}},
		{"goal_run", map[string]any{"id": "g1"}}, {"goal_remove", map[string]any{"id": "g1"}},
		{"kitty_journal", map[string]any{"ref": "ana"}},
	} {
		if _, isErr, _ := tool(t, s, c.name, c.args); !isErr {
			t.Errorf("%s without its service", c.name)
		}
	}
	ap := &fakeApprover{list: []approvals.Approval{{ID: "p1", Status: approvals.Pending, Kitty: "a1"}}}
	ag := newFakeAgenda()
	s.WithApprovals(ap).WithAgenda(ag)
	if text, _, _ := tool(t, s, "approval_list", map[string]any{"status": "pending"}); !strings.Contains(text, "p1") {
		t.Errorf("approval_list: %q", text)
	}
	if _, isErr, _ := tool(t, s, "approval_reject", map[string]any{"id": "ghost"}); !isErr {
		t.Error("unknown approval")
	}
	if text, _, _ := tool(t, s, "approval_reject", map[string]any{"id": "p1", "reason": "no"}); !strings.Contains(text, "rejected") {
		t.Errorf("approval_reject: %q", text)
	}
	// There is no tool to approve.
	resp, _ := rpc(s, "tools/list", nil)
	if strings.Contains(string(mustJSON(resp)), "approval_approve") {
		t.Error("approving is the person's")
	}
	text, _, _ := tool(t, s, "goal_add", map[string]any{"kitty": "ana", "title": "Inbox", "every": "@hourly"})
	if !strings.Contains(text, `"id": "g1"`) {
		t.Fatalf("goal_add: %q", text)
	}
	if _, isErr, _ := tool(t, s, "goal_add", map[string]any{"kitty": "ghost", "title": "t"}); !isErr {
		t.Error("goal for a missing kitty")
	}
	if _, isErr, _ := tool(t, s, "goal_add", map[string]any{"kitty": "ana", "title": "t", "on": []string{"nope"}}); !isErr {
		t.Error("goal on an unknown connector")
	}
	if _, isErr, _ := tool(t, s, "goal_add", map[string]any{"kitty": "ana", "title": "t", "every": "bad cron"}); !isErr {
		t.Error("bad schedule")
	}
	if text, _, _ = tool(t, s, "goal_list", map[string]any{"kitty": "ana"}); !strings.Contains(text, "Inbox") {
		t.Errorf("goal_list: %q", text)
	}
	if text, _, _ = tool(t, s, "goal_list", map[string]any{}); !strings.Contains(text, "Inbox") {
		t.Errorf("goal_list all: %q", text)
	}
	if _, isErr, _ := tool(t, s, "goal_list", map[string]any{"kitty": "ghost"}); !isErr {
		t.Error("goals of a missing kitty")
	}
	if text, _, _ = tool(t, s, "goal_run", map[string]any{"id": "g1"}); !strings.Contains(text, "chat-g1") {
		t.Errorf("goal_run: %q", text)
	}
	if _, isErr, _ := tool(t, s, "goal_run", map[string]any{"id": "zz"}); !isErr {
		t.Error("run a missing goal")
	}
	ag.entries["a1"] = []agenda.Entry{{}, {}, {}}
	if text, _, _ = tool(t, s, "kitty_journal", map[string]any{"ref": "ana", "n": 2}); strings.Count(text, "{") != 3 {
		t.Errorf("kitty_journal: %q", text)
	}
	if text, _, _ = tool(t, s, "kitty_journal", map[string]any{"ref": "ana", "n": 9999}); !strings.Contains(text, "journal") {
		t.Errorf("journal default: %q", text)
	}
	if text, _, _ = tool(t, s, "kitty_journal", map[string]any{"ref": "bob"}); text == "" {
		t.Error("journal of a missing kitty")
	}
	if text, _, _ = tool(t, s, "goal_remove", map[string]any{"id": "g1"}); !strings.Contains(text, "removed") {
		t.Errorf("goal_remove: %q", text)
	}
	if _, isErr, _ := tool(t, s, "goal_remove", map[string]any{"id": "g1"}); !isErr {
		t.Error("remove twice")
	}
	// An approval service that holds nothing answers with an error result.
	s.WithApprovals(&fakeApprover{none: true})
	if _, isErr, _ := tool(t, s, "approval_list", map[string]any{}); !isErr {
		t.Error("approvals off")
	}
	// An empty journal is a list, not null.
	ag.entries = map[string][]agenda.Entry{}
	if text, _, _ = tool(t, s, "kitty_journal", map[string]any{"ref": "ana"}); !strings.Contains(text, "[]") {
		t.Errorf("empty journal: %q", text)
	}
}

func TestMCPOAuth(t *testing.T) {
	f := &fakeFleet{kitties: map[string]*kitty.Kitty{"a1": {ID: "a1", Name: "ana", Status: kitty.StatusRunning}}}
	c := &fakeChats{sessions: map[string]*chat.Session{}}
	s := New(f, c, "gw-token", "test").WithPublicURL("https://k.example/").WithMCPOAuth(mcpToken, false, true)
	// The 401 points to the metadata, which names this gateway.
	rec := do(s, "POST", "/mcp", `{}`, nil)
	if rec.Code != 401 || !strings.Contains(rec.Header().Get("WWW-Authenticate"), "https://k.example/.well-known/oauth-protected-resource") {
		t.Fatalf("challenge: %d %q", rec.Code, rec.Header().Get("WWW-Authenticate"))
	}
	rec = do(s, "GET", "/.well-known/oauth-authorization-server", "", nil)
	if !strings.Contains(rec.Body.String(), "https://k.example/oauth/token") {
		t.Fatalf("metadata: %s", rec.Body)
	}
	// Register, approve with the connector's token, trade the code (PKCE) and call a tool.
	rec = do(s, "POST", "/oauth/register", `{"client_name":"Claude","redirect_uris":["https://claude.ai/api/mcp/auth_callback"]}`, nil)
	var reg map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &reg)
	id := reg["client_id"].(string)
	verifier := "verifier-verifier-verifier-verifier-1234"
	sum := sha256.Sum256([]byte(verifier))
	form := url.Values{"client_id": {id}, "redirect_uri": {"https://claude.ai/api/mcp/auth_callback"}, "response_type": {"code"}, "state": {"s"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}, "token": {mcpToken}}
	post := func(path string, v url.Values) *httptest.ResponseRecorder {
		return do(s, "POST", path, v.Encode(), func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") })
	}
	// A wrong token on the consent page counts against the client, like any wrong token.
	bad := url.Values{}
	for k, v := range form {
		bad[k] = v
	}
	bad.Set("token", "nope")
	if rec = post("/oauth/authorize", bad); !strings.Contains(rec.Body.String(), "not the connector") {
		t.Errorf("wrong token: %d", rec.Code)
	}
	rec = post("/oauth/authorize", form)
	loc, _ := url.Parse(rec.Header().Get("Location"))
	code := loc.Query().Get("code")
	if rec.Code != 302 || code == "" {
		t.Fatalf("authorize: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = post("/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "code": {code},
		"redirect_uri": {"https://claude.ai/api/mcp/auth_callback"}, "code_verifier": {verifier}})
	var tok map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &tok)
	access, _ := tok["access_token"].(string)
	if rec.Code != 200 || access == "" {
		t.Fatalf("token: %d %s", rec.Code, rec.Body)
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	if rec = do(s, "POST", "/mcp", body, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+access) }); rec.Code != 200 {
		t.Errorf("with the access token: %d", rec.Code)
	}
	// The gateway's own token still does not open it, and OAuth off means no endpoints.
	if rec = do(s, "POST", "/mcp", body, func(r *http.Request) { r.Header.Set("Authorization", "Bearer gw-token") }); rec.Code != 401 {
		t.Errorf("gateway token: %d", rec.Code)
	}
	plain, _, _ := mcpServer(false)
	if rec = do(plain, "GET", "/.well-known/oauth-authorization-server", "", nil); rec.Code == 200 && strings.Contains(rec.Body.String(), "issuer") {
		t.Error("oauth is off by default")
	}
	// Without a public address the issuer is what the client typed, https when a proxy says so.
	s2 := New(f, c, "gw", "test").WithProxy(true).WithMCPOAuth(mcpToken, false, true)
	rec = do(s2, "GET", "/.well-known/oauth-protected-resource", "", func(r *http.Request) {
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-Host", "kitties.example.com")
	})
	if !strings.Contains(rec.Body.String(), "https://kitties.example.com/mcp") {
		t.Errorf("issuer from the request: %s", rec.Body)
	}
	rec = do(s2, "GET", "/.well-known/oauth-protected-resource", "", nil)
	if !strings.Contains(rec.Body.String(), "http://") {
		t.Errorf("plain http issuer: %s", rec.Body)
	}
}

func TestMCPClientsAndApprove(t *testing.T) {
	f := &fakeFleet{kitties: map[string]*kitty.Kitty{"a1": {ID: "a1", Name: "ana", Status: kitty.StatusRunning}}}
	c := &fakeChats{sessions: map[string]*chat.Session{}}
	ap := &fakeApprover{list: []approvals.Approval{{ID: "p1", Status: approvals.Pending}, {ID: "p2", Status: approvals.Pending}}}
	s := New(f, c, "gw", "test").WithApprovals(ap).WithMCPOptions(mcpToken, MCPOptions{OAuth: true, Approve: true, ClientsFile: t.TempDir() + "/c.json"})
	// Approving is offered only when the person allowed it, and only this once.
	if text, isErr, _ := tool(t, s, "approval_approve", map[string]any{"id": "p1"}); isErr || !strings.Contains(text, "approved") || ap.list[0].Always {
		t.Errorf("approve: %q %v", text, isErr)
	}
	if _, isErr, _ := tool(t, s, "approval_approve", map[string]any{"id": "p1"}); !isErr {
		t.Error("approving twice fails")
	}
	if _, _, resp := tool(t, s, "approval_approve", map[string]any{}); resp["error"] == nil {
		t.Error("an id is required")
	}
	bare := New(f, c, "gw", "test").WithMCPOptions(mcpToken, MCPOptions{Approve: true})
	if _, isErr, _ := tool(t, bare, "approval_approve", map[string]any{"id": "p2"}); !isErr {
		t.Error("without an approver")
	}
	// The clients are listed and revoked with the gateway's token, never the connector's.
	if rec := do(s, "GET", "/v1/mcp/clients", "", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+mcpToken) }); rec.Code != 401 {
		t.Errorf("connector token on the clients list: %d", rec.Code)
	}
	gw := func(r *http.Request) { r.Header.Set("Authorization", "Bearer gw") }
	if rec := do(s, "GET", "/v1/mcp/clients", "", gw); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"clients":[]`) {
		t.Errorf("empty list: %d %s", rec.Code, rec.Body)
	}
	if rec := do(s, "DELETE", "/v1/mcp/clients/zzz", "", gw); rec.Code != 404 {
		t.Errorf("unknown client: %d", rec.Code)
	}
	// Sign a client in, revoke it, and its token stops opening /mcp.
	rec := do(s, "POST", "/oauth/register", `{"client_name":"Claude","redirect_uris":["https://claude.ai/cb"]}`, nil)
	var reg map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &reg)
	id := reg["client_id"].(string)
	verifier := "verifier-verifier-verifier-verifier-1234"
	sum := sha256.Sum256([]byte(verifier))
	form := url.Values{"client_id": {id}, "redirect_uri": {"https://claude.ai/cb"}, "response_type": {"code"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"}, "token": {mcpToken}}
	formPost := func(path string, v url.Values) *httptest.ResponseRecorder {
		return do(s, "POST", path, v.Encode(), func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") })
	}
	loc, _ := url.Parse(formPost("/oauth/authorize", form).Header().Get("Location"))
	rec = formPost("/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "code": {loc.Query().Get("code")},
		"redirect_uri": {"https://claude.ai/cb"}, "code_verifier": {verifier}})
	var tok map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &tok)
	access := tok["access_token"].(string)
	rec = do(s, "GET", "/v1/mcp/clients", "", gw)
	var list struct {
		Clients []struct {
			ID string `json:"id"`
		} `json:"clients"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Clients) != 1 {
		t.Fatalf("clients: %s", rec.Body)
	}
	ping := func() int {
		return do(s, "POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+access) }).Code
	}
	if ping() != 200 {
		t.Fatal("the client's token works")
	}
	if rec = do(s, "DELETE", "/v1/mcp/clients/"+list.Clients[0].ID, "", gw); rec.Code != 200 || ping() != 401 {
		t.Errorf("revoke: %d, ping %d", rec.Code, ping())
	}
	// A revoke that cannot be kept (the file cannot be written) is an error, not a silent success.
	bad := New(f, c, "gw", "test").WithMCPOptions(mcpToken, MCPOptions{OAuth: true, ClientsFile: t.TempDir() + "/no/dir/c.json"})
	rec = do(bad, "POST", "/oauth/register", `{"redirect_uris":["https://claude.ai/cb"]}`, nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &reg)
	badForm := url.Values{"client_id": {reg["client_id"].(string)}, "redirect_uri": {"https://claude.ai/cb"}, "response_type": {"code"},
		"code_challenge": {"x"}, "code_challenge_method": {"S256"}, "token": {mcpToken}}
	do(bad, "POST", "/oauth/authorize", badForm.Encode(), func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") })
	rec = do(bad, "GET", "/v1/mcp/clients", "", gw)
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Clients) != 1 {
		t.Fatalf("bad store clients: %s", rec.Body)
	}
	if rec = do(bad, "DELETE", "/v1/mcp/clients/"+list.Clients[0].ID, "", func(r *http.Request) { r.Header.Set("Authorization", "Bearer gw") }); rec.Code != 500 {
		t.Errorf("unwritable store: %d", rec.Code)
	}
}
