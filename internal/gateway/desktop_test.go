package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

// powerFleet is a fleet whose kitties have a machine.
type powerFleet struct {
	*fakeFleet
	power  kitty.Power
	inUse  bool
	ons    []string
	offs   []kitty.OffOptions
	offErr error
}

func (p *powerFleet) Power(ref string) (kitty.Power, error) {
	if _, err := p.Get(ref); err != nil {
		return kitty.Power{}, err
	}
	if ref == "bo" {
		return kitty.Power{}, kitty.ErrNoMachine
	}
	return p.power, nil
}
func (p *powerFleet) PowerOn(ref string) (*kitty.Kitty, error) {
	if ref == "bo" {
		return nil, kitty.ErrNoMachine
	}
	p.ons = append(p.ons, ref)
	p.power.State = kitty.PowerStarting
	return p.Get(ref)
}
func (p *powerFleet) PowerOff(_ context.Context, ref string, o kitty.OffOptions) error {
	if p.inUse && !o.Force {
		return &kitty.InUseError{Kitty: "ana", By: kitty.UseTask}
	}
	p.offs = append(p.offs, o)
	p.power.State = kitty.PowerOff
	return p.offErr
}

func TestPowerRoutes(t *testing.T) {
	f := &fakeFleet{kitties: map[string]*kitty.Kitty{
		"a1": {ID: "a1", Name: "ana", Desktop: true, Status: kitty.StatusRunning},
		"b1": {ID: "b1", Name: "bo"},
	}}
	chats := &fakeChats{sessions: map[string]*chat.Session{}}
	// A fleet that cannot turn machines on and off.
	plain := httptest.NewServer(New(f, chats, "tok", "test"))
	defer plain.Close()
	if code, _, _ := call(t, plain, "GET", "/v1/kitties/ana/power", "tok", nil); code != 404 {
		t.Errorf("without power = %d", code)
	}
	p := &powerFleet{fakeFleet: f, power: kitty.Power{State: kitty.PowerOn, AutoOffMinutes: 15}}
	srv := httptest.NewServer(New(p, chats, "tok", "test"))
	defer srv.Close()
	if code, _, _ := call(t, srv, "GET", "/v1/kitties/ana/power", "", nil); code != 401 {
		t.Errorf("without a token = %d", code)
	}
	code, got, _ := call(t, srv, "GET", "/v1/kitties/ana/power", "tok", nil)
	pw, _ := got["power"].(map[string]any)
	if code != 200 || pw["state"] != "on" || pw["auto_off_minutes"] != 15.0 {
		t.Errorf("power = %d %v", code, got)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/kitties/zz/power", "tok", nil); code != 404 {
		t.Errorf("unknown = %d", code)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/kitties/bo/power", "tok", nil); code != 400 {
		t.Errorf("no machine = %d", code)
	}
	// Off, then on (in the background: 202), and the body is checked.
	if code, got, _ := call(t, srv, "POST", "/v1/kitties/ana/power", "tok", `{"on": false}`); code != 200 || len(p.offs) != 1 || p.offs[0].Reason != kitty.OffManual || p.offs[0].Force || got["power"].(map[string]any)["state"] != "off" {
		t.Errorf("off = %d %v %+v", code, got, p.offs)
	}
	if code, got, _ := call(t, srv, "POST", "/v1/kitties/ana/power", "tok", `{"on": true}`); code != http.StatusAccepted || len(p.ons) != 1 || got["power"].(map[string]any)["state"] != "starting" {
		t.Errorf("on = %d %v", code, got)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/kitties/ana/power", "tok", `{}`); code != 400 {
		t.Errorf("no choice = %d", code)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/kitties/bo/power", "tok", `{"on": true}`); code != 400 {
		t.Errorf("on without a machine = %d", code)
	}
	// In use: 409 unless forced.
	p.inUse = true
	if code, got, _ := call(t, srv, "POST", "/v1/kitties/ana/power", "tok", `{"on": false}`); code != 409 || !strings.Contains(got["error"].(string), "working on a task") {
		t.Errorf("off in use = %d %v", code, got)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/kitties/ana/power", "tok", `{"on": false, "force": true}`); code != 200 || !p.offs[len(p.offs)-1].Force {
		t.Errorf("forced off = %d", code)
	}
}

// fleetMaintainer also looks for a new desktop image and updates every desktop.
type fleetMaintainer struct {
	fakeMaintainer
	checkErr, updErr error
	checks           int
	asked            [][]string
}

func (f *fleetMaintainer) DesktopUpdates(context.Context) orchestrator.DesktopFleet {
	return orchestrator.DesktopFleet{Image: "img:latest", Available: 1, Kitties: []orchestrator.FleetDesktop{{ID: "a1", Name: "ana", UpdateAvailable: true}}}
}
func (f *fleetMaintainer) CheckDesktopUpdates() error { f.checks++; return f.checkErr }
func (f *fleetMaintainer) UpdateDesktops(_ context.Context, ids []string) (*orchestrator.DesktopJob, error) {
	if f.updErr != nil {
		return nil, f.updErr
	}
	f.asked = append(f.asked, ids)
	return &orchestrator.DesktopJob{Total: 1}, nil
}

func TestDesktopFleetRoutes(t *testing.T) {
	f := &fakeFleet{kitties: map[string]*kitty.Kitty{"a1": {ID: "a1", Name: "ana", Status: kitty.StatusRunning}}}
	chats := &fakeChats{sessions: map[string]*chat.Session{}}
	// A maintainer that cannot update every desktop (an older one), and no maintainer at all.
	for _, srv := range []*httptest.Server{
		httptest.NewServer(New(f, chats, "tok", "test")),
		httptest.NewServer(New(f, chats, "tok", "test").WithMaintenance(&fakeMaintainer{})),
	} {
		for _, c := range [][2]string{{"GET", "/v1/desktops/updates"}, {"POST", "/v1/desktops/updates/check"}, {"POST", "/v1/desktops/updates"}} {
			if code, _, _ := call(t, srv, c[0], c[1], "tok", nil); code != 404 {
				t.Errorf("%s %s = %d, want 404", c[0], c[1], code)
			}
		}
		srv.Close()
	}
	m := &fleetMaintainer{}
	srv := httptest.NewServer(New(f, chats, "tok", "test").WithMaintenance(m))
	defer srv.Close()
	if code, got, _ := call(t, srv, "GET", "/v1/desktops/updates", "tok", nil); code != 200 || got["available"] != 1.0 || got["image"] != "img:latest" {
		t.Errorf("news = %d %v", code, got)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/desktops/updates/check", "tok", nil); code != 202 || m.checks != 1 {
		t.Errorf("check = %d (%d)", code, m.checks)
	}
	// All of them (no body), or the ones named.
	if code, _, _ := call(t, srv, "POST", "/v1/desktops/updates", "tok", nil); code != 202 || len(m.asked) != 1 || m.asked[0] != nil {
		t.Errorf("update all = %d %v", code, m.asked)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/desktops/updates", "tok", map[string]any{"kitties": []string{"a1"}}); code != 202 || len(m.asked) != 2 || m.asked[1][0] != "a1" {
		t.Errorf("update some = %d %v", code, m.asked)
	}
	if code, _, _ := call(t, srv, "POST", "/v1/desktops/updates", "tok", "{nope"); code != 400 {
		t.Errorf("a bad body = %d", code)
	}
	// One at a time; nothing to update is the caller's mistake; an unknown kitty is not found.
	for err, want := range map[string]int{"an update of the desktops is already running": 409, "no desktop on this machine waits for an update": 400, "no such kitty": 404} {
		m.checkErr, m.updErr = errors.New(err), errors.New(err)
		if code, _, _ := call(t, srv, "POST", "/v1/desktops/updates", "tok", nil); code != want {
			t.Errorf("update with %q = %d, want %d", err, code, want)
		}
		if err != "no such kitty" {
			if code, _, _ := call(t, srv, "POST", "/v1/desktops/updates/check", "tok", nil); code != map[bool]int{true: 409, false: 500}[want == 409] {
				t.Errorf("check with %q = %d", err, code)
			}
		}
	}
}
