package oauth

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestTrelloImplicitFlow(t *testing.T) {
	old := builtin
	t.Cleanup(func() { builtin = old })
	builtin = "trello=kitties-key"
	st := &memStore{provider: "trello", settings: map[string]string{"token": "typed"}}
	m := New(st)
	link, err := m.Start("c", "http://127.0.0.1:7488/v1/oauth/callback")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(link)
	q := u.Query()
	if u.Host != "trello.com" || q.Get("key") != "kitties-key" || q.Get("expiration") != "never" || q.Get("response_type") != "token" || q.Get("scope") != "read,write" {
		t.Errorf("authorize = %s", link)
	}
	back, _ := url.Parse(q.Get("return_url"))
	state := back.Query().Get("state")
	if back.Path != "/v1/oauth/callback" || state == "" || !m.Implicit(state) || m.Implicit("nope") {
		t.Errorf("return = %s", back)
	}
	// A code exchange does not take an implicit flow.
	if _, err := m.Deliver("nope", "t"); err == nil {
		t.Error("unknown state")
	}
	if _, err := m.Deliver(state, " "); err == nil || !strings.Contains(err.Error(), "no token") {
		t.Errorf("empty token = %v", err)
	}
	// The empty token used the flow up: start again.
	link, _ = m.Start("c", "http://127.0.0.1:7488/v1/oauth/callback")
	u, _ = url.Parse(link)
	back, _ = url.Parse(u.Query().Get("return_url"))
	state = back.Query().Get("state")
	if name, err := m.Deliver(state, "tok"); err != nil || name != "c" {
		t.Fatalf("deliver = %s %v", name, err)
	}
	// The key Kitties signed in with is kept for the calls; the typed token gives way.
	if st.settings["key"] != "kitties-key" || st.settings["token"] != "" || st.creds.AccessToken != "tok" || !st.creds.SignedIn() || m.Due("c") {
		t.Errorf("saved = %v %+v", st.settings, st.creds)
	}
	// The person's own key wins, and is not written again.
	own := &memStore{provider: "trello", settings: map[string]string{"key": "mine"}}
	m = New(own)
	link, _ = m.Start("c", "http://gw/cb")
	if !strings.Contains(link, "key=mine") {
		t.Errorf("own key = %s", link)
	}
	u, _ = url.Parse(link)
	back, _ = url.Parse(u.Query().Get("return_url"))
	own.learnErr = errors.New("disk full")
	if _, err := m.Deliver(back.Query().Get("state"), "tok"); err == nil {
		t.Error("what cannot be kept fails")
	}
	gone := &memStore{provider: "trello", settings: map[string]string{"key": "k"}}
	m = New(gone)
	link, _ = m.Start("c", "http://gw/cb")
	u, _ = url.Parse(link)
	back, _ = url.Parse(u.Query().Get("return_url"))
	gone.appErr = errors.New("gone")
	if _, err := m.Deliver(back.Query().Get("state"), "tok"); err == nil {
		t.Error("a connector removed meanwhile fails")
	}
	// Without Kitties' key nor the person's, it says what to make.
	builtin = ""
	if _, err := New(&memStore{provider: "trello", settings: map[string]string{}}).Start("c", "http://gw/cb"); err == nil {
		t.Error("no key, no sign-in")
	}
}
