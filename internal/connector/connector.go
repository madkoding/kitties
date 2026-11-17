// Package connector lets a kitty reach the outside world: a chat channel, a webhook, a shell
// command on the host, or a motita gateway that runs a coding task.
//
// The model asks for a connector with a native tool call (connector_<name>) when its provider
// has tools, or by writing one line of the form
//
//	@connector <name> {"text": "..."}
//
// in its reply (also read: "@connector_<name> {...}" and "@<name> {...}", see Parse). The chat runtime parses those lines (Parse), runs them (Registry.Call) and
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
	// Kind is the connector's kind in the catalogue, for its logo.
	Kind string `json:"kind,omitempty"`
	// Action and Detail say what was asked, for the page's card ("request", "GET /rest/…").
	Action string `json:"action,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Detail is a short line of what a call asks: the method and path of a request, or the first
// words of its main argument.
func Detail(args map[string]any) string {
	if path, _ := args["path"].(string); path != "" {
		method, _ := args["method"].(string)
		if method == "" {
			method = "GET"
		}
		return clip(strings.ToUpper(method)+" "+path, 120)
	}
	for _, k := range []string{"summary", "subject", "title", "query", "name", "text", "message", "task", "url", "id"} {
		if v, ok := args[k].(string); ok && strings.TrimSpace(v) != "" {
			return clip(strings.Join(strings.Fields(v), " "), 80)
		}
	}
	return ""
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

// Actions lists what a connector can do, as its kind in the catalogue says (request included for
// the kinds over an HTTP API); none for a connector of no kind (a fake, a plain webhook).
func (r *Registry) Actions(name string) []Action {
	c, ok := r.Get(name)
	if !ok {
		return nil
	}
	k, ok := c.(Kinder)
	if !ok {
		return nil
	}
	kind, ok := LookupKind(k.Kind())
	if !ok {
		return nil
	}
	return append([]Action(nil), kind.Actions...)
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
	if k, ok := c.(Kinder); ok {
		res.Kind = k.Kind()
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

// nativePrefix is the name a connector has as a native tool; a model that writes the call as
// text sometimes keeps it ("@connector_jira {...}").
const nativePrefix = "@connector_"

// Parse finds the connector lines in a reply and returns them with the reply's remaining text.
//
// Besides the documented "@connector <name> <json>", it takes the shapes models write when they
// mean the same thing: the native tool's name ("@connector_<name> <json>") and, for the names
// given (the connectors the kitty has), the short "@<name> <json>". The short shape needs the
// arguments to be a JSON object, so "@luna hola" in a group chat stays a mention. An action
// written before the object ("@jira request {...}") is taken as the action. Lines inside a code
// fence count too, and a fence left empty is dropped with them.
func Parse(text string, names ...string) ([]Call, string) {
	var calls []Call
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		call, ok := parseLine(trimmed, names)
		if !ok {
			kept = append(kept, line)
			continue
		}
		calls = append(calls, call)
	}
	if len(calls) > 0 {
		kept = dropEmptyFences(kept)
	}
	return calls, strings.TrimSpace(strings.Join(kept, "\n"))
}

// parseLine reads one line as a connector call.
func parseLine(line string, names []string) (Call, bool) {
	var name, argsText string
	switch {
	case strings.HasPrefix(line, Prefix):
		rest := strings.TrimSpace(strings.TrimPrefix(line, Prefix))
		name, argsText, _ = strings.Cut(rest, " ")
	case strings.HasPrefix(line, nativePrefix):
		rest := strings.TrimPrefix(line, nativePrefix)
		name, argsText, _ = strings.Cut(rest, " ")
	case strings.HasPrefix(line, "@"):
		rest := strings.TrimPrefix(line, "@")
		name, argsText, _ = strings.Cut(rest, " ")
		argsText = strings.TrimSpace(argsText)
		if !contains(names, name) || !strings.HasSuffix(argsText, "}") || !strings.Contains(argsText, "{") {
			return Call{}, false
		}
	default:
		return Call{}, false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Call{}, false
	}
	call := Call{Name: name, Raw: line, Args: map[string]any{}}
	argsText = strings.TrimSpace(argsText)
	if argsText == "" {
		return call, true
	}
	if json.Unmarshal([]byte(argsText), &call.Args) == nil {
		return call, true
	}
	// "request {...}": the action, then its arguments.
	if verb, obj, ok := strings.Cut(argsText, " "); ok && isWord(verb) {
		args := map[string]any{}
		if json.Unmarshal([]byte(strings.TrimSpace(obj)), &args) == nil {
			if _, set := args["action"]; !set {
				args["action"] = verb
			}
			call.Args = args
			return call, true
		}
	}
	if !strings.HasPrefix(line, Prefix) && !strings.HasPrefix(line, nativePrefix) {
		// A short "@name" line whose object does not read is a mention, not a call.
		return Call{}, false
	}
	call.Args = map[string]any{"text": argsText}
	return call, true
}

// isWord reports whether s is an action's name (letters, digits, - and _).
func isWord(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// dropEmptyFences removes the code fences that held only connector lines.
func dropEmptyFences(lines []string) []string {
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
			j := i + 1
			for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
				j++
			}
			if j < len(lines) && strings.TrimSpace(lines[j]) == "```" {
				i = j
				continue
			}
		}
		out = append(out, lines[i])
	}
	return out
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
