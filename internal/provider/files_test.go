package provider

import (
	"context"
	"testing"
)

var pdfConvo = []Message{{Role: "system", Content: "s"},
	{Role: "user", Content: "read it", Files: []File{{Name: "a.pdf", MIME: "application/pdf", Data: []byte("%PDF-")}}}}

const okChat = `{"choices":[{"message":{"content":"ok"}}]}`

func lastParts(t *testing.T, call map[string]any, listKey, partsKey string) []any {
	t.Helper()
	list := call[listKey].([]any)
	return list[len(list)-1].(map[string]any)[partsKey].([]any)
}

func TestPDFTravelsInEachFamilysShape(t *testing.T) {
	cases := []struct {
		kind, body, list, parts, field string
		check                          func(map[string]any) bool
	}{
		{"openai", okChat, "messages", "content", "file", func(p map[string]any) bool {
			f := p["file"].(map[string]any)
			return f["filename"] == "a.pdf" && f["file_data"] == "data:application/pdf;base64,JVBERi0="
		}},
		{"anthropic", `{"content":[{"type":"text","text":"ok"}]}`, "messages", "content", "document", func(p map[string]any) bool {
			s := p["source"].(map[string]any)
			return p["title"] == "a.pdf" && s["media_type"] == "application/pdf" && s["data"] == "JVBERi0="
		}},
		{"gemini", `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`, "contents", "parts", "", func(p map[string]any) bool {
			d, ok := p["inlineData"].(map[string]any)
			return ok && d["mimeType"] == "application/pdf" && d["data"] == "JVBERi0="
		}},
		{"codex", `{"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`, "input", "content", "input_file", func(p map[string]any) bool {
			return p["filename"] == "a.pdf" && p["file_data"] == "data:application/pdf;base64,JVBERi0="
		}},
	}
	for _, c := range cases {
		cl, r := client(t, c.kind, func(int) (int, string) { return 200, c.body })
		if _, err := cl.Complete(context.Background(), pdfConvo); err != nil {
			t.Fatalf("%s: %v", c.kind, err)
		}
		parts := lastParts(t, r.calls[0], c.list, c.parts)
		part := parts[len(parts)-1].(map[string]any)
		if c.field != "" && part["type"] != c.field {
			t.Errorf("%s: the last part is %v", c.kind, part)
		}
		if !c.check(part) {
			t.Errorf("%s: the document part is %v", c.kind, part)
		}
	}
}

func TestWhatEachProviderReads(t *testing.T) {
	cases := map[string][2]bool{ // sees images, reads PDF
		"openai": {true, true}, "anthropic": {true, true}, "gemini": {true, true}, "codex": {true, true},
		"openrouter": {true, false}, "copilot": {true, false},
		"deepseek": {false, false}, "ollama": {false, false}, "groq": {false, false}, "custom": {false, false},
	}
	for kind, want := range cases {
		c, err := New(Options{Kind: kind, APIKey: "k", BaseURL: "http://x", Model: "m"})
		if err != nil {
			t.Fatal(err)
		}
		if SeesImages(c) != want[0] || ReadsPDF(c) != want[1] {
			t.Errorf("%s: sees %v reads %v, want %v", kind, SeesImages(c), ReadsPDF(c), want)
		}
	}
	// A ChatGPT plan's backend takes no documents.
	c, err := New(Options{Kind: "codex", Auth: AuthLogin, Model: "m", Login: func(context.Context) (Account, error) { return Account{}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if ReadsPDF(c) || !SeesImages(c) {
		t.Error("a signed-in ChatGPT account sees pictures but reads no documents")
	}
	// A client that says nothing is taken to see pictures and read no documents.
	f := Func(func(context.Context, []Message) (Reply, error) { return Reply{}, nil })
	if !SeesImages(f) || ReadsPDF(f) {
		t.Error("default capabilities")
	}
}
