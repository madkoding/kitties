package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequestAndConnect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":"no"}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, ".png") {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("PNG"))
			return
		}
		_, _ = w.Write([]byte(`{"kitties":[]}`))
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	a := NewApp()
	a.startup(t.Context())
	if a.Gateway().URL != "http://127.0.0.1:7488" || a.Gateway().Token != "" {
		t.Error("default gateway")
	}
	// The token the gateway left on this machine is picked up without typing.
	_ = os.MkdirAll(filepath.Dir(configPath()), 0o700)
	_ = os.WriteFile(filepath.Join(filepath.Dir(configPath()), "gateway.token"), []byte("tok\n"), 0o600)
	a.startup(t.Context())
	if a.Gateway().Token != "tok" {
		t.Errorf("local token = %q", a.Gateway().Token)
	}
	a.conn.URL = srv.URL
	a.conn.Token = "stale"
	if raw, err := a.Request("GET", "/v1/kitties", ""); err != nil || !strings.Contains(raw, `"status":200`) {
		t.Errorf("a stale token is refreshed from the file: %s %v", raw, err)
	}
	_ = os.Remove(filepath.Join(filepath.Dir(configPath()), "gateway.token"))
	if _, err := a.Connect("ftp://x", ""); err == nil {
		t.Error("bad scheme")
	}
	if _, err := a.Connect(srv.URL, "wrong"); err == nil || !strings.Contains(err.Error(), "refused the token") {
		t.Errorf("wrong token: %v", err)
	}
	if msg, err := a.Connect(srv.URL+"/", "tok"); err != nil || !strings.Contains(msg, "connected") {
		t.Errorf("connect: %s %v", msg, err)
	}
	if loadConnection().Token != "tok" {
		t.Error("the connection is saved")
	}
	raw, err := a.Request("GET", "/v1/kitties", "")
	var res Response
	_ = json.Unmarshal([]byte(raw), &res)
	if err != nil || res.Status != 200 || res.Body != `{"kitties":[]}` {
		t.Errorf("request: %s %v", raw, err)
	}
	raw, _ = a.Request("GET", "/v1/chats/c/shots/1-1.png", "")
	_ = json.Unmarshal([]byte(raw), &res)
	if res.Base64 != "UE5H" {
		t.Errorf("binary answer: %s", raw)
	}
	if _, err := a.Request("GET", "/etc/passwd", ""); err == nil {
		t.Error("only the API")
	}
	a.conn.URL = "http://127.0.0.1:1"
	if _, err := a.Request("POST", "/v1/kitties", `{"name":"x"}`); err == nil {
		t.Error("a gateway that does not answer")
	}
	if _, err := a.Connect("http://127.0.0.1:1", ""); err == nil {
		t.Error("connect to nothing")
	}
	a.shutdown(t.Context())
}
