package gateway

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/madkoding/kitties/internal/chat"
	"github.com/madkoding/kitties/internal/kitty"
)

func newServer(token string) *Server {
	f := &fakeFleet{kitties: map[string]*kitty.Kitty{"a1": {ID: "a1", Name: "ana", Status: kitty.StatusRunning}}}
	return New(f, &fakeChats{sessions: map[string]*chat.Session{}}, token, "test")
}

// do runs one request through the handler.
func do(s *Server, method, target, body string, mod func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.RemoteAddr = "203.0.113.9:5555"
	if mod != nil {
		mod(r)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	return rec
}

func cookieOf(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	return nil
}

func TestLoginSessionAndLogout(t *testing.T) {
	s := newServer("sekret-token-1234")
	origin := func(r *http.Request) { r.Header.Set("Origin", "http://example.com") }
	// Wrong token, malformed body, another site's page: none signs in.
	if rec := do(s, "POST", "/v1/login", `{"token":"nope"}`, origin); rec.Code != 401 || cookieOf(rec) != nil {
		t.Errorf("wrong token = %d", rec.Code)
	}
	if rec := do(s, "POST", "/v1/login", `not json`, origin); rec.Code != 400 {
		t.Errorf("malformed = %d", rec.Code)
	}
	if rec := do(s, "POST", "/v1/login", `{"token":"sekret-token-1234"}`, func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }); rec.Code != 403 {
		t.Errorf("another site = %d", rec.Code)
	}
	// The right one, even with stray spaces, leaves a session cookie that is not the token.
	rec := do(s, "POST", "/v1/login", `{"token":" sekret-token-1234 "}`, origin)
	c := cookieOf(rec)
	if rec.Code != 200 || c == nil || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Secure || c.Value == "" || strings.Contains(c.Value, "sekret") {
		t.Fatalf("login = %d cookie=%+v", rec.Code, c)
	}
	// The cookie opens the API: reads need nothing more, changes must come from the page.
	withCookie := func(extra func(*http.Request)) func(*http.Request) {
		return func(r *http.Request) {
			r.AddCookie(c)
			if extra != nil {
				extra(r)
			}
		}
	}
	if rec := do(s, "GET", "/v1/kitties", "", withCookie(nil)); rec.Code != 200 {
		t.Errorf("cookie read = %d", rec.Code)
	}
	if rec := do(s, "POST", "/v1/chats", `{"members":["a1"]}`, withCookie(nil)); rec.Code != 403 {
		t.Errorf("a cookie write with no origin = %d", rec.Code)
	}
	if rec := do(s, "POST", "/v1/chats", `{"members":["a1"]}`, withCookie(func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") })); rec.Code != 403 {
		t.Errorf("a cookie write from another site = %d", rec.Code)
	}
	if rec := do(s, "POST", "/v1/chats", `{"members":["a1"]}`, withCookie(origin)); rec.Code != 201 {
		t.Errorf("a cookie write from the page = %d %s", rec.Code, rec.Body)
	}
	if rec := do(s, "POST", "/v1/chats", `{"members":["a1"]}`, withCookie(func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-origin") })); rec.Code != 201 {
		t.Errorf("a cookie write the browser calls same-origin = %d", rec.Code)
	}
	// The bearer token has no such rule: it is not a browser's ambient credential.
	if rec := do(s, "POST", "/v1/chats", `{"members":["a1"]}`, func(r *http.Request) { r.Header.Set("Authorization", "Bearer sekret-token-1234") }); rec.Code != 201 {
		t.Errorf("bearer write = %d", rec.Code)
	}
	// A cookie of another token is no session.
	other := newServer("another-token-5678")
	if rec := do(other, "GET", "/v1/kitties", "", withCookie(nil)); rec.Code != 401 {
		t.Errorf("a cookie of another gateway = %d", rec.Code)
	}
	// The session endpoint says where the page stands.
	if rec := do(s, "GET", "/v1/session", "", withCookie(nil)); !strings.Contains(rec.Body.String(), `"signed_in":true`) {
		t.Errorf("session = %s", rec.Body)
	}
	if rec := do(s, "GET", "/v1/session", "", nil); !strings.Contains(rec.Body.String(), `"signed_in":false`) || !strings.Contains(rec.Body.String(), `"local_token":false`) {
		t.Errorf("anonymous session = %s", rec.Body)
	}
	// Signing out clears it, and only from the page.
	if rec := do(s, "POST", "/v1/logout", "", withCookie(func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") })); rec.Code != 403 {
		t.Errorf("logout from another site = %d", rec.Code)
	}
	out := do(s, "POST", "/v1/logout", "", withCookie(origin))
	if oc := cookieOf(out); out.Code != 200 || oc == nil || oc.MaxAge >= 0 || oc.Value != "" {
		t.Errorf("logout = %d %+v", out.Code, oc)
	}
	// No token configured: nobody signs in.
	none := newServer("")
	if rec := do(none, "POST", "/v1/login", `{"token":""}`, origin); rec.Code != 401 {
		t.Errorf("no token to match = %d", rec.Code)
	}
}

func TestSecureCookieAndHeaders(t *testing.T) {
	login := `{"token":"sekret-token-1234"}`
	same := func(r *http.Request) { r.Header.Set("Origin", "http://example.com") }
	// Behind a trusted https proxy the cookie is Secure and the page says so with HSTS.
	proxied := newServer("sekret-token-1234").WithProxy(true)
	rec := do(proxied, "POST", "/v1/login", login, func(r *http.Request) {
		r.Header.Set("Origin", "https://kitties.example.com")
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-Host", "kitties.example.com")
	})
	if c := cookieOf(rec); rec.Code != 200 || c == nil || !c.Secure {
		t.Errorf("proxied https login = %d %+v", rec.Code, c)
	}
	if rec.Header().Get("Strict-Transport-Security") == "" || rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("X-Frame-Options") != "DENY" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("headers = %v", rec.Header())
	}
	// Without trust in the proxy the same headers are only words.
	plain := newServer("sekret-token-1234")
	rec = do(plain, "POST", "/v1/login", login, func(r *http.Request) {
		same(r)
		r.Header.Set("X-Forwarded-Proto", "https")
	})
	if c := cookieOf(rec); c == nil || c.Secure || rec.Header().Get("Strict-Transport-Security") != "" {
		t.Errorf("untrusted proxy headers must not mark https: %+v", c)
	}
	// A public https address does.
	public := newServer("sekret-token-1234").WithPublicURL("https://kitties.example.com/")
	rec = do(public, "POST", "/v1/login", login, func(r *http.Request) { r.Header.Set("Origin", "https://kitties.example.com") })
	if c := cookieOf(rec); rec.Code != 200 || c == nil || !c.Secure {
		t.Errorf("public https login = %d %+v", rec.Code, c)
	}
	// Behind a proxy that rewrites the host, the page's origin is still its own.
	rec = do(proxied, "POST", "/v1/login", login, func(r *http.Request) {
		r.Host = "127.0.0.1:7488"
		r.Header.Set("Origin", "https://kitties.example.com")
		r.Header.Set("X-Forwarded-Host", "kitties.example.com, other")
	})
	if rec.Code != 200 {
		t.Errorf("origin through a proxy = %d", rec.Code)
	}
	if rec := do(proxied, "POST", "/v1/login", login, func(r *http.Request) { r.Header.Set("Origin", "%zz") }); rec.Code != 403 {
		t.Errorf("an unparsable origin = %d", rec.Code)
	}
	if rec := do(proxied, "POST", "/v1/login", login, func(r *http.Request) { r.Header.Set("Origin", "null") }); rec.Code != 403 {
		t.Errorf("a null origin = %d", rec.Code)
	}
}

func TestWrongGuessesAreLimited(t *testing.T) {
	s := newServer("sekret-token-1234")
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s.limit.now = func() time.Time { return now }
	bad := func(r *http.Request) { r.Header.Set("Authorization", "Bearer wrong") }
	for i := 0; i < 10; i++ {
		if rec := do(s, "GET", "/v1/kitties", "", bad); rec.Code != 401 {
			t.Fatalf("guess %d = %d", i, rec.Code)
		}
	}
	// The eleventh is refused whatever it carries, the right token included, and told when to return.
	good := func(r *http.Request) { r.Header.Set("Authorization", "Bearer sekret-token-1234") }
	if rec := do(s, "GET", "/v1/kitties", "", good); rec.Code != 429 || rec.Header().Get("Retry-After") == "" {
		t.Errorf("blocked = %d retry=%q", rec.Code, rec.Header().Get("Retry-After"))
	}
	if rec := do(s, "POST", "/v1/login", `{"token":"sekret-token-1234"}`, func(r *http.Request) { r.Header.Set("Origin", "http://example.com") }); rec.Code != 429 {
		t.Errorf("login while blocked = %d", rec.Code)
	}
	// Another client is unaffected, and silence is not a guess.
	if rec := do(s, "GET", "/v1/kitties", "", func(r *http.Request) { r.RemoteAddr = "198.51.100.1:1"; good(r) }); rec.Code != 200 {
		t.Errorf("another client = %d", rec.Code)
	}
	quiet := newServer("sekret-token-1234")
	for i := 0; i < 30; i++ {
		do(quiet, "GET", "/v1/kitties", "", nil)
	}
	if rec := do(quiet, "GET", "/v1/kitties", "", good); rec.Code != 200 {
		t.Errorf("requests without credentials are not guesses: %d", rec.Code)
	}
	// After the window it may try again, and a good login forgets the rest.
	now = now.Add(61 * time.Second)
	if rec := do(s, "GET", "/v1/kitties", "", good); rec.Code != 200 {
		t.Errorf("after the window = %d", rec.Code)
	}
	for i := 0; i < 3; i++ {
		do(s, "GET", "/v1/kitties", "", bad)
	}
	if rec := do(s, "POST", "/v1/login", `{"token":"sekret-token-1234"}`, func(r *http.Request) { r.Header.Set("Origin", "http://example.com") }); rec.Code != 200 {
		t.Errorf("login = %d", rec.Code)
	}
	if n := len(s.limit.hits); n != 0 {
		t.Errorf("a good login forgets the guesses: %d", n)
	}
	// A stale cookie of the same kind is a guess too, and the table stays bounded.
	l := newLimiter(2, time.Minute)
	l.now = func() time.Time { return now }
	for i := 0; i < 10001; i++ {
		l.hits[string(rune(i))+"k"] = []time.Time{now.Add(-time.Hour)}
	}
	l.fail("fresh")
	if len(l.hits) > 2 {
		t.Errorf("old entries are forgotten: %d", len(l.hits))
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:99"
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2")
	direct, proxied := newServer("t"), newServer("t").WithProxy(true)
	if got := direct.clientIP(r); got != "10.0.0.1" {
		t.Errorf("untrusted = %q", got)
	}
	if got := proxied.clientIP(r); got != "2.2.2.2" {
		t.Errorf("trusted takes the proxy's own hop = %q", got)
	}
	r.Header.Del("X-Forwarded-For")
	r.Header.Set("X-Real-IP", "3.3.3.3")
	if got := proxied.clientIP(r); got != "3.3.3.3" {
		t.Errorf("real ip = %q", got)
	}
	r.Header.Del("X-Real-IP")
	if got := proxied.clientIP(r); got != "10.0.0.1" {
		t.Errorf("no headers = %q", got)
	}
	r.Header.Set("X-Forwarded-For", " ,")
	if got := proxied.clientIP(r); got != "10.0.0.1" {
		t.Errorf("blank hop = %q", got)
	}
	r.RemoteAddr = "garbage"
	if got := direct.clientIP(r); got != "garbage" {
		t.Errorf("unreadable peer = %q", got)
	}
}

func TestLocalTokenIsNotForAServer(t *testing.T) {
	local := func(r *http.Request) { r.RemoteAddr = "127.0.0.1:4000"; r.Host = "127.0.0.1:7488" }
	ask := func(s *Server, mod func(*http.Request)) int {
		return do(s, "GET", "/v1/local-token", "", func(r *http.Request) { local(r); mod(r) }).Code
	}
	nop := func(*http.Request) {}
	if code := ask(newServer("tok"), nop); code != 200 {
		t.Errorf("this machine, directly = %d", code)
	}
	for name, tc := range map[string]struct {
		s   *Server
		mod func(*http.Request)
	}{
		"a proxy's forwarded address": {newServer("tok"), func(r *http.Request) { r.Header.Set("X-Forwarded-For", "8.8.8.8") }},
		"a forwarded host":            {newServer("tok"), func(r *http.Request) { r.Header.Set("X-Forwarded-Host", "k.example") }},
		"a real ip header":            {newServer("tok"), func(r *http.Request) { r.Header.Set("X-Real-IP", "8.8.8.8") }},
		"the standard header":         {newServer("tok"), func(r *http.Request) { r.Header.Set("Forwarded", "for=8.8.8.8") }},
		"a trusted proxy":             {newServer("tok").WithProxy(true), nop},
		"a switched-off gateway":      {newServer("tok").WithLocalToken(false), nop},
		"a rebound name (nginx on this machine keeps its own host)": {newServer("tok"), func(r *http.Request) { r.Host = "kitties.example.com" }},
	} {
		if code := ask(tc.s, tc.mod); code != 403 {
			t.Errorf("%s = %d, want 403", name, code)
		}
	}
	for _, host := range []string{"localhost:7488", "[::1]:7488", "127.0.0.1", "LOCALHOST"} {
		if code := ask(newServer("tok"), func(r *http.Request) { r.Host = host }); code != 200 {
			t.Errorf("host %q = %d", host, code)
		}
	}
	if hostIsLoopback("10.0.0.1:80") || hostIsLoopback("") || hostIsLoopback("example.com") {
		t.Error("hostIsLoopback accepts only this machine")
	}
	if rec := do(newServer("tok").WithLocalToken(false), "GET", "/v1/session", "", func(r *http.Request) { local(r) }); !strings.Contains(rec.Body.String(), `"local_token":false`) {
		t.Errorf("session reports the switch: %s", rec.Body)
	}
	if rec := do(newServer("tok"), "GET", "/v1/session", "", local); !strings.Contains(rec.Body.String(), `"local_token":true`) {
		t.Errorf("session reports a local gateway: %s", rec.Body)
	}
}

func TestOAuthRedirectOnAServer(t *testing.T) {
	redirectOf := func(s *Server, mod func(*http.Request)) string {
		rec := do(s, "POST", "/v1/oauth/g/start", "", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer tok")
			if mod != nil {
				mod(r)
			}
		})
		if rec.Code != 200 {
			t.Fatalf("start = %d %s", rec.Code, rec.Body)
		}
		var out struct{ Redirect string }
		_ = jsonDecode(rec.Body, &out)
		return out.Redirect
	}
	if got := redirectOf(newServer("tok").WithPublicURL("https://kitties.example.com/"), nil); got != "https://kitties.example.com/v1/oauth/callback" {
		t.Errorf("public url = %q", got)
	}
	proxied := newServer("tok").WithProxy(true)
	if got := redirectOf(proxied, func(r *http.Request) {
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-Host", "k.example.com")
	}); got != "https://k.example.com/v1/oauth/callback" {
		t.Errorf("proxied = %q", got)
	}
	if got := redirectOf(newServer("tok"), func(r *http.Request) { r.Header.Set("X-Forwarded-Host", "evil.example") }); got != "http://example.com/v1/oauth/callback" {
		t.Errorf("an untrusted proxy header changes nothing: %q", got)
	}
}

// slowFS hands out files that cannot seek.
type slowFS struct{ fs.FS }
type slowFile struct{ fs.File }

func (f slowFS) Open(name string) (fs.File, error) {
	file, err := f.FS.Open(name)
	if err != nil {
		return nil, err
	}
	return slowFile{file}, nil
}

func TestServesTheWebInterface(t *testing.T) {
	site := fstest.MapFS{
		"index.html":         {Data: []byte("<!doctype html><title>Kitties</title>")},
		"assets/app-abc.js":  {Data: []byte("console.log(1)")},
		"assets/app-abc.css": {Data: []byte("body{}")},
		"icon.svg":           {Data: []byte("<svg/>")},
		"notes":              {Data: []byte("plain")},
	}
	s := newServer("tok").WithWeb(site)
	rec := do(s, "GET", "/", "", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<title>Kitties</title>") || rec.Header().Get("Cache-Control") != "no-cache" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Errorf("index = %d %v", rec.Code, rec.Header())
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "frame-ancestors 'none'") || strings.Contains(csp, "unsafe-eval") {
		t.Errorf("csp = %q", csp)
	}
	if rec := do(s, "GET", "/index.html", "", nil); rec.Code != 200 {
		t.Errorf("index.html = %d", rec.Code)
	}
	rec = do(s, "GET", "/assets/app-abc.js", "", nil)
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") || !strings.Contains(rec.Header().Get("Content-Type"), "javascript") || rec.Header().Get("Content-Security-Policy") != "" {
		t.Errorf("asset = %d %v", rec.Code, rec.Header())
	}
	if rec := do(s, "GET", "/assets/app-abc.css", "", nil); !strings.Contains(rec.Header().Get("Content-Type"), "css") {
		t.Errorf("css = %v", rec.Header())
	}
	if rec := do(s, "GET", "/icon.svg", "", nil); rec.Code != 200 || rec.Header().Get("Cache-Control") != "public, max-age=3600" {
		t.Errorf("icon = %d %v", rec.Code, rec.Header())
	}
	if rec := do(s, "GET", "/notes", "", nil); rec.Code != 200 || rec.Body.String() != "plain" {
		t.Errorf("a file of no known type = %d", rec.Code)
	}
	// Nothing outside the build, no directories, no way up, and the API is not shadowed.
	for _, p := range []string{"/nope.js", "/assets", "/assets/", "/%2e%2e/secret", "/v1/unknown"} {
		if rec := do(s, "GET", p, "", nil); rec.Code != 404 {
			t.Errorf("%s = %d", p, rec.Code)
		}
	}
	// The mux cleans a path that climbs and sends the browser to the clean one, which is not served either.
	if rec := do(s, "GET", "/../etc/passwd", "", nil); rec.Code != http.StatusTemporaryRedirect || do(s, "GET", "/etc/passwd", "", nil).Code != 404 {
		t.Errorf("a path that climbs = %d", rec.Code)
	}
	if rec := do(s, "GET", "/v1/health", "", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Errorf("the API still answers: %d", rec.Code)
	}
	// Files that cannot seek are still served.
	slow := newServer("tok").WithWeb(slowFS{site})
	if rec := do(slow, "GET", "/", "", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Kitties") {
		t.Errorf("no seeker = %d", rec.Code)
	}
	// Without a build, the built-in page answers and nothing else is served.
	if rec := do(newServer("tok"), "GET", "/", "", nil); rec.Code != 200 || rec.Header().Get("Content-Security-Policy") != "" {
		t.Errorf("built-in page = %d", rec.Code)
	}
}

type brokenFS struct{ fstest.MapFS }
type brokenFile struct{ fs.File }

func (brokenFile) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (b brokenFS) Open(name string) (fs.File, error) {
	f, err := b.MapFS.Open(name)
	if err != nil {
		return nil, err
	}
	return brokenFile{struct{ fs.File }{f}}, nil
}

func TestWebReadFailure(t *testing.T) {
	s := newServer("tok").WithWeb(brokenFS{fstest.MapFS{"index.html": {Data: []byte("x")}}})
	if rec := do(s, "GET", "/", "", nil); rec.Code != 500 {
		t.Errorf("an unreadable file = %d", rec.Code)
	}
}

// selfSigned writes a certificate for 127.0.0.1 and its key.
func selfSigned(t *testing.T) (certFile, keyFile string, pool *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "kitties"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(key)
	dir := t.TempDir()
	certFile, keyFile = filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem")
	_ = os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	_ = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600)
	cert, _ := x509.ParseCertificate(der)
	pool = x509.NewCertPool()
	pool.AddCert(cert)
	return certFile, keyFile, pool
}

func TestListenOnTLSAndWarnings(t *testing.T) {
	certFile, keyFile, pool := selfSigned(t)
	s := newServer("tok").WithTLS(certFile, keyFile).WithWeb(fstest.MapFS{"index.html": {Data: []byte("<title>Kitties</title>")}})
	ctx, cancel := contextWithCancel()
	lines := make(chan string, 8)
	logf := func(format string, args ...any) { lines <- fmt.Sprintf(format, args...) }
	addr := freeAddr(t)
	done := make(chan error, 1)
	go func() { done <- s.Listen(ctx, addr, logf) }()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}, Timeout: 3 * time.Second}
	var resp *http.Response
	var err error
	for i := 0; i < 50; i++ {
		if resp, err = client.Get("https://" + addr + "/v1/health"); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "ok") || resp.TLS == nil || resp.Header.Get("Strict-Transport-Security") == "" {
		t.Errorf("https = %d tls=%v hsts=%q", resp.StatusCode, resp.TLS != nil, resp.Header.Get("Strict-Transport-Security"))
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Listen: %v", err)
	}
	close(lines)
	var all []string
	for l := range lines {
		all = append(all, l)
	}
	joined := strings.Join(all, "|")
	if strings.Contains(joined, "warning") || !strings.Contains(joined, "web interface") {
		t.Errorf("a TLS gateway with the page says so and does not warn: %v", all)
	}
	// A server on the network with neither TLS nor a proxy warns; behind a proxy or https it does not.
	for name, tc := range map[string]struct {
		s    *Server
		warn bool
	}{
		"plain network": {newServer("tok"), true},
		"proxy":         {newServer("tok").WithProxy(true), false},
		"https public":  {newServer("tok").WithPublicURL("https://k.example"), false},
	} {
		ctx, cancel := contextWithCancel()
		var got []string
		done := make(chan error, 1)
		go func() {
			done <- tc.s.Listen(ctx, "0.0.0.0:0", func(f string, a ...any) { got = append(got, fmt.Sprintf(f, a...)) })
		}()
		time.Sleep(100 * time.Millisecond)
		cancel()
		<-done
		if warned := strings.Contains(strings.Join(got, "|"), "warning"); warned != tc.warn {
			t.Errorf("%s: warned=%v want %v (%v)", name, warned, tc.warn, got)
		}
	}
	// A key that is not there stops the server instead of serving plain.
	bad := newServer("tok").WithTLS("missing.pem", "missing.key")
	ctx2, cancel2 := contextWithCancel()
	defer cancel2()
	if err := bad.Listen(ctx2, "127.0.0.1:0", func(string, ...any) {}); err == nil {
		t.Error("a missing certificate is an error")
	}
}

func contextWithCancel() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func jsonDecode(r io.Reader, v any) error { return json.NewDecoder(r).Decode(v) }
