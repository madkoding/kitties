package oauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type memStore struct {
	provider, id, secret string
	kind                 string
	settings             map[string]string
	mu                   sync.Mutex
	creds                Credentials
	appErr, saveErr      error
	learnErr             error
}

func (m *memStore) OAuthApp(string) (App, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := map[string]string{}
	for k, v := range m.settings {
		s[k] = v
	}
	id, secret := m.id, m.secret
	if s["client_id"] != "" {
		id, secret = s["client_id"], s["client_secret"]
	}
	return App{Provider: m.provider, Kind: m.kind, ClientID: id, ClientSecret: secret, Settings: s}, m.appErr
}
func (m *memStore) SaveOAuthSettings(_ string, learned map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.settings == nil {
		m.settings = map[string]string{}
	}
	for k, v := range learned {
		m.settings[k] = v
	}
	return m.learnErr
}
func (m *memStore) OAuthCredentials(string) (Credentials, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.creds, m.appErr
}
func (m *memStore) SaveOAuthCredentials(_ string, c Credentials) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.creds = c
	return m.saveErr
}
func (m *memStore) token() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.creds.AccessToken
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

func TestSignInDropsTheTypedToken(t *testing.T) {
	// A token typed before would win over the account: the sign-in forgets it.
	st := &memStore{provider: "github", id: "cid", settings: map[string]string{"token": "old", "repo": "a/b"}}
	m := manager(st, &answer{code: 200, body: `{"access_token":"at","expires_in":3600}`})
	link, err := m.Start("gh", "http://127.0.0.1:7488/v1/oauth/callback")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(link)
	if _, err := m.Exchange(context.Background(), u.Query().Get("state"), "code"); err != nil {
		t.Fatal(err)
	}
	if v, ok := st.settings["token"]; !ok || v != "" || st.settings["repo"] != "a/b" {
		t.Errorf("settings after the sign-in = %v", st.settings)
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
	if _, err := m.token(context.Background(), Provider{TokenURL: "::bad"}, App{}, "", map[string]string{}); err == nil {
		t.Error("bad token url")
	}
}

func TestSlackAndGitHub(t *testing.T) {
	// Slack: comma-joined scopes, a token that never expires, the workspace as the account.
	st := &memStore{provider: "slack", id: "cid", secret: "sec"}
	a := &answer{code: 200, body: `{"ok":true,"access_token":"xoxb-1","team":{"name":"Acme"}}`}
	m := manager(st, a)
	link, err := m.Start("sl", "r")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(link)
	if u.Host != "slack.com" || u.Query().Get("scope") != "chat:write,chat:write.public,channels:read" {
		t.Errorf("link = %s", link)
	}
	if _, err := m.Exchange(context.Background(), strings.Repeat("s", 24), "c"); err != nil {
		t.Fatal(err)
	}
	if st.creds.AccessToken != "xoxb-1" || st.creds.Account != "Acme" || !st.creds.SignedIn() || st.creds.Expiry.Before(m.now().Add(forever-time.Hour)) {
		t.Errorf("creds = %+v", st.creds)
	}
	if a.req.Header.Get("Accept") != "application/json" {
		t.Error("the token request asks for JSON")
	}
	// Without a refresh token nothing is due, and Token serves the stored one.
	if m.Due("sl") {
		t.Error("a token for good is never due")
	}
	if tok, err := m.Token(context.Background(), "sl"); err != nil || tok != "xoxb-1" {
		t.Errorf("Token = %q %v", tok, err)
	}
	// GitHub: an OAuth app token without expires_in is kept for good too; a GitHub App's
	// user token comes with expires_in and a refresh token and is refreshed like any other.
	st = &memStore{provider: "github", id: "cid", secret: "sec"}
	m = manager(st, &answer{code: 200, body: `{"access_token":"gho_1","token_type":"bearer","scope":"repo"}`})
	if link, _ := m.Start("gh", "r"); !strings.Contains(link, "github.com/login/oauth/authorize") || !strings.Contains(link, "scope=repo") {
		t.Errorf("link = %s", link)
	}
	if _, err := m.Exchange(context.Background(), strings.Repeat("s", 24), "c"); err != nil || st.creds.AccessToken != "gho_1" || !st.creds.SignedIn() {
		t.Fatalf("github exchange: %v %+v", err, st.creds)
	}
	// Slack's own failure shape.
	m = manager(&memStore{provider: "slack", id: "cid"}, &answer{code: 200, body: `{"ok":false,"error":"invalid_code"}`})
	m.Start("sl", "r")
	if _, err := m.Exchange(context.Background(), strings.Repeat("s", 24), "c"); err == nil || !strings.Contains(err.Error(), "invalid_code") {
		t.Errorf("slack failure: %v", err)
	}
}

func TestKeepFresh(t *testing.T) {
	st := &memStore{provider: "google", id: "cid", creds: Credentials{AccessToken: "old", RefreshToken: "rt"}}
	refresh := &answer{code: 200, body: `{"access_token":"new","expires_in":3600}`}
	m := manager(st, refresh)
	st.creds.Expiry = m.now().Add(5 * time.Minute) // within refreshAhead
	if !m.Due("gw") {
		t.Fatal("a token expiring in five minutes is due")
	}
	var warned []string
	warn := func(f string, a ...any) { warned = append(warned, fmt.Sprintf(f, a...)) }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.KeepFresh(ctx, time.Hour, func() []string { return []string{"gw", "none"} }, warn)
	}()
	// The first round runs at once; wait for it to land, then stop.
	deadline := time.Now().Add(5 * time.Second)
	for st.token() != "new" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if st.creds.AccessToken != "new" || len(warned) != 0 {
		t.Errorf("refreshed = %+v warned = %v", st.creds, warned)
	}
	if m.Due("gw") {
		t.Error("fresh for an hour: not due")
	}
	// A refresh that fails is warned about, not fatal; a store that errors is not due.
	st.creds.Expiry = m.now()
	m2 := manager(st)
	m2.refreshDue(context.Background(), []string{"gw"}, warn)
	if len(warned) != 1 || !strings.Contains(warned[0], "oauth: gw") {
		t.Errorf("warned = %v", warned)
	}
	if New(&memStore{appErr: errors.New("x")}).Due("gw") {
		t.Error("a failing store is not due")
	}
}
