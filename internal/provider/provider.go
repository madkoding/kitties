// Package provider talks to the AI services a kitty can think with.
//
// It is a dependency-free client in the spirit of motita's connectors: every provider is one of
// four API families, and every reply is normalised to plain text so the rest of the framework
// never knows which service answered.
//
//   - chat:       POST /chat/completions (Bearer): openai, ollama, ollama-cloud, groq, openrouter,
//     deepseek, qwen, mistral, copilot and any OpenAI-compatible server
//   - responses:  POST /responses (Bearer): codex
//   - anthropic:  POST /v1/messages (x-api-key + anthropic-version)
//   - gemini:     POST /v1beta/models/<model>:generateContent (x-goog-api-key)
package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// The API families.
const (
	FamilyChat      = "chat"
	FamilyResponses = "responses"
	FamilyAnthropic = "anthropic"
	FamilyGemini    = "gemini"
)

// Kind describes one supported provider: its family, where it lives and which variable holds its key.
type Kind struct {
	Name    string
	Family  string
	BaseURL string
	KeyEnv  string
	// NoKey is true for a service that runs without a key (a local Ollama).
	NoKey bool
	// DefaultModel is the model used when the profile names none.
	DefaultModel string
}

// kinds is every provider the framework knows, keyed by name.
var kinds = map[string]Kind{
	"openai":       {Name: "openai", Family: FamilyChat, BaseURL: "https://api.openai.com/v1", KeyEnv: "OPENAI_API_KEY", DefaultModel: "gpt-5.5"},
	"codex":        {Name: "codex", Family: FamilyResponses, BaseURL: "https://api.openai.com/v1", KeyEnv: "OPENAI_API_KEY", DefaultModel: "gpt-5.5-codex"},
	"anthropic":    {Name: "anthropic", Family: FamilyAnthropic, BaseURL: "https://api.anthropic.com", KeyEnv: "ANTHROPIC_API_KEY", DefaultModel: "claude-sonnet-5-5"},
	"gemini":       {Name: "gemini", Family: FamilyGemini, BaseURL: "https://generativelanguage.googleapis.com", KeyEnv: "GEMINI_API_KEY", DefaultModel: "gemini-2.5-flash"},
	"ollama":       {Name: "ollama", Family: FamilyChat, BaseURL: "http://127.0.0.1:11434/v1", KeyEnv: "OLLAMA_API_KEY", NoKey: true, DefaultModel: "qwen3:8b"},
	"ollama-cloud": {Name: "ollama-cloud", Family: FamilyChat, BaseURL: "https://ollama.com/v1", KeyEnv: "OLLAMA_API_KEY", DefaultModel: "gpt-oss:120b"},
	"groq":         {Name: "groq", Family: FamilyChat, BaseURL: "https://api.groq.com/openai/v1", KeyEnv: "GROQ_API_KEY", DefaultModel: "llama-3.3-70b-versatile"},
	"openrouter":   {Name: "openrouter", Family: FamilyChat, BaseURL: "https://openrouter.ai/api/v1", KeyEnv: "OPENROUTER_API_KEY", DefaultModel: "openai/gpt-5.5"},
	"deepseek":     {Name: "deepseek", Family: FamilyChat, BaseURL: "https://api.deepseek.com/v1", KeyEnv: "DEEPSEEK_API_KEY", DefaultModel: "deepseek-chat"},
	"qwen":         {Name: "qwen", Family: FamilyChat, BaseURL: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", KeyEnv: "DASHSCOPE_API_KEY", DefaultModel: "qwen-plus"},
	"mistral":      {Name: "mistral", Family: FamilyChat, BaseURL: "https://api.mistral.ai/v1", KeyEnv: "MISTRAL_API_KEY", DefaultModel: "mistral-large-latest"},
	"copilot":      {Name: "copilot", Family: FamilyChat, BaseURL: "https://api.githubcopilot.com", KeyEnv: "COPILOT_API_KEY", DefaultModel: "gpt-4.1"},
	"custom":       {Name: "custom", Family: FamilyChat, KeyEnv: "LLM_API_KEY", NoKey: true},
}

// Kinds lists the supported provider names, sorted.
func Kinds() []string {
	out := make([]string, 0, len(kinds))
	for k := range kinds {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Lookup returns the kind called name.
func Lookup(name string) (Kind, bool) {
	k, ok := kinds[strings.ToLower(strings.TrimSpace(name))]
	return k, ok
}

// Options configures one client.
type Options struct {
	Kind        string
	Model       string
	APIKey      string
	BaseURL     string
	MaxTokens   int
	Temperature *float64
	Timeout     time.Duration
	MaxAttempts int
	Backoff     time.Duration
	// Headers are extra headers sent with every request (a gateway's own token, say).
	Headers map[string]string
}

// Message is one turn of a conversation.
type Message struct {
	Role    string `json:"role"` // system | user | assistant
	Content string `json:"content"`
	// Images are pictures a user turn shows the model (a screenshot of the kitty's display).
	Images []Image `json:"images,omitempty"`
}

// Image is one picture attached to a turn.
type Image struct {
	MIME string `json:"mime"` // image/png, image/jpeg
	Data []byte `json:"data"`
}

// dataURL renders the image as a data: URL.
func (i Image) dataURL() string {
	return "data:" + i.MIME + ";base64," + base64.StdEncoding.EncodeToString(i.Data)
}

// Usage counts the tokens of one reply, when the provider reported them.
type Usage struct {
	Input  int `json:"input"`
	Output int `json:"output"`
}

// Reply is a provider's answer.
type Reply struct {
	Content string `json:"content"`
	Usage   Usage  `json:"usage"`
}

// Client is anything that can answer a conversation. The HTTP client below is the real one;
// tests and the group chat use fakes.
type Client interface {
	Complete(ctx context.Context, messages []Message) (Reply, error)
	// Describe names the provider and the model, for people.
	Describe() string
}

// HTTPClient is the client for every HTTP provider.
type HTTPClient struct {
	opts  Options
	kind  Kind
	http  *http.Client
	sleep func(time.Duration)
}

// HTTPError is a non-2xx answer.
type HTTPError struct {
	Code int
	Body string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.Code, clip(e.Body, 300))
}

// Retryable reports whether the status is worth another attempt.
func (e *HTTPError) Retryable() bool {
	return e.Code == http.StatusTooManyRequests || e.Code >= 500
}

// ErrNoKey is returned when a provider needs a key and none was given.
var ErrNoKey = errors.New("the API key is missing")

// New builds the client for opts, reading the key from the environment when none is set.
func New(opts Options) (*HTTPClient, error) {
	kind, ok := Lookup(opts.Kind)
	if !ok {
		return nil, fmt.Errorf("unsupported provider %q (one of %s)", opts.Kind, strings.Join(Kinds(), ", "))
	}
	if opts.APIKey == "" && kind.KeyEnv != "" {
		opts.APIKey = os.Getenv(kind.KeyEnv)
	}
	if opts.APIKey == "" && !kind.NoKey {
		return nil, fmt.Errorf("%w for %s: set %s", ErrNoKey, kind.Name, kind.KeyEnv)
	}
	if opts.BaseURL == "" {
		opts.BaseURL = kind.BaseURL
	}
	if opts.BaseURL == "" {
		return nil, fmt.Errorf("provider %s needs a base_url", kind.Name)
	}
	opts.BaseURL = strings.TrimRight(opts.BaseURL, "/")
	if opts.Model == "" {
		opts.Model = kind.DefaultModel
	}
	if opts.Model == "" {
		return nil, fmt.Errorf("provider %s needs a model", kind.Name)
	}
	if opts.MaxTokens <= 0 {
		opts.MaxTokens = 4096
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 120 * time.Second
	}
	if opts.MaxAttempts < 1 {
		opts.MaxAttempts = 3
	}
	if opts.Backoff <= 0 {
		opts.Backoff = time.Second
	}
	return &HTTPClient{
		opts:  opts,
		kind:  kind,
		http:  &http.Client{Timeout: opts.Timeout, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}},
		sleep: time.Sleep,
	}, nil
}

// WithTransport sends every request through rt; tests use it to answer without a network.
func (c *HTTPClient) WithTransport(rt http.RoundTripper) *HTTPClient {
	c.http.Transport = rt
	return c
}

// Describe names the provider and model.
func (c *HTTPClient) Describe() string {
	return c.kind.Name + "/" + c.opts.Model
}

// Model is the configured model.
func (c *HTTPClient) Model() string { return c.opts.Model }

// Complete sends the conversation and returns the answer, retrying what is worth retrying.
func (c *HTTPClient) Complete(ctx context.Context, messages []Message) (Reply, error) {
	var last error
	wait := c.opts.Backoff
	for attempt := 1; attempt <= c.opts.MaxAttempts; attempt++ {
		reply, err := c.once(ctx, messages)
		if err == nil {
			return reply, nil
		}
		last = err
		var he *HTTPError
		if errors.As(err, &he) && !he.Retryable() {
			return Reply{}, err
		}
		if ctx.Err() != nil || attempt == c.opts.MaxAttempts {
			break
		}
		c.sleep(wait)
		if wait < 30*time.Second {
			wait *= 2
		}
	}
	return Reply{}, fmt.Errorf("%s: %w", c.Describe(), last)
}

func (c *HTTPClient) once(ctx context.Context, messages []Message) (Reply, error) {
	switch c.kind.Family {
	case FamilyAnthropic:
		return c.callAnthropic(ctx, messages)
	case FamilyGemini:
		return c.callGemini(ctx, messages)
	case FamilyResponses:
		return c.callResponses(ctx, messages)
	default:
		return c.callChat(ctx, messages)
	}
}

// post sends a JSON body and returns the answer's bytes.
func (c *HTTPClient) post(ctx context.Context, path string, body any) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.opts.BaseURL+path, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "kitties")
	switch c.kind.Family {
	case FamilyAnthropic:
		req.Header.Set("x-api-key", c.opts.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	case FamilyGemini:
		req.Header.Set("x-goog-api-key", c.opts.APIKey)
	default:
		if c.opts.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.opts.APIKey)
		}
	}
	for k, v := range c.opts.Headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &HTTPError{Code: resp.StatusCode, Body: string(data)}
	}
	return data, nil
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// temperature is the configured temperature, or def.
func (c *HTTPClient) temperature(def float64) float64 {
	if c.opts.Temperature != nil {
		return *c.opts.Temperature
	}
	return def
}

// splitSystem separates the system turns (joined) from the conversation.
func splitSystem(messages []Message) (string, []Message) {
	var system []string
	rest := make([]Message, 0, len(messages))
	for _, m := range messages {
		if m.Role == "system" {
			if strings.TrimSpace(m.Content) != "" {
				system = append(system, m.Content)
			}
			continue
		}
		rest = append(rest, m)
	}
	return strings.Join(system, "\n\n"), rest
}
