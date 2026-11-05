package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBoatURL is Boat's API root.
const DefaultBoatURL = "https://boat.dev/api/v1"

// Labels every machine this package makes carries, so it never touches a machine it did not make.
const (
	labelDeployment = "kitties.deployment"
	labelKitty      = "kitties.kitty"
	labelKey        = "kitties.key"
)

// APIError is a refusal from Boat. It never carries the API key.
type APIError struct {
	Status     int
	Code       string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("boat: %s (HTTP %d)", e.Code, e.Status)
}

// Boat is a Provider backed by Boat's sandbox API (https://docs.boat.dev). Zero fields take
// defaults except Key and Deployment, which are required.
type Boat struct {
	Key        string // API key, from the vault
	Deployment string // stable id of this deployment; machines of other deployments are invisible
	BaseURL    string
	Client     *http.Client
	// MaxRunning refuses a create past this many running machines; zero means no limit here.
	MaxRunning int
	// TTL is the lifetime asked for a machine; it expires on its own if the gateway vanishes.
	TTL time.Duration
	// Poll is how often a stop, resume or snapshot is checked; Timeout bounds the wait.
	Poll    time.Duration
	Timeout time.Duration
}

func (b *Boat) base() string {
	if b.BaseURL != "" {
		return strings.TrimRight(b.BaseURL, "/")
	}
	return DefaultBoatURL
}

func (b *Boat) client() *http.Client {
	if b.Client != nil {
		return b.Client
	}
	return &http.Client{
		Timeout: 60 * time.Second,
		// A redirect would carry the bearer token somewhere else.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (b *Boat) ttl() int {
	if b.TTL > 0 {
		return int(b.TTL / time.Second)
	}
	return 3600
}

func (b *Boat) poll() time.Duration {
	if b.Poll > 0 {
		return b.Poll
	}
	return 2 * time.Second
}

func (b *Boat) timeout() time.Duration {
	if b.Timeout > 0 {
		return b.Timeout
	}
	return 10 * time.Minute
}

type boatSandbox struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	State              string            `json:"state"`
	Labels             map[string]string `json:"labels"`
	Access             string            `json:"access"`
	Hydrated           *bool             `json:"hydrated"`
	SnapshotAvailable  bool              `json:"snapshotAvailable"`
	LastSnapshotStatus string            `json:"lastSnapshotStatus"`
}

type boatSnapshot struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

// do sends one request and decodes the answer into out (nil to ignore it).
func (b *Boat) do(ctx context.Context, method, path string, headers map[string]string, body, out any) error {
	if b.Key == "" || b.Deployment == "" {
		return errors.New("boat: the API key and the deployment id are required")
	}
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.base()+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+b.Key)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := b.client().Do(req)
	if err != nil {
		return fmt.Errorf("boat: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		e := &APIError{Status: resp.StatusCode, Code: "BOAT_UNAVAILABLE"}
		var f struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(data, &f) == nil && f.Code != "" && len(f.Code) <= 80 {
			e.Code = f.Code
		}
		if n, _ := strconv.Atoi(resp.Header.Get("Retry-After")); n > 0 {
			e.RetryAfter = min(time.Duration(n)*time.Second, 5*time.Minute)
		}
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%w: %w", ErrNotFound, e)
		}
		return e
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

func (b *Boat) owned(s boatSandbox) bool {
	return s.Labels[labelDeployment] == b.Deployment && s.Access != "shared"
}

func (b *Boat) machine(s boatSandbox) Machine {
	m := Machine{ID: s.ID, Name: s.Labels[labelKitty], Key: s.Labels[labelKey], Generation: 1}
	switch s.State {
	case "archived", "stopped":
		m.State = Asleep
	default:
		m.State = Running
	}
	return m
}

func (b *Boat) get(ctx context.Context, id string) (boatSandbox, error) {
	var r struct {
		Sandbox boatSandbox `json:"sandbox"`
	}
	if err := b.do(ctx, http.MethodGet, "/sandboxes/"+url.PathEscape(id), nil, nil, &r); err != nil {
		return boatSandbox{}, err
	}
	if !b.owned(r.Sandbox) {
		return boatSandbox{}, ErrNotFound // not ours: pretend it is absent, never touch it
	}
	return r.Sandbox, nil
}

func (b *Boat) list(ctx context.Context) ([]boatSandbox, error) {
	var found []boatSandbox
	cursor := ""
	for range 100 {
		path := "/sandboxes?limit=100"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		var r struct {
			Sandboxes []boatSandbox `json:"sandboxes"`
			PageInfo  *struct {
				HasMore    bool   `json:"hasMore"`
				NextCursor string `json:"nextCursor"`
			} `json:"pageInfo"`
		}
		if err := b.do(ctx, http.MethodGet, path, nil, nil, &r); err != nil {
			return nil, err
		}
		for _, s := range r.Sandboxes {
			if b.owned(s) {
				found = append(found, s)
			}
		}
		if r.PageInfo == nil || !r.PageInfo.HasMore {
			return found, nil
		}
		if r.PageInfo.NextCursor == "" || r.PageInfo.NextCursor == cursor {
			break
		}
		cursor = r.PageInfo.NextCursor
	}
	// A partial inventory must never be taken for a whole one.
	return nil, errors.New("boat: the inventory is incomplete")
}

// Create makes the machine for req, or returns the one the same Key already made. Boat remembers
// an Idempotency-Key for 24 hours, so a retry later than that must go through Ensure.
func (b *Boat) Create(ctx context.Context, req CreateRequest) (Machine, error) {
	if req.Key == "" || req.Name == "" {
		return Machine{}, errors.New("cloud: a request needs a key and a name")
	}
	if b.MaxRunning > 0 {
		all, err := b.list(ctx)
		if err != nil {
			return Machine{}, err
		}
		n := 0
		for _, s := range all {
			if b.machine(s).State == Running {
				if s.Labels[labelKey] == req.Key {
					return b.machine(s), nil // already made: not a new one
				}
				n++
			}
		}
		if n >= b.MaxRunning {
			return Machine{}, ErrLimit
		}
	}
	body := map[string]any{
		"name":       "kn-" + req.Name + "-" + shortKey(req.Key),
		"noEnv":      true,
		"type":       "default",
		"ttlSeconds": b.ttl(),
		"snapshots":  true,
		"labels": map[string]string{
			labelDeployment: b.Deployment,
			labelKitty:      req.Name,
			labelKey:        req.Key,
		},
	}
	var r struct {
		Sandbox boatSandbox `json:"sandbox"`
	}
	if err := b.do(ctx, http.MethodPost, "/sandboxes", map[string]string{"Idempotency-Key": req.Key}, body, &r); err != nil {
		return Machine{}, err
	}
	if r.Sandbox.ID == "" {
		return Machine{}, errors.New("boat: the answer names no machine")
	}
	if r.Sandbox.Labels == nil { // Boat may leave labels out of the answer
		r.Sandbox.Labels = map[string]string{labelDeployment: b.Deployment, labelKitty: req.Name, labelKey: req.Key}
	}
	return b.machine(r.Sandbox), nil
}

func shortKey(k string) string {
	if len(k) > 12 {
		return k[:12]
	}
	return k
}

func (b *Boat) Get(ctx context.Context, id string) (Machine, error) {
	s, err := b.get(ctx, id)
	if err != nil {
		return Machine{}, err
	}
	return b.machine(s), nil
}

func (b *Boat) List(ctx context.Context) ([]Machine, error) {
	all, err := b.list(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Machine, 0, len(all))
	for _, s := range all {
		out = append(out, b.machine(s))
	}
	return out, nil
}

// wait polls until done reports true, fails, or the timeout passes.
func (b *Boat) wait(ctx context.Context, what string, done func(context.Context) (bool, error)) error {
	ctx, cancel := context.WithTimeout(ctx, b.timeout())
	defer cancel()
	for {
		ok, err := done(ctx)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("boat: waiting for %s: %w", what, ctx.Err())
		case <-time.After(b.poll()):
		}
	}
}

// Sleep stops the machine in an orderly way, waits until it is archived, then keeps a permanent
// checkpoint. A forced or failed stop is an error: that state is not safe to call saved.
func (b *Boat) Sleep(ctx context.Context, id string) (Machine, error) {
	s, err := b.get(ctx, id)
	if err != nil {
		return Machine{}, err
	}
	if b.machine(s).State == Running {
		if err := b.do(ctx, http.MethodPost, "/sandboxes/"+url.PathEscape(id)+"/stop", nil, map[string]any{"force": false}, nil); err != nil {
			return Machine{}, err
		}
	}
	var cur boatSandbox
	if err := b.wait(ctx, "the machine to archive", func(ctx context.Context) (bool, error) {
		var err error
		if cur, err = b.get(ctx, id); err != nil {
			return false, err
		}
		if cur.LastSnapshotStatus == "failed" {
			return false, errors.New("boat: the machine's snapshot failed")
		}
		return b.machine(cur).State == Asleep && cur.SnapshotAvailable, nil
	}); err != nil {
		return Machine{}, err
	}
	name := "kn-" + shortKey(b.Deployment) + "-" + id + "-" + strconv.FormatInt(time.Now().Unix(), 36)
	if err := b.do(ctx, http.MethodPost, "/saved-snapshots", nil, map[string]string{"sandboxId": id, "name": name}, nil); err != nil {
		return Machine{}, err
	}
	if err := b.wait(ctx, "the checkpoint", func(ctx context.Context) (bool, error) {
		var r struct {
			Snapshot boatSnapshot `json:"snapshot"`
		}
		if err := b.do(ctx, http.MethodGet, "/saved-snapshots/"+url.PathEscape(name), nil, nil, &r); err != nil {
			return false, err
		}
		if r.Snapshot.Status == "failed" {
			return false, errors.New("boat: the checkpoint failed")
		}
		return r.Snapshot.Status == "ready", nil
	}); err != nil {
		return Machine{}, err
	}
	m := b.machine(cur)
	m.Checkpoint = name
	return m, nil
}

// Wake resumes an archived machine and waits until it is ready. Restoring a checkpoint into a new
// machine, after Boat has expired the old one, is a later step.
func (b *Boat) Wake(ctx context.Context, id string) (Machine, error) {
	s, err := b.get(ctx, id)
	if err != nil {
		return Machine{}, err
	}
	if b.machine(s).State == Asleep {
		if err := b.do(ctx, http.MethodPost, "/sandboxes/"+url.PathEscape(id)+"/resume", nil, map[string]any{"ttlSeconds": b.ttl(), "noEnv": true}, nil); err != nil {
			return Machine{}, err
		}
	}
	var cur boatSandbox
	if err := b.wait(ctx, "the machine to be ready", func(ctx context.Context) (bool, error) {
		var err error
		if cur, err = b.get(ctx, id); err != nil {
			return false, err
		}
		return b.machine(cur).State == Running && (cur.Hydrated == nil || *cur.Hydrated), nil
	}); err != nil {
		return Machine{}, err
	}
	return b.machine(cur), nil
}

// Delete removes the machine. Boat asks for its id again in a header as a confirmation. Saved
// checkpoints stay: purging them is a separate action.
func (b *Boat) Delete(ctx context.Context, id string) error {
	if _, err := b.get(ctx, id); err != nil {
		return err
	}
	return b.do(ctx, http.MethodDelete, "/sandboxes/"+url.PathEscape(id), map[string]string{"X-Ascii-Confirm-Delete": id}, nil, nil)
}
