package connector

import (
	"context"
	"strings"
	"testing"
	"time"
)

var fb = Settings{"page_id": "P1", "token": "EAAtok"}

func TestFacebook(t *testing.T) {
	cases := []struct {
		name   string
		args   map[string]any
		answer string
		method string
		url    string
		body   string
		want   string
	}{
		{"text post", map[string]any{"text": "hello", "link": "https://kitties.app"}, `{"id":"P1_9"}`,
			"POST", "https://graph.facebook.com/v23.0/P1/feed", `"link":"https://kitties.app","message":"hello"`, "id: P1_9"},
		{"picture post", map[string]any{"action": "post", "text": "look", "image_url": "https://x/cat.jpg"}, `{"id":"ph1"}`,
			"POST", "/v23.0/P1/photos", `"caption":"look","url":"https://x/cat.jpg"`, "id: ph1"},
		{"posts", map[string]any{"action": "posts", "limit": 3.0}, `{"data":[{"id":"P1_9","created_time":"2026-10-22","message":"hello","permalink_url":"https://fb/p"}]}`,
			"GET", "/P1/posts?fields=id,message,created_time,permalink_url&limit=3", "", "- P1_9 | 2026-10-22 | hello | https://fb/p"},
		{"comments", map[string]any{"action": "comments", "id": "P1_9"}, `{"data":[{"id":"c1","from":{"name":"Ana"},"message":"price?","created_time":"t"}]}`,
			"GET", "/P1_9/comments?fields=id,from,message,created_time", "", "- c1 | Ana | price? | t"},
		{"reply", map[string]any{"action": "reply", "id": "c1", "text": "10 USD"}, `{"id":"c2"}`,
			"POST", "/c1/comments", `{"message":"10 USD"}`, "id: c2"},
		{"like", map[string]any{"action": "like", "id": "c1"}, `{"success":true}`, "POST", "/c1/likes", "", "done"},
		{"inbox", map[string]any{"action": "inbox"}, `{"data":[{"updated_time":"u","messages":{"data":[{"message":"hi","from":{"id":"PS1","name":"Leo"}}]}}]}`,
			"GET", "/P1/conversations?platform=messenger&fields=updated_time%2Cmessages.limit%281%29%7Bmessage%2Cfrom%7D&limit=10", "", "- PS1 | Leo | hi | u"},
		{"send", map[string]any{"action": "send", "to": "PS1", "text": "on our way"}, `{"message_id":"m1"}`,
			"POST", "/P1/messages", `"messaging_type":"RESPONSE","recipient":{"id":"PS1"}`, "message_id"},
	}
	for _, c := range cases {
		out, err, cap := run(t, "facebook", fb, c.args, c.answer)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		r := cap.reqs[0]
		if r.Method != c.method || !strings.Contains(r.URL.String(), c.url) || r.Header.Get("Authorization") != "Bearer EAAtok" {
			t.Errorf("%s: %s %s %v", c.name, r.Method, r.URL, r.Header)
		}
		if !strings.Contains(cap.bodies[0], c.body) || !strings.Contains(out, c.want) {
			t.Errorf("%s: body %s, out %q", c.name, cap.bodies[0], out)
		}
	}
	for _, args := range []map[string]any{{}, {"action": "comments"}, {"action": "reply", "id": "c1"}, {"action": "reply", "text": "x"},
		{"action": "like"}, {"action": "send", "text": "x"}, {"action": "send", "to": "PS1"}} {
		if _, err, _ := run(t, "facebook", fb, args); err == nil {
			t.Errorf("%v passes", args)
		}
	}
}

func TestInstagramPublish(t *testing.T) {
	ig := Settings{"account_id": "IG1", "token": "EAAtok"}
	out, err, cap := run(t, "instagram", ig, map[string]any{"image_url": "https://x/cat.jpg", "text": "a cat"},
		`{"id":"C1"}`, `{"status_code":"FINISHED"}`, `{"id":"M1"}`, `{"permalink":"https://instagram.com/p/abc"}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "published: https://instagram.com/p/abc (id M1)" || len(cap.reqs) != 4 {
		t.Fatalf("out = %q after %d requests", out, len(cap.reqs))
	}
	if !strings.HasSuffix(cap.reqs[0].URL.Path, "/v23.0/IG1/media") || !strings.Contains(cap.bodies[0], `"image_url":"https://x/cat.jpg"`) || !strings.Contains(cap.bodies[0], `"caption":"a cat"`) {
		t.Errorf("container = %s %s", cap.reqs[0].URL, cap.bodies[0])
	}
	if !strings.HasSuffix(cap.reqs[2].URL.Path, "/IG1/media_publish") || cap.bodies[2] != `{"creation_id":"C1"}` || cap.reqs[2].Header.Get("Authorization") != "Bearer EAAtok" {
		t.Errorf("publish = %s %s", cap.reqs[2].URL, cap.bodies[2])
	}

	// A reel is processed first: the container is checked until it is ready.
	old := igPoll
	igPoll = time.Millisecond
	t.Cleanup(func() { igPoll = old })
	out, err, cap = run(t, "instagram", Settings{"account_id": "IG1", "token": "IGAAtok"}, map[string]any{"video_url": "https://x/r.mp4"},
		`{"id":"C2"}`, `{"status_code":"IN_PROGRESS"}`, `{"status_code":"FINISHED"}`, `{"id":"M2"}`, `{"error":{}}`)
	if err != nil || !strings.Contains(out, "id M2") || len(cap.reqs) != 5 || !strings.Contains(cap.bodies[0], `"media_type":"REELS"`) ||
		!strings.HasPrefix(cap.reqs[0].URL.String(), "https://graph.instagram.com/v23.0/") {
		t.Errorf("reel = %q %v, %d requests, %s %s", out, err, len(cap.reqs), cap.reqs[0].URL, cap.bodies[0])
	}
	// What cannot be published says why.
	if _, err, _ := run(t, "instagram", ig, map[string]any{"image_url": "https://x/a.png"}, `{"id":"C3"}`, `{"status_code":"ERROR","status":"bad format"}`); err == nil || !strings.Contains(err.Error(), "bad format") {
		t.Errorf("error status = %v", err)
	}
	if _, err, _ := run(t, "instagram", ig, map[string]any{"image_url": "https://x/a.jpg"}, `{}`); err == nil || !strings.Contains(err.Error(), "did not take") {
		t.Errorf("no container = %v", err)
	}
	if out, err, _ := run(t, "instagram", ig, map[string]any{"image_url": "https://x/a.jpg"}, `{"id":"C4"}`, `{}`, `{"ok":true}`); err != nil || out != "done" {
		t.Errorf("publish without id = %q %v", out, err)
	}
	if _, err, _ := run(t, "instagram", ig, map[string]any{"text": "no picture"}); err == nil || !strings.Contains(err.Error(), "image_url") {
		t.Errorf("no media = %v", err)
	}
	// A reel still processing when the call ends says so.
	(&capture{answer: []string{`{"status_code":"IN_PROGRESS"}`}}).install(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := igPublish(ctx, ig, []byte(`{"id":"C5"}`), nil); err == nil || !strings.Contains(err.Error(), "still processing") {
		t.Errorf("a cancelled publish = %v", err)
	}
}

func TestInstagramReads(t *testing.T) {
	ig := Settings{"account_id": "IG1", "token": "EAAtok"}
	out, _, cap := run(t, "instagram", ig, map[string]any{"action": "posts", "limit": 2.0},
		`{"data":[{"id":"M1","timestamp":"t","media_type":"IMAGE","caption":"a cat","like_count":4,"comments_count":1,"permalink":"https://ig/p"}]}`)
	if !strings.Contains(cap.reqs[0].URL.String(), "/IG1/media?fields=") || !strings.Contains(out, "- M1 | t | IMAGE | a cat | 4 | 1 | https://ig/p") {
		t.Errorf("posts = %s %q", cap.reqs[0].URL, out)
	}
	out, _, cap = run(t, "instagram", ig, map[string]any{"action": "comments", "id": "M1"}, `{"data":[{"id":"k1","username":"ana","text":"love it","timestamp":"t"}]}`)
	if !strings.Contains(cap.reqs[0].URL.String(), "/M1/comments") || !strings.Contains(out, "- k1 | ana | love it | t") {
		t.Errorf("comments = %q", out)
	}
	_, err, cap := run(t, "instagram", ig, map[string]any{"action": "reply", "id": "k1", "text": "thanks!"}, `{"id":"k2"}`)
	if err != nil || !strings.HasSuffix(cap.reqs[0].URL.Path, "/k1/replies") || cap.bodies[0] != `{"message":"thanks!"}` {
		t.Errorf("reply = %v %s %s", err, cap.reqs[0].URL, cap.bodies[0])
	}
	for _, args := range []map[string]any{{"action": "comments"}, {"action": "reply", "id": "k1"}, {"action": "reply", "text": "x"}} {
		if _, err, _ := run(t, "instagram", ig, args); err == nil {
			t.Errorf("%v passes", args)
		}
	}
	if metaList("instagram", "post", []byte(`{"id":"9"}`)) != "done, id: 9" {
		t.Error("other answers are summarised")
	}
}

func TestMetaRisks(t *testing.T) {
	for kind, actions := range map[string][]string{"facebook": {"post", "reply", "like", "send"}, "instagram": {"post", "reply"}} {
		for _, a := range actions {
			if RiskOf(kind, a) != RiskAsk {
				t.Errorf("%s.%s should ask", kind, a)
			}
		}
		if RiskOf(kind, "posts") != RiskNone || RiskOf(kind, "comments") != RiskNone {
			t.Errorf("%s reading asks", kind)
		}
		k, ok := LookupKind(kind)
		if !ok || k.Category != CatSocial || len(k.Fields) != 2 || k.Inbound {
			t.Errorf("%s = %+v", kind, k)
		}
	}
}
