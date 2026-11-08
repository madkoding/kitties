package provider

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// shots is a conversation of n rounds, each with one screenshot of size bytes.
func shots(n, size int) []Message {
	msgs := []Message{{Role: "system", Content: "s"}}
	for i := 0; i < n; i++ {
		msgs = append(msgs, Message{Role: "assistant", Content: "click"},
			Message{Role: "user", Content: "screen", Images: []Image{{MIME: "image/png", Data: bytes.Repeat([]byte{byte(i)}, size)}}})
	}
	return msgs
}

func countImages(msgs []Message) (n int) {
	for _, m := range msgs {
		n += len(m.Images)
	}
	return n
}

func TestLightenKeepsTheLastPicturesInSteps(t *testing.T) {
	for _, c := range []struct{ rounds, want int }{{3, 3}, {6, 6}, {7, 7}, {11, 11}, {12, 6}, {13, 7}, {40, 10}} {
		msgs := shots(c.rounds, 10)
		got := lighten(msgs, 6, BodyBudget)
		if n := countImages(got); n != c.want {
			t.Errorf("%d rounds: %d pictures sent, want %d", c.rounds, n, c.want)
		}
		if countImages(msgs) != c.rounds {
			t.Fatalf("lighten changed its input")
		}
		last := got[len(got)-1]
		if len(last.Images) != 1 || last.Images[0].Data[0] != byte(c.rounds-1) {
			t.Errorf("%d rounds: the newest screenshot must go", c.rounds)
		}
		if c.want < c.rounds && !strings.Contains(got[2].Content, "left out to keep the request light") {
			t.Errorf("%d rounds: the dropped picture is not said: %q", c.rounds, got[2].Content)
		}
	}
}

func TestLightenKeepsTheCachedPrefixBetweenSteps(t *testing.T) {
	// From 12 to 17 rounds the cut does not move: what was sent stays as it was.
	a, b := lighten(shots(12, 10), 6, BodyBudget), lighten(shots(17, 10), 6, BodyBudget)
	for i := range a {
		if a[i].Content != b[i].Content || len(a[i].Images) != len(b[i].Images) {
			t.Fatalf("message %d changed between rounds: %q vs %q", i, a[i].Content, b[i].Content)
		}
	}
}

func TestLightenBudget(t *testing.T) {
	got := lighten(shots(5, 1000), 6, 2500)
	if n := countImages(got); n != 2 {
		t.Errorf("%d pictures within a budget of two, want 2", n)
	}
	// The newest picture goes even when it alone is over the budget.
	got = lighten(shots(2, 1000), 6, 10)
	if n := countImages(got); n != 1 || len(got[len(got)-1].Images) != 1 {
		t.Errorf("the newest picture must go: %d", n)
	}
	// An old document over the budget goes as a line; the last message's always goes.
	msgs := []Message{{Role: "user", Content: "read", Files: []File{{Name: "a.pdf", Data: make([]byte, 100)}}},
		{Role: "user", Content: "and", Files: []File{{Name: "b.pdf", Data: make([]byte, 100)}}}}
	got = lighten(msgs, 6, 150)
	if len(got[0].Files) != 0 || !strings.Contains(got[0].Content, "1 earlier document(s)") || len(got[1].Files) != 1 {
		t.Errorf("documents: %+v", got)
	}
	// Nothing to cut: the same slice comes back.
	plain := []Message{{Role: "user", Content: "hi"}}
	if got := lighten(plain, 6, 10); &got[0] != &plain[0] {
		t.Error("a conversation within the limits must be sent as it is")
	}
}

func TestBodyUnread(t *testing.T) {
	for _, c := range []struct {
		err  error
		want bool
	}{
		{&HTTPError{Code: 400, Body: `{"error":{"message":"failed to read request body (ref: d96b)","type":"invalid_request_error"}}`}, true},
		{&HTTPError{Code: 413, Body: "Request Entity Too Large"}, true},
		{&HTTPError{Code: 400, Body: `{"error":{"message":"invalid tool schema"}}`}, false},
		{&HTTPError{Code: 500, Body: "failed to read request body"}, false},
		{nil, false},
	} {
		if got := bodyUnread(c.err); got != c.want {
			t.Errorf("bodyUnread(%v) = %v, want %v", c.err, got, c.want)
		}
		if c.want && !Transient(c.err) {
			t.Errorf("Transient(%v) must be true: it passes when sent again lighter", c.err)
		}
	}
}

// A request the provider could not read is sent again with fewer pictures, and goes through.
func TestRetryLighterWhenTheBodyIsUnread(t *testing.T) {
	c, r := client(t, "openai", func(n int) (int, string) {
		if n == 1 {
			return 400, `{"error":{"message":"failed to read request body (ref: x)","type":"invalid_request_error","param":null,"code":null}}`
		}
		return 200, `{"choices":[{"message":{"content":"ok"}}]}`
	})
	reply, err := c.Complete(t.Context(), shots(20, 10))
	if err != nil || reply.Content != "ok" {
		t.Fatalf("reply %+v, err %v", reply, err)
	}
	if len(r.calls) != 2 {
		t.Fatalf("%d calls, want 2", len(r.calls))
	}
	pics := func(call map[string]any) int {
		raw, _ := json.Marshal(call)
		return strings.Count(string(raw), "data:image/png;base64,")
	}
	if first, second := pics(r.calls[0]), pics(r.calls[1]); first != 8 || second != lightKeep {
		t.Errorf("pictures sent: %d then %d, want 8 then %d", first, second, lightKeep)
	}
}

// Any other 400 is the request's own fault: it is not sent again.
func TestOtherBadRequestIsNotRetried(t *testing.T) {
	c, r := client(t, "openai", func(int) (int, string) { return 400, `{"error":{"message":"bad schema"}}` })
	if _, err := c.Complete(t.Context(), convo); err == nil || len(r.calls) != 1 {
		t.Errorf("err %v after %d calls, want one failed call", err, len(r.calls))
	}
}
