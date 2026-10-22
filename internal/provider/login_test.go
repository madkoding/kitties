package provider

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func loginClient(t *testing.T, kind string, acc Account, accErr error, reply func(int) (int, string)) (*HTTPClient, *rt) {
	t.Helper()
	c, err := New(Options{Kind: kind, Auth: AuthLogin, Backoff: time.Millisecond, APIKey: "ignored",
		Login: func(context.Context) (Account, error) { return acc, accErr }})
	if err != nil {
		t.Fatal(err)
	}
	r := &rt{t: t, reply: reply}
	c.WithTransport(r)
	c.sleep = func(time.Duration) {}
	return c, r
}

func TestLoginOptions(t *testing.T) {
	if _, err := New(Options{Kind: "openai", Auth: AuthLogin, Login: func(context.Context) (Account, error) { return Account{}, nil }}); err == nil {
		t.Error("openai has no account to sign in with")
	}
	if _, err := New(Options{Kind: "copilot", Auth: AuthLogin}); !errors.Is(err, ErrNotSignedIn) {
		t.Errorf("a login without a source = %v", err)
	}
}

func TestLoginHeaders(t *testing.T) {
	ok := func(int) (int, string) { return 200, `{"choices":[{"message":{"content":"hi"}}]}` }
	c, r := loginClient(t, "copilot", Account{BaseURL: "https://copilot.example/", Headers: map[string]string{"Authorization": "Bearer cop", "Editor-Version": "v"}}, nil, ok)
	if _, err := c.Complete(context.Background(), convo); err != nil {
		t.Fatal(err)
	}
	if r.paths[0] != "https://copilot.example/chat/completions" || r.heads[0].Get("Authorization") != "Bearer cop" || r.heads[0].Get("Editor-Version") != "v" {
		t.Errorf("sent %s %v", r.paths[0], r.heads[0])
	}
	g, gr := loginClient(t, "gemini", Account{Headers: map[string]string{"Authorization": "Bearer ya29", "x-goog-user-project": "p"}}, nil,
		func(int) (int, string) { return 200, `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}` })
	if _, err := g.Complete(context.Background(), convo); err != nil {
		t.Fatal(err)
	}
	if gr.heads[0].Get("x-goog-api-key") != "" || gr.heads[0].Get("x-goog-user-project") != "p" || !strings.HasPrefix(gr.paths[0], "https://generativelanguage.googleapis.com/") {
		t.Errorf("gemini sent %s %v", gr.paths[0], gr.heads[0])
	}
	bad, br := loginClient(t, "copilot", Account{}, ErrNotSignedIn, ok)
	if _, err := bad.Complete(context.Background(), convo); err == nil || !strings.Contains(err.Error(), "401") || len(br.calls) != 0 {
		t.Errorf("an account that cannot be used = %v (%d calls)", err, len(br.calls))
	}
}

func TestChatGPTStream(t *testing.T) {
	stream := strings.Join([]string{
		`event: response.created`, `data: {"type":"response.created"}`, ``,
		`data: {"type":"response.output_text.delta","delta":"Hel"}`,
		`data: {"type":"response.output_text.delta","delta":"lo"}`,
		`data: not json`,
		`data: {"type":"response.output_item.done","item":{"type":"function_call","call_id":"c1","name":"click","arguments":"{\"x\":1}"}}`,
		`data: {"type":"response.output_item.done","item":{"type":"function_call","name":"done","arguments":""}}`,
		`data: {"type":"response.output_item.done","item":{"type":"message"}}`,
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":3,"output_tokens":4}}}`,
	}, "\n")
	c, r := loginClient(t, "codex", Account{BaseURL: "https://chatgpt.example/codex", ChatGPT: true, Headers: map[string]string{"chatgpt-account-id": "a"}}, nil,
		func(int) (int, string) { return 200, stream })
	reply, err := c.CompleteTools(context.Background(), convo, []Tool{{Name: "click"}})
	if err != nil {
		t.Fatal(err)
	}
	if reply.Content != "Hello" || len(reply.Calls) != 2 || reply.Calls[0].Args["x"] != float64(1) || reply.Calls[1].ID != "call_1" || reply.Usage.Output != 4 {
		t.Errorf("reply = %+v", reply)
	}
	body := r.calls[0]
	if body["stream"] != true || body["store"] != false || body["max_output_tokens"] != nil || body["instructions"] != "be brief" || r.paths[0] != "https://chatgpt.example/codex/responses" {
		t.Errorf("body = %v at %s", body, r.paths[0])
	}
	input := body["input"].([]any)
	if first := input[0].(map[string]any); first["type"] != "message" || first["role"] != "user" {
		t.Errorf("input = %v", input[0])
	}
	if a := input[1].(map[string]any); a["role"] != "assistant" || a["content"].([]any)[0].(map[string]any)["type"] != "output_text" {
		t.Errorf("assistant = %v", a)
	}

	for in, want := range map[string]string{
		`data: {"type":"response.failed","response":{"error":{"message":"quota"}}}`: "quota",
		`data: {"type":"response.failed"}`:                                          "the response failed",
		`data: {"type":"error","message":"bad model"}`:                              "bad model",
		`data: {"type":"response.completed"}`:                                       "no text",
		`data: {"type":"response.output_text.delta","delta":""}`:                    "ended before",
	} {
		reply, err := readResponsesStream([]byte(in))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %+v %v", in, reply, err)
		}
	}
}

func TestRegistryLogin(t *testing.T) {
	reg := NewRegistry()
	asked := ""
	reg.SetLogin(func(kind string) LoginSource {
		asked = kind
		return func(context.Context) (Account, error) { return Account{}, nil }
	})
	if err := reg.Build("cop", Options{Kind: "copilot", Auth: AuthLogin}); err != nil || asked != "copilot" {
		t.Errorf("build = %v, asked %q", err, asked)
	}
	if err := reg.Build("cc", Options{Kind: "claude-code"}); err != nil {
		t.Fatal(err)
	}
	c, _ := reg.Get("cc")
	if _, ok := c.(*ClaudeClient); !ok || c.Describe() != "claude-code/sonnet" {
		t.Errorf("claude-code = %T %s", c, c.Describe())
	}
	if _, ok := c.(ToolClient); ok {
		t.Error("claude-code takes its tools as text")
	}
}

// fakeClaude answers as the claude program would, recording what it was given.
type fakeClaude struct {
	args  []string
	env   []string
	stdin string
	sys   string
	out   string
	err   error
}

func (f *fakeClaude) run(_ context.Context, _ string, args, env []string, _ string, stdin []byte) ([]byte, []byte, error) {
	f.args, f.env, f.stdin, f.sys = args, env, string(stdin), ""
	for i, a := range args {
		if a == "--system-prompt-file" {
			data, _ := os.ReadFile(args[i+1])
			f.sys = string(data)
		}
	}
	return []byte(f.out), []byte("boom"), f.err
}

func claudeWith(t *testing.T, f *fakeClaude) *ClaudeClient {
	t.Helper()
	c, err := NewClaude(Options{Kind: "claude-code"})
	if err != nil {
		t.Fatal(err)
	}
	c.exec = f.run
	c.find = func() (string, error) { return "/opt/claude/bin/claude", nil }
	return c
}

func TestClaude(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-should-not-pass")
	f := &fakeClaude{out: `{"type":"system","subtype":"init"}` + "\n" +
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Hi"}]}}` + "\n" +
		`{"type":"result","subtype":"success","is_error":false,"result":" Hi there ","usage":{"input_tokens":2,"cache_read_input_tokens":3,"output_tokens":5}}`}
	c := claudeWith(t, f)
	reply, err := c.Complete(context.Background(), convo)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Content != "Hi there" || reply.Usage.Input != 5 || reply.Usage.Output != 5 || c.Model() != "sonnet" {
		t.Errorf("reply = %+v", reply)
	}
	joined := strings.Join(f.args, " ")
	if !strings.Contains(joined, "--model sonnet") || !strings.Contains(joined, "--tools  --setting-sources") || f.sys != "be brief" {
		t.Errorf("args = %q, system %q", joined, f.sys)
	}
	if !strings.Contains(f.stdin, "### Assistant (you)") || !strings.Contains(f.stdin, `"type":"image"`) || !strings.Contains(f.stdin, "1 picture(s)") {
		t.Errorf("stdin = %s", f.stdin)
	}
	env := strings.Join(f.env, "\n")
	if strings.Contains(env, "ANTHROPIC_API_KEY") || !strings.Contains(env, "PATH=/opt/claude/bin") || !strings.Contains(env, "CLAUDE_CODE_MAX_OUTPUT_TOKENS=4096") {
		t.Errorf("env = %s", env)
	}

	// A lone question goes as it is, after a space so a leading / is no command.
	if _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "/help"}}); err != nil || !strings.Contains(f.stdin, `"text":" /help"`) || f.sys != "" {
		t.Errorf("lone = %v %s", err, f.stdin)
	}
	tool := []Message{{Role: "user", Content: "go"}, {Role: "assistant", ToolCalls: []ToolCall{{Name: "click", Args: map[string]any{"x": 1}}}}, {Role: "tool", Content: "clicked"}}
	if _, err := c.Complete(context.Background(), tool); err != nil || !strings.Contains(f.stdin, `@tool click {\"x\":1}`) || !strings.Contains(f.stdin, "### Tool result") {
		t.Errorf("tools = %v %s", err, f.stdin)
	}
}

func TestClaudeErrors(t *testing.T) {
	f := &fakeClaude{out: `{"type":"result","subtype":"success","is_error":true,"result":"Invalid API key · Please run /login"}`}
	c := claudeWith(t, f)
	if _, err := c.Complete(context.Background(), convo); err == nil || !strings.Contains(err.Error(), "claude auth login") {
		t.Errorf("not signed in = %v", err)
	}
	f.out = `{"type":"result","is_error":true,"result":"overloaded"}`
	if _, err := c.Complete(context.Background(), convo); err == nil || !strings.Contains(err.Error(), "502") {
		t.Errorf("failed = %v", err)
	}
	f.out, f.err = "", errors.New("exit status 1")
	if _, err := c.Complete(context.Background(), convo); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("crashed = %v", err)
	}
	f.err = nil
	if _, err := c.Complete(context.Background(), convo); err == nil || !strings.Contains(err.Error(), "without an answer") {
		t.Errorf("silent = %v", err)
	}
	f.out = `{"type":"result","result":"  "}`
	if _, err := c.Complete(context.Background(), convo); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty = %v", err)
	}
	if _, err := c.Complete(context.Background(), []Message{{Role: "system", Content: "x"}}); err == nil {
		t.Error("no conversation fails")
	}
	c.find = func() (string, error) { return "", ErrNoClaude }
	if _, err := c.Complete(context.Background(), convo); !errors.Is(err, ErrNoClaude) {
		t.Errorf("missing = %v", err)
	}
}

func TestFindClaude(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho '{\"type\":\"result\",\"result\":\"ok\"}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KITTIES_CLAUDE_BIN", bin)
	if p, err := FindClaude(); err != nil || p != bin {
		t.Errorf("env = %q %v", p, err)
	}
	// The real program runs, with stdin and in a folder of its own.
	c, _ := NewClaude(Options{Kind: "claude-code", Model: "haiku"})
	if reply, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}); err != nil || reply.Content != "ok" {
		t.Errorf("run = %+v %v", reply, err)
	}
	t.Setenv("KITTIES_CLAUDE_BIN", "")
	t.Setenv("PATH", dir)
	if p, err := FindClaude(); err != nil || p != bin {
		t.Errorf("path = %q %v", p, err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	if _, err := FindClaude(); !errors.Is(err, ErrNoClaude) {
		t.Errorf("none = %v", err)
	}
	local := filepath.Join(home, ".local", "bin")
	_ = os.MkdirAll(local, 0o755)
	_ = os.WriteFile(filepath.Join(local, "claude"), []byte("#!/bin/sh\n"), 0o755)
	if p, err := FindClaude(); err != nil || p != filepath.Join(local, "claude") {
		t.Errorf("installer folder = %q %v", p, err)
	}
}
