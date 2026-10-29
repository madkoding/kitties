package gateway

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/kitty"
)

type fakeExposure struct {
	info     ExposureInfo
	set      string
	relaunch chan struct{}
}

func (f *fakeExposure) Exposure() ExposureInfo { return f.info }
func (f *fakeExposure) SetExposure(m string) error {
	f.set = m
	return nil
}
func (f *fakeExposure) Relaunch() error { close(f.relaunch); return nil }

func TestExposureRoutes(t *testing.T) {
	old := restartAfter
	restartAfter = time.Millisecond
	t.Cleanup(func() { restartAfter = old })
	fleet := &fakeFleet{kitties: map[string]*kitty.Kitty{}}
	chats := &fakeChats{sessions: map[string]*chat.Session{}}
	plain := httptest.NewServer(New(fleet, chats, "tok", "test"))
	defer plain.Close()
	if code, _, _ := call(t, plain, "GET", "/v1/exposure", "tok", nil); code != 404 {
		t.Errorf("without exposure = %d", code)
	}
	f := &fakeExposure{info: ExposureInfo{Mode: "local", Listen: "127.0.0.1:7488", CanChange: true}, relaunch: make(chan struct{})}
	srv := httptest.NewServer(New(fleet, chats, "tok", "test").WithExposure(f))
	defer srv.Close()
	if code, got, _ := call(t, srv, "GET", "/v1/exposure", "tok", nil); code != 200 || got["mode"] != "local" {
		t.Fatalf("GET = %d %v", code, got)
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/exposure", "tok", map[string]any{"mode": "moon"}); code != 400 {
		t.Errorf("bad mode = %d", code)
	}
	// wan needs the risk accepted
	if code, _, _ := call(t, srv, "PUT", "/v1/exposure", "tok", map[string]any{"mode": "wan"}); code != 400 || f.set != "" {
		t.Errorf("wan without accept_risk = %d (set %q)", code, f.set)
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/exposure", "tok", map[string]any{"mode": "wan", "accept_risk": true}); code != 202 || f.set != "wan" {
		t.Errorf("wan accepted = %d (set %q)", code, f.set)
	}
	select {
	case <-f.relaunch:
	case <-time.After(2 * time.Second):
		t.Error("the gateway did not restart")
	}
}
