package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

var tools1 = []Tool{{Name: "a", Parameters: map[string]any{}}, {Name: "b", Parameters: map[string]any{}}}

func cc(m any) map[string]any {
	if mm, ok := m.(map[string]any); ok {
		c, _ := mm["cache_control"].(map[string]any)
		return c
	}
	return nil
}

func TestAnthropicCacheBreakpoints(t *testing.T) {
	c, r := client(t, "anthropic", func(int) (int, string) {
		return 200, `{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":10,"output_tokens":2,"cache_read_input_tokens":300,"cache_creation_input_tokens":50}}`
	})
	reply, err := c.CompleteTools(context.Background(), convo, tools1)
	if err != nil {
		t.Fatal(err)
	}
	want := Usage{Input: 360, Output: 2, CacheRead: 300, CacheWrite: 50}
	if reply.Usage != want {
		t.Errorf("usage = %+v, want %+v", reply.Usage, want)
	}
	call := r.calls[0]
	tools := call["tools"].([]any)
	if cc(tools[0]) != nil || cc(tools[1])["type"] != "ephemeral" {
		t.Errorf("only the last tool is marked: %v", tools)
	}
	if cc(call["system"].([]any)[0])["type"] != "ephemeral" {
		t.Errorf("system not marked: %v", call["system"])
	}
	msgs := call["messages"].([]any)
	for i, m := range msgs {
		blocks := m.(map[string]any)["content"].([]any)
		marked := cc(blocks[len(blocks)-1]) != nil
		if marked != (i == len(msgs)-1) {
			t.Errorf("message %d marked = %v", i, marked)
		}
	}
	if _, ttl := cc(tools[1])["ttl"]; ttl {
		t.Error("default ttl must be left to the API (5m)")
	}
}

func TestAnthropicCacheTTLAndOff(t *testing.T) {
	off := false
	for name, opts := range map[string]Options{
		"1h":  {Kind: "anthropic", APIKey: "k", CacheTTL: "1h"},
		"off": {Kind: "anthropic", APIKey: "k", PromptCache: &off},
	} {
		c, err := New(opts)
		if err != nil {
			t.Fatal(err)
		}
		r := &rt{reply: func(int) (int, string) { return 200, `{"content":[{"type":"text","text":"ok"}]}` }}
		c.WithTransport(r)
		if _, err := c.Complete(context.Background(), convo); err != nil {
			t.Fatal(err)
		}
		call := r.calls[0]
		if name == "1h" {
			if cc(call["system"].([]any)[0])["ttl"] != "1h" {
				t.Errorf("ttl missing: %v", call["system"])
			}
			continue
		}
		if call["system"] != "be brief" || strings.Contains(string(mustJSON(call)), "cache_control") {
			t.Errorf("caching off must send a plain request: %v", call)
		}
	}
}

func TestChatCacheUsageAndKeys(t *testing.T) {
	body := `{"choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":100,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":64}}}`
	c, r := client(t, "openai", func(int) (int, string) { return 200, body })
	reply, err := c.Complete(context.Background(), convo)
	if err != nil || reply.Usage != (Usage{Input: 100, Output: 5, CacheRead: 64}) {
		t.Fatalf("reply = %+v, %v", reply, err)
	}
	if k, _ := r.calls[0]["prompt_cache_key"].(string); !strings.HasPrefix(k, "kitties-") {
		t.Errorf("openai wants a cache key: %v", r.calls[0])
	}
	// Servers that cache by themselves get no extra field.
	c, r = client(t, "groq", func(int) (int, string) { return 200, body })
	if _, err := c.Complete(context.Background(), convo); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.calls[0]["prompt_cache_key"]; ok {
		t.Error("groq must not get prompt_cache_key")
	}
	// DeepSeek reports its own hit counter.
	c, _ = client(t, "deepseek", func(int) (int, string) {
		return 200, `{"choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":10,"completion_tokens":1,"prompt_cache_hit_tokens":8}}`
	})
	if reply, _ := c.Complete(context.Background(), convo); reply.Usage.CacheRead != 8 {
		t.Errorf("deepseek usage = %+v", reply.Usage)
	}
}

func TestOpenRouterMarksAnthropicModels(t *testing.T) {
	ok := `{"choices":[{"message":{"content":"hi"}}]}`
	c, r := client(t, "openrouter", func(int) (int, string) { return 200, ok })
	c.opts.Model = "anthropic/claude-sonnet-5-5"
	if _, err := c.Complete(context.Background(), convo); err != nil {
		t.Fatal(err)
	}
	msgs := r.calls[0]["messages"].([]any)
	sys := msgs[0].(map[string]any)["content"].([]any)
	if cc(sys[len(sys)-1]) == nil {
		t.Errorf("system not marked: %v", msgs[0])
	}
	last := msgs[len(msgs)-1].(map[string]any)["content"].([]any)
	if cc(last[len(last)-1]) == nil {
		t.Errorf("last turn not marked: %v", last)
	}
	c, r = client(t, "openrouter", func(int) (int, string) { return 200, ok })
	c.opts.Model = "openai/gpt-5.5"
	if _, err := c.Complete(context.Background(), convo); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mustJSON(r.calls[0])), "cache_control") {
		t.Error("an OpenAI model on OpenRouter caches by itself")
	}
}

func TestResponsesAndGeminiCacheUsage(t *testing.T) {
	c, r := client(t, "codex", func(int) (int, string) {
		return 200, `{"output":[{"type":"message","content":[{"type":"output_text","text":"a"}]}],"usage":{"input_tokens":50,"output_tokens":2,"input_tokens_details":{"cached_tokens":40}}}`
	})
	reply, err := c.Complete(context.Background(), convo)
	if err != nil || reply.Usage != (Usage{Input: 50, Output: 2, CacheRead: 40}) {
		t.Fatalf("responses = %+v, %v", reply, err)
	}
	if _, ok := r.calls[0]["prompt_cache_key"]; !ok {
		t.Error("responses wants a cache key")
	}
	c, _ = client(t, "gemini", func(int) (int, string) {
		return 200, `{"candidates":[{"content":{"parts":[{"text":"y"}]}}],"usageMetadata":{"promptTokenCount":90,"candidatesTokenCount":3,"cachedContentTokenCount":70}}`
	})
	reply, err = c.Complete(context.Background(), convo)
	if err != nil || reply.Usage != (Usage{Input: 90, Output: 3, CacheRead: 70}) {
		t.Fatalf("gemini = %+v, %v", reply, err)
	}
}

func TestUsageAdd(t *testing.T) {
	got := Usage{1, 2, 3, 4}.Add(Usage{10, 20, 30, 40})
	if got != (Usage{11, 22, 33, 44}) {
		t.Errorf("add = %+v", got)
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
