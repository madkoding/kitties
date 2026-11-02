package chat

import (
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
