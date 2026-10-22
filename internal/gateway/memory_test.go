package gateway

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/memory"
)

// fakeMemory keeps a kitty's notes in a slice.
type fakeMemory struct {
	notes   []memory.Note
	profile memory.Profile
	rated   []string
	none    bool
	failing bool
}

func (m *fakeMemory) MemoryOf(ref string) (*chat.MemoryView, error) {
	if ref == "zz" {
		return nil, errors.New("no such kitty zz")
	}
	return &chat.MemoryView{Notes: m.notes, Profile: m.profile, Level: memory.LevelText(m.profile.Demand)}, nil
}
func (m *fakeMemory) Remember(_, scope, text string) (*memory.Note, error) {
	if text == "" {
		return nil, errors.New("a note needs text")
	}
	if scope == "bogus" {
		return nil, errors.New("scope \"bogus\" is not user, work or project:<name>")
	}
	n := memory.Note{ID: "n" + string(rune('0'+len(m.notes))), Scope: scope, Text: text, Pinned: true}
	m.notes = append(m.notes, n)
	return &n, nil
}
func (m *fakeMemory) ForgetNote(_, id string) error {
	for i, n := range m.notes {
		if n.ID == id {
			m.notes = append(m.notes[:i], m.notes[i+1:]...)
			return nil
		}
	}
	return errors.New("no such note " + id)
}
func (m *fakeMemory) SetProfile(_ string, demand int, auto bool) (memory.Profile, error) {
	if demand < 1 || demand > 5 {
		return memory.Profile{}, errors.New("demand is a level from 1 (relaxed) to 5 (strict)")
	}
	m.profile.Demand, m.profile.Auto = demand, auto
	return m.profile, nil
}
func (m *fakeMemory) Distill(context.Context, string) ([]memory.Note, error) {
	if m.failing {
		return nil, errors.New("the brain did not answer with a list of notes")
	}
	return m.notes, nil
}
func (m *fakeMemory) Feedback(chatID string, index int, rating, note string) error {
	if rating == "meh" {
		return errors.New("rating is up or down")
	}
	if chatID == "zz" {
		return errors.New("no such chat \"zz\"")
	}
	m.rated = append(m.rated, chatID+":"+rating+":"+note)
	return nil
}

func TestMemoryAPI(t *testing.T) {
	mem := &fakeMemory{profile: memory.Profile{Demand: 3, Auto: true}}
	f := &fakeFleet{kitties: map[string]*kitty.Kitty{"a1": {ID: "a1", Name: "ana"}}}
	srv := httptest.NewServer(New(f, &fakeChats{}, "tok", "").WithMemory(mem))
	defer srv.Close()
	if code, m, _ := call(t, srv, "GET", "/v1/kitties/ana/memory", "tok", nil); code != 200 || m["profile"].(map[string]any)["demand"] != 3.0 || !strings.Contains(m["level"].(string), "balanced") {
		t.Errorf("view = %d %v", code, m)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/kitties/zz/memory", "tok", nil); code != 404 {
		t.Error("unknown kitty")
	}
	if code, m, _ := call(t, srv, "POST", "/v1/kitties/ana/memory", "tok", map[string]any{"text": "Answer in Spanish", "scope": "user"}); code != 201 || m["note"].(map[string]any)["pinned"] != true {
		t.Errorf("remember = %d %v", code, m)
	}
	for name, body := range map[string]any{"empty": map[string]any{"text": ""}, "bad scope": map[string]any{"text": "x", "scope": "bogus"}, "bad json": "{nope"} {
		if code, _, _ := call(t, srv, "POST", "/v1/kitties/ana/memory", "tok", body); code != 400 {
			t.Errorf("%s must be a 400: %d", name, code)
		}
	}
	if code, _, _ := call(t, srv, "DELETE", "/v1/kitties/ana/memory/n0", "tok", nil); code != 200 || len(mem.notes) != 0 {
		t.Error("forget")
	}
	if code, _, _ := call(t, srv, "DELETE", "/v1/kitties/ana/memory/zz", "tok", nil); code != 404 {
		t.Error("forget unknown")
	}
	auto := true
	if code, m, _ := call(t, srv, "PUT", "/v1/kitties/ana/memory/profile", "tok", map[string]any{"demand": 5, "auto": auto}); code != 200 || m["profile"].(map[string]any)["demand"] != 5.0 || m["profile"].(map[string]any)["auto"] != true {
		t.Errorf("profile = %d %v", code, m)
	}
	if code, m, _ := call(t, srv, "PUT", "/v1/kitties/ana/memory/profile", "tok", map[string]any{"demand": 2}); code != 200 || m["profile"].(map[string]any)["auto"] != false {
		t.Errorf("a manual level stops following the feedback: %d %v", code, m)
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/kitties/ana/memory/profile", "tok", map[string]any{"demand": 9}); code != 400 {
		t.Error("level out of range")
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/kitties/ana/memory/profile", "tok", "{nope"); code != 400 {
		t.Error("bad json")
	}
	if code, _, _ := call(t, srv, "POST", "/v1/kitties/ana/memory/distill", "tok", nil); code != 200 {
		t.Error("distill")
	}
	mem.failing = true
	if code, _, _ := call(t, srv, "POST", "/v1/kitties/ana/memory/distill", "tok", nil); code != 502 {
		t.Error("a brain that answers nonsense is a bad gateway")
	}
	// Feedback on a turn.
	if code, _, _ := call(t, srv, "POST", "/v1/chats/c1/turns/1/feedback", "tok", map[string]any{"rating": "down", "note": "too long"}); code != 200 || len(mem.rated) != 1 || mem.rated[0] != "c1:down:too long" {
		t.Errorf("feedback = %v", mem.rated)
	}
	for name, tc := range map[string]struct {
		path string
		body any
		code int
	}{
		"bad rating": {"/v1/chats/c1/turns/1/feedback", map[string]any{"rating": "meh"}, 400},
		"bad index":  {"/v1/chats/c1/turns/x/feedback", map[string]any{"rating": "up"}, 400},
		"unknown":    {"/v1/chats/zz/turns/1/feedback", map[string]any{"rating": "up"}, 404},
		"bad json":   {"/v1/chats/c1/turns/1/feedback", "{nope", 400},
	} {
		if code, _, _ := call(t, srv, "POST", tc.path, "tok", tc.body); code != tc.code {
			t.Errorf("%s = %d, want %d", name, code, tc.code)
		}
	}
	if code, _, _ := call(t, srv, "GET", "/v1/kitties/ana/memory", "", nil); code != 401 {
		t.Error("auth")
	}
	// Without a memory every route says so.
	none := httptest.NewServer(New(f, &fakeChats{}, "tok", ""))
	defer none.Close()
	for _, tc := range []struct{ method, path string }{{"GET", "/v1/kitties/ana/memory"}, {"POST", "/v1/kitties/ana/memory"}, {"DELETE", "/v1/kitties/ana/memory/x"}, {"PUT", "/v1/kitties/ana/memory/profile"}, {"POST", "/v1/kitties/ana/memory/distill"}, {"POST", "/v1/chats/c/turns/1/feedback"}} {
		if code, _, data := call(t, none, tc.method, tc.path, "tok", nil); code != 404 || !strings.Contains(string(data), "no memory") {
			t.Errorf("%s %s without memory = %d", tc.method, tc.path, code)
		}
	}
}

// fakeTester records the notifications it was asked to test.
type fakeTester struct {
	tried []string
	err   error
}

func (f *fakeTester) Test(_ context.Context, name string) error {
	f.tried = append(f.tried, name)
	return f.err
}

func TestNotifySettingsAPI(t *testing.T) {
	f := &fakeFleet{kitties: map[string]*kitty.Kitty{"a1": {ID: "a1", Name: "ana"}}}
	tester := &fakeTester{}
	srv := httptest.NewServer(New(f, &fakeChats{}, "tok", "").WithNotifier(tester))
	defer srv.Close()
	if code, _, _ := call(t, srv, "PUT", "/v1/settings/notify", "tok", map[string]any{"connector": "push", "on": []string{"help"}}); code != 200 || f.notify.Connector != "push" || len(f.notify.On) != 1 {
		t.Errorf("set = %+v", f.notify)
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/settings/notify", "tok", map[string]any{"connector": "bad"}); code != 400 {
		t.Error("a bad connector")
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/settings/notify", "tok", "{nope"); code != 400 {
		t.Error("bad json")
	}
	// The test goes through the connector in the body, or the configured one.
	if code, m, _ := call(t, srv, "POST", "/v1/settings/notify/test", "tok", map[string]any{"connector": "other"}); code != 200 || m["ok"] != true || tester.tried[0] != "other" {
		t.Errorf("test = %d %v %v", code, m, tester.tried)
	}
	if code, m, _ := call(t, srv, "POST", "/v1/settings/notify/test", "tok", nil); code != 200 || m["ok"] != true || tester.tried[1] != "push" {
		t.Errorf("test the configured one = %d %v %v", code, m, tester.tried)
	}
	tester.err = errors.New("HTTP 401")
	if code, m, _ := call(t, srv, "POST", "/v1/settings/notify/test", "tok", nil); code != 200 || m["ok"] != false || m["error"] != "HTTP 401" {
		t.Errorf("a failing test is reported = %d %v", code, m)
	}
	f.notify.Connector = ""
	if code, _, _ := call(t, srv, "POST", "/v1/settings/notify/test", "tok", nil); code != 400 {
		t.Error("nothing to test through")
	}
	if code, _, _ := call(t, srv, "POST", "/v1/settings/notify/test", "tok", "{nope"); code != 400 {
		t.Error("bad json")
	}
	none := httptest.NewServer(New(f, &fakeChats{}, "tok", ""))
	defer none.Close()
	if code, _, _ := call(t, none, "POST", "/v1/settings/notify/test", "tok", nil); code != 404 {
		t.Error("no notifier")
	}
}
