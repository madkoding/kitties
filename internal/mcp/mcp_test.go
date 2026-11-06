package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer() *Server {
	s := New("test", "1.0")
	s.Add(Tool{Name: "echo", Description: "echo", InputSchema: map[string]any{"type": "object"},
		Call: func(_ context.Context, a json.RawMessage) (Result, error) {
			return Result{Content: []Content{Text(string(a))}}, nil
		}})
	s.Add(Tool{Name: "boom", Call: func(context.Context, json.RawMessage) (Result, error) { return Result{}, errors.New("boom") }})
	s.Add(Tool{Name: "bad", Call: func(context.Context, json.RawMessage) (Result, error) { return Result{}, &ArgError{Msg: "nope"} }})
	s.Add(Tool{Name: "nil", Call: func(context.Context, json.RawMessage) (Result, error) { return Result{}, nil }})
	return s
}

func post(s *Server, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("POST", "/mcp", strings.NewReader(body)))
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("not json: %q", rec.Body.String())
	}
	return m
}

func errCode(m map[string]any) float64 {
	e, _ := m["error"].(map[string]any)
	c, _ := e["code"].(float64)
	return c
}

func TestInitializeAndPing(t *testing.T) {
	s := newTestServer()
	m := decode(t, post(s, `{"jsonrpc":"2.0","id":7,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`))
	r := m["result"].(map[string]any)
	if r["protocolVersion"] != "2024-11-05" || m["id"] != float64(7) || r["serverInfo"].(map[string]any)["name"] != "test" {
		t.Errorf("initialize: %v", m)
	}
	// An unknown version gets the newest this server speaks.
	m = decode(t, post(s, `{"jsonrpc":"2.0","id":"a","method":"initialize","params":{"protocolVersion":"1999"}}`))
	if m["result"].(map[string]any)["protocolVersion"] != versions[0] || m["id"] != "a" {
		t.Errorf("negotiation: %v", m)
	}
	if m = decode(t, post(s, `{"jsonrpc":"2.0","id":1,"method":"ping"}`)); m["error"] != nil {
		t.Errorf("ping: %v", m)
	}
}

func TestToolsListAndReplace(t *testing.T) {
	s := newTestServer()
	s.Add(Tool{Name: "echo", Description: "second"})
	if s.Len() != 4 {
		t.Errorf("replacing a tool must not add one: %d", s.Len())
	}
	m := decode(t, post(s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	tools := m["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 4 || tools[0].(map[string]any)["description"] != "second" {
		t.Errorf("tools/list: %v", tools)
	}
}

func TestToolsCall(t *testing.T) {
	s := newTestServer()
	call := func(params string) map[string]any {
		return decode(t, post(s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+params+`}`))
	}
	m := call(`{"name":"echo","arguments":{"a":1}}`)
	if c := m["result"].(map[string]any)["content"].([]any)[0].(map[string]any); c["text"] != `{"a":1}` {
		t.Errorf("echo: %v", m)
	}
	// No arguments means an empty object.
	if c := call(`{"name":"echo"}`)["result"].(map[string]any)["content"].([]any)[0].(map[string]any); c["text"] != "{}" {
		t.Errorf("default args: %v", c)
	}
	// A failing tool is a result the model reads.
	r := call(`{"name":"boom"}`)["result"].(map[string]any)
	if r["isError"] != true || r["content"].([]any)[0].(map[string]any)["text"] != "boom" {
		t.Errorf("tool error: %v", r)
	}
	if errCode(call(`{"name":"bad"}`)) != codeParams {
		t.Error("an ArgError is a protocol error")
	}
	if errCode(call(`{"name":"ghost"}`)) != codeParams || errCode(call(`{}`)) != codeParams || errCode(call(`"x"`)) != codeParams {
		t.Error("unknown tool or bad params")
	}
	if c := call(`{"name":"nil"}`)["result"].(map[string]any)["content"]; c == nil {
		t.Error("content must be a list, never null")
	}
}

func TestProtocolErrors(t *testing.T) {
	s := newTestServer()
	if errCode(decode(t, post(s, `{"jsonrpc":"2.0","id":1,"method":"nope"}`))) != codeMethod {
		t.Error("unknown method")
	}
	if errCode(decode(t, post(s, `{not json`))) != codeParse {
		t.Error("parse error")
	}
	if errCode(decode(t, post(s, `{"jsonrpc":"1.0","id":1,"method":"ping"}`))) != codeRequest {
		t.Error("wrong version")
	}
	if errCode(decode(t, post(s, `{"jsonrpc":"2.0","id":1}`))) != codeRequest {
		t.Error("no method")
	}
}

func TestNotificationsAndBatch(t *testing.T) {
	s := newTestServer()
	if rec := post(s, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); rec.Code != http.StatusAccepted || rec.Body.Len() != 0 {
		t.Errorf("notification: %d %q", rec.Code, rec.Body.String())
	}
	rec := post(s, `[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","method":"notifications/x"},{"jsonrpc":"2.0","id":2,"method":"ping"}]`)
	var out []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out) != 2 {
		t.Errorf("batch: %q", rec.Body.String())
	}
	if rec = post(s, `[{"jsonrpc":"2.0","method":"notifications/x"}]`); rec.Code != http.StatusAccepted {
		t.Errorf("batch of notifications: %d", rec.Code)
	}
	if rec = post(s, `[]`); rec.Code != http.StatusBadRequest {
		t.Errorf("empty batch: %d", rec.Code)
	}
	if rec = post(s, `[1,`); rec.Code != http.StatusBadRequest {
		t.Errorf("broken batch: %d", rec.Code)
	}
}

func TestMethodsAndSize(t *testing.T) {
	s := newTestServer()
	for _, m := range []string{"GET", "DELETE", "PUT"} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(m, "/mcp", nil))
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "POST" {
			t.Errorf("%s: %d", m, rec.Code)
		}
	}
	if rec := post(s, `{"jsonrpc":"2.0","id":1,"method":"ping","params":"`+strings.Repeat("a", maxBody)+`"}`); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("big body: %d", rec.Code)
	}
}

func TestImage(t *testing.T) {
	c := Image("image/png", "AAAA")
	if c.Type != "image" || c.MIMEType != "image/png" || c.Data != "AAAA" {
		t.Errorf("%+v", c)
	}
}
