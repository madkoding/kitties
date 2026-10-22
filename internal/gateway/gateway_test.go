package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/artifacts"
	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/config"
	"github.com/madkoding/kitties/internal/connector"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/oauth"
	"github.com/madkoding/kitties/internal/orchestrator"
	"github.com/madkoding/kitties/internal/provider"
	"github.com/madkoding/kitties/internal/skills"
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
func (f *fakeFleet) Edit(ref string, u orchestrator.Update) (*kitty.Kitty, error) {
	k, err := f.Get(ref)
	if err != nil {
		return nil, err
	}
	if u.Avatar != nil {
		a, err := u.Avatar.Normalize(k.Name)
		if err != nil {
			return nil, err
		}
		k.Avatar = a
	}
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
func (f *fakeFleet) Settings() orchestrator.SettingsView {
	return orchestrator.SettingsView{Providers: []orchestrator.ProviderView{{Name: "p", Kind: "ollama"}}, Connectors: []orchestrator.ConnectorView{}, DefaultProvider: "p"}
}
func (f *fakeFleet) SetProvider(name string, p config.Provider) error {
	return config.ValidateProvider(name, p)
}
func (f *fakeFleet) RemoveProvider(name string) error {
	if name != "p" {
		return orchestrator.ErrNotFound
	}
	return nil
}
func (f *fakeFleet) SetDefaultProvider(name string) error { return f.RemoveProvider(name) }
func (f *fakeFleet) TestProvider(_ context.Context, name string) (string, error) {
	if name != "p" {
		return "", errors.New("no such provider")
	}
	return "hello", nil
}
func (f *fakeFleet) SetConnector(name string, c config.Connector) error {
	return config.ValidateConnector(name, c, "")
}
func (f *fakeFleet) RemoveConnector(name string) error {
	if name != "c" {
		return orchestrator.ErrNotFound
	}
	return nil
}

// oauthStore is a fleet's OAuth side for the tests: one connector "g" with a Google app.
type oauthStore struct{ creds oauth.Credentials }

func (o *oauthStore) OAuthApp(name string) (string, string, string, error) {
	if name != "g" {
		return "", "", "", errors.New("connector \"x\" does not sign in with OAuth")
	}
	return "google", "cid", "sec", nil
}
func (o *oauthStore) OAuthCredentials(string) (oauth.Credentials, error) { return o.creds, nil }
func (o *oauthStore) SaveOAuthCredentials(_ string, c oauth.Credentials) error {
	o.creds = c
	return nil
}

var sharedOAuth = oauth.New(&oauthStore{})

func (f *fakeFleet) OAuth() *oauth.Manager { return sharedOAuth }
func (f *fakeFleet) InboundSecret(name string) (string, error) {
	switch name {
	case "gh":
		return "shh", nil
	case "sl":
		return "", nil
	}
	return "", fmt.Errorf("%w: connector %q", orchestrator.ErrNotFound, name)
}

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
func (c *fakeChats) Cancel(id string) bool { return id == "c1" }
func (c *fakeChats) Subscribe(id string) (<-chan chat.Event, func()) {
	ch := make(chan chat.Event, 4)
	ch <- chat.Event{Type: "turn", Session: id, Index: 0, Turn: &chat.Turn{From: "user", Text: "hi"}}
	ch <- chat.Event{Type: "deleted", Session: id}
	return ch, func() {}
}
func (c *fakeChats) Shot(id, name string) ([]byte, error) {
	if id != "c1" || name != "1-1.png" {
		return nil, errors.New("no such screenshot")
	}
	return []byte("png"), nil
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
	if code, m, _ := call(t, srv, "GET", "/v1/avatars", "tok", nil); code != 200 || len(m["breeds"].([]any)) != 5 || len(m["colors"].([]any)) != 10 {
		t.Errorf("avatars = %v", m)
	}
	if code, m, _ := call(t, srv, "PATCH", "/v1/kitties/ana", "tok", map[string]any{"avatar": map[string]any{"breed": "fluffy", "color": "mint"}}); code != 200 || m["kitty"].(map[string]any)["avatar"].(map[string]any)["breed"] != "fluffy" {
		t.Errorf("edit = %d %v", code, m)
	}
	if code, _, _ := call(t, srv, "PATCH", "/v1/kitties/ana", "tok", map[string]any{"avatar": map[string]any{"breed": "lion"}}); code != 400 {
		t.Error("edit bad avatar")
	}
	if code, _, _ := call(t, srv, "PATCH", "/v1/kitties/ana", "tok", "{"); code != 400 {
		t.Error("edit bad json")
	}
	if code, _, _ := call(t, srv, "PATCH", "/v1/kitties/zz", "tok", map[string]any{}); code != 404 {
		t.Error("edit unknown")
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
	c.fail = chat.ErrBusy
	if code, _, _ := call(t, srv, "POST", "/v1/chats/c1/messages", "tok", map[string]any{"text": "hi"}); code != 409 {
		t.Error("busy is a conflict")
	}
	if code, m, _ := call(t, srv, "POST", "/v1/chats/c1/cancel", "tok", nil); code != 200 || m["cancelled"] != true {
		t.Errorf("cancel = %v", m)
	}
	if code, _, data := call(t, srv, "GET", "/v1/chats/c1/shots/1-1.png", "tok", nil); code != 200 || string(data) != "png" {
		t.Error("shot")
	}
	if code, _, _ := call(t, srv, "GET", "/v1/chats/c1/shots/9-9.png", "tok", nil); code != 404 {
		t.Error("shot unknown")
	}
	if code, _, _ := call(t, srv, "DELETE", "/v1/chats/c1", "tok", nil); code != 200 {
		t.Error("delete")
	}
	if code, _, _ := call(t, srv, "DELETE", "/v1/chats/c1", "tok", nil); code != 404 {
		t.Error("delete unknown")
	}
}

func TestSettingsRoutes(t *testing.T) {
	srv, _, _ := setup(t)
	if code, m, _ := call(t, srv, "GET", "/v1/settings", "tok", nil); code != 200 || m["default_provider"] != "p" {
		t.Errorf("settings = %v", m)
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/settings/providers/fast", "tok", map[string]any{"kind": "openai", "model": "m"}); code != 200 {
		t.Error("set provider")
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/settings/providers/fast", "tok", map[string]any{"kind": "nope"}); code != 400 {
		t.Error("bad kind is 400")
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/settings/providers/fast", "tok", "{"); code != 400 {
		t.Error("bad json")
	}
	if code, _, _ := call(t, srv, "DELETE", "/v1/settings/providers/p", "tok", nil); code != 200 {
		t.Error("remove provider")
	}
	if code, _, _ := call(t, srv, "DELETE", "/v1/settings/providers/zz", "tok", nil); code != 404 {
		t.Error("remove unknown provider")
	}
	if code, m, _ := call(t, srv, "POST", "/v1/settings/providers/p/test", "tok", nil); code != 200 || m["ok"] != true || m["reply"] != "hello" {
		t.Errorf("test provider = %v", m)
	}
	if code, m, _ := call(t, srv, "POST", "/v1/settings/providers/zz/test", "tok", nil); code != 200 || m["ok"] != false {
		t.Errorf("test unknown provider = %v", m)
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/settings/default-provider", "tok", map[string]any{"name": "p"}); code != 200 {
		t.Error("default provider")
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/settings/default-provider", "tok", map[string]any{"name": "zz"}); code != 404 {
		t.Error("default unknown")
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/settings/default-provider", "tok", "{"); code != 400 {
		t.Error("default bad json")
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/settings/connectors/c", "tok", map[string]any{"kind": "webhook", "url": "http://x"}); code != 200 {
		t.Error("set connector")
	}
	if code, m, _ := call(t, srv, "PUT", "/v1/settings/connectors/c", "tok", map[string]any{"kind": "webhook"}); code != 400 {
		t.Errorf("connector needs a url: %d %v", code, m)
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/settings/connectors/c", "tok", "{"); code != 400 {
		t.Error("connector bad json")
	}
	if code, _, _ := call(t, srv, "DELETE", "/v1/settings/connectors/c", "tok", nil); code != 200 {
		t.Error("remove connector")
	}
	if code, _, _ := call(t, srv, "DELETE", "/v1/settings/connectors/zz", "tok", nil); code != 404 {
		t.Error("remove unknown connector")
	}
}

func TestLibraryRoutes(t *testing.T) {
	srv, _, _ := setup(t)
	if code, _, _ := call(t, srv, "GET", "/v1/skills", "tok", nil); code != 404 {
		t.Error("no library, no skills")
	}
	for _, path := range []string{"/v1/skills/x", "/v1/artifacts", "/v1/artifacts/x"} {
		if code, _, _ := call(t, srv, "GET", path, "tok", nil); code != 404 {
			t.Errorf("%s without a library", path)
		}
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/skills/x", "tok", map[string]any{"body": "# x"}); code != 404 {
		t.Error("put skill without a library")
	}
	if code, _, _ := call(t, srv, "DELETE", "/v1/skills/x", "tok", nil); code != 404 {
		t.Error("delete skill without a library")
	}
	if code, _, _ := call(t, srv, "POST", "/v1/skills/x/accept", "tok", nil); code != 404 {
		t.Error("accept without a library")
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/artifacts/x.txt", "tok", "x"); code != 404 {
		t.Error("put artifact without a library")
	}
	if code, _, _ := call(t, srv, "DELETE", "/v1/artifacts/x.txt", "tok", nil); code != 404 {
		t.Error("delete artifact without a library")
	}
	dir := t.TempDir()
	lib := New(&fakeFleet{kitties: map[string]*kitty.Kitty{}}, &fakeChats{sessions: map[string]*chat.Session{}}, "tok", "").
		WithLibrary(skills.New(filepath.Join(dir, "skills")), artifacts.New(filepath.Join(dir, "artifacts")))
	srv2 := httptest.NewServer(lib)
	defer srv2.Close()
	if code, m, _ := call(t, srv2, "GET", "/v1/skills", "tok", nil); code != 200 || len(m["skills"].([]any)) < 3 {
		t.Errorf("skills = %d %v", code, m)
	}
	if code, m, _ := call(t, srv2, "PUT", "/v1/skills/Deploy It", "tok", map[string]any{"body": "# Deploy\n\nHow."}); code != 200 || m["skill"].(map[string]any)["name"] != "deploy-it" {
		t.Errorf("save skill = %d %v", code, m)
	}
	if code, _, _ := call(t, srv2, "PUT", "/v1/skills/x", "tok", map[string]any{"body": ""}); code != 400 {
		t.Error("an empty skill is 400")
	}
	if code, _, _ := call(t, srv2, "PUT", "/v1/skills/x", "tok", "{"); code != 400 {
		t.Error("bad json")
	}
	if code, m, _ := call(t, srv2, "GET", "/v1/skills/deploy-it", "tok", nil); code != 200 || !strings.Contains(m["skill"].(map[string]any)["body"].(string), "How.") {
		t.Errorf("get skill = %d %v", code, m)
	}
	if code, _, _ := call(t, srv2, "GET", "/v1/skills/zz", "tok", nil); code != 404 {
		t.Error("unknown skill")
	}
	if code, _, _ := call(t, srv2, "POST", "/v1/skills/zz/accept", "tok", nil); code != 404 {
		t.Error("accept unknown")
	}
	if code, _, _ := call(t, srv2, "DELETE", "/v1/skills/browse-a-site", "tok", nil); code != 400 {
		t.Error("a built-in cannot be deleted")
	}
	if code, _, _ := call(t, srv2, "DELETE", "/v1/skills/deploy-it", "tok", nil); code != 200 {
		t.Error("delete skill")
	}
	if code, m, _ := call(t, srv2, "PUT", "/v1/artifacts/notes.md", "tok", "# notes"); code != 200 || m["artifact"].(map[string]any)["type"] != "text/markdown" {
		t.Errorf("put artifact = %d %v", code, m)
	}
	if code, _, _ := call(t, srv2, "PUT", "/v1/artifacts/.hidden", "tok", "x"); code != 400 {
		t.Error("bad artifact name")
	}
	if code, m, _ := call(t, srv2, "GET", "/v1/artifacts", "tok", nil); code != 200 || len(m["artifacts"].([]any)) != 1 {
		t.Errorf("artifacts = %v", m)
	}
	if code, _, data := call(t, srv2, "GET", "/v1/artifacts/notes.md", "tok", nil); code != 200 || string(data) != "# notes" {
		t.Error("get artifact")
	}
	if code, _, _ := call(t, srv2, "GET", "/v1/artifacts/zz.md", "tok", nil); code != 404 {
		t.Error("unknown artifact")
	}
	if code, _, _ := call(t, srv2, "DELETE", "/v1/artifacts/notes.md", "tok", nil); code != 200 {
		t.Error("delete artifact")
	}
	if code, _, _ := call(t, srv2, "DELETE", "/v1/artifacts/notes.md", "tok", nil); code != 404 {
		t.Error("delete unknown artifact")
	}
	// A library on a broken folder.
	file := filepath.Join(dir, "file")
	_ = os.WriteFile(file, nil, 0o600)
	broken := httptest.NewServer(New(&fakeFleet{kitties: map[string]*kitty.Kitty{}}, &fakeChats{}, "tok", "").WithLibrary(skills.New(file), artifacts.New(file)))
	defer broken.Close()
	if code, _, _ := call(t, broken, "GET", "/v1/skills", "tok", nil); code != 500 {
		t.Error("unreadable skills")
	}
	if code, _, _ := call(t, broken, "GET", "/v1/artifacts", "tok", nil); code != 500 {
		t.Error("unreadable artifacts")
	}
}

func TestEventsAndOAuth(t *testing.T) {
	srv, _, c := setup(t)
	c.sessions["c1"] = &chat.Session{ID: "c1", Members: []string{"a1"}}
	code, _, data := call(t, srv, "GET", "/v1/chats/c1/events", "tok", nil)
	text := string(data)
	if code != 200 || !strings.HasPrefix(text, "event: hello") || !strings.Contains(text, "event: turn\ndata: {\"type\":\"turn\"") || !strings.Contains(text, "event: deleted") {
		t.Errorf("events = %d %q", code, text)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/chats/zz/events", "tok", nil); code != 404 {
		t.Error("events of an unknown chat")
	}
	rec := httptest.NewRecorder()
	// A writer that cannot flush is refused; the recorder flushes, so a plain writer stands in.
	plain := &noFlush{ResponseWriter: rec}
	r := httptest.NewRequest("GET", "/v1/chats/c1/events", nil)
	r.Header.Set("Authorization", "Bearer tok")
	New(&fakeFleet{kitties: map[string]*kitty.Kitty{}}, c, "tok", "").ServeHTTP(plain, r)
	if rec.Code != 500 {
		t.Errorf("no flusher = %d", rec.Code)
	}
	code, m, _ := call(t, srv, "POST", "/v1/oauth/g/start", "tok", nil)
	if code != 200 || !strings.Contains(m["url"].(string), "accounts.google.com") || !strings.Contains(m["url"].(string), "redirect_uri=http%3A%2F%2F127.0.0.1") || !strings.HasSuffix(m["redirect"].(string), "/v1/oauth/callback") {
		t.Errorf("oauth start = %d %v", code, m)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/oauth/x/start", "tok", nil); code != 400 {
		t.Error("a connector without OAuth")
	}
	if code, _, data := call(t, srv, "GET", "/v1/oauth/callback?error=access_denied&error_description=no", "", nil); code != 400 || !strings.Contains(string(data), "Sign-in refused") {
		t.Errorf("callback error = %d %s", code, data)
	}
	if code, _, data := call(t, srv, "GET", "/v1/oauth/callback?state=nope&code=c", "", nil); code != 400 || !strings.Contains(string(data), "Sign-in failed") {
		t.Errorf("callback unknown state = %d %s", code, data)
	}
	// A real exchange, against a token endpoint that answers.
	token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"at","refresh_token":"rt","expires_in":3600}`))
	}))
	defer token.Close()
	google := oauth.Providers["google"]
	google.TokenURL = token.URL
	oauth.Providers["google"] = google
	defer func() {
		google.TokenURL = "https://oauth2.googleapis.com/token"
		oauth.Providers["google"] = google
	}()
	_, m, _ = call(t, srv, "POST", "/v1/oauth/g/start", "tok", nil)
	u, _ := http.NewRequest("GET", m["url"].(string), nil)
	state := u.URL.Query().Get("state")
	if code, _, data := call(t, srv, "GET", "/v1/oauth/callback?state="+state+"&code=the-code", "", nil); code != 200 || !strings.Contains(string(data), "Connected") {
		t.Errorf("callback = %d %s", code, data)
	}
}

type noFlush struct{ http.ResponseWriter }

func TestLocalToken(t *testing.T) {
	srv, _, _ := setup(t)
	if code, m, _ := call(t, srv, "GET", "/v1/local-token", "", nil); code != 200 || m["token"] != "tok" {
		t.Errorf("a loopback client gets the token: %d %v", code, m)
	}
	req, _ := http.NewRequest("GET", srv.URL+"/v1/local-token", nil)
	req.Header.Set("Origin", "http://evil.example")
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 403 {
		t.Error("another site gets nothing")
	}
	req.Header.Set("Origin", srv.URL)
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 200 {
		t.Error("the gateway's own page gets it")
	}
	s := New(&fakeFleet{kitties: map[string]*kitty.Kitty{}}, &fakeChats{}, "tok", "")
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/local-token", nil)
	r.RemoteAddr = "10.0.0.5:4000"
	s.ServeHTTP(rec, r)
	if rec.Code != 403 {
		t.Error("a network peer gets nothing")
	}
	r.RemoteAddr = "garbage"
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code != 403 {
		t.Error("an unreadable peer gets nothing")
	}
	none := New(&fakeFleet{kitties: map[string]*kitty.Kitty{}}, &fakeChats{}, "", "")
	rec = httptest.NewRecorder()
	r.RemoteAddr = "127.0.0.1:1"
	r.Host = "127.0.0.1:7488"
	none.ServeHTTP(rec, r)
	if rec.Code != 404 {
		t.Error("no token, nothing to hand out")
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
