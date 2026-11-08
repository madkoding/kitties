package gateway

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/config"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/tidy"
)

type fakeStorage struct {
	r       config.Retention
	cleaned int
}

func (f *fakeStorage) Usage() tidy.Usage {
	return tidy.Usage{Total: 3000, Areas: []tidy.Area{{Key: tidy.AreaShots, Bytes: 3000, Count: 3}}, Retention: f.r}
}
func (f *fakeStorage) Clean(context.Context) tidy.Result { f.cleaned++; return tidy.Result{Shots: 1} }
func (f *fakeStorage) SetRetention(r config.Retention) error {
	if r.Validate() != nil {
		return errors.New("bad retention")
	}
	f.r = r
	return nil
}

func TestStorageRoutes(t *testing.T) {
	fleet := &fakeFleet{kitties: map[string]*kitty.Kitty{}}
	chats := &fakeChats{sessions: map[string]*chat.Session{}}
	plain := httptest.NewServer(New(fleet, chats, "tok", "test"))
	defer plain.Close()
	for _, c := range []struct{ method, path string }{{"GET", "/v1/settings/storage"}, {"PUT", "/v1/settings/storage"}, {"POST", "/v1/settings/storage/clean"}} {
		if code, _, _ := call(t, plain, c.method, c.path, "tok", map[string]any{}); code != 404 {
			t.Errorf("%s %s without storage = %d", c.method, c.path, code)
		}
	}
	f := &fakeStorage{}
	srv := httptest.NewServer(New(fleet, chats, "tok", "test").WithStorage(f))
	defer srv.Close()
	if code, got, _ := call(t, srv, "GET", "/v1/settings/storage", "tok", nil); code != 200 || got["total"] != float64(3000) {
		t.Fatalf("GET = %d %v", code, got)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/settings/storage", "", nil); code != 401 {
		t.Errorf("without the token = %d", code)
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/settings/storage", "tok", map[string]any{"shot_days": -5}); code != 400 {
		t.Errorf("bad retention = %d", code)
	}
	if code, _, _ := call(t, srv, "PUT", "/v1/settings/storage", "tok", "nope"); code != 400 {
		t.Errorf("bad body = %d", code)
	}
	code, got, _ := call(t, srv, "PUT", "/v1/settings/storage", "tok", map[string]any{"shot_days": 7, "shots_mb": 500})
	if code != 200 || f.r.ShotDays != 7 || f.cleaned != 1 {
		t.Errorf("PUT = %d %v (%+v, cleaned %d)", code, got, f.r, f.cleaned)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/settings/storage/clean", "tok", nil); code != 200 || f.cleaned != 2 {
		t.Errorf("clean = %d (cleaned %d)", code, f.cleaned)
	}
}
