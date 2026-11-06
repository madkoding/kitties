package oauth

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDavRoot(t *testing.T) {
	for in, want := range map[string]string{
		"https://cloud.example.com/":                      "https://cloud.example.com",
		"https://cloud.example.com/remote.php/dav":        "https://cloud.example.com",
		"https://example.com/nc/remote.php/dav/?x=1#y":    "https://example.com/nc",
		" http://192.168.1.5:8080/nextcloud/index.php/ ": "http://192.168.1.5:8080/nextcloud/index.php",
	} {
		if u, err := davRoot(in); err != nil || u.String() != want {
			t.Errorf("%q = %v %v, want %s", in, u, err, want)
		}
	}
	for _, bad := range []string{"", "cloud.example.com", "ftp://x", "::"} {
		if _, err := davRoot(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

// flowStore records what a login flow saved, safely from the poll's goroutine.
type flowStore struct {
	memStore
	saved chan struct{}
	once  sync.Once
}

func (f *flowStore) SaveOAuthCredentials(name string, c Credentials) error {
	err := f.memStore.SaveOAuthCredentials(name, c)
	f.once.Do(func() { close(f.saved) })
	return err
}

func TestNextcloudLoginFlow(t *testing.T) {
	if !Providers["nextcloud"].Ready() {
		t.Error("the login flow needs no app")
	}
	st := &flowStore{memStore: memStore{provider: "nextcloud", settings: map[string]string{"url": "https://cloud.example.com/remote.php/dav"}}, saved: make(chan struct{})}
	r := &routes{answers: map[string]string{
		"cloud.example.com/index.php/login/v2": `{"poll":{"token":"tok","endpoint":"https://cloud.example.com/login/v2/poll"},"login":"https://cloud.example.com/login/v2/flow/abc"}`,
	}, codes: map[string]int{}}
	var mu sync.Mutex
	polls := 0
	m := New(st)
	m.httpDo = func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		if strings.HasSuffix(req.URL.Path, "/poll") {
			polls++
			if polls == 2 {
				// The person allowed Kitties: the next poll answers.
				r.answers["cloud.example.com/login/v2/poll"] = `{"server":"https://cloud.example.com","loginName":"ana","appPassword":"app-pw"}`
			}
		}
		return r.do(req)
	}
	m.sleep = func(time.Duration) {}
	link, err := m.Start("c", "http://127.0.0.1:7488/v1/oauth/callback")
	if err != nil || link != "https://cloud.example.com/login/v2/flow/abc" {
		t.Fatalf("start = %s %v", link, err)
	}
	select {
	case <-st.saved:
	case <-time.After(5 * time.Second):
		t.Fatal("the flow never finished")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.settings["user"] != "ana" || st.settings["password"] != "app-pw" || st.creds.Account != "ana" || !st.creds.SignedIn() || st.creds.RefreshToken != "" {
		t.Errorf("saved = %v %+v", st.settings, st.creds)
	}
	if got := r.seen[0].Header.Get("User-Agent"); got != "Kitties" {
		t.Errorf("user agent = %q", got)
	}
	if !strings.Contains(r.bodies[len(r.bodies)-1], "token=tok") {
		t.Errorf("poll body = %q", r.bodies[len(r.bodies)-1])
	}
}

func TestNextcloudLoginFlowFailures(t *testing.T) {
	start := func(settings map[string]string, answer string, code int) error {
		st := &memStore{provider: "nextcloud", settings: settings}
		r := &routes{answers: map[string]string{"cloud.example.com/index.php/login/v2": answer}, codes: map[string]int{"cloud.example.com/index.php/login/v2": code}}
		m := New(st)
		m.httpDo = r.do
		m.sleep = func(time.Duration) {}
		_, err := m.Start("c", "http://gw/cb")
		return err
	}
	at := map[string]string{"url": "https://cloud.example.com"}
	if err := start(map[string]string{}, "", 0); err == nil {
		t.Error("no address, no sign-in")
	}
	if err := start(at, "nope", 404); err == nil || !strings.Contains(err.Error(), "is it a Nextcloud") {
		t.Errorf("not a Nextcloud = %v", err)
	}
	// A poll address on another host is not followed: the app password would go there.
	if err := start(at, `{"poll":{"token":"t","endpoint":"https://evil.example/poll"},"login":"https://cloud.example.com/l"}`, 0); err == nil {
		t.Error("a poll elsewhere must be refused")
	}
	if err := start(at, `{"poll":{"token":"","endpoint":"https://cloud.example.com/poll"},"login":"x"}`, 0); err == nil {
		t.Error("no token must be refused")
	}

	// The poll: not yet (404), then an answer without a password, then a store that fails.
	st := &memStore{provider: "nextcloud"}
	r := &routes{answers: map[string]string{"cloud.example.com/poll": `{}`}, codes: map[string]int{"cloud.example.com/poll": 404}}
	m := New(st)
	m.httpDo = r.do
	if done, err := m.pollLoginFlow("c", "https://cloud.example.com/poll", "t"); done || err != nil {
		t.Errorf("not yet = %v %v", done, err)
	}
	r.codes["cloud.example.com/poll"] = 200
	if done, err := m.pollLoginFlow("c", "https://cloud.example.com/poll", "t"); !done || err == nil {
		t.Errorf("no password = %v %v", done, err)
	}
	r.answers["cloud.example.com/poll"] = `{"loginName":"ana","appPassword":"pw"}`
	st.learnErr = context.Canceled
	if done, err := m.pollLoginFlow("c", "https://cloud.example.com/poll", "t"); !done || err == nil {
		t.Errorf("store fails = %v %v", done, err)
	}
	if done, err := m.pollLoginFlow("c", "://bad", "t"); !done || err == nil {
		t.Errorf("bad endpoint = %v %v", done, err)
	}
	// The follow gives up when the page expires, and reports a flow that failed.
	clock := time.Now()
	m.now = func() time.Time { return clock }
	m.sleep = func(d time.Duration) { clock = clock.Add(5 * time.Minute) }
	r.codes["cloud.example.com/poll"] = 404
	m.followLoginFlow("c", "https://cloud.example.com/poll", "t")
	warned := ""
	m.warn = func(f string, a ...any) { warned = f }
	r.codes["cloud.example.com/poll"] = 200
	m.followLoginFlow("c", "https://cloud.example.com/poll", "t")
	if warned == "" {
		t.Error("a failed flow is reported")
	}
}
