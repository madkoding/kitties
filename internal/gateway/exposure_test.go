package gateway

import (
	"errors"
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

type fakeRotating struct {
	fakeExposure
	rotated int
	err     error
}

func (f *fakeRotating) RotateGatewayToken() error {
	if f.err != nil {
		return f.err
	}
	f.rotated++
	return nil
}

func TestRotateTokenRoute(t *testing.T) {
	old := restartAfter
	restartAfter = time.Millisecond
	t.Cleanup(func() { restartAfter = old })
	fleet := &fakeFleet{kitties: map[string]*kitty.Kitty{}}
	chats := &fakeChats{sessions: map[string]*chat.Session{}}
	// A gateway without the control, or whose exposure cannot rotate, says so.
	plain := httptest.NewServer(New(fleet, chats, "tok", "test"))
	defer plain.Close()
	if code, _, _ := call(t, plain, "POST", "/v1/token/rotate", "tok", nil); code != 404 {
		t.Errorf("without exposure = %d", code)
	}
	only := httptest.NewServer(New(fleet, chats, "tok", "test").WithExposure(&fakeExposure{relaunch: make(chan struct{})}))
	defer only.Close()
	if code, _, _ := call(t, only, "POST", "/v1/token/rotate", "tok", nil); code != 404 {
		t.Errorf("exposure that cannot rotate = %d", code)
	}
	fixed := &fakeRotating{fakeExposure: fakeExposure{relaunch: make(chan struct{})}, err: errors.New("set in the configuration")}
	fsrv := httptest.NewServer(New(fleet, chats, "tok", "test").WithExposure(fixed))
	defer fsrv.Close()
	if code, _, _ := call(t, fsrv, "POST", "/v1/token/rotate", "tok", nil); code != 400 {
		t.Errorf("a fixed token = %d", code)
	}
	f := &fakeRotating{fakeExposure: fakeExposure{relaunch: make(chan struct{})}}
	srv := httptest.NewServer(New(fleet, chats, "tok", "test").WithExposure(f))
	defer srv.Close()
	if code, _, _ := call(t, srv, "POST", "/v1/token/rotate", "wrong", nil); code != 401 || f.rotated != 0 {
		t.Errorf("without the token = %d (rotated %d)", code, f.rotated)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/token/rotate", "tok", nil); code != 202 || f.rotated != 1 {
		t.Errorf("rotate = %d (rotated %d)", code, f.rotated)
	}
	select {
	case <-f.relaunch:
	case <-time.After(2 * time.Second):
		t.Error("the gateway did not restart")
	}
}
