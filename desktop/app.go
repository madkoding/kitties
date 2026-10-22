package main

import (
	"bufio"
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
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is what the page can call: the connection to the gateway and a proxy for its API, so the
// token never reaches the page.
type App struct {
	ctx  context.Context
	http *http.Client
	conn Connection
	// emit hands an event to the page (the Wails runtime once the app runs).
	emit    func(name string, data any)
	mu      sync.Mutex
	watches map[string]context.CancelFunc
}

// Connection is where the gateway is and how to talk to it.
type Connection struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

// NewApp creates the application.
func NewApp() *App {
	return &App{http: &http.Client{Timeout: 10 * time.Minute}, watches: map[string]context.CancelFunc{}}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.conn = loadConnection()
	if a.emit == nil {
		a.emit = func(name string, data any) { runtime.EventsEmit(ctx, name, data) }
	}
}

func (a *App) shutdown(context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, cancel := range a.watches {
		cancel()
		delete(a.watches, id)
	}
}

// Watch follows a chat's events from the gateway and hands each one to the page as the
// runtime event "kitties:chat:<id>". Watching again restarts the stream.
func (a *App) Watch(id string) error {
	if strings.ContainsAny(id, "/?#") || id == "" {
		return errors.New("not a chat id")
	}
	a.Unwatch(id)
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.watches[id] = cancel
	a.mu.Unlock()
	go a.follow(ctx, id)
	return nil
}

// Unwatch stops following a chat.
func (a *App) Unwatch(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if cancel, ok := a.watches[id]; ok {
		cancel()
		delete(a.watches, id)
	}
}

// follow reads the event stream until the context ends, reconnecting after a drop.
func (a *App) follow(ctx context.Context, id string) {
	for ctx.Err() == nil {
		if err := a.stream(ctx, id); err != nil && ctx.Err() == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(reconnectAfter):
			}
		}
	}
}

var reconnectAfter = 3 * time.Second

func (a *App) stream(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.conn.URL+"/v1/chats/"+id+"/events", nil)
	if err != nil {
		return err
	}
	if a.conn.Token != "" {
		req.Header.Set("Authorization", "Bearer "+a.conn.Token)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the gateway answered HTTP %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	event := ""
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			var payload map[string]any
			if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &payload) == nil && event != "" && event != "hello" {
				if a.emit != nil {
					a.emit("kitties:chat:"+id, payload)
				}
				if event == "deleted" {
					return nil
				}
			}
		case line == "":
			event = ""
		}
	}
	return scanner.Err()
}

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
	return asciiOnly(string(encoded)), err
}

// asciiOnly rewrites every non-ASCII character of a JSON text as a \uXXXX escape (a surrogate pair
// above U+FFFF), which JSON reads back as the same text. What the page receives from this app
// crosses the webview's bridge as a script, and the answers of the gateway are full of dashes,
// ellipses and accents: plain ASCII leaves nothing in them for that bridge to choke on.
func asciiOnly(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r < 0x80:
			b.WriteRune(r)
		case r < 0x10000:
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			r -= 0x10000
			fmt.Fprintf(&b, "\\u%04x\\u%04x", 0xd800+(r>>10), 0xdc00+(r&0x3ff))
		}
	}
	return b.String()
}
