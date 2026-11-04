package chat

import (
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/provider"
)

// Cutting the history must not leave a tool result without the call that precedes it.
func TestTrimHistoryKeepsToolExchangesWhole(t *testing.T) {
	msgs := []provider.Message{
		{Role: "system", Content: "task"},
		{Role: "assistant", ToolCalls: []provider.ToolCall{{ID: "a", Name: "click"}, {ID: "b", Name: "type"}}},
		{Role: "tool", ToolCallID: "a"},
		{Role: "tool", ToolCallID: "b"},
		{Role: "user", Content: "screen"},
	}
	// keep=3 would start at the first tool result.
	got := trimHistory(msgs, 3)
	if len(got) != 2 || got[0].Role != "system" || got[1].Role != "user" {
		t.Fatalf("trimmed = %+v", got)
	}
	if got := trimHistory(msgs, 10); len(got) != len(msgs) {
		t.Errorf("short history was cut: %+v", got)
	}
	if &msgs[0] == &trimHistory(msgs, 3)[0] {
		t.Error("trim must not write over the caller's backing array")
	}
}

// A thread past the window loses its oldest messages, never the system prompt or the last one,
// and never leaves a tool result without its call.
func TestFitContextDropsTheOldestFirst(t *testing.T) {
	big := strings.Repeat("x", 3000)
	msgs := []provider.Message{
		{Role: "system", Content: "task"},
		{Role: "user", Content: big},
		{Role: "assistant", ToolCalls: []provider.ToolCall{{ID: "a", Name: "click"}}},
		{Role: "tool", ToolCallID: "a", Content: big},
		{Role: "user", Content: big},
	}
	if got := fitContext(msgs, 100000); len(got) != len(msgs) {
		t.Errorf("a thread that fits was cut: %d", len(got))
	}
	got := fitContext(msgs, 1000) // 2400 characters of room
	if len(got) != 2 || got[0].Content != "task" || got[1].Content != big {
		t.Errorf("fitted = %+v", got)
	}
	one := []provider.Message{{Role: "system", Content: "s"}, {Role: "user", Content: big}}
	if got := fitContext(one, 10); len(got) != 2 {
		t.Errorf("the last message must stay: %+v", got)
	}
}

// Pictures and documents weigh in the fit, not only text.
func TestFitContextCountsPicturesAndFiles(t *testing.T) {
	msgs := []provider.Message{
		{Role: "system", Content: "task"},
		{Role: "user", Content: "old", Images: []provider.Image{{Data: []byte("x")}, {Data: []byte("x")}}},
		{Role: "user", Content: "last"},
	}
	if got := fitContext(msgs, 3000); len(got) != 2 { // two pictures are ~3,200 tokens, more than the 2,400 that fit
		t.Errorf("pictures did not count: %d messages", len(got))
	}
	msgs[1] = provider.Message{Role: "user", Content: "old", Files: []provider.File{{Data: make([]byte, 400000)}}}
	if got := fitContext(msgs, 3000); len(got) != 2 {
		t.Errorf("a document did not count: %d messages", len(got))
	}
}
