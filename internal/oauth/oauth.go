// Package oauth signs a connector in with OAuth 2.0 (authorization code with PKCE), the way
// Google Workspace and Microsoft 365 want it: the person's own OAuth app (client id and
// secret), a browser window to consent, the callback on the gateway, and a refresh token kept
// in the connector's settings so the kitties never ask again.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Provider is one identity service.
type Provider struct {
	Name     string   `json:"name"`
	Label    string   `json:"label"`
	AuthURL  string   `json:"auth_url"`
	TokenURL string   `json:"token_url"`
	Scopes   []string `json:"scopes"`
	// Extra are query parameters the consent page needs (Google's offline access).
	Extra map[string]string `json:"-"`
	// ScopeSep joins the scopes in the consent URL; a space unless the service wants commas.
	ScopeSep string `json:"-"`
	// NoExpiry marks a service whose tokens last until revoked (Slack bots, GitHub OAuth
	// apps): an answer without expires_in is kept for good instead of an hour.
	NoExpiry bool `json:"-"`
}

// Providers are the services with an OAuth flow.
var Providers = map[string]Provider{
	"google": {
		Name: "google", Label: "Google", AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token",
		Scopes: []string{"https://www.googleapis.com/auth/gmail.send", "https://www.googleapis.com/auth/calendar.events", "https://www.googleapis.com/auth/drive.file", "https://www.googleapis.com/auth/spreadsheets", "https://www.googleapis.com/auth/documents"},
		Extra:  map[string]string{"access_type": "offline", "prompt": "consent"},
	},
	"microsoft": {
		Name: "microsoft", Label: "Microsoft", AuthURL: "https://login.microsoftonline.com/common/oauth2/v2.0/authorize", TokenURL: "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		Scopes: []string{"offline_access", "Mail.Send", "Calendars.ReadWrite", "Files.ReadWrite", "User.Read"},
	},
	"slack": {
		Name: "slack", Label: "Slack", AuthURL: "https://slack.com/oauth/v2/authorize", TokenURL: "https://slack.com/api/oauth.v2.access",
		Scopes: []string{"chat:write", "chat:write.public", "channels:read"}, ScopeSep: ",", NoExpiry: true,
	},
	"github": {
		Name: "github", Label: "GitHub", AuthURL: "https://github.com/login/oauth/authorize", TokenURL: "https://github.com/login/oauth/access_token",
		Scopes: []string{"repo"}, NoExpiry: true,
	},
}

// forever is how long a token without expiry is kept: until the service revokes it.
const forever = 100 * 365 * 24 * time.Hour

// Credentials is what a signed-in connector holds, in its settings.
type Credentials struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
	// Account is who signed in, when the service said.
	Account string `json:"account,omitempty"`
}

// Keys of the connector settings the credentials live in.
const (
	KeyAccess  = "oauth_access_token"
	KeyRefresh = "oauth_refresh_token"
	KeyExpiry  = "oauth_expiry"
	KeyAccount = "oauth_account"
)

// FromSettings reads the credentials kept in a connector's settings.
func FromSettings(s map[string]string) Credentials {
	c := Credentials{AccessToken: s[KeyAccess], RefreshToken: s[KeyRefresh], Account: s[KeyAccount]}
	if t, err := time.Parse(time.RFC3339, s[KeyExpiry]); err == nil {
		c.Expiry = t
	}
	return c
}

// ToSettings writes the credentials into a connector's settings.
func (c Credentials) ToSettings(s map[string]string) {
	s[KeyAccess] = c.AccessToken
	s[KeyRefresh] = c.RefreshToken
	s[KeyExpiry] = c.Expiry.UTC().Format(time.RFC3339)
	if c.Account != "" {
		s[KeyAccount] = c.Account
	}
}

// SignedIn reports whether a refresh token is there.
func (c Credentials) SignedIn() bool {
	return c.RefreshToken != "" || (c.AccessToken != "" && time.Now().Before(c.Expiry))
}

// Store is where the gateway keeps a connector's credentials and app: the orchestrator's settings.
type Store interface {
	// OAuthApp returns the connector's provider name, client id and secret.
	OAuthApp(connector string) (provider, clientID, clientSecret string, err error)
	// OAuthCredentials returns what is saved for the connector.
	OAuthCredentials(connector string) (Credentials, error)
	// SaveOAuthCredentials keeps new credentials.
	SaveOAuthCredentials(connector string, c Credentials) error
}

// Manager runs the flows.
type Manager struct {
	store Store
	mu    sync.Mutex
	// pending are the flows waiting for their callback, by state.
	pending map[string]flow
	now     func() time.Time
	httpDo  func(*http.Request) (*http.Response, error)
	rand    func(n int) string
}

type flow struct {
	connector string
	provider  Provider
	verifier  string
	redirect  string
	started   time.Time
}

// New creates the manager over the store.
func New(store Store) *Manager {
	return &Manager{store: store, pending: map[string]flow{}, now: time.Now,
		httpDo: (&http.Client{Timeout: 30 * time.Second}).Do, rand: randomText}
}

func randomText(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Start begins a sign-in for the connector and returns the consent URL to open.
func (m *Manager) Start(connector, redirect string) (string, error) {
	providerName, clientID, _, err := m.store.OAuthApp(connector)
	if err != nil {
		return "", err
	}
	p, ok := Providers[providerName]
	if !ok {
		return "", fmt.Errorf("%q has no OAuth flow", providerName)
	}
	if clientID == "" {
		return "", errors.New("the connector needs a client id (an OAuth app of your own)")
	}
	state := m.rand(24)
	verifier := m.rand(48)
	sum := sha256.Sum256([]byte(verifier))
	m.mu.Lock()
	for k, f := range m.pending {
		if m.now().Sub(f.started) > 15*time.Minute {
			delete(m.pending, k)
		}
	}
	m.pending[state] = flow{connector: connector, provider: p, verifier: verifier, redirect: redirect, started: m.now()}
	m.mu.Unlock()
	sep := p.ScopeSep
	if sep == "" {
		sep = " "
	}
	q := url.Values{
		"client_id": {clientID}, "redirect_uri": {redirect}, "response_type": {"code"}, "scope": {strings.Join(p.Scopes, sep)},
		"state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
	}
	for k, v := range p.Extra {
		q.Set(k, v)
	}
	return p.AuthURL + "?" + q.Encode(), nil
}

// Exchange finishes a sign-in with the code the callback received, and saves the credentials.
func (m *Manager) Exchange(ctx context.Context, state, code string) (string, error) {
	m.mu.Lock()
	f, ok := m.pending[state]
	delete(m.pending, state)
	m.mu.Unlock()
	if !ok {
		return "", errors.New("unknown or expired sign-in; start again from Settings")
	}
	_, clientID, clientSecret, err := m.store.OAuthApp(f.connector)
	if err != nil {
		return "", err
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {f.redirect}, "client_id": {clientID}, "code_verifier": {f.verifier}}
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	creds, err := m.token(ctx, f.provider, form)
	if err != nil {
		return "", err
	}
	if err := m.store.SaveOAuthCredentials(f.connector, creds); err != nil {
		return "", err
	}
	return f.connector, nil
}

// token posts a token request and reads the answer.
func (m *Manager) token(ctx context.Context, p Provider, form url.Values) (Credentials, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// GitHub answers in form encoding unless asked for JSON; the others ignore the header.
	req.Header.Set("Accept", "application/json")
	resp, err := m.httpDo(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("%s did not answer: %w", p.Label, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		Description  string `json:"error_description"`
		// Team is the Slack workspace that installed the app: the account shown.
		Team struct {
			Name string `json:"name"`
		} `json:"team"`
	}
	_ = json.Unmarshal(data, &body)
	if resp.StatusCode >= 400 || body.Error != "" {
		msg := body.Description
		if msg == "" {
			msg = body.Error
		}
		if msg == "" {
			msg = strings.TrimSpace(string(data))
		}
		return Credentials{}, fmt.Errorf("%s refused: %s", p.Label, msg)
	}
	if body.AccessToken == "" {
		return Credentials{}, fmt.Errorf("%s returned no token", p.Label)
	}
	ttl := time.Duration(body.ExpiresIn) * time.Second
	switch {
	case body.ExpiresIn > 0:
	case p.NoExpiry:
		ttl = forever
	default:
		ttl = time.Hour
	}
	return Credentials{AccessToken: body.AccessToken, RefreshToken: body.RefreshToken, Expiry: m.now().Add(ttl), Account: body.Team.Name}, nil
}

// refreshAhead is how long before expiry a token is refreshed: before it fails, not after.
const refreshAhead = 10 * time.Minute

// Due reports whether the connector's token should be refreshed now: it is signed in with a
// refresh token and expires within refreshAhead.
func (m *Manager) Due(connector string) bool {
	creds, err := m.store.OAuthCredentials(connector)
	if err != nil || creds.RefreshToken == "" {
		return false
	}
	return !m.now().Add(refreshAhead).Before(creds.Expiry)
}

// KeepFresh refreshes, every `every`, the token of each connector that `names` lists and Due
// says is about to expire, until ctx ends. A refresh that fails is reported through warn and
// tried again on the next round; the connector itself refreshes on use as well.
func (m *Manager) KeepFresh(ctx context.Context, every time.Duration, names func() []string, warn func(format string, args ...any)) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		m.refreshDue(ctx, names(), warn)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (m *Manager) refreshDue(ctx context.Context, names []string, warn func(string, ...any)) {
	for _, name := range names {
		if !m.Due(name) {
			continue
		}
		creds, _ := m.store.OAuthCredentials(name)
		if _, err := m.refresh(ctx, name, creds); err != nil && warn != nil {
			warn("oauth: %s: %v", name, err)
		}
	}
}

// Token returns a valid access token for the connector, refreshing it when it is about to expire.
func (m *Manager) Token(ctx context.Context, connector string) (string, error) {
	creds, err := m.store.OAuthCredentials(connector)
	if err != nil {
		return "", err
	}
	if creds.AccessToken != "" && m.now().Add(time.Minute).Before(creds.Expiry) {
		return creds.AccessToken, nil
	}
	return m.refresh(ctx, connector, creds)
}

// refresh trades the refresh token for a new access token and saves it.
func (m *Manager) refresh(ctx context.Context, connector string, creds Credentials) (string, error) {
	if creds.RefreshToken == "" {
		return "", errors.New("not signed in: connect the account from Settings")
	}
	providerName, clientID, clientSecret, err := m.store.OAuthApp(connector)
	if err != nil {
		return "", err
	}
	p, ok := Providers[providerName]
	if !ok {
		return "", fmt.Errorf("%q has no OAuth flow", providerName)
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {creds.RefreshToken}, "client_id": {clientID}}
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	fresh, err := m.token(ctx, p, form)
	if err != nil {
		return "", err
	}
	if fresh.RefreshToken == "" {
		fresh.RefreshToken = creds.RefreshToken
	}
	fresh.Account = creds.Account
	if err := m.store.SaveOAuthCredentials(connector, fresh); err != nil {
		return "", err
	}
	return fresh.AccessToken, nil
}

// CallbackHTML is the page the browser shows once the sign-in landed.
const CallbackHTML = `<!doctype html><html><head><meta charset="utf-8"><title>Kitties</title>
<style>body{margin:0;height:100vh;display:grid;place-items:center;background:#0a0a0f;color:#e8e8ea;font:16px system-ui,sans-serif}
.card{padding:28px 32px;border:1px solid rgba(0,240,255,.4);box-shadow:0 0 14px rgba(0,240,255,.25);text-align:center}h1{font-size:18px;margin:0 0 8px}p{color:#9a9aaa;margin:0}</style></head>
<body><div class="card"><h1>%s</h1><p>%s</p></div></body></html>`
