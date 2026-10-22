package provider

import (
	"context"
	"strings"
	"testing"
)

var tools = []Tool{{Name: "click", Description: "click a point", Parameters: map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "integer"}}}}}

// toolConvo is a conversation with a native call and its answer, as the chat replays it.
var toolConvo = []Message{
	{Role: "system", Content: "sys"},
	{Role: "user", Content: "go"},
	{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "c1", Name: "click", Args: map[string]any{"x": 1}}}},
	{Role: "tool", ToolCallID: "c1", ToolName: "click", Content: "ok"},
	{Role: "tool", ToolCallID: "c1", ToolName: "click", Content: ""},
	{Role: "user", Content: "now?", Images: []Image{{MIME: "image/png", Data: []byte("P")}}},
}

func TestChatTools(t *testing.T) {
	c, r := client(t, "openai", func(int) (int, string) {
		return 200, `{"choices":[{"message":{"content":"","tool_calls":[{"id":"x1","function":{"name":"click","arguments":"{\"x\": 5}"}},{"function":{"name":"type","arguments":"plain"}},{"function":{"name":"done","arguments":""}}]}}]}`
	})
	reply, err := c.CompleteTools(context.Background(), toolConvo, tools)
	if err != nil || len(reply.Calls) != 3 || reply.Calls[0].ID != "x1" || reply.Calls[0].Args["x"] != 5.0 || reply.Calls[1].ID != "call_1" || reply.Calls[1].Args["text"] != "plain" || len(reply.Calls[2].Args) != 0 {
		t.Fatalf("reply = %+v %v", reply, err)
	}
	call := r.calls[0]
	defs := call["tools"].([]any)
	if len(defs) != 1 || defs[0].(map[string]any)["function"].(map[string]any)["name"] != "click" {
		t.Errorf("tools = %v", defs)
	}
	msgs := call["messages"].([]any)
	if a := msgs[2].(map[string]any); a["role"] != "assistant" || !strings.Contains(a["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["arguments"].(string), `"x":1`) {
		t.Errorf("assistant call = %v", a)
	}
	if tr := msgs[3].(map[string]any); tr["role"] != "tool" || tr["tool_call_id"] != "c1" || tr["content"] != "ok" {
		t.Errorf("tool result = %v", tr)
	}
	if !c.SupportsTools() {
		t.Error("native by default")
	}
	c.opts.Tools = "text"
	if c.SupportsTools() {
		t.Error("text mode")
	}
}

func TestResponsesTools(t *testing.T) {
	c, r := client(t, "codex", func(int) (int, string) {
		return 200, `{"output":[{"type":"function_call","call_id":"f1","name":"click","arguments":"{\"x\":2}"},{"type":"function_call","name":"done","arguments":"{}"}]}`
	})
	reply, err := c.CompleteTools(context.Background(), toolConvo, tools)
	if err != nil || len(reply.Calls) != 2 || reply.Calls[0].ID != "f1" || reply.Calls[1].ID != "call_1" {
		t.Fatalf("reply = %+v %v", reply, err)
	}
	call := r.calls[0]
	if defs := call["tools"].([]any); defs[0].(map[string]any)["name"] != "click" {
		t.Errorf("tools = %v", defs)
	}
	input := call["input"].([]any)
	if fc := input[1].(map[string]any); fc["type"] != "function_call" || fc["call_id"] != "c1" {
		t.Errorf("function_call = %v", fc)
	}
	if out := input[2].(map[string]any); out["type"] != "function_call_output" || out["output"] != "ok" {
		t.Errorf("output = %v", out)
	}
}

func TestAnthropicTools(t *testing.T) {
	c, r := client(t, "anthropic", func(int) (int, string) {
		return 200, `{"content":[{"type":"text","text":"Clicking."},{"type":"tool_use","id":"tu1","name":"click","input":{"x":3}},{"type":"tool_use","name":"done"}]}`
	})
	reply, err := c.CompleteTools(context.Background(), toolConvo, tools)
	if err != nil || reply.Content != "Clicking." || len(reply.Calls) != 2 || reply.Calls[0].ID != "tu1" || reply.Calls[1].ID != "toolu_2" || reply.Calls[1].Args == nil {
		t.Fatalf("reply = %+v %v", reply, err)
	}
	call := r.calls[0]
	if defs := call["tools"].([]any); defs[0].(map[string]any)["input_schema"] == nil {
		t.Errorf("tools = %v", defs)
	}
	msgs := call["messages"].([]any)
	if len(msgs) != 4 {
		t.Fatalf("messages = %d: %v", len(msgs), msgs)
	}
	blocks := func(i int) []any { return msgs[i].(map[string]any)["content"].([]any) }
	if use := blocks(1); use[0].(map[string]any)["type"] != "tool_use" || use[0].(map[string]any)["id"] != "c1" {
		t.Errorf("tool_use = %v", use)
	}
	if results := blocks(2); len(results) != 2 || results[0].(map[string]any)["type"] != "tool_result" || results[1].(map[string]any)["content"] != "done" {
		t.Errorf("tool results share a turn: %v", results)
	}
	if last := blocks(3); len(last) != 2 || last[1].(map[string]any)["type"] != "image" {
		t.Errorf("user with image = %v", last)
	}
}

func TestGeminiTools(t *testing.T) {
	c, r := client(t, "gemini", func(int) (int, string) {
		return 200, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"click","args":{"x":4}}},{"functionCall":{"name":"done"}},{"text":"ok"}]}}]}`
	})
	reply, err := c.CompleteTools(context.Background(), toolConvo, tools)
	if err != nil || len(reply.Calls) != 2 || reply.Calls[0].ID != "click#0" || reply.Calls[1].Args == nil || reply.Content != "ok" {
		t.Fatalf("reply = %+v %v", reply, err)
	}
	call := r.calls[0]
	if defs := call["tools"].([]any); defs[0].(map[string]any)["functionDeclarations"] == nil {
		t.Errorf("tools = %v", defs)
	}
	contents := call["contents"].([]any)
	parts := func(i int) []any { return contents[i].(map[string]any)["parts"].([]any) }
	if fc := parts(1); fc[0].(map[string]any)["functionCall"] == nil {
		t.Errorf("functionCall = %v", fc)
	}
	if fr := parts(2); fr[0].(map[string]any)["functionResponse"].(map[string]any)["name"] != "click" {
		t.Errorf("functionResponse = %v", fr)
	}
	if argsJSON(nil) != "{}" || len(argsOf(42.0)) != 0 || argsOf("  ")["text"] != nil {
		t.Error("args helpers")
	}
}
