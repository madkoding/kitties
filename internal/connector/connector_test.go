package connector

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type fakeConn struct {
	out string
	err error
}

func (f fakeConn) Describe() string { return "fake" }
func (f fakeConn) Call(context.Context, map[string]any) (string, error) {
	return f.out, f.err
}

func TestParseAndFormat(t *testing.T) {
	calls, rest := Parse("Sure.\n@connector slack {\"text\": \"hi\"}\n  @connector mail plain words \n@connector bare\nDone.")
	if len(calls) != 3 || rest != "Sure.\nDone." {
		t.Fatalf("Parse = %+v, %q", calls, rest)
	}
	if calls[0].Name != "slack" || calls[0].Args["text"] != "hi" {
		t.Errorf("json args: %+v", calls[0])
	}
	if calls[1].Name != "mail" || calls[1].Args["text"] != "plain words" {
		t.Errorf("plain args: %+v", calls[1])
	}
	if calls[2].Name != "bare" || len(calls[2].Args) != 0 {
		t.Errorf("no args: %+v", calls[2])
	}
	out := Format([]Result{{Name: "a", Output: "x"}, {Name: "b", Err: "boom"}, {Name: "c"}})
	if out != "[connector a]\nx\n[connector b] failed: boom\n[connector c]\ndone" {
		t.Errorf("Format = %q", out)
	}
	if text(map[string]any{"message": "m"}) != "m" || text(map[string]any{"n": 1}) != `{"n":1}` || text(map[string]any{"text": 1}) != `{"text":1}` {
		t.Error("text")
	}
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	r.Add("ok", fakeConn{out: strings.Repeat("x", 5000)})
	r.Add("bad", fakeConn{err: errors.New("nope")})
	if strings.Join(r.Names(), ",") != "bad,ok" {
		t.Error(r.Names())
	}
	if d := r.Descriptions(nil); len(d) != 2 || d["ok"] != "fake" {
		t.Error(d)
	}
	if d := r.Descriptions([]string{"ok"}); len(d) != 1 {
		t.Error(d)
	}
	res := r.Call(context.Background(), Call{Name: "ok"}, nil)
	if res.Err != "" || len(res.Output) != 4003 {
		t.Errorf("clipped output: %+v", len(res.Output))
	}
	if res := r.Call(context.Background(), Call{Name: "bad"}, nil); res.Err != "nope" {
		t.Error(res)
	}
	if res := r.Call(context.Background(), Call{Name: "ok"}, []string{"bad"}); !strings.Contains(res.Err, "not available") {
		t.Error(res)
	}
	if res := r.Call(context.Background(), Call{Name: "zz"}, nil); !strings.Contains(res.Err, "unknown connector") {
		t.Error(res)
	}
	if _, ok := r.Get("zz"); ok {
		t.Error("Get")
	}
	r.Remove("ok")
	if _, ok := r.Get("ok"); ok {
		t.Error("Remove")
	}
}

// stub answers HTTP requests in order and records them.
type stub struct {
	reqs  []*http.Request
	body  []string
	codes []int
	err   error
}

func (s *stub) install(t *testing.T) {
	t.Helper()
	old := httpDo
	httpDo = func(req *http.Request) (*http.Response, error) {
		if s.err != nil {
			return nil, s.err
		}
		data, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(strings.NewReader(string(data)))
		s.reqs = append(s.reqs, req)
		n := len(s.reqs) - 1
		code, body := 200, ""
		if n < len(s.codes) {
			code = s.codes[n]
		}
		if n < len(s.body) {
			body = s.body[n]
		}
		return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	t.Cleanup(func() { httpDo = old })
}

func bodyOf(r *http.Request) string {
	data, _ := io.ReadAll(r.Body)
	return string(data)
}

func TestHTTPConnectors(t *testing.T) {
	s := &stub{body: []string{"ok", "", "", "", ""}}
	s.install(t)
	ctx := context.Background()
	out, err := Webhook{URL: "http://h/x", Headers: map[string]string{"X-A": "1"}}.Call(ctx, map[string]any{"a": 1})
	if err != nil || out != "ok" || s.reqs[0].Header.Get("X-A") != "1" || !strings.Contains(bodyOf(s.reqs[0]), `"a":1`) {
		t.Errorf("webhook: %q %v", out, err)
	}
	if _, err := (Slack{URL: "http://h/s"}).Call(ctx, map[string]any{"text": "hi"}); err != nil || !strings.Contains(bodyOf(s.reqs[1]), `"text":"hi"`) {
		t.Errorf("slack: %v", err)
	}
	if _, err := (Discord{URL: "http://h/d"}).Call(ctx, map[string]any{"text": "hi"}); err != nil || !strings.Contains(bodyOf(s.reqs[2]), `"content":"hi"`) {
		t.Errorf("discord: %v", err)
	}
	if _, err := (Telegram{Token: "T", ChatID: "7"}).Call(ctx, map[string]any{"text": "hi"}); err != nil || s.reqs[3].URL.String() != "https://api.telegram.org/botT/sendMessage" {
		t.Errorf("telegram: %v %s", err, s.reqs[3].URL)
	}
	for _, c := range []Connector{Webhook{}, Slack{}, Discord{}, Telegram{}, Shell{Command: "echo"}, Motita{}, Webhook{Description: "d"}} {
		if c.Describe() == "" {
			t.Error("every connector describes itself")
		}
	}
	s2 := &stub{codes: []int{500}, body: []string{"down"}}
	s2.install(t)
	if _, err := (Webhook{URL: "http://h/x"}).Call(ctx, nil); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("status error: %v", err)
	}
	if _, err := (Webhook{URL: "::bad"}).Call(ctx, nil); err == nil {
		t.Error("bad url")
	}
	if _, err := postJSON(ctx, "http://h", nil, func() {}); err == nil {
		t.Error("unencodable body")
	}
	s3 := &stub{err: errors.New("net down")}
	s3.install(t)
	if _, err := (Webhook{URL: "http://h/x"}).Call(ctx, nil); err == nil {
		t.Error("transport error")
	}
}

func TestShell(t *testing.T) {
	out, err := Shell{Command: "echo", Args: []string{"-n"}}.Call(context.Background(), map[string]any{"text": "hello"})
	if err != nil || out != "hello" {
		t.Errorf("echo: %q %v", out, err)
	}
	if _, err := (Shell{Command: "false"}).Call(context.Background(), nil); err == nil {
		t.Error("a failing command is reported")
	}
}

func TestMotita(t *testing.T) {
	ctx := context.Background()
	stream := "event: progress\ndata: {\"text\":\"step 1\"}\n\nevent: other\ndata: {}\n\nevent: done\ndata: {\"result\":\"all good\"}\n\n"
	s := &stub{body: []string{`{"id":"s1"}`, stream}}
	s.install(t)
	m := Motita{URL: "http://m/", Token: "tok"}
	out, err := m.Call(ctx, map[string]any{"task": "fix it"})
	if err != nil || out != "all good" {
		t.Fatalf("motita: %q %v", out, err)
	}
	if s.reqs[0].URL.String() != "http://m/v1/sessions" || s.reqs[1].URL.String() != "http://m/v1/sessions/s1/task" ||
		s.reqs[1].Header.Get("Authorization") != "Bearer tok" || !strings.Contains(bodyOf(s.reqs[1]), "fix it") {
		t.Errorf("requests = %v %v", s.reqs[0].URL, s.reqs[1].URL)
	}
	if _, err := m.Call(ctx, map[string]any{}); err == nil {
		t.Error("an empty task is refused")
	}
	s = &stub{body: []string{`{}`}}
	s.install(t)
	if _, err := m.Call(ctx, map[string]any{"task": "x"}); err == nil || !strings.Contains(err.Error(), "session id") {
		t.Errorf("no session id: %v", err)
	}
	s = &stub{codes: []int{500}}
	s.install(t)
	if _, err := m.Call(ctx, map[string]any{"task": "x"}); err == nil || !strings.Contains(err.Error(), "open a motita session") {
		t.Errorf("session failure: %v", err)
	}
	s = &stub{body: []string{`{"id":"s1"}`, "busy"}, codes: []int{200, 409}}
	s.install(t)
	if _, err := m.Call(ctx, map[string]any{"task": "x"}); err == nil || !strings.Contains(err.Error(), "refused the task") {
		t.Errorf("task refused: %v", err)
	}
	s = &stub{body: []string{`{"id":"s1"}`, "event: progress\ndata: {\"text\":\"p\"}\n\nevent: error\ndata: {\"error\":\"boom\"}\n"}}
	s.install(t)
	if out, err := m.Call(ctx, map[string]any{"task": "x"}); err == nil || err.Error() != "boom" || out != "p" {
		t.Errorf("error event: %q %v", out, err)
	}
	s = &stub{body: []string{`{"id":"s1"}`, "event: progress\ndata: {\"text\":\"p\"}\n"}}
	s.install(t)
	if _, err := m.Call(ctx, map[string]any{"task": "x"}); err == nil || !strings.Contains(err.Error(), "ended without") {
		t.Errorf("truncated stream: %v", err)
	}
	if out, err := readMotitaStream(strings.NewReader("event: done\ndata: {}\n")); err != nil || out != "" {
		t.Errorf("empty done: %q %v", out, err)
	}
	if _, err := readMotitaStream(failingReader{}); err == nil {
		t.Error("read failure")
	}
	s = &stub{body: []string{`{"id":"s1"}`}, err: nil}
	s.install(t)
	m2 := Motita{URL: "::bad"}
	if _, err := m2.Call(ctx, map[string]any{"task": "x"}); err == nil {
		t.Error("bad url")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }
