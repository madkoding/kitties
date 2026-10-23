package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// routes answers each request by its path (and records them all).
type routes struct {
	answers map[string]string
	codes   map[string]int
	seen    []*http.Request
	bodies  []string
}

func (r *routes) do(req *http.Request) (*http.Response, error) {
	r.seen = append(r.seen, req)
	body := ""
	if req.Body != nil {
		data, _ := io.ReadAll(req.Body)
		body = string(data)
	}
	r.bodies = append(r.bodies, body)
	key := req.URL.Host + req.URL.Path
	text, ok := r.answers[key]
	if !ok {
		return nil, errors.New("no route for " + key)
	}
	code := r.codes[key]
	if code == 0 {
		code = 200
	}
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(text))}, nil
}

func signIn(t *testing.T, st *memStore, r *routes, redirect string) (*Manager, url.Values) {
	t.Helper()
	m := New(st)
	m.httpDo = r.do
	link, err := m.Start("c", redirect)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(link)
	return m, u.Query()
}

func TestBuiltinApps(t *testing.T) {
	old := builtin
	t.Cleanup(func() { builtin = old })
	builtin = "spotify=sp-id, facebook=fb-id:fb-secret"
	if id, secret := Builtin("spotify"); id != "sp-id" || secret != "" {
		t.Errorf("spotify = %s %s", id, secret)
	}
	if id, secret := Builtin("facebook"); id != "fb-id" || secret != "fb-secret" {
		t.Errorf("facebook = %s %s", id, secret)
	}
	t.Setenv("KITTIES_HOME_ASSISTANT_CLIENT_ID", "env-id")
	t.Setenv("KITTIES_HOME_ASSISTANT_CLIENT_SECRET", "env-sec")
	if id, secret := Builtin("home-assistant"); id != "env-id" || secret != "env-sec" {
		t.Errorf("env = %s %s", id, secret)
	}
	if id, _ := Builtin("tidal"); id != "" {
		t.Error("no app for tidal")
	}
	if !Providers["spotify"].Ready() || Providers["tidal"].Ready() || !Providers["mastodon"].Ready() || !Providers["home-assistant"].Ready() {
		t.Error("ready: an app built in, registered on the spot, or none needed")
	}
	// A connector with no app of its own signs in with the built-in one, and refreshes with it.
	st := &memStore{provider: "spotify"}
	r := &routes{answers: map[string]string{"accounts.spotify.com/api/token": `{"access_token":"at","refresh_token":"rt","expires_in":3600}`}}
	m, q := signIn(t, st, r, "http://127.0.0.1:7488/v1/oauth/callback")
	if q.Get("client_id") != "sp-id" {
		t.Errorf("client = %s", q.Get("client_id"))
	}
	if _, err := m.Exchange(context.Background(), q.Get("state"), "code"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.refresh(context.Background(), "c", Credentials{RefreshToken: "rt"}); err != nil || !strings.Contains(r.bodies[1], "client_id=sp-id") {
		t.Errorf("refresh = %v %s", err, r.bodies[1])
	}
	// Without any app, the message says what to do.
	if _, err := New(&memStore{provider: "tidal"}).Start("c", "http://x/cb"); err == nil || !strings.Contains(err.Error(), "needs an app") {
		t.Errorf("no app = %v", err)
	}
}

func TestInstanceAddresses(t *testing.T) {
	// GitLab's own site unless the connector names one; Zendesk's subdomain.
	_, q := signIn(t, &memStore{provider: "gitlab", id: "g"}, &routes{}, "http://gw/cb")
	if q.Get("scope") != "api" {
		t.Errorf("gitlab = %v", q)
	}
	m := New(&memStore{provider: "gitlab", id: "g", settings: map[string]string{"url": "https://git.acme.dev/"}})
	link, _ := m.Start("c", "http://gw/cb")
	if !strings.HasPrefix(link, "https://git.acme.dev/oauth/authorize?") {
		t.Errorf("self-hosted = %s", link)
	}
	if link, _ := New(&memStore{provider: "zendesk", id: "z", settings: map[string]string{"subdomain": "acme"}}).Start("c", "http://gw/cb"); !strings.HasPrefix(link, "https://acme.zendesk.com/oauth/authorizations/new?") {
		t.Errorf("zendesk = %s", link)
	}
	if _, err := New(&memStore{provider: "zendesk", id: "z"}).Start("c", "http://gw/cb"); err == nil || !strings.Contains(err.Error(), "subdomain") {
		t.Errorf("no subdomain = %v", err)
	}
	if _, err := New(&memStore{provider: "zendesk", id: "z"}).token(context.Background(), Providers["zendesk"], App{}, "", map[string]string{}); err == nil {
		t.Error("a token without its subdomain fails")
	}
}

func TestHomeAssistantNeedsNoApp(t *testing.T) {
	st := &memStore{provider: "home-assistant", settings: map[string]string{"url": "http://ha.local:8123"}}
	r := &routes{answers: map[string]string{"ha.local:8123/auth/token": `{"access_token":"at","refresh_token":"rt","expires_in":1800}`}}
	m, q := signIn(t, st, r, "http://127.0.0.1:7488/v1/oauth/callback")
	if q.Get("client_id") != "http://127.0.0.1:7488/" || q.Get("scope") != "" {
		t.Errorf("the gateway is the app: %v", q)
	}
	if _, err := m.Exchange(context.Background(), q.Get("state"), "code"); err != nil {
		t.Fatal(err)
	}
	if st.settings["client_id"] != "http://127.0.0.1:7488/" || st.creds.AccessToken != "at" {
		t.Errorf("kept = %v %+v", st.settings, st.creds)
	}
	if !strings.Contains(r.bodies[0], "client_id=http%3A%2F%2F127.0.0.1%3A7488%2F") {
		t.Errorf("token request = %s", r.bodies[0])
	}
	st.learnErr = errors.New("disk full")
	m2 := New(st)
	m2.httpDo = r.do
	link, _ := m2.Start("c", "http://127.0.0.1:7488/v1/oauth/callback")
	u, _ := url.Parse(link)
	if _, err := m2.Exchange(context.Background(), u.Query().Get("state"), "code"); err == nil {
		t.Error("a sign-in that cannot keep its app fails")
	}
	if origin("::") != "::" {
		t.Error("origin of nothing")
	}
}

func TestMastodonRegistersItself(t *testing.T) {
	st := &memStore{provider: "mastodon", settings: map[string]string{"url": "https://mastodon.social"}}
	r := &routes{answers: map[string]string{
		"mastodon.social/api/v1/apps":                        `{"client_id":"m-id","client_secret":"m-sec"}`,
		"mastodon.social/oauth/token":                        `{"access_token":"at"}`,
		"mastodon.social/api/v1/accounts/verify_credentials": `{"acct":"ana"}`,
	}}
	m, q := signIn(t, st, r, "http://127.0.0.1:7488/v1/oauth/callback")
	if q.Get("client_id") != "m-id" || st.settings["client_id"] != "m-id" || st.settings["client_secret"] != "m-sec" || q.Get("scope") != "read write" {
		t.Errorf("registered = %v %v", q, st.settings)
	}
	if !strings.Contains(r.bodies[0], "client_name=Kitties") || !strings.Contains(r.bodies[0], "redirect_uris=http") {
		t.Errorf("registration = %s", r.bodies[0])
	}
	if _, err := m.Exchange(context.Background(), q.Get("state"), "code"); err != nil {
		t.Fatal(err)
	}
	if st.creds.Account != "@ana" || st.creds.Expiry.Year() < 2100 {
		t.Errorf("signed in = %+v", st.creds)
	}
	// The next sign-in reuses the registered app.
	_, q = signIn(t, st, r, "http://127.0.0.1:7488/v1/oauth/callback")
	if q.Get("client_id") != "m-id" || len(r.seen) != 3 {
		t.Errorf("reused = %v after %d requests", q.Get("client_id"), len(r.seen))
	}
	for name, st := range map[string]*memStore{
		"no instance": {provider: "mastodon", settings: map[string]string{"url": " "}},
		"refused":     {provider: "mastodon", settings: map[string]string{"url": "https://down.example"}},
	} {
		m := New(st)
		m.httpDo = (&routes{answers: map[string]string{"down.example/api/v1/apps": `{}`}}).do
		if _, err := m.Start("c", "http://gw/cb"); err == nil {
			t.Errorf("%s: registers", name)
		}
	}
	saveFails := &memStore{provider: "mastodon", settings: map[string]string{"url": "https://mastodon.social"}, learnErr: errors.New("disk full")}
	m = New(saveFails)
	m.httpDo = r.do
	if _, err := m.Start("c", "http://gw/cb"); err == nil {
		t.Error("an app that cannot be kept fails")
	}
}

func TestTokenRequestShapes(t *testing.T) {
	// Notion: the secret as Basic, the body as JSON, a token for good, the workspace as the account.
	st := &memStore{provider: "notion", id: "n-id", secret: "n-sec"}
	r := &routes{answers: map[string]string{"api.notion.com/v1/oauth/token": `{"access_token":"at","workspace_name":"Acme"}`}}
	m, q := signIn(t, st, r, "http://gw/cb")
	if q.Get("owner") != "user" {
		t.Errorf("notion = %v", q)
	}
	if _, err := m.Exchange(context.Background(), q.Get("state"), "code"); err != nil {
		t.Fatal(err)
	}
	user, pass, ok := r.seen[0].BasicAuth()
	var body map[string]string
	_ = json.Unmarshal([]byte(r.bodies[0]), &body)
	if !ok || user != "n-id" || pass != "n-sec" || r.seen[0].Header.Get("Content-Type") != "application/json" || body["code"] != "code" || body["client_secret"] != "" {
		t.Errorf("notion request = %v %s", r.seen[0].Header, r.bodies[0])
	}
	if st.creds.Account != "Acme" || st.creds.Expiry.Year() < 2100 {
		t.Errorf("notion creds = %+v", st.creds)
	}
	// A refusal with an error object is a refusal.
	r.answers["api.notion.com/v1/oauth/token"] = `{"error":{"code":"invalid_grant"}}`
	r.codes = map[string]int{"api.notion.com/v1/oauth/token": 400}
	_, q = signIn(t, st, r, "http://gw/cb")
	m2 := New(st)
	m2.httpDo = r.do
	if _, err := m2.token(context.Background(), Providers["notion"], App{}, "n-sec", map[string]string{"client_id": "n-id"}); err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("refused = %v", err)
	}
}

func TestFinishes(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		finish  func(context.Context, HTTPDo, App, Credentials) (Credentials, map[string]string, error)
		app     App
		answers map[string]string
		account string
		learned map[string]string
		token   string
	}{
		{"facebook page", facebookFinish, App{Kind: "facebook", ClientID: "fb", ClientSecret: "s"}, map[string]string{
			"graph.facebook.com/v23.0/oauth/access_token": `{"access_token":"long"}`,
			"graph.facebook.com/v23.0/me/accounts":        `{"data":[{"id":"P1","name":"Kitties Café","access_token":"page-token"},{"id":"P2","name":"Other","access_token":"t2"}]}`,
		}, "Kitties Café", map[string]string{"page_id": "P1"}, "page-token"},
		{"the chosen page", facebookFinish, App{Kind: "facebook", Settings: map[string]string{"page_id": "P2"}}, map[string]string{
			"graph.facebook.com/v23.0/oauth/access_token": `{}`,
			"graph.facebook.com/v23.0/me/accounts":        `{"data":[{"id":"P1","name":"a","access_token":"t1"},{"id":"P2","name":"Other","access_token":"t2"}]}`,
		}, "Other", map[string]string{"page_id": "P2"}, "t2"},
		{"instagram", facebookFinish, App{Kind: "instagram"}, map[string]string{
			"graph.facebook.com/v23.0/oauth/access_token": `{"access_token":"long"}`,
			"graph.facebook.com/v23.0/me/accounts":        `{"data":[{"id":"P1","name":"a","access_token":"t1"},{"id":"P2","name":"Shop","access_token":"t2","instagram_business_account":{"id":"IG9"}}]}`,
		}, "Shop (Instagram)", map[string]string{"page_id": "P2", "account_id": "IG9"}, "t2"},
		{"linkedin", linkedinFinish, App{Settings: map[string]string{}}, map[string]string{
			"api.linkedin.com/v2/userinfo": `{"sub":"abc","name":"Ana Paz"}`,
		}, "Ana Paz", map[string]string{"author": "urn:li:person:abc"}, "user"},
		{"x", xFinish, App{}, map[string]string{"api.x.com/2/users/me": `{"data":{"id":"42","username":"ana"}}`}, "@ana", map[string]string{"user_id": "42"}, "user"},
		{"x without a profile", xFinish, App{}, map[string]string{"api.x.com/2/users/me": `{}`}, "", nil, "user"},
		{"jira", atlassianFinish, App{Settings: map[string]string{"url": "https://two.atlassian.net/"}}, map[string]string{
			"api.atlassian.com/oauth/token/accessible-resources": `[{"id":"c1","url":"https://one.atlassian.net","name":"One"},{"id":"c2","url":"https://two.atlassian.net","name":"Two"}]`,
		}, "Two", map[string]string{"cloud_id": "c2", "url": "https://two.atlassian.net"}, "user"},
		{"mastodon without a profile", mastodonFinish, App{Settings: map[string]string{"url": "https://m"}}, map[string]string{}, "", nil, "user"},
	}
	for _, c := range cases {
		r := &routes{answers: c.answers}
		got, learned, err := c.finish(ctx, r.do, c.app, Credentials{AccessToken: "user"})
		if err != nil || got.Account != c.account || got.AccessToken != c.token {
			t.Errorf("%s = %+v %v", c.name, got, err)
		}
		for k, v := range c.learned {
			if learned[k] != v {
				t.Errorf("%s learned %v, want %s=%s", c.name, learned, k, v)
			}
		}
	}
	failures := []struct {
		name    string
		finish  func(context.Context, HTTPDo, App, Credentials) (Credentials, map[string]string, error)
		app     App
		answers map[string]string
		want    string
	}{
		{"no page", facebookFinish, App{Kind: "facebook"}, map[string]string{"graph.facebook.com/v23.0/me/accounts": `{"data":[]}`}, "manages no Page"},
		{"no instagram", facebookFinish, App{Kind: "instagram"}, map[string]string{"graph.facebook.com/v23.0/me/accounts": `{"data":[{"id":"P1"}]}`}, "Instagram professional account"},
		{"pages refused", facebookFinish, App{Kind: "facebook"}, map[string]string{}, "did not list the Pages"},
		{"linkedin refused", linkedinFinish, App{}, map[string]string{}, "did not say who"},
		{"no jira site", atlassianFinish, App{}, map[string]string{"api.atlassian.com/oauth/token/accessible-resources": `[]`}, "no Jira site"},
		{"jira refused", atlassianFinish, App{}, map[string]string{}, "did not list"},
	}
	for _, c := range failures {
		r := &routes{answers: c.answers}
		if _, _, err := c.finish(ctx, r.do, c.app, Credentials{AccessToken: "user"}); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s = %v", c.name, err)
		}
	}
	r := &routes{answers: map[string]string{"x/y": "nope"}, codes: map[string]int{"x/y": 500}}
	if err := getJSON(ctx, r.do, "http://x/y", "", &struct{}{}); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("a failure = %v", err)
	}
	if err := getJSON(ctx, r.do, "::", "", &struct{}{}); err == nil {
		t.Error("a bad address fails")
	}
}

func TestFinishInTheFlow(t *testing.T) {
	// A sign-in whose Finish learns something keeps it; one whose Finish fails fails.
	st := &memStore{provider: "linkedin", id: "li", secret: "s", settings: map[string]string{}}
	r := &routes{answers: map[string]string{
		"www.linkedin.com/oauth/v2/accessToken": `{"access_token":"at","expires_in":5184000}`,
		"api.linkedin.com/v2/userinfo":          `{"sub":"abc","name":"Ana"}`,
	}}
	m, q := signIn(t, st, r, "http://gw/cb")
	if _, err := m.Exchange(context.Background(), q.Get("state"), "code"); err != nil {
		t.Fatal(err)
	}
	if st.settings["author"] != "urn:li:person:abc" || st.creds.Account != "Ana" {
		t.Errorf("learned = %v %+v", st.settings, st.creds)
	}
	delete(r.answers, "api.linkedin.com/v2/userinfo")
	m, q = signIn(t, st, r, "http://gw/cb")
	if _, err := m.Exchange(context.Background(), q.Get("state"), "code"); err == nil {
		t.Error("a Finish that fails fails the sign-in")
	}
	st.learnErr = errors.New("disk full")
	delete(st.settings, "author")
	r.answers["api.linkedin.com/v2/userinfo"] = `{"sub":"abc"}`
	m, q = signIn(t, st, r, "http://gw/cb")
	if _, err := m.Exchange(context.Background(), q.Get("state"), "code"); err == nil {
		t.Error("what cannot be kept fails the sign-in")
	}
	// The gateway as the app with a Finish too: the app is kept with what was learned.
	old := Providers["home-assistant"]
	t.Cleanup(func() { Providers["home-assistant"] = old })
	ha := old
	ha.Finish = func(ctx context.Context, do HTTPDo, app App, c Credentials) (Credentials, map[string]string, error) {
		return c, nil, nil
	}
	Providers["home-assistant"] = ha
	hs := &memStore{provider: "home-assistant", settings: map[string]string{"url": "http://ha"}}
	hr := &routes{answers: map[string]string{"ha/auth/token": `{"access_token":"at"}`}}
	m, q = signIn(t, hs, hr, "http://127.0.0.1:7488/v1/oauth/callback")
	if _, err := m.Exchange(context.Background(), q.Get("state"), "code"); err != nil || hs.settings["client_id"] != "http://127.0.0.1:7488/" {
		t.Errorf("self client with a finish = %v %v", err, hs.settings)
	}
	if _, err := New(&memStore{provider: "nope"}).refresh(context.Background(), "c", Credentials{RefreshToken: "r"}); err == nil {
		t.Error("an unknown provider cannot refresh")
	}
	if _, err := New(&memStore{provider: "google", appErr: errors.New("gone")}).refresh(context.Background(), "c", Credentials{RefreshToken: "r"}); err == nil {
		t.Error("a missing connector cannot refresh")
	}
	if _, err := New(&memStore{provider: "google", appErr: errors.New("gone")}).Exchange(context.Background(), "nope", "x"); err == nil {
		t.Error("an unknown state fails")
	}
}
