package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Nextcloud's Login Flow v2 is its click-and-allow: no app to register and no callback. Kitties
// asks the server for a login page and a poll address, the person opens the page and allows
// Kitties, and the poll answers with an app password, which becomes the connector's password.
// https://docs.nextcloud.com/server/latest/developer_manual/client_apis/LoginFlow/index.html

// loginFlowFor is how long a login page waits for the person: Nextcloud's own limit.
const loginFlowFor = 20 * time.Minute

// davRoot is the server a Nextcloud address belongs to: a CalDAV or CardDAV address ends in
// /remote.php/dav, which the login flow does not live under.
func davRoot(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("the server's address is missing (https://cloud.example.com)")
	}
	if i := strings.Index(u.Path, "/remote.php"); i >= 0 {
		u.Path = u.Path[:i]
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery, u.Fragment = "", ""
	return u, nil
}

// startLoginFlow asks the server for a login page and follows it in the background until the
// person allows Kitties, says no, or the page expires.
func (m *Manager) startLoginFlow(connector string, p Provider, app App) (string, error) {
	root, err := davRoot(app.Settings["url"])
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, root.String()+"/index.php/login/v2", nil)
	if err != nil {
		return "", err
	}
	// Nextcloud names the app password after the client that asked for it.
	req.Header.Set("User-Agent", "Kitties")
	var start struct {
		Poll struct {
			Token    string `json:"token"`
			Endpoint string `json:"endpoint"`
		} `json:"poll"`
		Login string `json:"login"`
	}
	if err := readAnswer(m.httpDo, req, &start); err != nil {
		return "", fmt.Errorf("%s did not start the sign-in (is it a Nextcloud?): %w", p.Label, err)
	}
	poll, err := url.Parse(start.Poll.Endpoint)
	if err != nil || start.Poll.Token == "" || start.Login == "" || poll.Host != root.Host {
		return "", fmt.Errorf("%s answered without a login page of its own", p.Label)
	}
	go m.followLoginFlow(connector, poll.String(), start.Poll.Token)
	return start.Login, nil
}

// followLoginFlow asks the poll address, every loginEvery, whether the person allowed Kitties,
// and keeps the app password once they did.
func (m *Manager) followLoginFlow(connector, endpoint, token string) {
	deadline := m.now().Add(loginFlowFor)
	for m.now().Before(deadline) {
		done, err := m.pollLoginFlow(connector, endpoint, token)
		if done {
			if err != nil && m.warn != nil {
				m.warn("oauth: %s: %v", connector, err)
			}
			return
		}
		m.sleep(m.loginEvery)
	}
}

// pollLoginFlow asks once; done is false while the person has not answered yet.
func (m *Manager) pollLoginFlow(connector, endpoint, token string) (done bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(url.Values{"token": {token}}.Encode()))
	if err != nil {
		return true, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var got struct {
		LoginName   string `json:"loginName"`
		AppPassword string `json:"appPassword"`
	}
	if err := readAnswer(m.httpDo, req, &got); err != nil {
		// 404 until the person allows Kitties; a network hiccup is asked again too.
		return false, nil
	}
	if got.AppPassword == "" {
		return true, errors.New("the server returned no app password")
	}
	if err := m.store.SaveOAuthSettings(connector, map[string]string{"user": got.LoginName, "password": got.AppPassword}); err != nil {
		return true, err
	}
	// The app password is the account: it lasts until it is revoked in Nextcloud's settings,
	// and is kept once, in the connector's password; the credentials only say who signed in.
	return true, m.store.SaveOAuthCredentials(connector, Credentials{AccessToken: "app-password", Expiry: m.now().Add(forever), Account: got.LoginName})
}
