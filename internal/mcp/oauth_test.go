package mcp

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	testRedirect = "https://claude.ai/api/mcp/auth_callback"
	verifier     = "a-long-random-verifier-0123456789"
)

func challenge() string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func newOAuth() (*OAuth, *http.ServeMux) {
	o := NewOAuth("connector-token", func(*http.Request) string { return "https://k.example" })
	mux := http.NewServeMux()
	o.Register(mux)
	return o, mux
}

func send(mux http.Handler, method, target string, form url.Values, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if form != nil {
		r = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

func register(t *testing.T, mux http.Handler) string {
	t.Helper()
	rec := send(mux, "POST", PathRegister, nil, `{"client_name":"Claude","redirect_uris":["`+testRedirect+`"]}`)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	id, _ := out["client_id"].(string)
	if rec.Code != 201 || id == "" {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	return id
}

func authParams(id string) url.Values {
	return url.Values{"client_id": {id}, "redirect_uri": {testRedirect}, "response_type": {"code"}, "state": {"st"},
		"code_challenge": {challenge()}, "code_challenge_method": {"S256"}}
}

// approve signs in on the consent page and returns the code.
func approve(t *testing.T, mux http.Handler, id string) string {
	t.Helper()
	form := authParams(id)
	form.Set("token", "connector-token")
	rec := send(mux, "POST", PathAuthorize, form, "")
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != 302 || loc.Query().Get("state") != "st" || loc.Query().Get("code") == "" {
		t.Fatalf("approve: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	return loc.Query().Get("code")
}

func exchange(mux http.Handler, id, code, ver string) *httptest.ResponseRecorder {
	return send(mux, "POST", PathToken, url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "code": {code},
		"redirect_uri": {testRedirect}, "code_verifier": {ver}}, "")
}

func TestOAuthFlow(t *testing.T) {
	o, mux := newOAuth()
	if rec := send(mux, "GET", PathResource, nil, ""); !strings.Contains(rec.Body.String(), "https://k.example/mcp") {
		t.Errorf("resource: %s", rec.Body)
	}
	if rec := send(mux, "GET", PathResource+"/mcp", nil, ""); rec.Code != 200 {
		t.Errorf("resource/mcp: %d", rec.Code)
	}
	if rec := send(mux, "GET", PathMetadata, nil, ""); !strings.Contains(rec.Body.String(), `"S256"`) || !strings.Contains(rec.Body.String(), "https://k.example/oauth/token") {
		t.Errorf("metadata: %s", rec.Body)
	}
	id := register(t, mux)
	// The consent page names the app and asks for the token.
	rec := send(mux, "GET", PathAuthorize+"?"+authParams(id).Encode(), nil, "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Claude") || !strings.Contains(rec.Body.String(), `type=password`) {
		t.Errorf("consent: %d %s", rec.Code, rec.Body)
	}
	code := approve(t, mux, id)
	rec = exchange(mux, id, code, verifier)
	var tok map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &tok)
	access, _ := tok["access_token"].(string)
	refresh, _ := tok["refresh_token"].(string)
	if rec.Code != 200 || tok["token_type"] != "Bearer" || !o.ValidAccess(access) || refresh == "" {
		t.Fatalf("token: %d %s", rec.Code, rec.Body)
	}
	if o.ValidAccess(refresh) || o.ValidAccess("junk") || o.ValidAccess("a.b") || o.ValidAccess(code) {
		t.Error("only an access token opens the connector")
	}
	// The code works once.
	if rec = exchange(mux, id, code, verifier); rec.Code != 400 || !strings.Contains(rec.Body.String(), "already used") {
		t.Errorf("replay: %d %s", rec.Code, rec.Body)
	}
	// Refresh.
	rec = send(mux, "POST", PathToken, url.Values{"grant_type": {"refresh_token"}, "client_id": {id}, "refresh_token": {refresh}}, "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "access_token") {
		t.Errorf("refresh: %d %s", rec.Code, rec.Body)
	}
	// An access token is not a refresh token, and the other way round.
	rec = send(mux, "POST", PathToken, url.Values{"grant_type": {"refresh_token"}, "client_id": {id}, "refresh_token": {access}}, "")
	if rec.Code != 400 {
		t.Errorf("access as refresh: %d", rec.Code)
	}
}

func TestOAuthRefusals(t *testing.T) {
	o, mux := newOAuth()
	id := register(t, mux)
	// Registration needs redirects that are https (or localhost).
	for _, body := range []string{`{}`, `not json`, `{"redirect_uris":["http://evil.example/cb"]}`, `{"redirect_uris":["https://a.b/c#f"]}`, `{"redirect_uris":["ftp://a.b/c"]}`,
		`{"redirect_uris":["https://a.b/1","https://a.b/2","https://a.b/3","https://a.b/4","https://a.b/5","https://a.b/6","https://a.b/7","https://a.b/8","https://a.b/9","https://a.b/10","https://a.b/11"]}`} {
		if rec := send(mux, "POST", PathRegister, nil, body); rec.Code != 400 {
			t.Errorf("register %q: %d", body, rec.Code)
		}
	}
	if rec := send(mux, "POST", PathRegister, nil, `{"client_name":"`+strings.Repeat("x", 300)+`","redirect_uris":["http://localhost:33418/cb"]}`); rec.Code != 201 {
		t.Errorf("localhost redirect: %d", rec.Code)
	}
	// An unknown client or a redirect it did not register is shown, never followed.
	bad := authParams("forged.sig")
	if rec := send(mux, "GET", PathAuthorize+"?"+bad.Encode(), nil, ""); rec.Code != 400 || rec.Header().Get("Location") != "" {
		t.Errorf("unknown client: %d", rec.Code)
	}
	p := authParams(id)
	p.Set("redirect_uri", "https://evil.example/cb")
	if rec := send(mux, "GET", PathAuthorize+"?"+p.Encode(), nil, ""); rec.Code != 400 || rec.Header().Get("Location") != "" {
		t.Errorf("foreign redirect: %d", rec.Code)
	}
	// Faults of the request go back to the client; PKCE S256 is required.
	p = authParams(id)
	p.Set("response_type", "token")
	if rec := send(mux, "GET", PathAuthorize+"?"+p.Encode(), nil, ""); !strings.Contains(rec.Header().Get("Location"), "unsupported_response_type") {
		t.Errorf("response_type: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	p = authParams(id)
	p.Set("code_challenge_method", "plain")
	if rec := send(mux, "POST", PathAuthorize, p, ""); !strings.Contains(rec.Header().Get("Location"), "invalid_request") {
		t.Errorf("plain: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	// A wrong token on the consent page is told and counted.
	var failed int
	o.Failed = func(*http.Request) { failed++ }
	p = authParams(id)
	p.Set("token", "nope")
	if rec := send(mux, "POST", PathAuthorize, p, ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "not the connector") || failed != 1 {
		t.Errorf("wrong token: %d failed=%d", rec.Code, failed)
	}
	o.Allow = func(*http.Request) bool { return false }
	p.Set("token", "connector-token")
	if rec := send(mux, "POST", PathAuthorize, p, ""); rec.Code != http.StatusTooManyRequests {
		t.Errorf("limited: %d", rec.Code)
	}
	o.Allow = nil
	if rec := send(mux, "POST", PathAuthorize, url.Values{}, ""); rec.Code != 400 {
		t.Errorf("empty post: %d", rec.Code)
	}
}

func TestOAuthTokenErrors(t *testing.T) {
	o, mux := newOAuth()
	id := register(t, mux)
	code := approve(t, mux, id)
	if rec := exchange(mux, id, code, "wrong-verifier"); rec.Code != 400 || !strings.Contains(rec.Body.String(), "code_verifier") {
		t.Errorf("wrong verifier: %d %s", rec.Code, rec.Body)
	}
	if rec := exchange(mux, id, code, ""); rec.Code != 400 {
		t.Errorf("no verifier: %d", rec.Code)
	}
	if rec := exchange(mux, id, "junk", verifier); rec.Code != 400 {
		t.Errorf("junk code: %d", rec.Code)
	}
	rec0 := send(mux, "POST", PathRegister, nil, `{"client_name":"Other","redirect_uris":["`+testRedirect+`"]}`)
	var reg map[string]any
	_ = json.Unmarshal(rec0.Body.Bytes(), &reg)
	other := reg["client_id"].(string)
	if rec := exchange(mux, other, code, verifier); rec.Code != 400 {
		t.Errorf("another client's code: %d", rec.Code)
	}
	if rec := exchange(mux, "forged.sig", code, verifier); rec.Code != 401 {
		t.Errorf("unknown client: %d", rec.Code)
	}
	if rec := send(mux, "POST", PathToken, url.Values{"grant_type": {"password"}, "client_id": {id}}, ""); rec.Code != 400 || !strings.Contains(rec.Body.String(), "unsupported_grant_type") {
		t.Errorf("grant: %d", rec.Code)
	}
	if rec := send(mux, "POST", PathToken, url.Values{"grant_type": {"refresh_token"}, "client_id": {id}, "refresh_token": {"x"}}, ""); rec.Code != 400 {
		t.Errorf("bad refresh: %d", rec.Code)
	}
	r := httptest.NewRequest("POST", PathToken, strings.NewReader("%zz"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	if rec.Code != 400 {
		t.Errorf("unreadable form: %d", rec.Code)
	}
	// An expired code, and the used-codes list forgets old entries.
	o.now = func() time.Time { return time.Now().Add(10 * time.Minute) }
	if rec := exchange(mux, id, code, verifier); rec.Code != 400 {
		t.Errorf("expired: %d", rec.Code)
	}
	o.now = time.Now
	o.used["old"] = time.Now().Add(-time.Hour)
	o.spend("new")
	if _, still := o.used["old"]; still {
		t.Error("old entries are dropped")
	}
	// Another secret signs differently: tokens do not cross connectors.
	o2 := NewOAuth("another-token", func(*http.Request) string { return "" })
	rec = exchange(mux, id, approve(t, mux, id), verifier)
	var tok map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &tok)
	if o2.ValidAccess(tok["access_token"].(string)) {
		t.Error("a token of another connector")
	}
}

func TestChallengeHeader(t *testing.T) {
	o, _ := newOAuth()
	got := o.Challenge(httptest.NewRequest("GET", "/mcp", nil))
	if !strings.Contains(got, `resource_metadata="https://k.example/.well-known/oauth-protected-resource"`) {
		t.Errorf("challenge: %s", got)
	}
}
