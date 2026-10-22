// Package connector lets a kitty reach the outside world: a chat channel, a webhook, a shell
// command on the host, or a motita gateway that runs a coding task.
//
// The model asks for a connector by writing one line of the form
//
//	@connector <name> {"text": "..."}
//
// in its reply. The chat runtime parses those lines (Parse), runs them (Registry.Call) and
// feeds the results back as a message, so the kitty sees what happened.
package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

// Call is one request a kitty made.
type Call struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
	// Raw is the line as written, for the record.
	Raw string `json:"raw"`
}

// Result is what a connector answered.
type Result struct {
	Name   string `json:"name"`
	Output string `json:"output"`
	Err    string `json:"error,omitempty"`
	// File is a file the connector produced (a rendered diagram), for the artifacts; it is
	// not sent to the page.
	File *File `json:"-"`
	// Artifact is the name the file was kept under, once saved.
	Artifact string `json:"artifact,omitempty"`
}

// File is a connector's binary answer.
type File struct {
	Name string
	Data []byte
}

// Connector is one external service.
type Connector interface {
	// Describe says what the connector does, for the model and for people.
	Describe() string
	// Call runs one request.
	Call(ctx context.Context, args map[string]any) (string, error)
}

// Registry is the connectors a deployment configured, by name.
type Registry struct {
	mu    sync.RWMutex
	items map[string]Connector
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry { return &Registry{items: map[string]Connector{}} }

// Add registers c under name.
func (r *Registry) Add(name string, c Connector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[name] = c
}

// Remove forgets the connector called name.
func (r *Registry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.items, name)
}

// Get returns the connector called name.
func (r *Registry) Get(name string) (Connector, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.items[name]
	return c, ok
}

// Names lists the registered connectors, sorted.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.items))
	for n := range r.items {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Descriptions returns name -> description for the connectors in allow (every one when allow is empty).
func (r *Registry) Descriptions(allow []string) map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := map[string]string{}
	for n, c := range r.items {
		if len(allow) == 0 || contains(allow, n) {
			out[n] = c.Describe()
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Call runs one call, refusing a connector not in allow (every one when allow is empty).
func (r *Registry) Call(ctx context.Context, call Call, allow []string) Result {
	res := Result{Name: call.Name}
	if len(allow) > 0 && !contains(allow, call.Name) {
		res.Err = fmt.Sprintf("connector %q is not available to this kitty", call.Name)
		return res
	}
	c, ok := r.Get(call.Name)
	if !ok {
		res.Err = fmt.Sprintf("unknown connector %q (available: %s)", call.Name, strings.Join(r.Names(), ", "))
		return res
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	var out string
	var err error
	if filer, ok := c.(Filer); ok {
		var name string
		var data []byte
		out, name, data, err = filer.CallFile(ctx, call.Args)
		if err == nil && name != "" {
			res.File = &File{Name: name, Data: data}
		}
	} else {
		out, err = c.Call(ctx, call.Args)
	}
	res.Output = clip(out, 4000)
	if err != nil {
		res.Err = err.Error()
	}
	return res
}

var callTimeout = 10 * time.Minute

// Prefix starts a connector line.
const Prefix = "@connector "

// Parse finds the connector lines in a reply and returns them with the reply's remaining text.
func Parse(text string) ([]Call, string) {
	var calls []Call
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, Prefix) {
			kept = append(kept, line)
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, Prefix))
		name, argsText, _ := strings.Cut(rest, " ")
		call := Call{Name: strings.TrimSpace(name), Raw: trimmed, Args: map[string]any{}}
		argsText = strings.TrimSpace(argsText)
		if argsText != "" {
			if err := json.Unmarshal([]byte(argsText), &call.Args); err != nil {
				call.Args = map[string]any{"text": argsText}
			}
		}
		calls = append(calls, call)
	}
	return calls, strings.TrimSpace(strings.Join(kept, "\n"))
}

// Format renders results as the message the kitty reads back.
func Format(results []Result) string {
	var b strings.Builder
	for _, r := range results {
		fmt.Fprintf(&b, "[connector %s]", r.Name)
		if r.Err != "" {
			fmt.Fprintf(&b, " failed: %s\n", r.Err)
			continue
		}
		b.WriteString("\n")
		if r.Output != "" {
			b.WriteString(r.Output + "\n")
		} else {
			b.WriteString("done\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// text takes the text argument, or renders the whole arguments.
func text(args map[string]any) string {
	for _, k := range []string{"text", "message", "task", "content"} {
		if v, ok := args[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	data, _ := json.Marshal(args)
	return string(data)
}

// --- HTTP-based connectors -----------------------------------------------------------------

// httpDo is the seam every HTTP connector posts through.
var httpDo = func(req *http.Request) (*http.Response, error) {
	return (&http.Client{Timeout: 60 * time.Second}).Do(req)
}

func postJSON(ctx context.Context, url string, headers map[string]string, body any) (string, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpDo(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, clip(string(data), 300))
	}
	return string(data), nil
}

// Webhook posts the arguments as JSON to a URL.
type Webhook struct {
	URL         string
	Headers     map[string]string
	Description string
}

// Describe implements Connector.
func (w Webhook) Describe() string {
	return orDefault(w.Description, "posts the arguments as JSON to a webhook")
}

// Call implements Connector.
func (w Webhook) Call(ctx context.Context, args map[string]any) (string, error) {
	return postJSON(ctx, w.URL, w.Headers, args)
}

// Slack posts text to a Slack incoming webhook.
type Slack struct {
	URL         string
	Description string
}

// Describe implements Connector.
func (s Slack) Describe() string {
	return orDefault(s.Description, "posts a message to a Slack channel")
}

// Call implements Connector.
func (s Slack) Call(ctx context.Context, args map[string]any) (string, error) {
	_, err := postJSON(ctx, s.URL, nil, map[string]any{"text": text(args)})
	return "posted to Slack", err
}

// Discord posts text to a Discord webhook.
type Discord struct {
	URL         string
	Description string
}

// Describe implements Connector.
func (d Discord) Describe() string {
	return orDefault(d.Description, "posts a message to a Discord channel")
}

// Call implements Connector.
func (d Discord) Call(ctx context.Context, args map[string]any) (string, error) {
	_, err := postJSON(ctx, d.URL, nil, map[string]any{"content": text(args)})
	return "posted to Discord", err
}

// Telegram sends text through a bot to a chat.
type Telegram struct {
	Token       string
	ChatID      string
	Description string
	// BaseURL is the API; empty is Telegram's own.
	BaseURL string
}

// Describe implements Connector.
func (t Telegram) Describe() string {
	return orDefault(t.Description, "sends a message to a Telegram chat")
}

// Call implements Connector.
func (t Telegram) Call(ctx context.Context, args map[string]any) (string, error) {
	base := orDefault(t.BaseURL, "https://api.telegram.org")
	_, err := postJSON(ctx, base+"/bot"+t.Token+"/sendMessage", nil, map[string]any{"chat_id": t.ChatID, "text": text(args)})
	return "sent to Telegram", err
}

// Shell runs a fixed command on the host with the text as its last argument. The command is
// chosen by the operator in the configuration, never by the model.
type Shell struct {
	Command     string
	Args        []string
	Description string
}

// Describe implements Connector.
func (s Shell) Describe() string {
	return orDefault(s.Description, "runs "+s.Command+" with the text as its argument")
}

// Call implements Connector.
func (s Shell) Call(ctx context.Context, args map[string]any) (string, error) {
	argv := append(append([]string(nil), s.Args...), text(args))
	out, err := exec.CommandContext(ctx, s.Command, argv...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %w", s.Command, err)
	}
	return string(out), nil
}

// Motita hands a task to a motita gateway and returns its result.
type Motita struct {
	URL         string
	Token       string
	Description string
}

// Describe implements Connector.
func (m Motita) Describe() string {
	return orDefault(m.Description, "runs a coding task in motita (an agent that proves it is done) and returns the result")
}

// Call implements Connector: opens a session, posts the task and reads the event stream to its end.
func (m Motita) Call(ctx context.Context, args map[string]any) (string, error) {
	task := text(args)
	if strings.TrimSpace(task) == "" {
		return "", errors.New("motita needs a task")
	}
	headers := map[string]string{}
	if m.Token != "" {
		headers["Authorization"] = "Bearer " + m.Token
	}
	base := strings.TrimRight(m.URL, "/")
	raw, err := postJSON(ctx, base+"/v1/sessions", headers, map[string]any{})
	if err != nil {
		return "", fmt.Errorf("open a motita session: %w", err)
	}
	var session struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(raw), &session); err != nil || session.ID == "" {
		return "", errors.New("motita did not return a session id")
	}
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(map[string]any{"task": task})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/sessions/"+session.ID+"/task", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpDo(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return "", fmt.Errorf("motita refused the task: HTTP %d: %s", resp.StatusCode, clip(string(data), 300))
	}
	return readMotitaStream(resp.Body)
}

// readMotitaStream reads the gateway's SSE until a done or error event.
func readMotitaStream(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, 16<<20))
	if err != nil {
		return "", err
	}
	var event string
	var progress []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			switch event {
			case "done":
				var d struct {
					Result string `json:"result"`
				}
				_ = json.Unmarshal([]byte(payload), &d)
				return orDefault(d.Result, strings.Join(progress, "\n")), nil
			case "error":
				var e struct {
					Error string `json:"error"`
				}
				_ = json.Unmarshal([]byte(payload), &e)
				return strings.Join(progress, "\n"), errors.New(orDefault(e.Error, "motita failed"))
			case "progress":
				var p struct {
					Text string `json:"text"`
				}
				if json.Unmarshal([]byte(payload), &p) == nil && p.Text != "" {
					progress = append(progress, p.Text)
				}
			}
		}
	}
	return strings.Join(progress, "\n"), errors.New("motita's stream ended without a result")
}

func orDefault(s, def string) string {
	if s != "" {
		return s
	}
	return def
}
