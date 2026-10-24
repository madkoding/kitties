package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/keep"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/orchestrator"
)

type fakeMaintainer struct {
	stale   bool
	busy    bool
	updated []string
	err     error
}

func (f *fakeMaintainer) DesktopInfo(_ context.Context, ref string) (orchestrator.DesktopInfo, error) {
	if f.err != nil {
		return orchestrator.DesktopInfo{}, f.err
	}
	return orchestrator.DesktopInfo{Image: "img:latest", UpdateAvailable: f.stale, Backup: &keep.Info{Saved: time.Unix(1, 0).UTC(), Bytes: 42}}, nil
}
func (f *fakeMaintainer) Backup(context.Context, string) (keep.Info, error) {
	if f.err != nil {
		return keep.Info{}, f.err
	}
	return keep.Info{Saved: time.Unix(2, 0).UTC(), Bytes: 7}, nil
}
func (f *fakeMaintainer) UpdateInBackground(ref string) (*kitty.Kitty, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.updated = append(f.updated, ref)
	return &kitty.Kitty{ID: ref, Name: "ana", Status: kitty.StatusStarting}, nil
}

// busyChats makes the fake chats say a kitty is in the middle of a task.
type busyFakeChats struct {
	*fakeChats
	busy bool
}

func (b busyFakeChats) KittyBusy(string) bool { return b.busy }

func TestDesktopRoutes(t *testing.T) {
	f := &fakeFleet{kitties: map[string]*kitty.Kitty{"a1": {ID: "a1", Name: "ana", Status: kitty.StatusRunning}}}
	chats := busyFakeChats{fakeChats: &fakeChats{sessions: map[string]*chat.Session{}}}
	plain := httptest.NewServer(New(f, chats, "tok", "test"))
	defer plain.Close()
	for _, c := range [][2]string{{"GET", "/v1/kitties/ana/desktop"}, {"POST", "/v1/kitties/ana/backup"}, {"POST", "/v1/kitties/ana/update"}} {
		if code, _, _ := call(t, plain, c[0], c[1], "tok", nil); code != 404 {
			t.Errorf("%s %s without a maintainer = %d", c[0], c[1], code)
		}
	}
	m := &fakeMaintainer{stale: true}
	srv := httptest.NewServer(New(f, chats, "tok", "test").WithMaintenance(m))
	defer srv.Close()
	code, got, _ := call(t, srv, "GET", "/v1/kitties/ana/desktop", "tok", nil)
	if code != 200 || got["update_available"] != true || got["image"] != "img:latest" || got["backup"].(map[string]any)["bytes"] != 42.0 {
		t.Errorf("info = %d %v", code, got)
	}
	if code, got, _ := call(t, srv, "POST", "/v1/kitties/ana/backup", "tok", nil); code != 200 || got["backup"].(map[string]any)["bytes"] != 7.0 {
		t.Errorf("backup = %d %v", code, got)
	}
	// An update starts in the background, by the kitty's id; one in the middle of a task is not interrupted.
	if code, _, _ := call(t, srv, "POST", "/v1/kitties/ana/update", "tok", nil); code != http.StatusAccepted || len(m.updated) != 1 || m.updated[0] != "a1" {
		t.Errorf("update = %d %v", code, m.updated)
	}
	chats.busy = true
	busy := httptest.NewServer(New(f, chats, "tok", "test").WithMaintenance(m))
	defer busy.Close()
	if code, _, _ := call(t, busy, "POST", "/v1/kitties/ana/update", "tok", nil); code != 409 || len(m.updated) != 1 {
		t.Errorf("a busy kitty was updated: %d %v", code, m.updated)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/kitties/zz/update", "tok", nil); code != 404 {
		t.Errorf("unknown kitty = %d", code)
	}
	// Why it failed picks the answer.
	for err, want := range map[string]int{"ana has the desktop of x": 409, "ana is not running: start it": 400, "there is nothing to back up yet": 400, "boom": 500} {
		m.err = errors.New(err)
		for _, c := range [][2]string{{"GET", "/v1/kitties/ana/desktop"}, {"POST", "/v1/kitties/ana/backup"}, {"POST", "/v1/kitties/ana/update"}} {
			if code, _, _ := call(t, srv, c[0], c[1], "tok", nil); code != want {
				t.Errorf("%s %s with %q = %d, want %d", c[0], c[1], err, code, want)
			}
		}
	}
}
