package connector

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/smtp"
	"strings"
	"testing"
)

func TestCatalogShape(t *testing.T) {
	kinds := Kinds()
	if len(kinds) < 25 || kinds[0].Category != CatMessaging || kinds[len(kinds)-1].Category != CatGeneric {
		t.Errorf("Kinds = %d, first %s, last %s", len(kinds), kinds[0].Category, kinds[len(kinds)-1].Category)
	}
	seen := map[string]bool{}
	for _, k := range kinds {
		if seen[k.Name] || k.Label == "" || k.Help == "" || k.Category == "" || len(k.Actions) == 0 {
			t.Errorf("kind %+v", k)
		}
		seen[k.Name] = true
		if indexOf(Categories, k.Category) == len(Categories) {
			t.Errorf("%s has an unknown category %s", k.Name, k.Category)
		}
	}
	for _, c := range []string{"messaging", "social media", "git", "productivity", "office", "drawing", "generic"} {
		found := false
		for _, k := range kinds {
			found = found || k.Category == c
		}
		if !found {
			t.Errorf("no kind in %s", c)
		}
	}
	if _, ok := LookupKind("nope"); ok {
		t.Error("LookupKind")
	}
	if _, err := Build("nope", nil, ""); err == nil {
		t.Error("Build unknown")
	}
	if _, err := Build("telegram", Settings{}, ""); err == nil || !strings.Contains(err.Error(), "needs bot token") {
		t.Errorf("Build validates: %v", err)
	}
	if _, err := Build("slack", Settings{"token": "t"}, ""); err == nil {
		t.Error("slack needs a channel with a token")
	}
	if c, err := Build("slack", Settings{"channel": "#x"}, ""); err != nil || c.(Kinder).Kind() != "slack" {
		t.Errorf("slack with a channel signs in later: %v", err)
	}
	if c, err := Build("github", Settings{}, ""); err != nil || c.(Kinder).Kind() != "github" {
		t.Errorf("github without a token signs in later: %v", err)
	}
	if c, _ := Build("shell", Settings{"command": "ls"}, "d"); c.(Kinder).Kind() != "shell" || c.Describe() == "" {
		t.Error("shell is described and knows its kind")
	}
	c, err := Build("github", Settings{"token": "t"}, "my repos")
	if err != nil || !strings.HasPrefix(c.Describe(), "my repos (actions") || !strings.Contains(c.Describe(), "create_issue(") {
		t.Errorf("described = %q %v", c.Describe(), err)
	}
	c, _ = Build("discord", Settings{"url": "u"}, "")
	if !strings.HasPrefix(c.Describe(), "posts messages to a Discord") {
		t.Error(c.Describe())
	}
	for kind, s := range map[string]Settings{"shell": {"command": "echo"}, "motita": {"url": "http://m"}, "webhook": {"url": "u", "headers": "A: 1\nbad\n: x"}} {
		if _, err := Build(kind, s, "d"); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
	w, _ := Build("webhook", Settings{"url": "u", "headers": "A: 1"}, "")
	if w.(described).Connector.(Webhook).Headers["A"] != "1" {
		t.Error("webhook headers")
	}
	if str(map[string]any{"n": 2.0}, "n") != "2" || str(map[string]any{"b": true}, "b") != "true" || str(map[string]any{"x": ""}, "x", "y") != "" {
		t.Error("str")
	}
	if summary([]byte(`{"html_url":"u","number":3}`)) != "done, html_url: u, number: 3" || summary([]byte("plain")) != "plain" || summary(nil) != "done" {
		t.Error("summary")
	}
}

// capture records the requests the catalogue connectors make, answering them all with ok.
type capture struct {
	reqs   []*http.Request
	bodies []string
	answer []string
	code   int
}

func (c *capture) install(t *testing.T) {
	t.Helper()
	old := httpDo
	httpDo = func(req *http.Request) (*http.Response, error) {
		body := ""
		if req.Body != nil {
			data, _ := io.ReadAll(req.Body)
			body = string(data)
		}
		c.reqs = append(c.reqs, req)
		c.bodies = append(c.bodies, body)
		code := c.code
		if code == 0 {
			code = 200
		}
		answer := `{"id":"1","html_url":"https://x/1"}`
		if n := len(c.reqs) - 1; n < len(c.answer) {
			answer = c.answer[n]
		}
		return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(answer))}, nil
	}
	t.Cleanup(func() { httpDo = old })
}

type call struct {
	kind     string
	settings Settings
	args     map[string]any
	method   string
	url      string // a substring of the URL
	body     string // a substring of the body
	header   [2]string
}

func TestCatalogActions(t *testing.T) {
	cases := []call{
		{"slack", Settings{"url": "https://hooks/x"}, map[string]any{"text": "hi"}, "POST", "hooks/x", `"text":"hi"`, [2]string{}},
		{"slack", Settings{"token": "xoxb", "channel": "#g"}, map[string]any{"text": "hi"}, "POST", "chat.postMessage", `"channel":"#g"`, [2]string{"Authorization", "Bearer xoxb"}},
		{"discord", Settings{"url": "https://d/w"}, map[string]any{"action": "send", "message": "yo"}, "POST", "d/w", `"content":"yo"`, [2]string{}},
		{"telegram", Settings{"token": "T", "chat_id": "7"}, map[string]any{"text": "hi"}, "POST", "/botT/sendMessage", `"chat_id":"7"`, [2]string{}},
		{"whatsapp", Settings{"token": "t", "phone_id": "p1", "to": "+1"}, map[string]any{"text": "hi"}, "POST", "/v20.0/p1/messages", `"to":"+1"`, [2]string{"Authorization", "Bearer t"}},
		{"google-chat", Settings{"url": "https://chat/x"}, map[string]any{"text": "hi"}, "POST", "chat/x", `"text":"hi"`, [2]string{}},
		{"teams", Settings{"url": "https://teams/x"}, map[string]any{"text": "hi"}, "POST", "teams/x", `"TextBlock"`, [2]string{}},
		{"matrix", Settings{"url": "https://m", "token": "t", "room": "!r:m"}, map[string]any{"text": "hi"}, "PUT", "/rooms/%21r:m/send/m.room.message/", `"msgtype":"m.text"`, [2]string{"Authorization", "Bearer t"}},
		{"mastodon", Settings{"url": "https://mas/", "token": "t"}, map[string]any{"text": "toot"}, "POST", "mas/api/v1/statuses", `"status":"toot"`, [2]string{}},
		{"x", Settings{"token": "t"}, map[string]any{"text": "post"}, "POST", "api.x.com/2/tweets", `"text":"post"`, [2]string{}},
		{"linkedin", Settings{"token": "t", "author": "urn:li:person:1"}, map[string]any{"text": "p"}, "POST", "/v2/ugcPosts", `"author":"urn:li:person:1"`, [2]string{"X-Restli-Protocol-Version", "2.0.0"}},
		{"github", Settings{"token": "t", "repo": "a/b"}, map[string]any{"title": "T", "body": "B"}, "POST", "/repos/a/b/issues", `"title":"T"`, [2]string{"Accept", "application/vnd.github+json"}},
		{"github", Settings{"token": "t"}, map[string]any{"action": "comment", "repo": "a/c", "number": 4.0, "text": "c"}, "POST", "/repos/a/c/issues/4/comments", `"body":"c"`, [2]string{}},
		{"github", Settings{"token": "t", "repo": "a/b"}, map[string]any{"action": "create_pr", "title": "T", "head": "h"}, "POST", "/repos/a/b/pulls", `"base":"main"`, [2]string{}},
		{"github", Settings{"token": "t", "repo": "a/b"}, map[string]any{"action": "get_file", "path": "/README.md", "ref": "dev"}, "GET", "/contents/README.md?ref=dev", "", [2]string{"Accept", "application/vnd.github.raw+json"}},
		{"gitlab", Settings{"token": "t", "project": "g/p"}, map[string]any{"title": "T"}, "POST", "/projects/g%2Fp/issues", `"title":"T"`, [2]string{"PRIVATE-TOKEN", "t"}},
		{"gitlab", Settings{"url": "https://gl", "token": "t"}, map[string]any{"action": "comment", "project": "g/p", "iid": "2", "text": "n"}, "POST", "gl/api/v4/projects/g%2Fp/issues/2/notes", `"body":"n"`, [2]string{}},
		{"gitea", Settings{"url": "https://gt", "token": "t"}, map[string]any{"repo": "a/b", "title": "T"}, "POST", "gt/api/v1/repos/a/b/issues", `"title":"T"`, [2]string{"Authorization", "token t"}},
		{"notion", Settings{"token": "t", "parent": "pp"}, map[string]any{"title": "T", "text": "x"}, "POST", "/v1/pages", `"page_id":"pp"`, [2]string{"Notion-Version", "2022-06-28"}},
		{"notion", Settings{"token": "t"}, map[string]any{"action": "append", "page": "pg", "text": "x"}, "PATCH", "/v1/blocks/pg/children", `"paragraph"`, [2]string{}},
		{"trello", Settings{"key": "k", "token": "t", "list": "l1"}, map[string]any{"name": "card"}, "POST", "idList=l1", "", [2]string{}},
		{"todoist", Settings{"token": "t", "project": "p"}, map[string]any{"content": "buy milk", "due": "tomorrow"}, "POST", "/rest/v2/tasks", `"due_string":"tomorrow"`, [2]string{}},
		{"linear", Settings{"token": "t", "team": "tm"}, map[string]any{"title": "T"}, "POST", "api.linear.app/graphql", `"teamId":"tm"`, [2]string{"Authorization", "t"}},
		{"jira", Settings{"url": "https://j", "email": "e", "token": "t", "project": "K"}, map[string]any{"summary": "S", "description": "D"}, "POST", "j/rest/api/3/issue", `"key":"K"`, [2]string{"Authorization", "Basic ZTp0"}},
		{"airtable", Settings{"token": "t", "base": "app1", "table": "T 1"}, map[string]any{"fields": map[string]any{"Name": "x"}}, "POST", "/v0/app1/T%201", `"Name":"x"`, [2]string{}},
		{"nextcloud", Settings{"url": "https://nc", "user": "u", "password": "p", "folder": "K"}, map[string]any{"name": "a.txt", "content": "hello"}, "PUT", "nc/remote.php/dav/files/u/K/a.txt", "hello", [2]string{"Authorization", "Basic dTpw"}},
		{"nextcloud", Settings{"url": "https://nc", "user": "u", "password": "p"}, map[string]any{"action": "download", "name": "a.txt"}, "GET", "/files/u/a.txt", "", [2]string{}},
		{"google-sheets", Settings{"url": "https://script/x"}, map[string]any{"values": []any{"a", 1.0}}, "POST", "script/x", `"values":["a",1]`, [2]string{}},
		{"figma", Settings{"token": "t"}, map[string]any{"file": "F", "text": "nice"}, "POST", "/v1/files/F/comments", `"message":"nice"`, [2]string{"X-Figma-Token", "t"}},
		{"figma", Settings{"token": "t"}, map[string]any{"action": "get_file", "file": "F"}, "GET", "/v1/files/F?depth=2", "", [2]string{}},
		{"http", Settings{"url": "https://api", "token": "t"}, map[string]any{"method": "post", "path": "things", "body": map[string]any{"a": 1}}, "POST", "api/things", `{"a":1}`, [2]string{"Authorization", "Bearer t"}},
		{"http", Settings{"url": "https://api"}, map[string]any{"path": "/x"}, "GET", "api/x", "", [2]string{}},
	}
	for _, tc := range cases {
		cap := &capture{}
		cap.install(t)
		c, err := Build(tc.kind, tc.settings, "")
		if err != nil {
			t.Errorf("%s: build: %v", tc.kind, err)
			continue
		}
		out, err := c.Call(context.Background(), tc.args)
		if err != nil || out == "" || len(cap.reqs) != 1 {
			t.Errorf("%s %v: out=%q err=%v reqs=%d", tc.kind, tc.args, out, err, len(cap.reqs))
			continue
		}
		req := cap.reqs[0]
		if req.Method != tc.method || !strings.Contains(req.URL.String(), tc.url) || !strings.Contains(cap.bodies[0], tc.body) {
			t.Errorf("%s %v: %s %s body=%s", tc.kind, tc.args, req.Method, req.URL, cap.bodies[0])
		}
		if tc.header[0] != "" && req.Header.Get(tc.header[0]) != tc.header[1] {
			t.Errorf("%s: header %s = %q", tc.kind, tc.header[0], req.Header.Get(tc.header[0]))
		}
	}
}

func TestCatalogRefusals(t *testing.T) {
	cases := []call{
		{"slack", Settings{"url": "u"}, map[string]any{}, "", "", "", [2]string{}},
		{"discord", Settings{"url": "u"}, map[string]any{"action": "fly"}, "", "", "", [2]string{}},
		{"whatsapp", Settings{"token": "t", "phone_id": "p"}, map[string]any{"text": "hi"}, "", "", "", [2]string{}},
		{"github", Settings{"token": "t"}, map[string]any{"title": "T"}, "", "", "", [2]string{}},
		{"github", Settings{"token": "t", "repo": "a/b"}, map[string]any{}, "", "", "", [2]string{}},
		{"github", Settings{"token": "t", "repo": "a/b"}, map[string]any{"action": "comment"}, "", "", "", [2]string{}},
		{"github", Settings{"token": "t", "repo": "a/b"}, map[string]any{"action": "comment", "number": "1"}, "", "", "", [2]string{}},
		{"github", Settings{"token": "t", "repo": "a/b"}, map[string]any{"action": "create_pr", "title": "T"}, "", "", "", [2]string{}},
		{"github", Settings{"token": "t", "repo": "a/b"}, map[string]any{"action": "create_pr"}, "", "", "", [2]string{}},
		{"github", Settings{"token": "t", "repo": "a/b"}, map[string]any{"action": "get_file"}, "", "", "", [2]string{}},
		{"github", Settings{"token": "t"}, map[string]any{"action": "get_file", "path": "x"}, "", "", "", [2]string{}},
		{"github", Settings{"token": "t"}, map[string]any{"action": "create_pr", "title": "t"}, "", "", "", [2]string{}},
		{"github", Settings{"token": "t"}, map[string]any{"action": "comment", "number": "1", "text": "x"}, "", "", "", [2]string{}},
		{"gitlab", Settings{"token": "t"}, map[string]any{"title": "T"}, "", "", "", [2]string{}},
		{"gitlab", Settings{"token": "t", "project": "p"}, map[string]any{}, "", "", "", [2]string{}},
		{"gitlab", Settings{"token": "t", "project": "p"}, map[string]any{"action": "comment"}, "", "", "", [2]string{}},
		{"gitlab", Settings{"token": "t", "project": "p"}, map[string]any{"action": "comment", "iid": "1"}, "", "", "", [2]string{}},
		{"gitlab", Settings{"token": "t"}, map[string]any{"action": "comment", "iid": "1", "text": "x"}, "", "", "", [2]string{}},
		{"gitea", Settings{"url": "u", "token": "t"}, map[string]any{"title": "T"}, "", "", "", [2]string{}},
		{"gitea", Settings{"url": "u", "token": "t", "repo": "a/b"}, map[string]any{}, "", "", "", [2]string{}},
		{"notion", Settings{"token": "t"}, map[string]any{"title": "T"}, "", "", "", [2]string{}},
		{"notion", Settings{"token": "t"}, map[string]any{}, "", "", "", [2]string{}},
		{"notion", Settings{"token": "t"}, map[string]any{"action": "append"}, "", "", "", [2]string{}},
		{"notion", Settings{"token": "t"}, map[string]any{"action": "append", "page": "p"}, "", "", "", [2]string{}},
		{"trello", Settings{"key": "k", "token": "t"}, map[string]any{"name": "c"}, "", "", "", [2]string{}},
		{"trello", Settings{"key": "k", "token": "t"}, map[string]any{}, "", "", "", [2]string{}},
		{"todoist", Settings{"token": "t"}, map[string]any{}, "", "", "", [2]string{}},
		{"linear", Settings{"token": "t", "team": "x"}, map[string]any{}, "", "", "", [2]string{}},
		{"jira", Settings{"url": "u", "email": "e", "token": "t", "project": "K"}, map[string]any{}, "", "", "", [2]string{}},
		{"airtable", Settings{"token": "t", "base": "b", "table": "t"}, map[string]any{}, "", "", "", [2]string{}},
		{"nextcloud", Settings{"url": "u", "user": "u", "password": "p"}, map[string]any{}, "", "", "", [2]string{}},
		{"nextcloud", Settings{"url": "u", "user": "u", "password": "p"}, map[string]any{"name": "../x"}, "", "", "", [2]string{}},
		{"nextcloud", Settings{"url": "u", "user": "u", "password": "p"}, map[string]any{"action": "download"}, "", "", "", [2]string{}},
		{"google-sheets", Settings{"url": "u"}, map[string]any{}, "", "", "", [2]string{}},
		{"figma", Settings{"token": "t"}, map[string]any{}, "", "", "", [2]string{}},
		{"figma", Settings{"token": "t"}, map[string]any{"file": "F"}, "", "", "", [2]string{}},
		{"figma", Settings{"token": "t"}, map[string]any{"action": "get_file"}, "", "", "", [2]string{}},
		{"kroki", Settings{}, map[string]any{}, "", "", "", [2]string{}},
		{"telegram", Settings{"token": "t", "chat_id": "1"}, map[string]any{}, "", "", "", [2]string{}},
		{"matrix", Settings{"url": "u", "token": "t", "room": "r"}, map[string]any{}, "", "", "", [2]string{}},
		{"mastodon", Settings{"url": "u", "token": "t"}, map[string]any{}, "", "", "", [2]string{}},
		{"x", Settings{"token": "t"}, map[string]any{}, "", "", "", [2]string{}},
		{"linkedin", Settings{"token": "t", "author": "a"}, map[string]any{}, "", "", "", [2]string{}},
		{"google-chat", Settings{"url": "u"}, map[string]any{}, "", "", "", [2]string{}},
		{"teams", Settings{"url": "u"}, map[string]any{}, "", "", "", [2]string{}},
	}
	for _, tc := range cases {
		cap := &capture{}
		cap.install(t)
		c, err := Build(tc.kind, tc.settings, "")
		if err != nil {
			t.Errorf("%s: build: %v", tc.kind, err)
			continue
		}
		if _, err := c.Call(context.Background(), tc.args); err == nil || len(cap.reqs) != 0 {
			t.Errorf("%s %v must be refused before any request (err=%v, reqs=%d)", tc.kind, tc.args, err, len(cap.reqs))
		}
	}
	cap := &capture{code: 500, answer: []string{"down"}}
	cap.install(t)
	c, _ := Build("x", Settings{"token": "t"}, "")
	if _, err := c.Call(context.Background(), map[string]any{"text": "x"}); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("status errors: %v", err)
	}
	cap = &capture{}
	cap.install(t)
	c, _ = Build("http", Settings{"url": "https://api"}, "")
	if _, err := c.Call(context.Background(), map[string]any{"path": "x", "body": func() {}}); err == nil {
		t.Error("an unencodable body")
	}
	if _, err := do(context.Background(), request{method: "GET", url: "::bad"}); err == nil {
		t.Error("a bad url")
	}
	old := httpDo
	httpDo = func(*http.Request) (*http.Response, error) { return nil, errors.New("net down") }
	if _, err := do(context.Background(), request{method: "GET", url: "https://x"}); err == nil {
		t.Error("a transport error")
	}
	httpDo = old
}

func TestFileConnectors(t *testing.T) {
	cap := &capture{answer: []string{"<svg/>"}}
	cap.install(t)
	c, _ := Build("kroki", Settings{}, "")
	filer := c.(Filer)
	text, name, data, err := filer.CallFile(context.Background(), map[string]any{"type": "mermaid", "source": "graph TD; a-->b", "name": "flow"})
	if err != nil || name != "flow.svg" || string(data) != "<svg/>" || !strings.Contains(text, "flow.svg") {
		t.Errorf("kroki = %q %q %q %v", text, name, data, err)
	}
	if cap.reqs[0].URL.String() != "https://kroki.io/mermaid/svg" || cap.bodies[0] != "graph TD; a-->b" || cap.reqs[0].Header.Get("Content-Type") != "text/plain" {
		t.Errorf("kroki request = %s %s", cap.reqs[0].URL, cap.bodies[0])
	}
	if out, _ := c.Call(context.Background(), map[string]any{"source": "x"}); !strings.HasPrefix(out, "saved as mermaid-") {
		t.Errorf("Call = %s", out)
	}
	r := NewRegistry()
	r.Add("diagrams", c)
	res := r.Call(context.Background(), Call{Name: "diagrams", Args: map[string]any{"source": "x", "name": "d.svg"}}, nil)
	if res.Err != "" || res.File == nil || res.File.Name != "d.svg" {
		t.Errorf("registry keeps the file: %+v", res)
	}
	ex, _ := Build("excalidraw", Settings{}, "")
	text, name, data, err = ex.(Filer).CallFile(context.Background(), map[string]any{"elements": []any{map[string]any{"type": "rectangle"}}, "name": "plan"})
	if err != nil || name != "plan.excalidraw" || !strings.Contains(string(data), `"rectangle"`) || !strings.Contains(text, "excalidraw.com") {
		t.Errorf("excalidraw = %q %q %v", text, name, err)
	}
	if out, err := ex.Call(context.Background(), map[string]any{"elements": []any{}}); err != nil || !strings.HasPrefix(out, "saved as scene-") {
		t.Errorf("Call = %q %v", out, err)
	}
	if _, err := ex.Call(context.Background(), map[string]any{}); err == nil {
		t.Error("elements are needed")
	}
	if ex.Describe() == "" || c.Describe() == "" {
		t.Error("Describe")
	}
}

func TestBlueskyAndFigmaRender(t *testing.T) {
	cap := &capture{answer: []string{`{"accessJwt":"jwt","did":"did:x"}`, `{"uri":"at://x"}`}}
	cap.install(t)
	c, _ := Build("bluesky", Settings{"handle": "h", "password": "p"}, "")
	out, err := c.Call(context.Background(), map[string]any{"text": "hello"})
	if err != nil || len(cap.reqs) != 2 || cap.reqs[1].Header.Get("Authorization") != "Bearer jwt" || !strings.Contains(cap.bodies[1], `"repo":"did:x"`) || out == "" {
		t.Errorf("bluesky = %q %v reqs=%d", out, err, len(cap.reqs))
	}
	if c.Describe() == "" {
		t.Error("Describe")
	}
	if _, err := c.Call(context.Background(), map[string]any{}); err == nil {
		t.Error("text needed")
	}
	cap = &capture{answer: []string{`{}`}}
	cap.install(t)
	if _, err := c.Call(context.Background(), map[string]any{"text": "x"}); err == nil {
		t.Error("no session")
	}
	cap = &capture{code: 401, answer: []string{"no"}}
	cap.install(t)
	if _, err := c.Call(context.Background(), map[string]any{"text": "x"}); err == nil {
		t.Error("login failure")
	}
	cap = &capture{answer: []string{`{"accessJwt":"jwt","did":"did:x"}`, "boom"}, code: 200}
	cap.install(t)
	httpDoOld := httpDo
	n := 0
	httpDo = func(req *http.Request) (*http.Response, error) {
		n++
		if n == 2 {
			return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("boom"))}, nil
		}
		return httpDoOld(req)
	}
	if _, err := c.Call(context.Background(), map[string]any{"text": "x"}); err == nil {
		t.Error("post failure")
	}
	httpDo = httpDoOld
	// Figma's file is rendered as its pages and frames.
	doc, _ := json.Marshal(map[string]any{"name": "Design", "document": map[string]any{"children": []map[string]any{{"name": "Page 1", "children": []map[string]any{{"name": "Home", "type": "FRAME"}}}}}})
	cap = &capture{answer: []string{string(doc)}}
	cap.install(t)
	f, _ := Build("figma", Settings{"token": "t"}, "")
	out, _ = f.Call(context.Background(), map[string]any{"action": "get_file", "file": "F"})
	if !strings.Contains(out, "Design\n- Page 1\n  - Home (FRAME)") {
		t.Errorf("figma file = %q", out)
	}
	cap = &capture{answer: []string{"not json"}}
	cap.install(t)
	if out, _ := f.Call(context.Background(), map[string]any{"action": "get_file", "file": "F"}); out != "not json" {
		t.Errorf("figma fallback = %q", out)
	}
	cap = &capture{answer: []string{"file text"}}
	cap.install(t)
	g, _ := Build("github", Settings{"token": "t", "repo": "a/b"}, "")
	if out, _ := g.Call(context.Background(), map[string]any{"action": "get_file", "path": "x"}); out != "file text" {
		t.Errorf("github file = %q", out)
	}
	cap = &capture{answer: []string{"file text"}}
	cap.install(t)
	nc, _ := Build("nextcloud", Settings{"url": "u", "user": "u", "password": "p"}, "")
	if out, _ := nc.Call(context.Background(), map[string]any{"action": "download", "name": "a"}); out != "file text" {
		t.Errorf("nextcloud download = %q", out)
	}
	cap.install(t)
	if out, _ := nc.Call(context.Background(), map[string]any{"name": "a", "content": "c"}); out != "uploaded" {
		t.Errorf("nextcloud upload = %q", out)
	}
}

func TestEmail(t *testing.T) {
	var got struct {
		addr, from string
		to         []string
		msg        string
	}
	sendMail = func(addr string, _ smtp.Auth, from string, to []string, msg []byte) error {
		got.addr, got.from, got.to, got.msg = addr, from, to, string(msg)
		if from == "fail@x" {
			return errors.New("refused")
		}
		return nil
	}
	defer func() { sendMail = smtp.SendMail }()
	c, err := Build("email", Settings{"host": "smtp.x:587", "user": "u", "password": "p", "from": "me@x", "to": "a@x,b@x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.Call(context.Background(), map[string]any{"text": "hello", "subject": "Hi"})
	if err != nil || out != "sent to a@x,b@x" || got.addr != "smtp.x:587" || len(got.to) != 2 || !strings.Contains(got.msg, "Subject: Hi\r\n") || !strings.HasSuffix(got.msg, "hello\r\n") {
		t.Errorf("email = %q %v %+v", out, err, got)
	}
	if _, err := c.Call(context.Background(), map[string]any{"text": "x", "to": "c@x"}); err != nil || got.to[0] != "c@x" || !strings.Contains(got.msg, "A message from your kitty") {
		t.Error("to and the default subject")
	}
	if _, err := c.Call(context.Background(), map[string]any{}); err == nil {
		t.Error("text needed")
	}
	noTo, _ := Build("email", Settings{"host": "h", "user": "u", "password": "p", "from": "me@x"}, "")
	if _, err := noTo.Call(context.Background(), map[string]any{"text": "x"}); err == nil {
		t.Error("to needed")
	}
	failing, _ := Build("email", Settings{"host": "h", "user": "u", "password": "p", "from": "fail@x", "to": "a@x"}, "")
	if _, err := failing.Call(context.Background(), map[string]any{"text": "x"}); err == nil {
		t.Error("a refused mail")
	}
	if c.Describe() == "" {
		t.Error("Describe")
	}
}

func TestOAuthKinds(t *testing.T) {
	for _, name := range []string{"google-workspace", "microsoft-365", "slack", "github"} {
		k, ok := LookupKind(name)
		if !ok || k.OAuth == "" {
			t.Errorf("%s has an OAuth flow", name)
		}
	}
	gh, _ := LookupKind("github")
	sl, _ := LookupKind("slack")
	gw, _ := LookupKind("google-workspace")
	if !gh.TokenSet(Settings{"token": "t"}) || gh.TokenSet(Settings{}) || !sl.TokenSet(Settings{"url": "u"}) || gw.TokenSet(Settings{"token": "t"}) {
		t.Error("TokenSet: a token or webhook of the kind's own")
	}
	// A kind with a token field: the typed token wins, a webhook needs no account, and
	// without either the sign-in's token signs the request.
	src := func(context.Context) (string, error) { return "tok", nil }
	for _, tc := range []struct {
		kind string
		s    Settings
		args map[string]any
		auth string
	}{
		{"github", Settings{"token": "ghp"}, map[string]any{"repo": "a/b", "title": "T"}, "Bearer ghp"},
		{"github", Settings{}, map[string]any{"repo": "a/b", "title": "T"}, "Bearer tok"},
		{"slack", Settings{"url": "https://hooks.slack.com/x"}, map[string]any{"text": "hi"}, ""},
		{"slack", Settings{"channel": "#c"}, map[string]any{"text": "hi"}, "Bearer tok"},
		{"slack", Settings{"channel": "#c", "token": "xoxb"}, map[string]any{"text": "hi"}, "Bearer xoxb"},
	} {
		cap := &capture{}
		cap.install(t)
		c, err := Build(tc.kind, WithTokenSource(tc.s, src), "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Call(context.Background(), tc.args); err != nil || len(cap.reqs) != 1 || cap.reqs[0].Header.Get("Authorization") != tc.auth {
			t.Errorf("%s %v: %v auth=%q", tc.kind, tc.s, err, cap.reqs[0].Header.Get("Authorization"))
		}
	}
	cap := &capture{}
	cap.install(t)
	c, _ := Build("github", Settings{}, "")
	if _, err := c.Call(context.Background(), map[string]any{"repo": "a/b", "title": "T"}); err == nil || !strings.Contains(err.Error(), "not signed in") {
		t.Errorf("github without token nor sign-in: %v", err)
	}
	found := false
	for _, k := range Kinds() {
		found = found || (k.Name == "google-workspace" && k.OAuth == "google")
	}
	if !found {
		t.Error("Kinds carries the oauth field")
	}
	// Without a token source the connector refuses before any request.
	cap = &capture{}
	cap.install(t)
	c, err := Build("google-workspace", Settings{"client_id": "cid"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(context.Background(), map[string]any{"to": "a@x", "text": "hi"}); err == nil || !strings.Contains(err.Error(), "not signed in") || len(cap.reqs) != 0 {
		t.Errorf("no token source: %v", err)
	}
	// With one, every request carries the bearer token.
	c, _ = Build("google-workspace", WithTokenSource(Settings{"client_id": "cid"}, src), "")
	cases := []call{
		{"google-workspace", nil, map[string]any{"to": "a@x", "subject": "S", "text": "hi"}, "POST", "gmail/v1/users/me/messages/send", `"raw":"`, [2]string{"Authorization", "Bearer tok"}},
		{"google-workspace", nil, map[string]any{"action": "create_event", "summary": "Meet", "start": "2026-01-01T10:00:00Z"}, "POST", "calendar/v3/calendars/primary/events", `"dateTime":"2026-01-01T10:00:00Z"`, [2]string{}},
		{"google-workspace", nil, map[string]any{"action": "upload", "name": "a.txt", "content": "body"}, "POST", "upload/drive/v3/files?uploadType=multipart", "body", [2]string{}},
		{"google-workspace", nil, map[string]any{"action": "append_row", "spreadsheet": "sid", "values": []any{"a", 1.0}}, "POST", "/spreadsheets/sid/values/Sheet1%21A1:append", `"values":[["a",1]]`, [2]string{}},
		{"google-workspace", nil, map[string]any{"action": "create_doc", "title": "Doc"}, "POST", "docs.googleapis.com/v1/documents", `"title":"Doc"`, [2]string{}},
	}
	for _, tc := range cases {
		cap = &capture{}
		cap.install(t)
		out, err := c.Call(context.Background(), tc.args)
		if err != nil || out == "" || len(cap.reqs) != 1 {
			t.Errorf("%v: %q %v reqs=%d", tc.args, out, err, len(cap.reqs))
			continue
		}
		req := cap.reqs[0]
		if req.Method != tc.method || !strings.Contains(req.URL.String(), tc.url) || !strings.Contains(cap.bodies[0], tc.body) || req.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("%v: %s %s body=%s auth=%s", tc.args, req.Method, req.URL, cap.bodies[0], req.Header.Get("Authorization"))
		}
	}
	for _, args := range []map[string]any{{}, {"to": "a"}, {"action": "create_event"}, {"action": "create_event", "summary": "x"}, {"action": "upload"}, {"action": "append_row"}, {"action": "append_row", "spreadsheet": "s"}, {"action": "create_doc"}} {
		cap = &capture{}
		cap.install(t)
		if _, err := c.Call(context.Background(), args); err == nil || len(cap.reqs) != 0 {
			t.Errorf("%v must be refused", args)
		}
	}
	// A token source that fails stops the call.
	failing, _ := Build("microsoft-365", WithTokenSource(Settings{"client_id": "cid"}, func(context.Context) (string, error) { return "", errors.New("expired") }), "")
	if _, err := failing.Call(context.Background(), map[string]any{"to": "a@x", "text": "hi"}); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("token failure: %v", err)
	}
	ms, _ := Build("microsoft-365", WithTokenSource(Settings{"client_id": "cid"}, src), "")
	msCases := []call{
		{"microsoft-365", nil, map[string]any{"to": "a@x, b@x", "subject": "S", "text": "hi"}, "POST", "graph.microsoft.com/v1.0/me/sendMail", `"address":"b@x"`, [2]string{}},
		{"microsoft-365", nil, map[string]any{"action": "create_event", "subject": "Meet", "start": "2026-01-01T10:00:00"}, "POST", "/v1.0/me/events", `"timeZone":"UTC"`, [2]string{}},
		{"microsoft-365", nil, map[string]any{"action": "upload", "name": "a.txt", "content": "body"}, "PUT", "/me/drive/root:/Kitties/a.txt:/content", "body", [2]string{}},
	}
	for _, tc := range msCases {
		cap = &capture{}
		cap.install(t)
		out, err := ms.Call(context.Background(), tc.args)
		if err != nil || out == "" || len(cap.reqs) != 1 || cap.reqs[0].Method != tc.method || !strings.Contains(cap.reqs[0].URL.String(), tc.url) || !strings.Contains(cap.bodies[0], tc.body) {
			t.Errorf("%v: %q %v", tc.args, out, err)
		}
	}
	for _, args := range []map[string]any{{}, {"to": "a"}, {"action": "create_event"}, {"action": "create_event", "subject": "x"}, {"action": "upload"}, {"action": "upload", "name": "../x"}} {
		cap = &capture{}
		cap.install(t)
		if _, err := ms.Call(context.Background(), args); err == nil || len(cap.reqs) != 0 {
			t.Errorf("%v must be refused", args)
		}
	}
	if tokenSourceOf(Settings{}) != nil {
		t.Error("no source")
	}
	if mailRFC822("me@x", "a@x", "S", "t") != "From: me@x\r\nTo: a@x\r\nSubject: S\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nt" {
		t.Error("mailRFC822")
	}
}
