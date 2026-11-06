package provider

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// toolFake is a client that calls tools natively (or not), answering with err or its name.
type toolFake struct {
	name  string
	err   error
	tools bool
	asked int
	files bool
}

func (f *toolFake) Complete(ctx context.Context, m []Message) (Reply, error) {
	return f.CompleteTools(ctx, m, nil)
}
func (f *toolFake) CompleteTools(context.Context, []Message, []Tool) (Reply, error) {
	f.asked++
	if f.err != nil {
		return Reply{}, f.err
	}
	return Reply{Content: f.name}, nil
}
func (f *toolFake) SupportsTools() bool { return f.tools }
func (f *toolFake) Describe() string    { return f.name }
func (f *toolFake) ContextWindow() int  { return map[string]int{"main": 200000, "spare": 32000}[f.name] }
func (f *toolFake) SeesImages() bool    { return f.name == "main" }
func (f *toolFake) ReadsPDF() bool      { return f.files }

func TestFallbackTakesOverWhileTheMainBrainIsDown(t *testing.T) {
	ctx := context.Background()
	down := &HTTPError{Code: 529}
	main, spare := &toolFake{name: "main", err: down, tools: true}, &toolFake{name: "spare", tools: true}
	f := WithFallback(main, spare)
	if r, err := f.CompleteTools(ctx, nil, nil); err != nil || r.Content != "spare" {
		t.Errorf("tools: %v %v", r, err)
	}
	if r, err := f.Complete(ctx, nil); err != nil || r.Content != "spare" {
		t.Errorf("text: %v %v", r, err)
	}
	// Both down: both errors are told.
	spare.err = errors.New("also down")
	if _, err := f.CompleteTools(ctx, nil, nil); err == nil || !errors.Is(err, down) || !strings.Contains(err.Error(), "fallback spare failed too") {
		t.Errorf("both down (tools): %v", err)
	}
	if _, err := f.Complete(ctx, nil); err == nil || !strings.Contains(err.Error(), "fallback spare failed too") {
		t.Errorf("both down (text): %v", err)
	}
	// Not an outage: the spare is not asked.
	spare.err, spare.asked = nil, 0
	main.err = &HTTPError{Code: 401}
	if _, err := f.CompleteTools(ctx, nil, nil); err == nil || spare.asked != 0 {
		t.Error("a refused key is the main brain's answer")
	}
	if _, err := f.Complete(ctx, nil); err == nil || spare.asked != 0 {
		t.Error("a refused key is the main brain's answer (text)")
	}
	// A spare without native tools is not asked with tools.
	main.err, spare.tools = down, false
	if _, err := f.CompleteTools(ctx, nil, nil); !errors.Is(err, down) || spare.asked != 0 {
		t.Errorf("spare without tools: %v", err)
	}
	// What the pair can do is what both can.
	if !f.SupportsTools() || f.ContextWindow() != 32000 || f.SeesImages() || f.ReadsPDF() || f.Describe() != "main (fallback spare)" {
		t.Error("abilities")
	}
	// A main brain without native tools: CompleteTools is Complete.
	plain := WithFallback(Func(func(context.Context, []Message) (Reply, error) { return Reply{}, down }), spare)
	if r, err := plain.CompleteTools(ctx, nil, nil); err != nil || r.Content != "spare" || plain.SupportsTools() {
		t.Errorf("plain main: %v %v", r, err)
	}
	// A cancelled context ends it at once.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	spare.asked = 0
	if _, err := f.CompleteTools(cctx, nil, nil); err == nil || spare.asked != 0 {
		t.Error("cancelled")
	}
}
