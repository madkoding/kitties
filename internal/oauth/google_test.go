package oauth

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"testing"
)

// idToken builds an unsigned OpenID token with the claims given.
func idToken(claims string) string {
	return "h." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".sig"
}

func TestGoogleAsksOnlyForTheServicesTicked(t *testing.T) {
	st := &memStore{provider: "google", id: "cid", settings: map[string]string{"services": "Calendar, drive,unknown"}}
	m := manager(st)
	link, err := m.Start("g", "http://localhost:7488/v1/oauth/callback")
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.ParseQuery(strings.SplitN(link, "?", 2)[1])
	want := "openid email https://www.googleapis.com/auth/calendar.events https://www.googleapis.com/auth/drive.file"
	if q.Get("scope") != want {
		t.Errorf("scope = %q, want %q", q.Get("scope"), want)
	}
	if q.Get("redirect_uri") != "http://127.0.0.1:7488/v1/oauth/callback" {
		t.Errorf("redirect = %q, want the loopback IP", q.Get("redirect_uri"))
	}
	if q.Get("prompt") != "consent select_account" || q.Get("access_type") != "offline" || q.Get("include_granted_scopes") != "true" || q.Get("code_challenge_method") != "S256" {
		t.Errorf("query = %v", q)
	}
	if q.Has("login_hint") {
		t.Error("a new connection gets the chooser, not a hint")
	}
	// Signed in before: the same account is suggested.
	st.settings[KeyAccount] = "me@gmail.com"
	link, _ = m.Start("g", "https://kitties.example.com/v1/oauth/callback")
	q, _ = url.ParseQuery(strings.SplitN(link, "?", 2)[1])
	if q.Get("login_hint") != "me@gmail.com" || q.Get("redirect_uri") != "https://kitties.example.com/v1/oauth/callback" {
		t.Errorf("query = %v", q)
	}
}

func TestGoogleScopesDefaultToEveryService(t *testing.T) {
	got := googleScopes(nil)
	if len(got) != len(googleIdentity)+8 || got[0] != "openid" {
		t.Errorf("scopes = %v", got)
	}
	for _, n := range GoogleServiceOrder {
		if len(GoogleServices[n]) == 0 {
			t.Errorf("%s has no scope", n)
		}
	}
	if len(GoogleServices) != len(GoogleServiceOrder) {
		t.Error("every service is in the order")
	}
	for _, sc := range got {
		if strings.HasSuffix(sc, "/auth/drive") || strings.HasSuffix(sc, "gmail.modify") || strings.Contains(sc, "youtube") {
			t.Errorf("%s is wider than the actions need", sc)
		}
	}
}

func TestGoogleRefusesARedirectItWouldNotTake(t *testing.T) {
	m := manager(&memStore{provider: "google", id: "cid"})
	for _, bad := range []string{"http://192.168.1.20:7488/v1/oauth/callback", "http://kitties.local/v1/oauth/callback", "::bad"} {
		if _, err := m.Start("g", bad); err == nil {
			t.Errorf("%s should be refused", bad)
		}
	}
	_, err := m.Start("g", "http://192.168.1.20:7488/v1/oauth/callback")
	if err == nil || !strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("err = %v", err)
	}
	for _, ok := range []string{"http://[::1]:7488/v1/oauth/callback", "http://127.0.0.1/v1/oauth/callback"} {
		if _, err := m.Start("g", ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
}

func TestGoogleSignInNamesTheAccountAndKeepsWhatWasGranted(t *testing.T) {
	st := &memStore{provider: "google", id: "cid", secret: "sec", settings: map[string]string{"services": "gmail,calendar,tasks"}}
	granted := "openid https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/gmail.readonly https://www.googleapis.com/auth/gmail.send https://www.googleapis.com/auth/tasks"
	a := &answer{code: 200, body: `{"access_token":"at","refresh_token":"rt","expires_in":3599,"scope":"` + granted + `","id_token":"` + idToken(`{"email":"me@gmail.com","sub":"1"}`) + `"}`}
	m := manager(st, a)
	link, err := m.Start("g", "http://127.0.0.1:7488/v1/oauth/callback")
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.ParseQuery(strings.SplitN(link, "?", 2)[1])
	if _, err := m.Exchange(context.Background(), q.Get("state"), "code"); err != nil {
		t.Fatal(err)
	}
	if st.creds.Account != "me@gmail.com" || st.creds.RefreshToken != "rt" {
		t.Errorf("creds = %+v", st.creds)
	}
	// Calendar was unticked on the consent page: the connection says so.
	if st.settings["services"] != "gmail,tasks" {
		t.Errorf("services = %q", st.settings["services"])
	}
	if a.form.Get("code_verifier") == "" || a.form.Get("redirect_uri") != "http://127.0.0.1:7488/v1/oauth/callback" {
		t.Errorf("form = %v", a.form)
	}
}

func TestGoogleFinish(t *testing.T) {
	app := App{Settings: map[string]string{"services": "drive"}}
	// No scope in the answer: what was asked stands.
	c, learned, err := googleFinish(context.Background(), nil, app, Credentials{AccessToken: "a", IDToken: "broken"})
	if err != nil || learned != nil || c.Account != "" {
		t.Errorf("no scope = %+v %v %v", c, learned, err)
	}
	// Only the identity granted: nothing to work with.
	if _, _, err := googleFinish(context.Background(), nil, app, Credentials{Scope: "openid email"}); err == nil || !strings.Contains(err.Error(), "no service") {
		t.Errorf("err = %v", err)
	}
}

func TestIDTokenEmail(t *testing.T) {
	cases := map[string]string{
		idToken(`{"email":"a@b.c"}`): "a@b.c",
		"one.part":                   "",
		"h.!!!.s":                    "",
		idToken(`not json`):          "",
		"h." + base64.RawURLEncoding.EncodeToString([]byte(`{"email":"x@y.z"}`)) + "==.s": "x@y.z",
	}
	for in, want := range cases {
		if got := idTokenEmail(in); got != want {
			t.Errorf("%q = %q, want %q", in, got, want)
		}
	}
}

func TestRefreshThatGoogleRefusesSignsOut(t *testing.T) {
	st := &memStore{provider: "google", id: "cid", creds: Credentials{RefreshToken: "dead", Account: "me@gmail.com"}}
	m := manager(st, &answer{code: 400, body: `{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`})
	_, err := m.Token(context.Background(), "g")
	if !errors.Is(err, ErrSignedOut) || !strings.Contains(err.Error(), "expired or revoked") {
		t.Fatalf("err = %v", err)
	}
	if st.creds.RefreshToken != "" || st.creds.Account != "me@gmail.com" || st.creds.SignedIn() {
		t.Errorf("creds = %+v", st.creds)
	}
	// Saving the signed-out state fails: that is what is reported.
	st = &memStore{provider: "google", id: "cid", creds: Credentials{RefreshToken: "dead"}, saveErr: errors.New("disk")}
	m = manager(st, &answer{code: 400, body: `{"error":"invalid_grant"}`})
	if _, err := m.Token(context.Background(), "g"); err == nil || err.Error() != "disk" {
		t.Errorf("err = %v", err)
	}
	// Another refusal is not a sign-out.
	st = &memStore{provider: "google", id: "cid", creds: Credentials{RefreshToken: "rt"}}
	m = manager(st, &answer{code: 500, body: `{"error":"backend_error"}`})
	var refused *RefusedError
	if _, err := m.Token(context.Background(), "g"); !errors.As(err, &refused) || refused.Code != "backend_error" || errors.Is(err, ErrSignedOut) || st.creds.RefreshToken != "rt" {
		t.Errorf("err = %v creds=%+v", err, st.creds)
	}
}
