package gateway

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAccounts(t *testing.T) {
	srv, _, _ := setup(t)
	if code, body, _ := call(t, srv, "POST", "/v1/settings/accounts/qwen/login", "tok", nil); code != 400 || !strings.Contains(body["error"].(string), "DashScope") {
		t.Errorf("qwen = %d %v", code, body)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/settings/accounts/codex/login", "tok", "{"); code != 400 {
		t.Errorf("a broken body = %d", code)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/settings/logins/nope", "tok", nil); code != 404 {
		t.Errorf("an unknown sign-in = %d", code)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/settings/logins/nope/paste", "tok", map[string]string{"code": "x"}); code != 400 {
		t.Errorf("a paste to nothing = %d", code)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/settings/logins/nope/paste", "tok", "{"); code != 400 {
		t.Errorf("a broken paste = %d", code)
	}
	if code, body, _ := call(t, srv, "DELETE", "/v1/settings/logins/nope", "tok", nil); code != 200 || body["ok"] != true {
		t.Errorf("cancel = %d %v", code, body)
	}
	if code, _, _ := call(t, srv, "DELETE", "/v1/settings/accounts/codex", "tok", nil); code != 200 {
		t.Errorf("sign out = %d", code)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/settings/accounts/codex/login", "", nil); code != 401 {
		t.Errorf("no token = %d", code)
	}
}

func TestAccountClaude(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts")
	}
	bin := filepath.Join(t.TempDir(), "claude")
	_ = os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("KITTIES_CLAUDE_BIN", bin)
	srv, _, _ := setup(t)
	code, body, _ := call(t, srv, "POST", "/v1/settings/accounts/claude-code/login", "tok", nil)
	if code != 200 || body["status"] != "connected" || body["method"] != "instant" {
		t.Errorf("claude = %d %v", code, body)
	}
}

func TestAccountCodexFlow(t *testing.T) {
	srv, _, _ := setup(t)
	code, body, _ := call(t, srv, "POST", "/v1/settings/accounts/codex/login", "tok", nil)
	if code != 200 || body["status"] != "pending" || body["paste"] != true {
		t.Fatalf("codex = %d %v", code, body)
	}
	id := body["id"].(string)
	defer call(t, srv, "DELETE", "/v1/settings/logins/"+id, "tok", nil)
	if code, got, _ := call(t, srv, "GET", "/v1/settings/logins/"+id, "tok", nil); code != 200 || got["id"] != id {
		t.Errorf("get = %d %v", code, got)
	}
	// A code for another sign-in fails the flow, and the page reads why.
	if code, got, _ := call(t, srv, "POST", "/v1/settings/logins/"+id+"/paste", "tok", map[string]string{"code": "c#other"}); code != 200 || got["id"] != id {
		t.Errorf("paste = %d %v", code, got)
	}
}

func TestAndroidAppOrigin(t *testing.T) {
	srv, _, _ := setup(t)
	pre, _ := http.NewRequest(http.MethodOptions, srv.URL+"/v1/kitties", nil)
	pre.Header.Set("Origin", AppOrigin)
	pre.Header.Set("Access-Control-Request-Method", "POST")
	resp, err := http.DefaultClient.Do(pre)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("Access-Control-Allow-Origin") != AppOrigin ||
		!strings.Contains(resp.Header.Get("Access-Control-Allow-Headers"), "Authorization") || resp.Header.Get("Access-Control-Allow-Credentials") != "" {
		t.Errorf("preflight = %d %v", resp.StatusCode, resp.Header)
	}
	get, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/kitties", nil)
	get.Header.Set("Origin", AppOrigin)
	get.Header.Set("Authorization", "Bearer tok")
	resp, err = http.DefaultClient.Do(get)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Access-Control-Allow-Origin") != AppOrigin {
		t.Errorf("get = %d %v", resp.StatusCode, resp.Header)
	}
	// Any other site, and the local token, stay closed to the app's origin rule.
	for _, c := range []struct{ origin, path string }{{"https://evil.example", "/v1/kitties"}, {AppOrigin, "/v1/local-token"}, {AppOrigin, "/"}} {
		r, _ := http.NewRequest(http.MethodGet, srv.URL+c.path, nil)
		r.Header.Set("Origin", c.origin)
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s %s is allowed", c.origin, c.path)
		}
	}
}
