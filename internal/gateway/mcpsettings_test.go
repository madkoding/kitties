package gateway

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/config"
	"github.com/madkoding/kitties/internal/kitty"
)

type fakeMCPControl struct {
	info     MCPInfo
	set      *config.MCPSettings
	rotated  bool
	tokenErr error
	rotErr   error
	setErr   error
	relaunch chan struct{}
}

func (f *fakeMCPControl) MCP() MCPInfo { return f.info }
func (f *fakeMCPControl) SetMCP(s config.MCPSettings) error {
	f.set = &s
	return f.setErr
}
func (f *fakeMCPControl) Token() (string, error) { return "secret-token-value", f.tokenErr }
func (f *fakeMCPControl) RotateToken() error     { f.rotated = f.rotErr == nil; return f.rotErr }
func (f *fakeMCPControl) Relaunch() error        { f.relaunch <- struct{}{}; return nil }

func TestMCPControlRoutes(t *testing.T) {
	old := restartAfter
	restartAfter = time.Millisecond
	t.Cleanup(func() { restartAfter = old })
	fleet := &fakeFleet{kitties: map[string]*kitty.Kitty{}}
	chats := &fakeChats{sessions: map[string]*chat.Session{}}
	plain := httptest.NewServer(New(fleet, chats, "tok", "test"))
	defer plain.Close()
	for _, r := range [][2]string{{"GET", "/v1/mcp"}, {"PUT", "/v1/mcp"}, {"GET", "/v1/mcp/token"}, {"POST", "/v1/mcp/token"}} {
		if code, _, _ := call(t, plain, r[0], r[1], "tok", map[string]any{}); code != 404 {
			t.Errorf("%s %s without control = %d", r[0], r[1], code)
		}
	}
	f := &fakeMCPControl{
		info:     MCPInfo{MCPSettings: config.MCPSettings{Enabled: true}, URL: "http://127.0.0.1:7488/mcp", CanChange: true},
		relaunch: make(chan struct{}, 4),
	}
	srv := httptest.NewServer(New(fleet, chats, "tok", "test").WithMCPControl(f))
	defer srv.Close()
	waitRelaunch := func() {
		t.Helper()
		select {
		case <-f.relaunch:
		case <-time.After(2 * time.Second):
			t.Fatal("the gateway did not restart")
		}
	}

	code, got, _ := call(t, srv, "GET", "/v1/mcp", "tok", nil)
	if code != 200 || got["enabled"] != true || got["url"] != "http://127.0.0.1:7488/mcp" {
		t.Fatalf("GET = %d %v", code, got)
	}
	if _, leaked := got["token"]; leaked {
		t.Error("the status carries the token")
	}
	if code, _, _ := call(t, srv, "GET", "/v1/mcp", "", nil); code != 401 {
		t.Errorf("without the gateway token = %d", code)
	}
	// same as now: nothing to do, no restart
	if code, got, _ := call(t, srv, "PUT", "/v1/mcp", "tok", map[string]any{"enabled": true}); code != 200 || got["restarting"] != false || f.set != nil {
		t.Errorf("unchanged = %d %v (set %v)", code, got, f.set)
	}
	// exec and approve give Claude power: they need the risk accepted
	if code, _, _ := call(t, srv, "PUT", "/v1/mcp", "tok", map[string]any{"enabled": true, "allow_exec": true}); code != 400 || f.set != nil {
		t.Errorf("exec without accept_risk = %d (set %v)", code, f.set)
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/mcp", "tok", map[string]any{"enabled": true, "allow_approve": true}); code != 400 || f.set != nil {
		t.Errorf("approve without accept_risk = %d (set %v)", code, f.set)
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/mcp", "tok", "not an object"); code != 400 {
		t.Errorf("bad body = %d", code)
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/mcp", "tok", map[string]any{"enabled": true, "allow_exec": true, "accept_risk": true}); code != 202 || f.set == nil || !f.set.AllowExec {
		t.Errorf("exec accepted = %d (set %v)", code, f.set)
	}
	waitRelaunch()
	// turning something off needs no confirmation
	f.set = nil
	f.info.AllowExec = true
	if code, _, _ := call(t, srv, "PUT", "/v1/mcp", "tok", map[string]any{"enabled": false}); code != 202 || f.set == nil || f.set.Enabled {
		t.Errorf("turn off = %d (set %v)", code, f.set)
	}
	waitRelaunch()
	f.setErr = errors.New("disk full")
	if code, _, _ := call(t, srv, "PUT", "/v1/mcp", "tok", map[string]any{"enabled": true}); code != 400 {
		t.Errorf("a failed save = %d", code)
	}

	// the token leaves only through its own route, never cached
	code, got, _ = call(t, srv, "GET", "/v1/mcp/token", "tok", nil)
	if code != 200 || got["token"] != "secret-token-value" {
		t.Errorf("token = %d %v", code, got)
	}
	req, _ := http.NewRequest("GET", srv.URL+"/v1/mcp/token", nil)
	req.Header.Set("Authorization", "Bearer tok")
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("token is cacheable: %v %v", err, resp)
	}
	f.tokenErr = errors.New("no token yet")
	if code, _, _ := call(t, srv, "GET", "/v1/mcp/token", "tok", nil); code != 404 {
		t.Errorf("token without one = %d", code)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/mcp/token", "tok", nil); code != 202 || !f.rotated {
		t.Errorf("rotate = %d (rotated %v)", code, f.rotated)
	}
	waitRelaunch()
	f.rotErr = errors.New("set in the configuration")
	if code, _, _ := call(t, srv, "POST", "/v1/mcp/token", "tok", nil); code != 400 {
		t.Errorf("rotate refused = %d", code)
	}
}

func TestMCPTestAsksTheConnectorItself(t *testing.T) {
	fleet := &fakeFleet{kitties: map[string]*kitty.Kitty{}}
	chats := &fakeChats{sessions: map[string]*chat.Session{}}
	f := &fakeMCPControl{info: MCPInfo{MCPSettings: config.MCPSettings{Enabled: true}}, relaunch: make(chan struct{}, 1)}
	// The fake's token is the one the connector below accepts.
	srv := httptest.NewServer(New(fleet, chats, "tok", "test").WithMCP("secret-token-value", false).WithMCPControl(f))
	defer srv.Close()
	code, got, _ := call(t, srv, "POST", "/v1/mcp/test", "tok", nil)
	if code != 200 || got["ok"] != true || got["tools"].(float64) < 5 {
		t.Fatalf("test = %d %v", code, got)
	}
	if _, leaked := got["token"]; leaked {
		t.Error("the test result carries the token")
	}
	// a token the connector does not know: it says so, without failing the request
	f.tokenErr = nil
	other := httptest.NewServer(New(fleet, chats, "tok", "test").WithMCP("another-token-value", false).WithMCPControl(f))
	defer other.Close()
	if code, got, _ := call(t, other, "POST", "/v1/mcp/test", "tok", nil); code != 200 || got["ok"] != false || got["error"] == "" {
		t.Errorf("wrong token = %d %v", code, got)
	}
	// off, or no token yet
	f.info.Enabled = false
	if code, got, _ := call(t, srv, "POST", "/v1/mcp/test", "tok", nil); code != 200 || got["ok"] != false {
		t.Errorf("off = %d %v", code, got)
	}
	f.info.Enabled = true
	f.tokenErr = errors.New("no token")
	if _, got, _ := call(t, srv, "POST", "/v1/mcp/test", "tok", nil); got["ok"] != false {
		t.Errorf("no token = %v", got)
	}
	plain := httptest.NewServer(New(fleet, chats, "tok", "test"))
	defer plain.Close()
	if code, _, _ := call(t, plain, "POST", "/v1/mcp/test", "tok", nil); code != 404 {
		t.Errorf("without control = %d", code)
	}
}
