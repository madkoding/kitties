package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// rt answers every request with the given handler; it records what was sent.
type rt struct {
	t     *testing.T
	calls []map[string]any
	paths []string
	heads []http.Header
	reply func(n int) (int, string)
}

func (r *rt) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	r.calls = append(r.calls, m)
	r.paths = append(r.paths, req.URL.String())
	r.heads = append(r.heads, req.Header.Clone())
	code, text := r.reply(len(r.calls))
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(text)), Header: http.Header{}}, nil
}

func client(t *testing.T, kind string, reply func(int) (int, string)) (*HTTPClient, *rt) {
	t.Helper()
	c, err := New(Options{Kind: kind, APIKey: "k", Backoff: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	r := &rt{t: t, reply: reply}
	c.WithTransport(r)
	c.sleep = func(time.Duration) {}
	return c, r
}

var convo = []Message{{Role: "system", Content: "be brief"}, {Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"},
	{Role: "user", Content: "?", Images: []Image{{MIME: "image/png", Data: []byte("PNG")}}}}

// imagePart checks that the last message of a request carries one picture in the family's shape.
func imagePart(t *testing.T, call map[string]any, listKey, partsKey string) map[string]any {
	t.Helper()
	list := call[listKey].([]any)
	last := list[len(list)-1].(map[string]any)
	parts := last[partsKey].([]any)
	if len(parts) != 2 {
		t.Fatalf("the picture turn has %d parts: %v", len(parts), parts)
	}
	return parts[1].(map[string]any)
}

func TestNewDefaultsAndErrors(t *testing.T) {
	if _, err := New(Options{Kind: "nope"}); err == nil {
		t.Error("unknown kind must fail")
	}
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := New(Options{Kind: "openai"}); !errors.Is(err, ErrNoKey) {
		t.Errorf("missing key: %v", err)
	}
	t.Setenv("OPENAI_API_KEY", "env")
	c, err := New(Options{Kind: "openai"})
	if err != nil || c.opts.APIKey != "env" || c.Model() != "gpt-5.5" || c.Describe() != "openai/gpt-5.5" {
		t.Errorf("env key: %v %+v", err, c)
	}
	if _, err := New(Options{Kind: "custom"}); err == nil {
		t.Error("custom needs a base_url")
	}
	if _, err := New(Options{Kind: "custom", BaseURL: "http://x/"}); err == nil {
		t.Error("custom needs a model")
	}
	c, err = New(Options{Kind: "custom", BaseURL: "http://x/", Model: "m"})
	if err != nil || c.opts.BaseURL != "http://x" || c.opts.MaxTokens != 4096 || c.opts.MaxAttempts != 3 {
		t.Errorf("defaults: %v %+v", err, c.opts)
	}
	if len(Kinds()) != len(kinds) || Kinds()[0] != "anthropic" {
		t.Errorf("Kinds = %v", Kinds())
	}
	if _, ok := Lookup(" OpenAI "); !ok {
		t.Error("Lookup is case-insensitive")
	}
}

func TestChatFamily(t *testing.T) {
	c, r := client(t, "groq", func(int) (int, string) {
		return 200, `{"choices":[{"message":{"content":" yo "}}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`
	})
	reply, err := c.Complete(context.Background(), convo)
	if err != nil || reply.Content != "yo" || reply.Usage != (Usage{3, 1}) {
		t.Fatalf("reply = %+v, %v", reply, err)
	}
	if r.paths[0] != "https://api.groq.com/openai/v1/chat/completions" || r.heads[0].Get("Authorization") != "Bearer k" {
		t.Errorf("request = %s %v", r.paths[0], r.heads[0])
	}
	if msgs := r.calls[0]["messages"].([]any); len(msgs) != 4 {
		t.Errorf("messages = %v", msgs)
	}
	if part := imagePart(t, r.calls[0], "messages", "content"); part["type"] != "image_url" || !strings.HasPrefix(part["image_url"].(map[string]any)["url"].(string), "data:image/png;base64,UE5H") {
		t.Errorf("chat image = %v", part)
	}
}

func TestChatFamilyErrors(t *testing.T) {
	cases := []struct{ body, want string }{
		{`{"choices":[]}`, "no choices"},
		{`{"error":{"message":"bad model"}}`, "bad model"},
		{`{"choices":[{"message":{"content":""},"finish_reason":"length"}]}`, "length"},
		{`not json`, "unreadable"},
	}
	for _, tc := range cases {
		c, _ := client(t, "openai", func(int) (int, string) { return 200, tc.body })
		_, err := c.Complete(context.Background(), convo)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v", tc.body, err)
		}
	}
}

func TestRetryPolicy(t *testing.T) {
	c, r := client(t, "openai", func(n int) (int, string) {
		if n < 3 {
			return 503, "down"
		}
		return 200, `{"choices":[{"message":{"content":"ok"}}]}`
	})
	if reply, err := c.Complete(context.Background(), convo); err != nil || reply.Content != "ok" || len(r.calls) != 3 {
		t.Errorf("retry: %+v %v calls=%d", reply, err, len(r.calls))
	}
	c, r = client(t, "openai", func(int) (int, string) { return 401, "no" })
	var he *HTTPError
	if _, err := c.Complete(context.Background(), convo); !errors.As(err, &he) || he.Code != 401 || len(r.calls) != 1 {
		t.Errorf("a 401 is not retried: %v calls=%d", err, len(r.calls))
	}
	if !strings.Contains(he.Error(), "HTTP 401") {
		t.Error(he.Error())
	}
	c, r = client(t, "openai", func(int) (int, string) { return 429, strings.Repeat("x", 400) })
	if _, err := c.Complete(context.Background(), convo); err == nil || len(r.calls) != 3 || !strings.Contains(err.Error(), "…") {
		t.Errorf("a 429 is retried until the attempts run out: %v calls=%d", err, len(r.calls))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, r = client(t, "openai", func(int) (int, string) { return 500, "x" })
	if _, err := c.Complete(ctx, convo); err == nil || len(r.calls) != 1 {
		t.Errorf("a cancelled context stops the retries: %v calls=%d", err, len(r.calls))
	}
	c, _ = client(t, "openai", nil)
	c.opts.BaseURL = "http://127.0.0.1:1"
	c.http.Transport = http.DefaultTransport
	if _, err := c.Complete(context.Background(), convo); err == nil {
		t.Error("a connection failure is reported")
	}
	c.opts.BaseURL = "::bad"
	if _, err := c.Complete(context.Background(), convo); err == nil {
		t.Error("a bad URL is reported")
	}
}

func TestResponsesFamily(t *testing.T) {
	c, r := client(t, "codex", func(int) (int, string) {
		return 200, `{"output":[{"type":"reasoning"},{"type":"message","content":[{"type":"output_text","text":"a"},{"type":"refusal","text":"z"}]}],"usage":{"input_tokens":2,"output_tokens":5}}`
	})
	reply, err := c.Complete(context.Background(), convo)
	if err != nil || reply.Content != "a" || reply.Usage != (Usage{2, 5}) {
		t.Fatalf("reply = %+v, %v", reply, err)
	}
	if r.calls[0]["instructions"] != "be brief" || len(r.calls[0]["input"].([]any)) != 3 || !strings.HasSuffix(r.paths[0], "/responses") {
		t.Errorf("request = %v", r.calls[0])
	}
	if part := imagePart(t, r.calls[0], "input", "content"); part["type"] != "input_image" {
		t.Errorf("responses image = %v", part)
	}
	for body, want := range map[string]string{`{"output":[]}`: "no text", `{"error":{"message":"nope"}}`: "nope", `x`: "unreadable", `{}`: "no text"} {
		c, _ := client(t, "codex", func(int) (int, string) { return 200, body })
		if _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", body, err)
		}
	}
}

func TestAnthropicFamily(t *testing.T) {
	temp := 0.3
	c, err := New(Options{Kind: "anthropic", APIKey: "k", Temperature: &temp})
	if err != nil {
		t.Fatal(err)
	}
	r := &rt{reply: func(int) (int, string) {
		return 200, `{"content":[{"type":"thinking","text":"hmm"},{"type":"text","text":"sure"}],"usage":{"input_tokens":1,"output_tokens":2}}`
	}}
	c.WithTransport(r)
	reply, err := c.Complete(context.Background(), convo)
	if err != nil || reply.Content != "sure" || reply.Usage != (Usage{1, 2}) {
		t.Fatalf("reply = %+v, %v", reply, err)
	}
	h := r.heads[0]
	if h.Get("x-api-key") != "k" || h.Get("anthropic-version") == "" || h.Get("Authorization") != "" {
		t.Errorf("headers = %v", h)
	}
	call := r.calls[0]
	if call["system"] != "be brief" || call["temperature"] != 0.3 || len(call["messages"].([]any)) != 3 || !strings.HasSuffix(r.paths[0], "/v1/messages") {
		t.Errorf("request = %v", call)
	}
	if part := imagePart(t, call, "messages", "content"); part["type"] != "image" || part["source"].(map[string]any)["data"] != "UE5H" {
		t.Errorf("anthropic image = %v", part)
	}
	for body, want := range map[string]string{`{"content":[]}`: "no text", `{"error":{"message":"nope"}}`: "nope", `x`: "unreadable"} {
		c, _ := client(t, "anthropic", func(int) (int, string) { return 200, body })
		if _, err := c.Complete(context.Background(), convo); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", body, err)
		}
	}
}

func TestGeminiFamily(t *testing.T) {
	c, r := client(t, "gemini", func(int) (int, string) {
		return 200, `{"candidates":[{"content":{"parts":[{"text":"think","thought":true},{"text":"yes"}]}}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":6}}`
	})
	reply, err := c.Complete(context.Background(), convo)
	if err != nil || reply.Content != "yes" || reply.Usage != (Usage{4, 6}) {
		t.Fatalf("reply = %+v, %v", reply, err)
	}
	if r.heads[0].Get("x-goog-api-key") != "k" || !strings.Contains(r.paths[0], "/v1beta/models/gemini-2.5-flash:generateContent") {
		t.Errorf("request = %s %v", r.paths[0], r.heads[0])
	}
	if _, ok := r.calls[0]["systemInstruction"]; !ok || len(r.calls[0]["contents"].([]any)) != 3 {
		t.Errorf("request = %v", r.calls[0])
	}
	if part := imagePart(t, r.calls[0], "contents", "parts"); part["inlineData"].(map[string]any)["mimeType"] != "image/png" {
		t.Errorf("gemini image = %v", part)
	}
	for body, want := range map[string]string{`{"candidates":[]}`: "no candidates", `{"candidates":[{"content":{"parts":[]},"finishReason":"SAFETY"}]}`: "SAFETY", `{"error":{"message":"nope"}}`: "nope", `x`: "unreadable"} {
		c, _ := client(t, "gemini", func(int) (int, string) { return 200, body })
		if _, err := c.Complete(context.Background(), convo); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", body, err)
		}
	}
}

func TestExtraHeadersAndPostEncoding(t *testing.T) {
	c, err := New(Options{Kind: "ollama", Headers: map[string]string{"X-Team": "cats"}})
	if err != nil {
		t.Fatal(err)
	}
	r := &rt{reply: func(int) (int, string) { return 200, `{"choices":[{"message":{"content":"m"}}]}` }}
	c.WithTransport(r)
	if _, err := c.Complete(context.Background(), convo); err != nil {
		t.Fatal(err)
	}
	if r.heads[0].Get("X-Team") != "cats" || r.heads[0].Get("Authorization") != "" {
		t.Errorf("headers = %v", r.heads[0])
	}
	if _, err := c.post(context.Background(), "/x", map[string]any{"f": func() {}}); err == nil {
		t.Error("an unencodable body fails")
	}
}

func TestRegistry(t *testing.T) {
	reg := NewRegistry()
	if _, err := reg.Get("x"); err == nil {
		t.Error("unknown profile fails")
	}
	if err := reg.Build("bad", Options{Kind: "nope"}); err == nil {
		t.Error("Build reports the provider error")
	}
	if err := reg.Build("local", Options{Kind: "ollama", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	reg.Add("fake", Func(func(context.Context, []Message) (Reply, error) { return Reply{Content: "f"}, nil }), Options{Kind: "fake"})
	if names := reg.Names(); strings.Join(names, ",") != "fake,local" {
		t.Errorf("Names = %v", names)
	}
	c, err := reg.Get("fake")
	if err != nil || c.Describe() != "func" {
		t.Fatal(err)
	}
	if reply, _ := c.Complete(context.Background(), nil); reply.Content != "f" {
		t.Error("Func adapter")
	}
	list := reg.List()
	if len(list) != 2 || list[1].Describe != "ollama/m" || list[1].Kind != "ollama" {
		t.Errorf("List = %+v", list)
	}
	reg.Remove("fake")
	reg.Remove("nope")
	if names := reg.Names(); len(names) != 1 || names[0] != "local" {
		t.Errorf("Remove: %v", names)
	}
}
