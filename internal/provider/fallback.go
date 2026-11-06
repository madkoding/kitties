package provider

import (
	"context"
	"fmt"
)

// A BRAIN WITH A SPARE.
//
// A profile can name a fallback: while the main brain is down (busy, failing, unreachable after
// its own retries) the conversation goes to the fallback instead, so an outage of one provider
// does not stop the kitties that think with it. Anything else (a bad key, a refused request) is
// the main brain's answer: a spare would only hide it.

// Fallback is a client that turns to a spare while its main one is down.
type Fallback struct {
	Main, Spare Client
}

// WithFallback returns main backed by spare.
func WithFallback(main, spare Client) *Fallback { return &Fallback{Main: main, Spare: spare} }

// Complete asks the main brain, and the spare while the main one is down.
func (f *Fallback) Complete(ctx context.Context, messages []Message) (Reply, error) {
	r, err := f.Main.Complete(ctx, messages)
	if !Transient(err) || ctx.Err() != nil {
		return r, err
	}
	r, ferr := f.Spare.Complete(ctx, messages)
	if ferr != nil {
		return r, fmt.Errorf("%w; the fallback %s failed too: %v", err, f.Spare.Describe(), ferr)
	}
	return r, nil
}

// CompleteTools is Complete with tools. The spare is asked with them only when it calls tools
// natively too; otherwise the main brain's error stands.
func (f *Fallback) CompleteTools(ctx context.Context, messages []Message, tools []Tool) (Reply, error) {
	main, ok := f.Main.(ToolClient)
	if !ok {
		return f.Complete(ctx, messages)
	}
	r, err := main.CompleteTools(ctx, messages, tools)
	if !Transient(err) || ctx.Err() != nil {
		return r, err
	}
	spare, ok := f.Spare.(ToolClient)
	if !ok || !spare.SupportsTools() {
		return r, err
	}
	r, ferr := spare.CompleteTools(ctx, messages, tools)
	if ferr != nil {
		return r, fmt.Errorf("%w; the fallback %s failed too: %v", err, f.Spare.Describe(), ferr)
	}
	return r, nil
}

// SupportsTools is the main brain's.
func (f *Fallback) SupportsTools() bool {
	tc, ok := f.Main.(ToolClient)
	return ok && tc.SupportsTools()
}

// Describe names the main brain and its spare.
func (f *Fallback) Describe() string {
	return f.Main.Describe() + " (fallback " + f.Spare.Describe() + ")"
}

// ContextWindow is the smaller of the two, so what is sent fits whichever answers.
func (f *Fallback) ContextWindow() int { return min(ContextWindow(f.Main), ContextWindow(f.Spare)) }

// SeesImages is true when both take pictures.
func (f *Fallback) SeesImages() bool { return SeesImages(f.Main) && SeesImages(f.Spare) }

// ReadsPDF is true when both take documents.
func (f *Fallback) ReadsPDF() bool { return ReadsPDF(f.Main) && ReadsPDF(f.Spare) }
