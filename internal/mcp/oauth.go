package mcp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// OAuth is the authorization server a remote MCP client (claude.ai) signs in through: discovery
// (RFC 9728 and 8414), dynamic client registration (RFC 7591), the authorization code flow with
// PKCE S256, and refresh tokens. It keeps no state on disk: clients, codes and tokens are signed
// with a key derived from the connector's token, so rotating that token revokes everything. The
// person approves a sign-in by typing the connector's token on the consent page.
type OAuth struct {
	secret []byte
	// Token is the connector's own token, the one the consent page asks for.
	token string
	// Issuer returns the public base address (https://host) of a request.
	Issuer func(r *http.Request) string
	// Allow, when set, is asked before a wrong token is counted; it returns false to refuse
	// a client that guessed too often.
	Allow func(r *http.Request) bool
	// Failed is told of a wrong token on the consent page.
	Failed func(r *http.Request)

	mu   sync.Mutex
	used map[string]time.Time
	now  func() time.Time
}

// NewOAuth makes the server for a connector with this token.
func NewOAuth(token string, issuer func(*http.Request) string) *OAuth {
	h := sha256.Sum256([]byte("kitties-mcp-oauth\x00" + token))
	return &OAuth{secret: h[:], token: token, Issuer: issuer, used: map[string]time.Time{}, now: time.Now}
}

const (
	codeTTL    = 5 * time.Minute
	accessTTL  = time.Hour
	refreshTTL = 30 * 24 * time.Hour
)

// Paths of the endpoints, relative to the issuer.
const (
	PathResource  = "/.well-known/oauth-protected-resource"
	PathMetadata  = "/.well-known/oauth-authorization-server"
	PathRegister  = "/oauth/register"
	PathAuthorize = "/oauth/authorize"
	PathToken     = "/oauth/token"
)

// Register adds the endpoints to a mux.
func (o *OAuth) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET "+PathResource, o.resource)
	mux.HandleFunc("GET "+PathResource+"/mcp", o.resource)
	mux.HandleFunc("GET "+PathMetadata, o.metadata)
	mux.HandleFunc("POST "+PathRegister, o.register)
	mux.HandleFunc("GET "+PathAuthorize, o.authorizeForm)
	mux.HandleFunc("POST "+PathAuthorize, o.authorizeSubmit)
	mux.HandleFunc("POST "+PathToken, o.tokenEndpoint)
}

// Challenge is the WWW-Authenticate value that points a client to the metadata.
func (o *OAuth) Challenge(r *http.Request) string {
	return fmt.Sprintf(`Bearer realm="kitties", resource_metadata="%s%s"`, o.Issuer(r), PathResource)
}

// ValidAccess reports whether a bearer value is an access token this server issued.
func (o *OAuth) ValidAccess(tok string) bool {
	var p claims
	return o.open(tok, "access", &p)
}

// --- signed values -----------------------------------------------------------------------------

type claims struct {
	Typ       string   `json:"t"`
	Exp       int64    `json:"e"`
	ClientID  string   `json:"c,omitempty"`
	Redirect  string   `json:"r,omitempty"`
	Challenge string   `json:"h,omitempty"`
	Nonce     string   `json:"n,omitempty"`
	Redirects []string `json:"u,omitempty"`
	Name      string   `json:"a,omitempty"`
}

func (o *OAuth) mac(b []byte) []byte {
	m := hmac.New(sha256.New, o.secret)
	m.Write(b)
	return m.Sum(nil)
}

func (o *OAuth) seal(c claims) string {
	b, _ := json.Marshal(c)
	enc := base64.RawURLEncoding
	return enc.EncodeToString(b) + "." + enc.EncodeToString(o.mac(b))
}

// open checks the signature, the kind and the expiry of a signed value.
func (o *OAuth) open(s, typ string, c *claims) bool {
	body, sig, ok := strings.Cut(s, ".")
	if !ok {
		return false
	}
	enc := base64.RawURLEncoding
	b, err1 := enc.DecodeString(body)
	want, err2 := enc.DecodeString(sig)
	if err1 != nil || err2 != nil || !hmac.Equal(want, o.mac(b)) {
		return false
	}
	if json.Unmarshal(b, c) != nil || c.Typ != typ || o.now().Unix() > c.Exp {
		return false
	}
	return true
}

func (o *OAuth) nonce() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// --- discovery -----------------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (o *OAuth) resource(w http.ResponseWriter, r *http.Request) {
	iss := o.Issuer(r)
	writeJSON(w, http.StatusOK, map[string]any{"resource": iss + "/mcp", "authorization_servers": []string{iss},
		"bearer_methods_supported": []string{"header"}})
}

func (o *OAuth) metadata(w http.ResponseWriter, r *http.Request) {
	iss := o.Issuer(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer": iss, "authorization_endpoint": iss + PathAuthorize, "token_endpoint": iss + PathToken,
		"registration_endpoint":                 iss + PathRegister,
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
	})
}

// --- registration --------------------------------------------------------------------------------

// okRedirect accepts an https address, or http on this machine.
func okRedirect(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.Fragment != "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	h := u.Hostname()
	return u.Scheme == "http" && (h == "localhost" || h == "127.0.0.1" || h == "::1")
}

func (o *OAuth) register(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name      string   `json:"client_name"`
		Redirects []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil || len(in.Redirects) == 0 || len(in.Redirects) > 10 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_client_metadata", "error_description": "redirect_uris is required"})
		return
	}
	for _, u := range in.Redirects {
		if !okRedirect(u) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_redirect_uri", "error_description": "use https, or http on localhost"})
			return
		}
	}
	if len(in.Name) > 100 {
		in.Name = in.Name[:100]
	}
	id := o.seal(claims{Typ: "client", Exp: o.now().Add(10 * 365 * 24 * time.Hour).Unix(), Redirects: in.Redirects, Name: in.Name})
	writeJSON(w, http.StatusCreated, map[string]any{"client_id": id, "client_name": in.Name, "redirect_uris": in.Redirects,
		"grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"},
		"token_endpoint_auth_method": "none"})
}

// --- authorization -------------------------------------------------------------------------------

type authReq struct {
	clientID, redirect, state, challenge, name string
}

// parse checks the request of the authorization endpoint. A bad client or redirect is shown on
// the page (never redirected to); other faults go back to the client in the redirect.
func (o *OAuth) parse(v url.Values) (authReq, string, string) {
	a := authReq{clientID: v.Get("client_id"), redirect: v.Get("redirect_uri"), state: v.Get("state"), challenge: v.Get("code_challenge")}
	var c claims
	if !o.open(a.clientID, "client", &c) {
		return a, "", "unknown client"
	}
	a.name = c.Name
	ok := false
	for _, u := range c.Redirects {
		if u == a.redirect {
			ok = true
		}
	}
	if !ok {
		return a, "", "redirect_uri is not one the client registered"
	}
	switch {
	case v.Get("response_type") != "code":
		return a, "unsupported_response_type", ""
	case a.challenge == "" || v.Get("code_challenge_method") != "S256":
		return a, "invalid_request", ""
	}
	return a, "", ""
}

func redirectTo(w http.ResponseWriter, r *http.Request, base string, q map[string]string) {
	u, _ := url.Parse(base)
	vals := u.Query()
	for k, v := range q {
		if v != "" {
			vals.Set(k, v)
		}
	}
	u.RawQuery = vals.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (o *OAuth) page(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self' https: http:")
	w.WriteHeader(code)
	fmt.Fprintf(w, `<!doctype html><meta charset=utf-8><meta name=viewport content="width=device-width,initial-scale=1"><title>Kitties</title>
<style>body{font:16px system-ui;max-width:28rem;margin:4rem auto;padding:0 1rem}input,button{font:inherit;padding:.5rem;width:100%%;box-sizing:border-box;margin-top:.5rem}code{word-break:break-all}</style>%s`, body)
}

func (o *OAuth) authorizeForm(w http.ResponseWriter, r *http.Request) {
	a, oerr, msg := o.parse(r.URL.Query())
	o.consent(w, r, a, oerr, msg, "")
}

func (o *OAuth) consent(w http.ResponseWriter, r *http.Request, a authReq, oerr, msg, problem string) {
	if msg != "" {
		o.page(w, http.StatusBadRequest, "<h1>Cannot sign in</h1><p>"+html.EscapeString(msg)+"</p>")
		return
	}
	if oerr != "" {
		redirectTo(w, r, a.redirect, map[string]string{"error": oerr, "state": a.state})
		return
	}
	who := a.name
	if who == "" {
		who = "An app"
	}
	extra := ""
	if problem != "" {
		extra = "<p><b>" + html.EscapeString(problem) + "</b></p>"
	}
	hidden := ""
	for k, v := range map[string]string{"client_id": a.clientID, "redirect_uri": a.redirect, "state": a.state, "code_challenge": a.challenge} {
		hidden += fmt.Sprintf(`<input type=hidden name=%s value="%s">`, k, html.EscapeString(v))
	}
	o.page(w, http.StatusOK, fmt.Sprintf(`<h1>Connect to Kitties</h1><p><b>%s</b> asks to run your kitties: create and stop them, send them tasks, see their desktops.</p>%s
<p>To allow it, type the connector's token (the file <code>mcp.token</code> in the data directory).</p>
<form method=post action="%s">%s<input type=password name=token autocomplete=off required autofocus placeholder="MCP token"><button>Allow</button></form>
<p>Close this page to refuse.</p>`, html.EscapeString(who), extra, PathAuthorize, hidden))
}

func (o *OAuth) authorizeSubmit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if r.ParseForm() != nil {
		o.page(w, http.StatusBadRequest, "<h1>Bad request</h1>")
		return
	}
	a, oerr, msg := o.parse(r.PostForm)
	if msg != "" || oerr != "" {
		o.consent(w, r, a, oerr, msg, "")
		return
	}
	if o.Allow != nil && !o.Allow(r) {
		o.page(w, http.StatusTooManyRequests, "<h1>Too many wrong tokens</h1><p>Try again in a minute.</p>")
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.PostForm.Get("token")), []byte(o.token)) != 1 {
		if o.Failed != nil {
			o.Failed(r)
		}
		o.consent(w, r, a, "", "", "That is not the connector's token.")
		return
	}
	code := o.seal(claims{Typ: "code", Exp: o.now().Add(codeTTL).Unix(), ClientID: a.clientID, Redirect: a.redirect, Challenge: a.challenge, Nonce: o.nonce()})
	redirectTo(w, r, a.redirect, map[string]string{"code": code, "state": a.state})
}

// --- tokens --------------------------------------------------------------------------------------

func tokenError(w http.ResponseWriter, code, desc string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": code, "error_description": desc})
}

// spend marks a code as used; false when it was already.
func (o *OAuth) spend(code string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	now := o.now()
	for k, t := range o.used {
		if now.After(t) {
			delete(o.used, k)
		}
	}
	if _, dup := o.used[code]; dup {
		return false
	}
	o.used[code] = now.Add(codeTTL + time.Minute)
	return true
}

func (o *OAuth) tokenEndpoint(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if r.ParseForm() != nil {
		tokenError(w, "invalid_request", "unreadable form")
		return
	}
	f := r.PostForm
	var cl claims
	if !o.open(f.Get("client_id"), "client", &cl) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
		return
	}
	switch f.Get("grant_type") {
	case "authorization_code":
		var c claims
		if !o.open(f.Get("code"), "code", &c) || c.ClientID != f.Get("client_id") || c.Redirect != f.Get("redirect_uri") {
			tokenError(w, "invalid_grant", "the code is not valid")
			return
		}
		sum := sha256.Sum256([]byte(f.Get("code_verifier")))
		if f.Get("code_verifier") == "" || subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(c.Challenge)) != 1 {
			tokenError(w, "invalid_grant", "code_verifier does not match")
			return
		}
		if !o.spend(f.Get("code")) {
			tokenError(w, "invalid_grant", "the code was already used")
			return
		}
	case "refresh_token":
		var c claims
		if !o.open(f.Get("refresh_token"), "refresh", &c) || c.ClientID != f.Get("client_id") {
			tokenError(w, "invalid_grant", "the refresh token is not valid")
			return
		}
	default:
		tokenError(w, "unsupported_grant_type", "use authorization_code or refresh_token")
		return
	}
	now := o.now()
	id := f.Get("client_id")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  o.seal(claims{Typ: "access", Exp: now.Add(accessTTL).Unix(), ClientID: id, Nonce: o.nonce()}),
		"token_type":    "Bearer",
		"expires_in":    int(accessTTL / time.Second),
		"refresh_token": o.seal(claims{Typ: "refresh", Exp: now.Add(refreshTTL).Unix(), ClientID: id, Nonce: o.nonce()}),
	})
}
