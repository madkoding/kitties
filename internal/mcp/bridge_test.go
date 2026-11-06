package mcp

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestBridge(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		newTestServer().ServeHTTP(w, r)
	}))
	defer srv.Close()
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
		``,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n")
	var out bytes.Buffer
	if err := Bridge(context.Background(), strings.NewReader(in), &out, srv.URL, "tok", nil); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"id":1`) || !strings.Contains(lines[1], `"tools"`) {
		t.Errorf("output: %q", out.String())
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("auth = %q", gotAuth)
	}
}

func TestBridgeErrors(t *testing.T) {
	// A gateway that cannot be reached: a request gets an error, a notification nothing.
	var out bytes.Buffer
	in := `{"jsonrpc":"2.0","id":5,"method":"ping"}` + "\n" + `{"jsonrpc":"2.0","method":"x"}` + "\n" + `not json` + "\n"
	if err := Bridge(context.Background(), strings.NewReader(in), &out, "http://127.0.0.1:1/mcp", "", http.DefaultClient); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out.String(), "\n"); n != 1 || !strings.Contains(out.String(), `"id":5`) || !strings.Contains(out.String(), `"error"`) {
		t.Errorf("unreachable: %q", out.String())
	}
	// 401 and other statuses are said plainly.
	for code, want := range map[int]string{401: "refused the token", 500: "answered 500"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }))
		out.Reset()
		_ = Bridge(context.Background(), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`), &out, srv.URL, "t", nil)
		srv.Close()
		if !strings.Contains(out.String(), want) {
			t.Errorf("%d: %q", code, out.String())
		}
	}
	// A bad URL, a closed output.
	out.Reset()
	_ = Bridge(context.Background(), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`), &out, "http://bad host/", "", nil)
	if !strings.Contains(out.String(), `"error"`) {
		t.Errorf("bad url: %q", out.String())
	}
	srv := httptest.NewServer(newTestServer())
	defer srv.Close()
	if err := Bridge(context.Background(), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`), failWriter{}, srv.URL, "", nil); err == nil {
		t.Error("a closed output ends the bridge")
	}
	// A pretty-printed answer is put on one line; text that is not JSON still is.
	if got := string(compact([]byte("{\n \"a\": 1\n}"))); got != `{"a":1}` {
		t.Errorf("compact: %q", got)
	}
	if got := string(compact([]byte("a\n b"))); got != "a b" {
		t.Errorf("compact text: %q", got)
	}
}
