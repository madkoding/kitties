package connector

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	ctx := context.Background()
	checkOf := func(kind string, s Settings) Checker {
		t.Helper()
		c, err := Build(kind, s, "")
		if err != nil {
			t.Fatal(err)
		}
		ch, ok := c.(Checker)
		if !ok {
			t.Fatalf("%s has no check", kind)
		}
		return ch
	}
	// The account's name comes from the answer; the request only reads.
	cap := &capture{answer: []string{`{"data":{"username":"madkoding"}}`}}
	cap.install(t)
	if who, err := checkOf("x", Settings{"token": "t"}).Check(ctx); err != nil || who != "madkoding" {
		t.Errorf("x = %q %v", who, err)
	}
	if r := cap.reqs[0]; r.Method != "GET" || r.URL.Path != "/2/users/me" || r.Header.Get("Authorization") != "Bearer t" {
		t.Errorf("x request = %s %s %s", r.Method, r.URL, r.Header.Get("Authorization"))
	}
	// X's OAuth 1.0 keys sign the check too.
	cap = &capture{answer: []string{`{"data":{"username":"k"}}`}}
	cap.install(t)
	if _, err := checkOf("x", Settings{"consumer_key": "ck", "consumer_secret": "cs", "access_token": "1-a", "access_secret": "as"}).Check(ctx); err != nil || !strings.HasPrefix(cap.reqs[0].Header.Get("Authorization"), "OAuth ") {
		t.Errorf("x keys = %v %s", err, cap.reqs[0].Header.Get("Authorization"))
	}
	// A refusal is the error, with what the service said.
	cap = &capture{answer: []string{`{"message":"Bad credentials"}`}, code: 401}
	cap.install(t)
	if _, err := checkOf("github", Settings{"token": "bad"}).Check(ctx); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("github refused = %v", err)
	}
	// Slack and Linear say no inside a 200.
	cap = &capture{answer: []string{`{"ok":false,"error":"invalid_auth"}`, `{"errors":[{"message":"Authentication required"}]}`}}
	cap.install(t)
	if _, err := checkOf("slack", Settings{"token": "xoxb", "channel": "#c"}).Check(ctx); err == nil || !strings.Contains(err.Error(), "invalid_auth") {
		t.Errorf("slack = %v", err)
	}
	if _, err := checkOf("linear", Settings{"token": "k", "team": "T"}).Check(ctx); err == nil || !strings.Contains(err.Error(), "Authentication required") {
		t.Errorf("linear = %v", err)
	}
	// A Slack webhook only posts: nothing to test without acting.
	if _, err := checkOf("slack", Settings{"url": "https://hooks/x"}).Check(ctx); !errors.Is(err, ErrNoCheck) {
		t.Errorf("slack webhook = %v", err)
	}
	// Kinds without a reading request say so.
	if _, err := checkOf("google-chat", Settings{"url": "https://chat/x"}).Check(ctx); !errors.Is(err, ErrNoCheck) {
		t.Errorf("google chat = %v", err)
	}
	// Bluesky opens a session.
	cap = &capture{answer: []string{`{"accessJwt":"j","did":"d"}`}}
	cap.install(t)
	if who, err := checkOf("bluesky", Settings{"handle": "me.bsky.social", "password": "p"}).Check(ctx); err != nil || who != "me.bsky.social" {
		t.Errorf("bluesky = %q %v", who, err)
	}
	cap = &capture{code: 401}
	cap.install(t)
	if _, err := checkOf("bluesky", Settings{"handle": "me", "password": "p"}).Check(ctx); err == nil {
		t.Error("bluesky refused")
	}
	// Every kind with a check builds its request, and an answer without the name still passes.
	all := Settings{"token": "t", "url": "https://h", "subdomain": "acme", "email": "a@x", "project": "P", "team": "T", "channel": "#c",
		"base": "b", "table": "t", "chat_id": "1", "author": "urn:li:person:1", "page_id": "1", "account_id": "1", "repo": "a/b", "instance": "https://h"}
	for kind := range checks {
		cap = &capture{answer: []string{`{}`}}
		cap.install(t)
		s := Settings{}
		for k, v := range all {
			s[k] = v
		}
		if kind == "slack" {
			delete(s, "url") // its webhook has nothing to read
		}
		c, err := Build(kind, s, "")
		if err != nil {
			t.Errorf("%s: %v", kind, err)
			continue
		}
		if who, err := c.(Checker).Check(ctx); err != nil || who != "" || len(cap.reqs) != 1 {
			t.Errorf("%s = %q %v (%d requests)", kind, who, err, len(cap.reqs))
		}
	}
	if lookup(map[string]any{"a": map[string]any{"b": 2.0}}, "a.b") != "2" || lookup("x", "a") != "" || jsonText(map[string]any{"code": 1}) != `{"code":1}` {
		t.Error("lookup and jsonText")
	}
}
