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
	// The fixture answers c1 twice: the second answer has no call left and becomes plain text.
	if len(msgs) != 5 {
		t.Fatalf("messages = %d: %v", len(msgs), msgs)
	}
	blocks := func(i int) []any { return msgs[i].(map[string]any)["content"].([]any) }
	if use := blocks(1); use[0].(map[string]any)["type"] != "tool_use" || use[0].(map[string]any)["id"] != "c1" {
		t.Errorf("tool_use = %v", use)
	}
	if results := blocks(2); len(results) != 1 || results[0].(map[string]any)["type"] != "tool_result" {
		t.Errorf("tool results share a turn: %v", results)
	}
	if orphan := blocks(3); orphan[0].(map[string]any)["type"] != "text" {
		t.Errorf("orphan result = %v", orphan)
	}
	if last := blocks(4); len(last) != 2 || last[1].(map[string]any)["type"] != "image" {
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

func TestAnthropicToolID(t *testing.T) {
	for in, want := range map[string]string{"toolu_01-A": "toolu_01-A", "click#0": "click_0", "a b.c/ñ": "a_b_c___", "": "call"} {
		if got := anthropicToolID(in); got != want {
			t.Errorf("anthropicToolID(%q) = %q, want %q", in, got, want)
		}
	}
}

// A history saved while talking to Gemini ("name#0" ids) must still be accepted by Anthropic.
func TestAnthropicSanitizesHistoryIDs(t *testing.T) {
	c, r := client(t, "anthropic", func(int) (int, string) { return 200, `{"content":[{"type":"text","text":"ok"}]}` })
	convo := []Message{
		{Role: "user", Content: "go"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "click#0", Name: "click"}, {ID: "type#1", Name: "type"}}},
		{Role: "tool", ToolCallID: "click#0", Content: "ok"},
		{Role: "tool", ToolCallID: "type#1", Content: "ok"},
	}
	if _, err := c.CompleteTools(context.Background(), convo, tools); err != nil {
		t.Fatal(err)
	}
	msgs := r.calls[0]["messages"].([]any)
	block := func(i, j int) map[string]any {
		return msgs[i].(map[string]any)["content"].([]any)[j].(map[string]any)
	}
	if block(1, 1)["id"] != "type_1" || block(2, 0)["tool_use_id"] != "click_0" || block(2, 1)["tool_use_id"] != "type_1" {
		t.Errorf("ids not sanitized consistently: %v", msgs)
	}
}

// Every tool_result must answer a tool_use of the turn right before it, whatever the history
// looks like (empty ids, truncated start, results with no call, calls with no result).
func TestAnthropicPairsToolTurns(t *testing.T) {
	cases := map[string][]Message{
		"empty ids": {
			{Role: "user", Content: "go"},
			{Role: "assistant", ToolCalls: []ToolCall{{Name: "click"}, {Name: "type"}}},
			{Role: "tool", ToolName: "type", Content: "a"},
			{Role: "tool", ToolName: "click", Content: "b"},
		},
		"repeated ids": {
			{Role: "user", Content: "go"},
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "x", Name: "click"}}},
			{Role: "tool", ToolCallID: "x", Content: "a"},
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "x", Name: "click"}}},
			{Role: "tool", ToolCallID: "x", Content: "b"},
		},
		"orphan result after truncation": {
			{Role: "tool", ToolCallID: "gone", Content: "stale"},
			{Role: "assistant", Content: "hi"},
			{Role: "tool", Content: "stale too"},
			{Role: "user", Content: "go"},
		},
		"result with an unknown id": {
			{Role: "user", Content: "go"},
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "a", Name: "click"}}},
			{Role: "tool", ToolCallID: "other", Content: "x"},
		},
		"call without result": {
			{Role: "user", Content: "go"},
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "a", Name: "click"}, {ID: "b", Name: "type"}}},
			{Role: "tool", ToolCallID: "a", Content: "x"},
			{Role: "user", Content: "again"},
		},
	}
	for name, convo := range cases {
		c, r := client(t, "anthropic", func(int) (int, string) { return 200, `{"content":[{"type":"text","text":"ok"}]}` })
		if _, err := c.CompleteTools(context.Background(), convo, tools); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		msgs := r.calls[0]["messages"].([]any)
		var open map[string]bool
		for i, m := range msgs {
			mm := m.(map[string]any)
			next := map[string]bool{}
			for _, b := range mm["content"].([]any) {
				b := b.(map[string]any)
				switch b["type"] {
				case "tool_result":
					id, _ := b["tool_use_id"].(string)
					if !open[id] {
						t.Errorf("%s: message %d: tool_result %q has no tool_use right before it: %v", name, i, id, msgs)
					}
					delete(open, id)
				case "tool_use":
					id, _ := b["id"].(string)
					if next[id] {
						t.Errorf("%s: duplicate tool_use id %q", name, id)
					}
					next[id] = true
				}
			}
			if len(open) > 0 {
				t.Errorf("%s: message %d: tool_use left unanswered: %v", name, i, open)
			}
			open = next
		}
		if len(open) > 0 {
			t.Errorf("%s: last assistant turn has unanswered tool_use: %v", name, open)
		}
	}
}
