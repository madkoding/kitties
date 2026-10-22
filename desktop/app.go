package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// App is what the page can call: the connection to the gateway and a proxy for its API, so the
// token never reaches the page.
type App struct {
	ctx  context.Context
	http *http.Client
	conn Connection
}

// Connection is where the gateway is and how to talk to it.
type Connection struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

// NewApp creates the application.
func NewApp() *App {
	return &App{http: &http.Client{Timeout: 10 * time.Minute}}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.conn = loadConnection()
}

func (a *App) shutdown(context.Context) {}

// configPath is where the connection is kept: ~/.kitties/desktop.json.
func configPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "desktop.json"
	}
	return filepath.Join(home, ".kitties", "desktop.json")
}

// loadConnection reads the saved connection. A local executable needs no typing: when no token
// was saved, the one the gateway left in ~/.kitties/gateway.token is used.
func loadConnection() Connection {
	conn := Connection{URL: "http://127.0.0.1:7488"}
	if data, err := os.ReadFile(configPath()); err == nil {
		_ = json.Unmarshal(data, &conn)
	}
	if conn.URL == "" {
		conn.URL = "http://127.0.0.1:7488"
	}
	if conn.Token == "" {
		conn.Token = localToken()
	}
	return conn
}

// localToken is the token of the gateway running on this machine, or empty.
func localToken() string {
	data, err := os.ReadFile(filepath.Join(filepath.Dir(configPath()), "gateway.token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// Gateway returns the connection the app uses, token included: the page shows it in Settings.
func (a *App) Gateway() Connection { return a.conn }

// Connect sets the gateway URL and token, checks them against /v1/health and saves them.
func (a *App) Connect(url, token string) (string, error) {
	url = strings.TrimRight(strings.TrimSpace(url), "/")
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return "", errors.New("the gateway URL must start with http:// or https://")
	}
	probe := &App{http: a.http, conn: Connection{URL: url, Token: strings.TrimSpace(token)}}
	raw, err := probe.Request(http.MethodGet, "/v1/kitties", "")
	if err != nil {
		return "", err
	}
	var res Response
	_ = json.Unmarshal([]byte(raw), &res)
	if res.Status == http.StatusUnauthorized {
		return "", errors.New("the gateway refused the token")
	}
	if res.Status >= 400 {
		return "", fmt.Errorf("the gateway answered HTTP %d", res.Status)
	}
	a.conn = probe.conn
	data, _ := json.MarshalIndent(a.conn, "", "  ")
	if err := os.MkdirAll(filepath.Dir(configPath()), 0o700); err == nil {
		_ = os.WriteFile(configPath(), data, 0o600)
	}
	return "connected to " + url, nil
}

// Response is what Request returns to the page, as JSON text.
type Response struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
	// Base64 carries a binary answer (a screenshot) the page shows as a data: URL.
	Base64 string `json:"base64,omitempty"`
}

// Request performs one API call against the gateway on the page's behalf.
func (a *App) Request(method, path, body string) (string, error) {
	if !strings.HasPrefix(path, "/v1/") {
		return "", errors.New("only the gateway API can be called")
	}
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, a.conn.URL+path, reader)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.conn.Token != "" {
		req.Header.Set("Authorization", "Bearer "+a.conn.Token)
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("the gateway at %s does not answer: %w", a.conn.URL, err)
	}
	if resp.StatusCode == http.StatusUnauthorized && body == "" {
		if fresh := localToken(); fresh != "" && fresh != a.conn.Token {
			resp.Body.Close()
			a.conn.Token = fresh
			return a.Request(method, path, body)
		}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return "", err
	}
	out := Response{Status: resp.StatusCode}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
		out.Base64 = base64.StdEncoding.EncodeToString(data)
	} else {
		out.Body = string(data)
	}
	encoded, err := json.Marshal(out)
	return string(encoded), err
}
