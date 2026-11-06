package gateway

import (
	"strings"
	"testing"

	"github.com/madkoding/kitties/internal/memory"
)

func TestMCPMemoryTools(t *testing.T) {
	s, _, _ := mcpServer(false)
	// Without a memory every tool says so.
	for _, c := range []struct {
		name string
		args map[string]any
	}{
		{"memory_list", map[string]any{"ref": "ana"}},
		{"memory_remember", map[string]any{"ref": "ana", "text": "x"}},
		{"memory_forget", map[string]any{"ref": "ana", "id": "n0"}},
	} {
		if text, isErr, _ := tool(t, s, c.name, c.args); !isErr || !strings.Contains(text, "no memory") {
			t.Errorf("%s without memory: %v %q", c.name, isErr, text)
		}
	}

	mem := &fakeMemory{profile: memory.Profile{Demand: 3, Auto: true}}
	s.WithMemory(mem)
	if text, isErr, _ := tool(t, s, "memory_remember", map[string]any{"ref": "ana", "text": "Answers in Spanish"}); isErr || !strings.Contains(text, "Answers in Spanish") {
		t.Fatalf("memory_remember: %v %q", isErr, text)
	}
	if _, isErr, _ := tool(t, s, "memory_remember", map[string]any{"ref": "ana", "text": "Uses tabs", "scope": "work"}); isErr {
		t.Fatal("memory_remember work")
	}
	if text, isErr, _ := tool(t, s, "memory_remember", map[string]any{"ref": "ana", "text": "x", "scope": "bogus"}); !isErr || !strings.Contains(text, "scope") {
		t.Errorf("bad scope: %v %q", isErr, text)
	}
	if _, isErr, _ := tool(t, s, "memory_remember", map[string]any{"ref": "ana"}); !isErr {
		t.Error("memory_remember without text")
	}
	if _, isErr, _ := tool(t, s, "memory_remember", "nope"); !isErr {
		t.Error("memory_remember with bad args")
	}

	text, isErr, _ := tool(t, s, "memory_list", map[string]any{"ref": "ana"})
	if isErr || !strings.Contains(text, "Answers in Spanish") || !strings.Contains(text, "Uses tabs") || !strings.Contains(text, "demand") {
		t.Errorf("memory_list: %v %q", isErr, text)
	}
	text, _, _ = tool(t, s, "memory_list", map[string]any{"ref": "ana", "scope": "work"})
	if strings.Contains(text, "Answers in Spanish") || !strings.Contains(text, "Uses tabs") {
		t.Errorf("memory_list work: %q", text)
	}
	if _, isErr, _ := tool(t, s, "memory_list", map[string]any{"ref": "zz"}); !isErr {
		t.Error("memory_list of a missing kitty")
	}
	if _, isErr, _ := tool(t, s, "memory_list", "nope"); !isErr {
		t.Error("memory_list with bad args")
	}

	if _, isErr, _ := tool(t, s, "memory_forget", map[string]any{"ref": "ana"}); !isErr {
		t.Error("memory_forget without id")
	}
	if _, isErr, _ := tool(t, s, "memory_forget", "nope"); !isErr {
		t.Error("memory_forget with bad args")
	}
	if _, isErr, _ := tool(t, s, "memory_forget", map[string]any{"ref": "ana", "id": "nx"}); !isErr {
		t.Error("memory_forget of a missing note")
	}
	if text, isErr, _ := tool(t, s, "memory_forget", map[string]any{"ref": "ana", "id": "n0"}); isErr || !strings.Contains(text, "n0") {
		t.Errorf("memory_forget: %v %q", isErr, text)
	}
	if len(mem.notes) != 1 || mem.notes[0].Text != "Uses tabs" {
		t.Errorf("notes left: %+v", mem.notes)
	}
}
