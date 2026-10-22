package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/connector"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/orchestrator"
	"github.com/madkoding/kitties/internal/provider"
)

type fakeFleet struct {
	kitties map[string]*kitty.Kitty
	fail    error
	calls   []string
}

func (f *fakeFleet) List() []*kitty.Kitty {
	var out []*kitty.Kitty
	for _, k := range f.kitties {
		out = append(out, k)
	}
	return out
}
func (f *fakeFleet) Get(ref string) (*kitty.Kitty, error) {
	for _, k := range f.kitties {
		if k.ID == ref || k.Name == ref {
			return k, nil
		}
	}
	return nil, orchestrator.ErrNotFound
}
func (f *fakeFleet) Create(_ context.Context, s orchestrator.Spec) (*kitty.Kitty, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	if _, err := f.Get(s.Name); err == nil {
		return nil, errors.New("a kitty called x already exists")
	}
	k := &kitty.Kitty{ID: "id-" + s.Name, Name: s.Name, Status: kitty.StatusCreated, Desktop: true}
	f.kitties[k.ID] = k
	return k, nil
}
func (f *fakeFleet) op(name, ref string, status string) error {
	f.calls = append(f.calls, name+":"+ref)
	if f.fail != nil {
		return f.fail
	}
	k, err := f.Get(ref)
	if err != nil {
		return err
	}
	if status == "" {
		delete(f.kitties, k.ID)
	} else {
		k.Status = status
	}
	return nil
}
func (f *fakeFleet) Start(_ context.Context, ref string) error {
	return f.op("start", ref, kitty.StatusRunning)
}
func (f *fakeFleet) Stop(_ context.Context, ref string) error {
	return f.op("stop", ref, kitty.StatusStopped)
}
func (f *fakeFleet) Remove(_ context.Context, ref string) error { return f.op("rm", ref, "") }
func (f *fakeFleet) Scale(_ context.Context, name string, n int) error {
	if name != "team" {
		return errors.New("\"" + name + "\" is not a kitty of the configuration")
	}
	return f.fail
}
func (f *fakeFleet) Exec(_ context.Context, ref string, argv ...string) (string, error) {
	if _, err := f.Get(ref); err != nil {
		return "", err
	}
	if f.fail != nil {
		return "partial", f.fail
	}
	return strings.Join(argv, " "), nil
}
func (f *fakeFleet) Screenshot(_ context.Context, ref string) ([]byte, error) {
	if _, err := f.Get(ref); err != nil {
		return nil, err
	}
	return []byte("png"), nil
}
func (f *fakeFleet) Providers() *provider.Registry {
	r := provider.NewRegistry()
	r.Add("p", provider.Func(nil), provider.Options{Kind: "ollama"})
	return r
}
func (f *fakeFleet) Connectors() *connector.Registry {
	r := connector.NewRegistry()
	r.Add("w", connector.Webhook{URL: "x"})
	return r
}
func (f *fakeFleet) HasContainers() (bool, error) { return false, errors.New("no docker") }

type fakeChats struct {
	sessions map[string]*chat.Session
	fail     error
}

func (c *fakeChats) Create(title string, members []string) (*chat.Session, error) {
	if len(members) == 0 {
		return nil, errors.New("a chat needs at least one kitty")
	}
	s := &chat.Session{ID: "c1", Title: title, Members: members}
	c.sessions[s.ID] = s
	return s, nil
}
func (c *fakeChats) Get(id string) (*chat.Session, error) {
	s, ok := c.sessions[id]
	if !ok {
		return nil, errors.New("no such chat")
	}
	return s, nil
}
func (c *fakeChats) List() []*chat.Session {
	var out []*chat.Session
	for _, s := range c.sessions {
		out = append(out, s)
	}
	return out
}
func (c *fakeChats) Delete(id string) error {
	if _, ok := c.sessions[id]; !ok {
		return errors.New("no such chat")
	}
	delete(c.sessions, id)
	return nil
}
func (c *fakeChats) AddMembers(id string, members []string) (*chat.Session, error) {
	s, err := c.Get(id)
	if err != nil {
		return nil, err
	}
	s.Members = append(s.Members, members...)
	return s, nil
}
func (c *fakeChats) RemoveMember(id, ref string) (*chat.Session, error) {
	s, err := c.Get(id)
	if err != nil {
		return nil, err
	}
	s.Members = nil
	return s, nil
}
func (c *fakeChats) Send(_ context.Context, id, text string) ([]chat.Turn, error) {
	if _, err := c.Get(id); err != nil {
		return nil, err
	}
	if text == "" {
		return nil, errors.New("the message is empty")
	}
	if c.fail != nil {
		return nil, c.fail
	}
	return []chat.Turn{{From: "ana", Text: "re: " + text}}, nil
}

func setup(t *testing.T) (*httptest.Server, *fakeFleet, *fakeChats) {
	t.Helper()
	f := &fakeFleet{kitties: map[string]*kitty.Kitty{"a1": {ID: "a1", Name: "ana", Status: kitty.StatusRunning}}}
	c := &fakeChats{sessions: map[string]*chat.Session{}}
	srv := httptest.NewServer(New(f, c, "tok", "test"))
	t.Cleanup(srv.Close)
	return srv, f, c
}

func call(t *testing.T, srv *httptest.Server, method, path, token string, body any) (int, map[string]any, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if s, ok := body.(string); ok {
		buf.WriteString(s)
	} else if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, srv.URL+path, &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	return resp.StatusCode, m, data
}

func TestAuthAndPublicRoutes(t *testing.T) {
	srv, _, _ := setup(t)
	if code, _, data := call(t, srv, "GET", "/", "", nil); code != 200 || !strings.Contains(string(data), "<title>Kitties</title>") {
		t.Errorf("index = %d", code)
	}
	if code, _, _ := call(t, srv, "GET", "/nope", "", nil); code != 404 {
		t.Error("unknown path")
	}
	if code, m, _ := call(t, srv, "GET", "/v1/health", "", nil); code != 200 || m["ok"] != true || m["containers_error"] != "no docker" || m["version"] != "test" {
		t.Errorf("health = %d %v", code, m)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/kitties", "", nil); code != 401 {
		t.Error("no token")
	}
	if code, _, _ := call(t, srv, "GET", "/v1/kitties", "wrong", nil); code != 401 {
		t.Error("wrong token")
	}
	if code, _, _ := call(t, srv, "GET", "/v1/kitties?token=tok", "", nil); code != 200 {
		t.Error("query token")
	}
	empty := httptest.NewServer(New(&fakeFleet{kitties: map[string]*kitty.Kitty{}}, &fakeChats{}, "", ""))
	defer empty.Close()
	if code, _, _ := call(t, empty, "GET", "/v1/kitties", "", nil); code != 401 {
		t.Error("an empty token refuses everything")
	}
	if len(NewToken()) != 32 || NewToken() == NewToken() {
		t.Error("NewToken")
	}
}

func TestKittyRoutes(t *testing.T) {
	srv, f, _ := setup(t)
	code, m, _ := call(t, srv, "GET", "/v1/providers", "tok", nil)
	if code != 200 || len(m["kinds"].([]any)) == 0 || len(m["providers"].([]any)) != 1 {
		t.Errorf("providers = %v", m)
	}
	if code, m, _ := call(t, srv, "GET", "/v1/connectors", "tok", nil); code != 200 || m["connectors"].(map[string]any)["w"] == nil {
		t.Errorf("connectors = %v", m)
	}
	if code, m, _ := call(t, srv, "GET", "/v1/kitties", "tok", nil); code != 200 || len(m["kitties"].([]any)) != 1 {
		t.Errorf("list = %v", m)
	}
	if code, m, _ := call(t, srv, "GET", "/v1/kitties/ana", "tok", nil); code != 200 || m["kitty"].(map[string]any)["id"] != "a1" {
		t.Errorf("get = %v", m)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/kitties/zz", "tok", nil); code != 404 {
		t.Error("get unknown")
	}
	code, m, _ = call(t, srv, "POST", "/v1/kitties", "tok", map[string]any{"name": "bo"})
	if code != 201 || m["kitty"].(map[string]any)["status"] != "running" {
		t.Errorf("create+start = %d %v", code, m)
	}
	no := false
	if code, m, _ := call(t, srv, "POST", "/v1/kitties", "tok", map[string]any{"name": "cy", "start": no}); code != 201 || m["kitty"].(map[string]any)["status"] != "created" {
		t.Errorf("create without start = %d %v", code, m)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/kitties", "tok", map[string]any{"name": "bo"}); code != 400 {
		t.Error("duplicate")
	}
	if code, _, _ := call(t, srv, "POST", "/v1/kitties", "tok", "{bad"); code != 400 {
		t.Error("bad json")
	}
	if code, m, _ := call(t, srv, "POST", "/v1/kitties/bo/stop", "tok", nil); code != 200 || m["kitty"].(map[string]any)["status"] != "stopped" {
		t.Errorf("stop = %v", m)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/kitties/bo/start", "tok", nil); code != 200 {
		t.Error("start")
	}
	if code, _, _ := call(t, srv, "POST", "/v1/kitties/zz/start", "tok", nil); code != 404 {
		t.Error("start unknown")
	}
	if code, m, _ := call(t, srv, "DELETE", "/v1/kitties/bo", "tok", nil); code != 200 || m["ok"] != true {
		t.Errorf("remove = %v", m)
	}
	if code, m, _ := call(t, srv, "POST", "/v1/kitties/ana/exec", "tok", map[string]any{"argv": []string{"ls", "-la"}}); code != 200 || m["output"] != "ls -la" {
		t.Errorf("exec = %v", m)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/kitties/ana/exec", "tok", "nope"); code != 400 {
		t.Error("exec bad body")
	}
	if code, _, _ := call(t, srv, "POST", "/v1/kitties/zz/exec", "tok", nil); code != 404 {
		t.Error("exec unknown")
	}
	if code, _, data := call(t, srv, "GET", "/v1/kitties/ana/screenshot", "tok", nil); code != 200 || string(data) != "png" {
		t.Error("screenshot")
	}
	if code, _, _ := call(t, srv, "GET", "/v1/kitties/zz/screenshot", "tok", nil); code != 404 {
		t.Error("screenshot unknown")
	}
	if code, _, _ := call(t, srv, "POST", "/v1/scale/team", "tok", map[string]any{"replicas": 2}); code != 200 {
		t.Error("scale")
	}
	if code, _, _ := call(t, srv, "POST", "/v1/scale/zz", "tok", map[string]any{"replicas": 2}); code != 400 {
		t.Error("scale unknown")
	}
	if code, _, _ := call(t, srv, "POST", "/v1/scale/team", "tok", "x"); code != 400 {
		t.Error("scale bad body")
	}
	f.fail = errors.New("docker exploded")
	if code, m, _ := call(t, srv, "POST", "/v1/kitties", "tok", map[string]any{"name": "dd"}); code != 500 || m["error"] != "docker exploded" {
		t.Errorf("create failure = %d %v", code, m)
	}
	if code, m, _ := call(t, srv, "POST", "/v1/kitties/ana/exec", "tok", map[string]any{"argv": []string{"x"}}); code != 500 || m["output"] != "partial" {
		t.Errorf("exec failure = %v", m)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/scale/team", "tok", nil); code != 500 {
		t.Error("scale failure")
	}
	f.fail = nil
	// Created but the start fails: the kitty exists and the answer says so.
	starting := &fakeFleet{kitties: map[string]*kitty.Kitty{}}
	s2 := httptest.NewServer(New(&startFails{starting}, &fakeChats{}, "tok", ""))
	defer s2.Close()
	if code, m, _ := call(t, s2, "POST", "/v1/kitties", "tok", map[string]any{"name": "ee"}); code != 201 || !strings.Contains(m["warning"].(string), "not started") {
		t.Errorf("start failure after create = %d %v", code, m)
	}
}

type startFails struct{ *fakeFleet }

func (s *startFails) Start(context.Context, string) error { return errors.New("no runtime") }

func TestChatRoutes(t *testing.T) {
	srv, _, c := setup(t)
	if code, _, _ := call(t, srv, "POST", "/v1/chats", "tok", map[string]any{}); code != 400 {
		t.Error("no members")
	}
	if code, _, _ := call(t, srv, "POST", "/v1/chats", "tok", "{"); code != 400 {
		t.Error("bad json")
	}
	code, m, _ := call(t, srv, "POST", "/v1/chats", "tok", map[string]any{"title": "t", "members": []string{"ana"}})
	if code != 201 || m["chat"].(map[string]any)["id"] != "c1" {
		t.Errorf("create = %d %v", code, m)
	}
	if code, m, _ := call(t, srv, "GET", "/v1/chats", "tok", nil); code != 200 || len(m["chats"].([]any)) != 1 {
		t.Errorf("list = %v", m)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/chats/c1", "tok", nil); code != 200 {
		t.Error("get")
	}
	if code, _, _ := call(t, srv, "GET", "/v1/chats/zz", "tok", nil); code != 404 {
		t.Error("get unknown")
	}
	if code, m, _ := call(t, srv, "POST", "/v1/chats/c1/members", "tok", map[string]any{"members": []string{"bo"}}); code != 200 || len(m["chat"].(map[string]any)["members"].([]any)) != 2 {
		t.Errorf("add members = %v", m)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/chats/c1/members", "tok", "{"); code != 400 {
		t.Error("add members bad json")
	}
	if code, _, _ := call(t, srv, "POST", "/v1/chats/zz/members", "tok", nil); code != 404 {
		t.Error("add members unknown")
	}
	if code, _, _ := call(t, srv, "DELETE", "/v1/chats/c1/members/bo", "tok", nil); code != 200 {
		t.Error("remove member")
	}
	if code, _, _ := call(t, srv, "DELETE", "/v1/chats/zz/members/bo", "tok", nil); code != 404 {
		t.Error("remove member unknown")
	}
	if code, m, _ := call(t, srv, "POST", "/v1/chats/c1/messages", "tok", map[string]any{"text": "hi"}); code != 200 || m["turns"].([]any)[0].(map[string]any)["text"] != "re: hi" {
		t.Errorf("send = %v", m)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/chats/c1/messages", "tok", map[string]any{}); code != 400 {
		t.Error("send empty")
	}
	if code, _, _ := call(t, srv, "POST", "/v1/chats/c1/messages", "tok", "{"); code != 400 {
		t.Error("send bad json")
	}
	c.fail = errors.New("brain down")
	if code, _, _ := call(t, srv, "POST", "/v1/chats/c1/messages", "tok", map[string]any{"text": "hi"}); code != 500 {
		t.Error("send failure")
	}
	if code, _, _ := call(t, srv, "DELETE", "/v1/chats/c1", "tok", nil); code != 200 {
		t.Error("delete")
	}
	if code, _, _ := call(t, srv, "DELETE", "/v1/chats/c1", "tok", nil); code != 404 {
		t.Error("delete unknown")
	}
}

func TestListen(t *testing.T) {
	f := &fakeFleet{kitties: map[string]*kitty.Kitty{}}
	s := New(f, &fakeChats{}, "tok", "")
	ctx, cancel := context.WithCancel(context.Background())
	var lines []string
	logf := func(format string, args ...any) { lines = append(lines, strings.ToLower(format)) }
	done := make(chan error, 1)
	go func() { done <- s.Listen(ctx, "127.0.0.1:0", logf) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Error(err)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "listening") {
		t.Errorf("logs = %v", lines)
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	if err := s.Listen(context.Background(), ln.Addr().String(), logf); err == nil {
		t.Error("a busy port fails")
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	lines = nil
	go func() { done <- s.Listen(ctx2, "0.0.0.0:0", logf) }()
	time.Sleep(50 * time.Millisecond)
	cancel2()
	<-done
	if len(lines) != 2 || !strings.Contains(lines[0], "warning") {
		t.Errorf("a network address warns: %v", lines)
	}
}

func TestCodeFor(t *testing.T) {
	cases := map[string]int{"no such chat": 404, "x already exists": 400, "weird": 500, "unknown provider": 400, "the message is empty": 400}
	for msg, want := range cases {
		if got := codeFor(errors.New(msg)); got != want {
			t.Errorf("%q = %d, want %d", msg, got, want)
		}
	}
	if codeFor(orchestrator.ErrNotFound) != 404 {
		t.Error("ErrNotFound")
	}
}
