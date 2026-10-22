package oauth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type memStore struct {
	provider, id, secret string
	creds                Credentials
	appErr, saveErr      error
}

func (m *memStore) OAuthApp(string) (string, string, string, error) {
	return m.provider, m.id, m.secret, m.appErr
}
func (m *memStore) OAuthCredentials(string) (Credentials, error) { return m.creds, m.appErr }
func (m *memStore) SaveOAuthCredentials(_ string, c Credentials) error {
	m.creds = c
	return m.saveErr
}

type answer struct {
	code int
	body string
	req  *http.Request
	form url.Values
}

func manager(st *memStore, answers ...*answer) *Manager {
	m := New(st)
	n := 0
	m.now = func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) }
	m.rand = func(k int) string { return strings.Repeat("s", k) }
	m.httpDo = func(req *http.Request) (*http.Response, error) {
		if n >= len(answers) {
			return nil, errors.New("no network")
		}
		a := answers[n]
		n++
		data, _ := io.ReadAll(req.Body)
		a.req = req
		a.form, _ = url.ParseQuery(string(data))
		return &http.Response{StatusCode: a.code, Body: io.NopCloser(strings.NewReader(a.body))}, nil
	}
	return m
}

func TestStartExchangeToken(t *testing.T) {
	st := &memStore{provider: "google", id: "cid", secret: "sec"}
	a := &answer{code: 200, body: `{"access_token":"at","refresh_token":"rt","expires_in":3600}`}
	m := manager(st, a)
	link, err := m.Start("gw", "http://127.0.0.1:7488/v1/oauth/callback")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(link)
	q := u.Query()
	if u.Host != "accounts.google.com" || q.Get("client_id") != "cid" || q.Get("code_challenge_method") != "S256" || q.Get("access_type") != "offline" || !strings.Contains(q.Get("scope"), "gmail.send") || q.Get("state") == "" {
		t.Errorf("link = %s", link)
	}
	if _, err := m.Exchange(context.Background(), "nope", "code"); err == nil {
		t.Error("unknown state")
	}
	name, err := m.Exchange(context.Background(), q.Get("state"), "the-code")
	if err != nil || name != "gw" || st.creds.AccessToken != "at" || st.creds.RefreshToken != "rt" || !st.creds.SignedIn() {
		t.Fatalf("Exchange = %q %v creds=%+v", name, err, st.creds)
	}
	if a.form.Get("code_verifier") == "" || a.form.Get("client_secret") != "sec" || a.form.Get("grant_type") != "authorization_code" || a.req.URL.Host != "oauth2.googleapis.com" {
		t.Errorf("token form = %v", a.form)
	}
	// A valid token is served from the store without a request.
	if tok, err := m.Token(context.Background(), "gw"); err != nil || tok != "at" {
		t.Errorf("Token = %q %v", tok, err)
	}
	// An expired one is refreshed, the refresh token kept when the answer has none.
	st.creds.Expiry = m.now().Add(-time.Hour)
	st.creds.Account = "me@x"
	refresh := &answer{code: 200, body: `{"access_token":"at2","expires_in":100}`}
	m2 := manager(st, refresh)
	if tok, err := m2.Token(context.Background(), "gw"); err != nil || tok != "at2" || st.creds.RefreshToken != "rt" || st.creds.Account != "me@x" || refresh.form.Get("grant_type") != "refresh_token" {
		t.Errorf("refresh = %q %v creds=%+v", tok, err, st.creds)
	}
	// Settings round trip.
	s := map[string]string{}
	st.creds.ToSettings(s)
	back := FromSettings(s)
	if back.AccessToken != "at2" || back.RefreshToken != "rt" || back.Account != "me@x" || back.Expiry.IsZero() {
		t.Errorf("round trip = %+v", back)
	}
	if FromSettings(map[string]string{}).SignedIn() {
		t.Error("empty is not signed in")
	}
}

func TestFailures(t *testing.T) {
	st := &memStore{provider: "microsoft", id: "cid"}
	m := manager(st)
	if _, err := New(&memStore{appErr: errors.New("no app")}).Start("x", "r"); err == nil {
		t.Error("store failure")
	}
	if _, err := New(&memStore{provider: "nope", id: "c"}).Start("x", "r"); err == nil {
		t.Error("unknown provider")
	}
	if _, err := New(&memStore{provider: "google"}).Start("x", "r"); err == nil {
		t.Error("no client id")
	}
	link, _ := m.Start("ms", "r")
	state := strings.Repeat("s", 24)
	if !strings.Contains(link, "login.microsoftonline.com") || !strings.Contains(link, "offline_access") {
		t.Errorf("link = %s", link)
	}
	if _, err := m.Exchange(context.Background(), state, "c"); err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Errorf("network: %v", err)
	}
	for _, body := range []string{`{"error":"invalid_grant","error_description":"bad code"}`, `{}`, `plain failure`} {
		m := manager(st, &answer{code: 400, body: body})
		m.Start("ms", "r")
		if _, err := m.Exchange(context.Background(), state, "c"); err == nil {
			t.Errorf("%s must fail", body)
		}
	}
	m = manager(st, &answer{code: 200, body: `{"access_token":"a"}`})
	m.Start("ms", "r")
	st.saveErr = errors.New("disk full")
	if _, err := m.Exchange(context.Background(), state, "c"); err == nil {
		t.Error("save failure")
	}
	st.saveErr = nil
	st.creds = Credentials{}
	if _, err := m.Token(context.Background(), "ms"); err == nil || !strings.Contains(err.Error(), "not signed in") {
		t.Errorf("no refresh token: %v", err)
	}
	st.creds = Credentials{RefreshToken: "rt"}
	st.appErr = errors.New("no app")
	if _, err := m.Token(context.Background(), "ms"); err == nil {
		t.Error("store failure on refresh")
	}
	st.appErr = nil
	st.provider = "nope"
	if _, err := m.Token(context.Background(), "ms"); err == nil {
		t.Error("unknown provider on refresh")
	}
	st.provider = "microsoft"
	m = manager(st, &answer{code: 500, body: "down"})
	if _, err := m.Token(context.Background(), "ms"); err == nil {
		t.Error("refresh failure")
	}
	m = manager(st, &answer{code: 200, body: `{"access_token":"b","expires_in":10}`})
	st.saveErr = errors.New("disk full")
	if _, err := m.Token(context.Background(), "ms"); err == nil {
		t.Error("refresh save failure")
	}
	// Stale pending flows are swept.
	m = manager(st)
	m.pending["old"] = flow{started: m.now().Add(-time.Hour)}
	m.Start("ms", "r")
	if _, ok := m.pending["old"]; ok {
		t.Error("old flows are swept")
	}
	if len(randomText(8)) == 0 {
		t.Error("randomText")
	}
	if _, err := m.token(context.Background(), Provider{TokenURL: "::bad"}, url.Values{}); err == nil {
		t.Error("bad token url")
	}
}
