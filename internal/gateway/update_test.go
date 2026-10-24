package gateway

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/selfupdate"
)

type fakeUpdater struct {
	mu        sync.Mutex
	st        selfupdate.Status
	checkErr  error
	applyErr  error
	checks    int
	applied   int
	relaunch  chan struct{}
	relaunchE error
}

func (f *fakeUpdater) Status() selfupdate.Status { return f.st }
func (f *fakeUpdater) Check(context.Context) (selfupdate.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks++
	return f.st, f.checkErr
}
func (f *fakeUpdater) Apply(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied++
	return f.st.Latest, f.applyErr
}
func (f *fakeUpdater) Relaunch() error { close(f.relaunch); return f.relaunchE }

func TestUpdateRoutes(t *testing.T) {
	old := restartAfter
	restartAfter = time.Millisecond
	t.Cleanup(func() { restartAfter = old })
	fleet := &fakeFleet{kitties: map[string]*kitty.Kitty{}}
	chats := &fakeChats{sessions: map[string]*chat.Session{}}
	plain := httptest.NewServer(New(fleet, chats, "tok", "test"))
	defer plain.Close()
	for _, c := range [][2]string{{"GET", "/v1/update"}, {"POST", "/v1/update/apply"}} {
		if code, _, _ := call(t, plain, c[0], c[1], "tok", nil); code != 404 {
			t.Errorf("%s %s without an updater = %d", c[0], c[1], code)
		}
	}
	f := &fakeUpdater{st: selfupdate.Status{Current: "v1.0.0", Latest: "v1.1.0", Available: true, CanApply: true, Mode: "notify"}, relaunch: make(chan struct{})}
	srv := httptest.NewServer(New(fleet, chats, "tok", "test").WithUpdater(f))
	defer srv.Close()
	// The status is what the last look found; ?refresh=1 looks now.
	code, got, _ := call(t, srv, "GET", "/v1/update", "tok", nil)
	if code != 200 || got["latest"] != "v1.1.0" || got["available"] != true || f.checks != 0 {
		t.Errorf("status = %d %v checks %d", code, got, f.checks)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/update?refresh=1", "tok", nil); code != 200 || f.checks != 1 {
		t.Errorf("refresh = %d checks %d", code, f.checks)
	}
	// Applying installs and, once it has answered, restarts.
	code, got, _ = call(t, srv, "POST", "/v1/update/apply", "tok", nil)
	if code != 202 || got["updated_to"] != "v1.1.0" || got["restarting"] != true || f.applied != 1 {
		t.Fatalf("apply = %d %v", code, got)
	}
	select {
	case <-f.relaunch:
	case <-time.After(3 * time.Second):
		t.Fatal("it did not restart")
	}
	// Why it does not: cannot, already the newest, busy, a failed install, a GitHub that does not answer.
	f.relaunch = make(chan struct{})
	for _, c := range []struct {
		name string
		set  func()
		want int
	}{
		{"it cannot", func() { f.st.CanApply, f.st.Why = false, "in a container" }, 400},
		{"the newest", func() { f.st.CanApply, f.st.Why, f.st.Available = true, "", false }, 409},
		{"busy", func() { f.st.Available, f.applyErr = true, selfupdate.ErrBusy }, 409},
		{"a bad install", func() { f.applyErr = errors.New("does not match its SHA256SUMS") }, 502},
		{"no GitHub", func() { f.checkErr = errors.New("down") }, 502},
	} {
		c.set()
		if code, _, _ := call(t, srv, "POST", "/v1/update/apply", "tok", nil); code != c.want {
			t.Errorf("%s = %d, want %d", c.name, code, c.want)
		}
	}
	if f.applied != 3 {
		t.Errorf("applied %d times", f.applied)
	}
	if code, _, _ := call(t, srv, "GET", "/v1/update", "", nil); code != 401 {
		t.Errorf("no token = %d", code)
	}
}
