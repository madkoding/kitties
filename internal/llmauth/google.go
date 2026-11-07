package llmauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// --- Google (gemini) in the browser ---------------------------------------------------------
//
// With a Google app (a "Desktop app" OAuth client: KITTIES_GOOGLE_CLIENT_ID/_SECRET on the
// gateway, or the one Kitties was built with), Gemini signs in like ChatGPT: PKCE, the browser
// sent back to 127.0.0.1 on a port of this machine (a desktop client takes any port), or the
// address it landed on pasted back when the browser runs on another machine. The requests are
// billed to the Cloud project the person names.

// geminiScopes are what the Gemini API takes from a user's token (the ones gcloud asks for too),
// and who signed in.
var geminiScopes = []string{"openid", "email",
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/generative-language.retriever"}

// googleBrowser reports whether Gemini can sign in in the browser.
func (m *Manager) googleBrowser() bool {
	id, _ := m.googleApp()
	return id != ""
}

// ErrNoProject asks for the Cloud project Gemini is billed to.
var ErrNoProject = errors.New("Google bills Gemini to a Cloud project: type its id (Project ID)")

func (m *Manager) startGoogleBrowser(opts StartOptions) (Flow, error) {
	project := strings.TrimSpace(opts.Project)
	if project == "" {
		return Flow{}, ErrNoProject
	}
	id, secret := m.googleApp()
	ln, err := m.listen("127.0.0.1:0")
	if err != nil {
		return Flow{}, fmt.Errorf("the sign-in could not wait for Google: %w", err)
	}
	redirect := "http://" + ln.Addr().String() + "/auth/callback"
	p := newPKCE()
	q := url.Values{
		"response_type": {"code"}, "client_id": {id}, "redirect_uri": {redirect},
		"scope": {strings.Join(geminiScopes, " ")}, "code_challenge": {p.challenge}, "code_challenge_method": {"S256"},
		"state": {p.state}, "access_type": {"offline"}, "prompt": {"consent select_account"},
	}
	link := m.ep.GoogleAuth + "?" + q.Encode()
	callbacks := make(chan callback, 1)
	srv := serveCallback(ln, p.state, "Google account", callbacks)
	paste := make(chan string, 1)
	return m.track(Flow{Kind: "gemini", Method: "browser", URL: link, Paste: true}, paste, func(ctx context.Context) (Credential, error) {
		defer func() {
			stop, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = srv.Shutdown(stop)
		}()
		code, err := waitCode(ctx, p.state, callbacks, paste)
		if err != nil {
			return Credential{}, err
		}
		return m.googleExchange(ctx, Credential{Kind: "gemini", ClientID: id, ClientSecret: secret, ProjectID: project},
			code, p.verifier, redirect)
	}), nil
}

// googleExchange trades the code for the account's tokens and names it by its email.
func (m *Manager) googleExchange(ctx context.Context, c Credential, code, verifier, redirect string) (Credential, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect},
		"client_id": {c.ClientID}, "code_verifier": {verifier}}
	if c.ClientSecret != "" {
		form.Set("client_secret", c.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.ep.GoogleToken, strings.NewReader(form.Encode()))
	if err != nil {
		return Credential{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var a struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		IDToken      string `json:"id_token"`
		Error        string `json:"error"`
		Description  string `json:"error_description"`
	}
	err = m.call(req, &a)
	if a.Error != "" {
		return Credential{}, fmt.Errorf("Google refused: %s", strings.TrimSpace(a.Error+" "+a.Description))
	}
	if err != nil {
		return Credential{}, err
	}
	if a.AccessToken == "" || a.RefreshToken == "" {
		return Credential{}, errors.New("Google returned no lasting token: start again")
	}
	c.AccessToken, c.RefreshToken = a.AccessToken, a.RefreshToken
	c.ExpiresAt = m.now().Add(time.Hour)
	if a.ExpiresIn > 0 {
		c.ExpiresAt = m.now().Add(time.Duration(a.ExpiresIn) * time.Second)
	}
	who, _ := jwtClaims(a.IDToken)["email"].(string)
	if who == "" {
		who = "Google"
	}
	c.Who = who + ", project " + c.ProjectID
	return c, nil
}
