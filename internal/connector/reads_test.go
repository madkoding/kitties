package connector

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
)

// run builds a connector, answers its requests with the given bodies and calls it once.
func run(t *testing.T, kind string, s Settings, args map[string]any, answers ...string) (string, error, *capture) {
	t.Helper()
	cap := &capture{answer: answers}
	cap.install(t)
	// The signed-in kinds are given a token, as the orchestrator gives them one.
	if k, _ := LookupKind(kind); k.OAuth != "" && s["token"] == "" && s["url"] == "" {
		s = WithTokenSource(s, func(context.Context) (string, error) { return "tok", nil })
	}
	c, err := Build(kind, s, "")
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.Call(context.Background(), args)
	return out, err, cap
}

func TestReadingActions(t *testing.T) {
	type tc struct {
		name     string
		kind     string
		settings Settings
		args     map[string]any
		answers  []string
		method   string
		url      string
		body     string
		want     []string // substrings of the text the model reads
	}
	cases := []tc{
		{"slack history", "slack", Settings{"token": "xoxb", "channel": "#g"}, map[string]any{"action": "history", "limit": 5.0},
			[]string{`{"ok":true,"messages":[{"user":"U1","text":"where is my invoice?","ts":"1.2"}]}`}, "GET", "conversations.history?channel=%23g&limit=5", "", []string{"- U1 | where is my invoice? | 1.2"}},
		{"telegram updates", "telegram", Settings{"token": "T", "chat_id": "7"}, map[string]any{"action": "updates"},
			[]string{`{"result":[{"message":{"chat":{"id":7},"from":{"username":"ana"},"text":"hola"}}]}`}, "GET", "/botT/getUpdates?limit=10", "", []string{"- 7 | ana | hola"}},
		{"mastodon timeline", "mastodon", Settings{"url": "https://mas", "token": "t"}, map[string]any{"action": "timeline", "limit": 99.0},
			[]string{`[{"id":"9","account":{"acct":"pat"},"content":"<p>Hello &amp; <b>welcome</b></p>"}]`}, "GET", "/api/v1/timelines/home?limit=30", "", []string{"- 9 | pat | Hello & welcome"}},
		{"mastodon mentions", "mastodon", Settings{"url": "https://mas", "token": "t"}, map[string]any{"action": "mentions"},
			[]string{`[{"status":{"id":"5","content":"<p>@us hi</p>"},"account":{"acct":"lee"}}]`}, "GET", "notifications?types%5B%5D=mention", "", []string{"- 5 | lee | @us hi"}},
		{"mastodon reply", "mastodon", Settings{"url": "https://mas", "token": "t"}, map[string]any{"action": "reply", "id": "5", "text": "thanks"},
			nil, "POST", "/api/v1/statuses", `"in_reply_to_id":"5"`, []string{"done"}},
		{"mastodon like", "mastodon", Settings{"url": "https://mas", "token": "t"}, map[string]any{"action": "like", "id": "5"},
			nil, "POST", "/api/v1/statuses/5/favourite", "", nil},
		{"mastodon boost", "mastodon", Settings{"url": "https://mas", "token": "t"}, map[string]any{"action": "boost", "id": "5"},
			nil, "POST", "/api/v1/statuses/5/reblog", "", nil},
		{"x mentions", "x", Settings{"token": "t", "user_id": "42"}, map[string]any{"action": "mentions"},
			[]string{`{"data":[{"id":"1","author_id":"8","text":"hey @us"}]}`}, "GET", "/2/users/42/mentions", "", []string{"- 1 | 8 | hey @us"}},
		{"x reply", "x", Settings{"token": "t"}, map[string]any{"action": "reply", "id": "77", "text": "ok"},
			nil, "POST", "/2/tweets", `"in_reply_to_tweet_id":"77"`, nil},
		{"x like", "x", Settings{"token": "t", "user_id": "42"}, map[string]any{"action": "like", "id": "77"},
			nil, "POST", "/2/users/42/likes", `"tweet_id":"77"`, nil},
		{"linkedin comment", "linkedin", Settings{"token": "t", "author": "urn:li:person:1"}, map[string]any{"action": "comment", "post": "urn:li:share:9", "text": "nice"},
			nil, "POST", "/v2/socialActions/urn:li:share:9/comments", `"actor":"urn:li:person:1"`, nil},
		{"linkedin like", "linkedin", Settings{"token": "t", "author": "urn:li:person:1"}, map[string]any{"action": "like", "post": "urn:li:share:9"},
			nil, "POST", "/likes", `"object":"urn:li:share:9"`, nil},
		{"github list issues", "github", Settings{"token": "t", "repo": "a/b"}, map[string]any{"action": "list_issues", "state": "bogus"},
			[]string{`[{"number":3,"state":"open","title":"Crash","user":{"login":"pat"},"labels":[{"name":"bug"}]}]`}, "GET", "/repos/a/b/issues?state=open&per_page=10", "", []string{"- 3 | open | Crash | pat | bug"}},
		{"github get issue", "github", Settings{"token": "t", "repo": "a/b"}, map[string]any{"action": "get_issue", "number": 3.0},
			[]string{`{"number":3,"state":"open","title":"Crash","user":{"login":"pat"},"body":"It breaks."}`}, "GET", "/repos/a/b/issues/3", "", []string{"#3 Crash (open) by pat", "It breaks."}},
		{"jira search", "jira", Settings{"url": "https://j.example", "email": "e", "token": "t", "project": "KIT"}, map[string]any{"action": "search"},
			[]string{`{"issues":[{"key":"KIT-1","fields":{"summary":"Fix it","status":{"name":"To Do"},"assignee":{"displayName":"Ana"}}}]}`}, "GET", "/rest/api/3/search/jql?", "", []string{"- KIT-1 | To Do | Fix it | Ana"}},
		{"google calendar", "google-workspace", Settings{"client_id": "c"}, map[string]any{"action": "list_events", "from": "2026-01-01T00:00:00Z"},
			[]string{`{"items":[{"id":"e1","start":{"dateTime":"2026-01-02T10:00:00Z"},"summary":"Review"}]}`}, "GET", "calendar/v3/calendars/primary/events?singleEvents=true&orderBy=startTime&timeMin=2026-01-01T00%3A00%3A00Z", "", []string{"- e1 | 2026-01-02T10:00:00Z | Review"}},
		{"outlook list", "microsoft-365", Settings{"client_id": "c"}, map[string]any{"action": "list_mail", "query": "invoice"},
			[]string{`{"value":[{"id":"m1","from":{"emailAddress":{"address":"a@x"}},"subject":"Invoice 7","receivedDateTime":"2026-01-02","bodyPreview":"Please pay"}]}`}, "GET", "graph.microsoft.com/v1.0/me/messages?", "", []string{"- m1 | a@x | Invoice 7 | 2026-01-02 | Please pay"}},
		{"outlook read", "microsoft-365", Settings{"client_id": "c"}, map[string]any{"action": "read_mail", "id": "m1"},
			[]string{`{"from":{"emailAddress":{"address":"a@x"}},"subject":"Invoice 7","receivedDateTime":"2026-01-02","body":{"content":"Please pay by Friday."}}`}, "GET", "/me/messages/m1", "", []string{"from: a@x", "subject: Invoice 7", "Please pay by Friday."}},
		{"outlook events", "microsoft-365", Settings{"client_id": "c"}, map[string]any{"action": "list_events"},
			[]string{`{"value":[{"id":"e","subject":"Sync","start":{"dateTime":"2026-02-01T09:00:00"}}]}`}, "GET", "/me/events?", "", []string{"- e | 2026-02-01T09:00:00 | Sync"}},
		{"stripe balance", "stripe", Settings{"token": "sk"}, map[string]any{},
			[]string{`{"available":[{"amount":123456,"currency":"usd"}],"pending":[{"amount":500,"currency":"usd"}]}`}, "GET", "api.stripe.com/v1/balance", "", []string{"available: 1234.56 USD", "pending: 5.00 USD"}},
		{"stripe invoices", "stripe", Settings{"token": "sk"}, map[string]any{"action": "list_invoices", "overdue": "true"},
			[]string{`{"data":[{"id":"in_1","number":"A-7","status":"open","customer_email":"c@x","amount_due":25000,"currency":"eur","due_date":1767225600}]}`}, "GET", "/v1/invoices?status=open&limit=10&due_date%5Blt%5D=", "", []string{"- in_1 | A-7 | open | c@x | 250.00 | eur | 2026-01-01"}},
		{"stripe payments", "stripe", Settings{"token": "sk"}, map[string]any{"action": "list_payments"},
			[]string{`{"data":[{"id":"pi_1","status":"succeeded","amount":999,"currency":"usd"}]}`}, "GET", "/v1/payment_intents?limit=10", "", []string{"- pi_1 | succeeded | 9.99 | usd"}},
		{"stripe send invoice", "stripe", Settings{"token": "sk"}, map[string]any{"action": "send_invoice", "id": "in_1"},
			nil, "POST", "/v1/invoices/in_1/send", "", nil},
		{"stripe refund", "stripe", Settings{"token": "sk"}, map[string]any{"action": "refund", "payment": "pi_1", "amount": "500"},
			nil, "POST", "/v1/refunds", "amount=500&payment_intent=pi_1", nil},
		{"zendesk tickets", "zendesk", Settings{"subdomain": "acme", "email": "a@x", "token": "t"}, map[string]any{"action": "list_tickets"},
			[]string{`{"results":[{"id":12,"status":"open","priority":"high","subject":"Refund please"}]}`}, "GET", "acme.zendesk.com/api/v2/search.json?sort_by=updated_at&sort_order=desc&query=type%3Aticket+status%3Csolved", "", []string{"- 12 | open | high | Refund please"}},
		{"zendesk read", "zendesk", Settings{"subdomain": "acme", "email": "a@x", "token": "t"}, map[string]any{"action": "read_ticket", "id": 12.0},
			[]string{`{"comments":[{"author_id":5,"plain_body":"I was charged twice"}]}`}, "GET", "/tickets/12/comments.json", "", []string{"- 5 | I was charged twice"}},
		{"zendesk reply", "zendesk", Settings{"subdomain": "acme", "email": "a@x", "token": "t"}, map[string]any{"action": "reply", "id": 12.0, "text": "Fixed.", "public": "false"},
			nil, "PUT", "/tickets/12.json", `"public":false`, nil},
		{"ntfy", "ntfy", Settings{"topic": "kitties-abc", "token": "tk"}, map[string]any{"text": "need you", "title": "mimi", "priority": "4", "click": "https://k/#chat/1"},
			nil, "POST", "ntfy.sh", `"topic":"kitties-abc"`, nil},
	}
	for _, c := range cases {
		out, err, cap := run(t, c.kind, c.settings, c.args, c.answers...)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		req := cap.reqs[len(cap.reqs)-1]
		if req.Method != c.method || !strings.Contains(req.URL.String(), c.url) || !strings.Contains(cap.bodies[len(cap.bodies)-1], c.body) {
			t.Errorf("%s: %s %s body=%s", c.name, req.Method, req.URL, cap.bodies[len(cap.bodies)-1])
		}
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("%s: %q lacks %q", c.name, out, w)
			}
		}
	}
	// A few request details the table does not say.
	_, _, cap := run(t, "stripe", Settings{"token": "sk"}, map[string]any{"action": "refund", "payment": "pi_1"}, "{}")
	if cap.reqs[0].Header.Get("Content-Type") != "application/x-www-form-urlencoded" || cap.reqs[0].Header.Get("Authorization") != "Bearer sk" {
		t.Errorf("stripe wants a form and the key: %v", cap.reqs[0].Header)
	}
	_, _, cap = run(t, "zendesk", Settings{"subdomain": "acme", "email": "a@x", "token": "t"}, map[string]any{"action": "list_tickets", "query": "status:open"}, "{}")
	if user, pass, ok := cap.reqs[0].BasicAuth(); !ok || user != "a@x/token" || pass != "t" || !strings.Contains(cap.reqs[0].URL.RawQuery, "query=status%3Aopen") {
		t.Errorf("zendesk basic auth: %v %v", cap.reqs[0].URL, cap.reqs[0].Header)
	}
	_, _, cap = run(t, "ntfy", Settings{"topic": "t"}, map[string]any{"text": "x", "priority": "9"})
	if strings.Contains(cap.bodies[0], "priority") || cap.reqs[0].Header.Get("Authorization") != "" {
		t.Errorf("a bad priority is dropped and no token sends no header: %s", cap.bodies[0])
	}
}

func TestReadingFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		kind string
		s    Settings
		args map[string]any
		want string
	}{
		"slack webhook cannot read":  {"slack", Settings{"url": "https://hooks/x"}, map[string]any{"action": "history", "channel": "#c"}, "bot token"},
		"slack needs a channel":      {"slack", Settings{"token": "t", "url": "https://hooks/x"}, map[string]any{"action": "history"}, "channel is missing"},
		"x needs a user id":          {"x", Settings{"token": "t"}, map[string]any{"action": "mentions"}, "user_id"},
		"x like needs a user id":     {"x", Settings{"token": "t"}, map[string]any{"action": "like", "id": "1"}, "user_id"},
		"x reply needs an id":        {"x", Settings{"token": "t"}, map[string]any{"action": "reply", "text": "x"}, "id is missing"},
		"x reply needs text":         {"x", Settings{"token": "t"}, map[string]any{"action": "reply", "id": "1"}, "text is missing"},
		"x like needs a post":        {"x", Settings{"token": "t", "user_id": "1"}, map[string]any{"action": "like"}, "id is missing"},
		"mastodon reply needs text":  {"mastodon", Settings{"url": "https://m", "token": "t"}, map[string]any{"action": "reply", "id": "1"}, "text is missing"},
		"mastodon reply needs id":    {"mastodon", Settings{"url": "https://m", "token": "t"}, map[string]any{"action": "reply", "text": "x"}, "id is missing"},
		"mastodon like needs id":     {"mastodon", Settings{"url": "https://m", "token": "t"}, map[string]any{"action": "like"}, "id is missing"},
		"linkedin needs a post":      {"linkedin", Settings{"token": "t", "author": "a"}, map[string]any{"action": "like"}, "post is missing"},
		"linkedin comment text":      {"linkedin", Settings{"token": "t", "author": "a"}, map[string]any{"action": "comment", "post": "p"}, "text is missing"},
		"github needs a number":      {"github", Settings{"token": "t", "repo": "a/b"}, map[string]any{"action": "get_issue"}, "number is missing"},
		"github needs a repo":        {"github", Settings{"token": "t"}, map[string]any{"action": "list_issues"}, "repo is missing"},
		"github issue needs a repo":  {"github", Settings{"token": "t"}, map[string]any{"action": "get_issue", "number": 1.0}, "repo is missing"},
		"mail needs an id":           {"google-workspace", Settings{"client_id": "c"}, map[string]any{"action": "read_mail"}, "id is missing"},
		"outlook needs an id":        {"microsoft-365", Settings{"client_id": "c"}, map[string]any{"action": "read_mail"}, "id is missing"},
		"stripe invoice needs an id": {"stripe", Settings{"token": "k"}, map[string]any{"action": "send_invoice"}, "id is missing"},
		"stripe refund needs a pi":   {"stripe", Settings{"token": "k"}, map[string]any{"action": "refund"}, "payment is missing"},
		"zendesk needs an id":        {"zendesk", Settings{"subdomain": "a", "email": "e", "token": "t"}, map[string]any{"action": "read_ticket"}, "id is missing"},
		"zendesk reply needs an id":  {"zendesk", Settings{"subdomain": "a", "email": "e", "token": "t"}, map[string]any{"action": "reply", "text": "x"}, "id is missing"},
		"zendesk reply needs text":   {"zendesk", Settings{"subdomain": "a", "email": "e", "token": "t"}, map[string]any{"action": "reply", "id": "1"}, "text is missing"},
		"ntfy needs text":            {"ntfy", Settings{"topic": "t"}, map[string]any{}, "text is missing"},
	} {
		cap := &capture{}
		cap.install(t)
		c, err := Build(tc.kind, tc.s, "")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := c.Call(context.Background(), tc.args); err == nil || !strings.Contains(err.Error(), tc.want) || len(cap.reqs) != 0 {
			t.Errorf("%s: %v (requests: %d)", name, err, len(cap.reqs))
		}
	}
	// Slack answers 200 with ok: false when it refuses.
	if _, err, _ := run(t, "slack", Settings{"token": "t", "channel": "#c"}, map[string]any{"action": "history"}, `{"ok":false,"error":"not_in_channel"}`); err == nil || !strings.Contains(err.Error(), "not_in_channel") {
		t.Errorf("slack ok:false: %v", err)
	}
	// A successful send answers {"ok":true}: not an error.
	if _, err, _ := run(t, "slack", Settings{"token": "t", "channel": "#c"}, map[string]any{"text": "x"}, `{"ok":true,"ts":"1"}`); err != nil {
		t.Errorf("slack ok:true: %v", err)
	}
	// An empty list and a malformed answer still read.
	if out, _, _ := run(t, "mastodon", Settings{"url": "https://m", "token": "t"}, map[string]any{"action": "timeline"}, `[]`); out != "(nothing)" {
		t.Errorf("empty = %q", out)
	}
	if out, _, _ := run(t, "mastodon", Settings{"url": "https://m", "token": "t"}, map[string]any{"action": "timeline"}, `not json`); out != "not json" {
		t.Errorf("malformed = %q", out)
	}
	if out, _, _ := run(t, "stripe", Settings{"token": "k"}, map[string]any{"action": "balance"}, `{}`); out != "(no balance)" {
		t.Errorf("no balance = %q", out)
	}
	if out, _, _ := run(t, "stripe", Settings{"token": "k"}, map[string]any{"action": "balance"}, `nope`); out != "nope" {
		t.Errorf("bad balance = %q", out)
	}
	if out, _, _ := run(t, "github", Settings{"token": "t", "repo": "a/b"}, map[string]any{"action": "get_issue", "number": "1"}, `plain`); out != "plain" {
		t.Errorf("bad issue = %q", out)
	}
	if out, _, _ := run(t, "microsoft-365", Settings{"client_id": "c"}, map[string]any{"action": "read_mail", "id": "m"}, `plain`); out != "plain" {
		t.Errorf("bad outlook read = %q", out)
	}
}

func TestListingHelpers(t *testing.T) {
	data := []byte(`[{"a":1,"b":{"c":"x"},"n":2000,"t":1767225600,"f":true,"o":{"k":1},"l":[{"m":"first"}]},{"a":2},{"a":3}]`)
	got := listing(data, "", 2, "a", "b.c", "n:cents", "t:date", "f", "o", "l.0.m", "l.5.m", "a.b")
	if want := "- 1 | x | 20.00 | 2026-01-01 | true | {\"k\":1} | first\n- 2\n… and 1 more"; got != want {
		t.Errorf("listing = %q\nwant %q", got, want)
	}
	if listing([]byte(`{"x":1}`), "x", 5, "a") != "(nothing)" || listing([]byte(`{"x":[]}`), "x", 5, "a") != "(nothing)" || listing([]byte(`{}`), "missing.deep", 5, "a") != "(nothing)" {
		t.Error("nothing to list")
	}
	if getPath("plain", "a") != nil || getPath([]any{1}, "x") != nil || getPath([]any{1}, "-1") != nil {
		t.Error("getPath stops on what is not a path")
	}
	if limitOf(map[string]any{"limit": "x"}) != 10 || limitOf(map[string]any{"limit": 0.0}) != 10 || limitOf(map[string]any{"limit": 7.0}) != 7 || limitOf(map[string]any{"limit": 500.0}) != 30 {
		t.Error("limitOf")
	}
	if string(form("a", "1", "b", "", "c", "x y")) != "a=1&c=x+y" {
		t.Errorf("form = %s", form("a", "1", "b", "", "c", "x y"))
	}
	if cell("<p>a  &lt;b&gt;</p>\n<br/>c", "") != "a <b> c" || cell(nil, "") != "" || cell(1.5, "") != "1.5" {
		t.Error("cell")
	}
}

func TestGmail(t *testing.T) {
	list := `{"messages":[{"id":"m1"},{"id":"m2"}]}`
	meta := func(from, subject string) string {
		return `{"snippet":"hello there","payload":{"headers":[{"name":"From","value":"` + from + `"},{"name":"Subject","value":"` + subject + `"},{"name":"Date","value":"Mon"}]}}`
	}
	cap := &capture{answer: []string{list, meta("a@x", "Invoice"), meta("b@x", "Hi")}}
	cap.install(t)
	src := func(context.Context) (string, error) { return "tok", nil }
	c, err := Build("google-workspace", WithTokenSource(Settings{"client_id": "c"}, src), "")
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.Call(context.Background(), map[string]any{"action": "list_mail", "query": "is:unread", "limit": 2.0})
	if err != nil || !strings.Contains(out, "- m1 | a@x | Invoice | Mon | hello there") || !strings.Contains(out, "- m2 | b@x | Hi") {
		t.Fatalf("gmail list = %q %v", out, err)
	}
	if len(cap.reqs) != 3 || !strings.Contains(cap.reqs[0].URL.String(), "q=is%3Aunread&") && !strings.Contains(cap.reqs[0].URL.String(), "q=is%3Aunread") || !strings.Contains(cap.reqs[1].URL.String(), "/messages/m1?format=metadata") || cap.reqs[1].Header.Get("Authorization") != "Bearer tok" {
		t.Errorf("gmail requests = %v", cap.reqs)
	}
	// No mail, an unreadable message and more than ten are all told.
	for _, tc := range []struct {
		answers []string
		want    string
	}{
		{[]string{`{}`}, "(no mail)"},
		{[]string{`nope`}, "(no mail)"},
		{[]string{`{"messages":[{"id":"x"}]}`, `{}`}, "- x |"},
	} {
		cap := &capture{answer: tc.answers}
		cap.install(t)
		c, _ := Build("google-workspace", WithTokenSource(Settings{"client_id": "c"}, src), "")
		if out, err := c.Call(context.Background(), map[string]any{"action": "list_mail"}); err != nil || !strings.Contains(out, tc.want) {
			t.Errorf("%v = %q %v", tc.answers, out, err)
		}
	}
	var many strings.Builder
	many.WriteString(`{"messages":[`)
	for i := 0; i < 12; i++ {
		if i > 0 {
			many.WriteString(",")
		}
		many.WriteString(`{"id":"m"}`)
	}
	many.WriteString(`]}`)
	cap2 := &capture{answer: []string{many.String()}}
	cap2.install(t)
	c, _ = Build("google-workspace", WithTokenSource(Settings{"client_id": "c"}, src), "")
	if out, _ := c.Call(context.Background(), map[string]any{"action": "list_mail"}); !strings.Contains(out, "… and 2 more") {
		t.Errorf("more than ten: %q", out)
	}
	// A failing message fetch is told, not fatal.
	cap3 := &capture{answer: []string{`{"messages":[{"id":"m1"}]}`}}
	cap3.install(t)
	old := httpDo
	n := 0
	httpDo = func(req *http.Request) (*http.Response, error) {
		n++
		if n == 2 {
			return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("boom"))}, nil
		}
		return old(req)
	}
	c, _ = Build("google-workspace", WithTokenSource(Settings{"client_id": "c"}, src), "")
	if out, _ := c.Call(context.Background(), map[string]any{"action": "list_mail"}); !strings.Contains(out, "unreadable") {
		t.Errorf("failed fetch: %q", out)
	}
	httpDo = old
	// Reading one message: the headers and the plain text, found in a multipart body or the snippet.
	text := base64.RawURLEncoding.EncodeToString([]byte("Dear team,\nPlease pay."))
	full := `{"snippet":"Dear","payload":{"mimeType":"multipart/alternative","headers":[{"name":"From","value":"a@x"},{"name":"To","value":"me"},{"name":"Subject","value":"Invoice"},{"name":"Date","value":"Mon"}],"parts":[{"mimeType":"text/html","body":{"data":"PGI-"}},{"mimeType":"text/plain","body":{"data":"` + text + `"}}]}}`
	out, err = runOne(t, "google-workspace", map[string]any{"action": "read_mail", "id": "m1"}, full)
	if err != nil || !strings.Contains(out, "from: a@x") || !strings.Contains(out, "subject: Invoice") || !strings.Contains(out, "Dear team,\nPlease pay.") {
		t.Errorf("read = %q %v", out, err)
	}
	if out, _ := runOne(t, "google-workspace", map[string]any{"action": "read_mail", "id": "m1"}, `{"snippet":"only a snippet","payload":{"mimeType":"text/html","headers":[]}}`); !strings.Contains(out, "only a snippet") {
		t.Errorf("snippet fallback = %q", out)
	}
	if out, _ := runOne(t, "google-workspace", map[string]any{"action": "read_mail", "id": "m1"}, `nope`); out != "nope" {
		t.Errorf("unreadable = %q", out)
	}
	bad := `{"payload":{"mimeType":"text/plain","body":{"data":"!!!"}},"snippet":"s"}`
	if out, _ := runOne(t, "google-workspace", map[string]any{"action": "read_mail", "id": "m1"}, bad); !strings.Contains(out, "s") {
		t.Errorf("undecodable text = %q", out)
	}
}

// runOne calls a signed-in kind once, answering with one body.
func runOne(t *testing.T, kind string, args map[string]any, answer string) (string, error) {
	t.Helper()
	cap := &capture{answer: []string{answer}}
	cap.install(t)
	src := func(context.Context) (string, error) { return "tok", nil }
	c, err := Build(kind, WithTokenSource(Settings{"client_id": "c"}, src), "")
	if err != nil {
		t.Fatal(err)
	}
	return c.Call(context.Background(), args)
}

func TestRisk(t *testing.T) {
	for _, tc := range []struct {
		kind, action string
		want         Risk
	}{
		{"slack", "send", RiskAsk}, {"slack", "history", RiskNone}, {"email", "send", RiskAsk}, {"mastodon", "post", RiskAsk},
		{"mastodon", "like", RiskAsk}, {"mastodon", "timeline", RiskNone}, {"x", "reply", RiskAsk}, {"github", "create_pr", RiskAsk},
		{"github", "comment", RiskAsk}, {"github", "list_issues", RiskNone}, {"github", "create_issue", RiskNone}, {"trello", "create_card", RiskNone},
		{"stripe", "send_invoice", RiskAsk}, {"stripe", "refund", RiskAlways}, {"stripe", "balance", RiskNone}, {"shell", "run", RiskAlways},
		{"motita", "run", RiskAsk}, {"google-workspace", "send_mail", RiskAsk}, {"google-workspace", "list_mail", RiskNone}, {"kroki", "render", RiskNone},
		{"nextcloud", "delete", RiskAlways}, {"zendesk", "reply", RiskAsk}, {"unknown", "SEND", RiskAsk},
	} {
		if got := RiskOf(tc.kind, tc.action); got != tc.want {
			t.Errorf("%s.%s = %v, want %v", tc.kind, tc.action, got, tc.want)
		}
	}
	for _, tc := range []struct {
		kind string
		args map[string]any
		want Risk
	}{
		{"http", map[string]any{"method": "get", "path": "/x"}, RiskNone}, {"http", map[string]any{"path": "/x"}, RiskNone}, {"http", map[string]any{"method": "POST"}, RiskAsk},
		{"http", map[string]any{"method": "delete"}, RiskAlways}, {"slack", map[string]any{"text": "hi"}, RiskAsk}, {"slack", map[string]any{"action": "history"}, RiskNone},
		{"stripe", map[string]any{}, RiskNone}, {"stripe", map[string]any{"action": "refund"}, RiskAlways}, {"nope", map[string]any{}, RiskNone},
	} {
		if got := RiskOfCall(tc.kind, tc.args); got != tc.want {
			t.Errorf("%s %v = %v, want %v", tc.kind, tc.args, got, tc.want)
		}
	}
	if RiskNone.String() != "" || RiskAsk.String() != "ask" || RiskAlways.String() != "always" {
		t.Error("String")
	}
	// The catalogue and the text the model reads carry the risk.
	k, _ := LookupKind("stripe")
	risks := map[string]string{}
	for _, a := range k.Actions {
		risks[a.Name] = a.Risk
	}
	if risks["balance"] != "" || risks["send_invoice"] != "ask" || risks["refund"] != "always" {
		t.Errorf("risks = %v", risks)
	}
	c, _ := Build("stripe", Settings{"token": "k"}, "")
	if d := c.Describe(); !strings.Contains(d, "send_invoice(id (in_…)) [the person approves it first]") || !strings.Contains(d, "[always needs the person's approval]") {
		t.Errorf("describe = %s", d)
	}
	if ActionOf("slack", nil) != "send" || ActionOf("slack", map[string]any{"action": "History"}) != "history" || ActionOf("nope", nil) != "" {
		t.Error("ActionOf")
	}
	if len(Categories) != 11 || Categories[2] != CatPersonal || Categories[6] != CatFinance || Categories[8] != CatMusic {
		t.Errorf("categories = %v", Categories)
	}
}

func TestNewKindsInTheCatalogue(t *testing.T) {
	for name, cat := range map[string]string{"stripe": CatFinance, "zendesk": CatProductivity, "ntfy": CatMessaging} {
		k, ok := LookupKind(name)
		if !ok || k.Category != cat || len(k.Actions) == 0 {
			t.Errorf("%s = %+v %v", name, k, ok)
		}
	}
	if k, _ := LookupKind("stripe"); !k.Inbound {
		t.Error("stripe takes webhooks")
	}
	if k, _ := LookupKind("x"); len(k.Fields) < 2 || k.Fields[len(k.Fields)-1].Name == "" {
		t.Error("x has a user id field")
	}
	if _, err := Build("stripe", Settings{}, ""); err == nil {
		t.Error("stripe needs its key")
	}
	if _, err := Build("ntfy", Settings{}, ""); err == nil {
		t.Error("ntfy needs a topic")
	}
}

func TestBluesky(t *testing.T) {
	session := `{"accessJwt":"jwt","did":"did:plc:me"}`
	settings := Settings{"handle": "me.bsky.social", "password": "pw"}
	post := `{"posts":[{"uri":"at://did:plc:x/app.bsky.feed.post/1","cid":"bafy1","record":{}}]}`
	replyPost := `{"posts":[{"uri":"at://did:plc:x/app.bsky.feed.post/2","cid":"bafy2","record":{"reply":{"root":{"uri":"at://did:plc:r/app.bsky.feed.post/0","cid":"bafy0"}}}}]}`
	for _, tc := range []struct {
		name    string
		args    map[string]any
		answers []string
		urls    []string // substrings of each request's URL, in order
		body    string   // a substring of the last request's body
		want    string
	}{
		{"post", map[string]any{"text": "hello"}, []string{session, `{"uri":"at://x"}`}, []string{"createSession", "createRecord"}, `"text":"hello"`, "done"},
		{"timeline", map[string]any{"action": "timeline"}, []string{session, `{"feed":[{"post":{"uri":"at://p","author":{"handle":"a.bsky"},"record":{"text":"hi"}}}]}`}, []string{"createSession", "getTimeline?limit=10"}, "", "- at://p | a.bsky | hi"},
		{"notifications", map[string]any{"action": "notifications", "limit": 3.0}, []string{session, `{"notifications":[{"uri":"at://n","reason":"mention","author":{"handle":"b.bsky"},"record":{"text":"@me look"}}]}`}, []string{"createSession", "listNotifications?limit=3"}, "", "- at://n | mention | b.bsky | @me look"},
		{"reply", map[string]any{"action": "reply", "uri": "at://did:plc:x/app.bsky.feed.post/1", "text": "thanks"}, []string{session, post, `{"uri":"at://r"}`}, []string{"createSession", "getPosts?uris=at%3A%2F%2Fdid", "createRecord"}, `"root":{"cid":"bafy1","uri":"at://did:plc:x/app.bsky.feed.post/1"}`, "done"},
		{"reply in a thread", map[string]any{"action": "reply", "uri": "at://did:plc:x/app.bsky.feed.post/2", "text": "yes"}, []string{session, replyPost, `{"uri":"at://r"}`}, []string{"createSession", "getPosts", "createRecord"}, `"root":{"cid":"bafy0","uri":"at://did:plc:r/app.bsky.feed.post/0"}`, "done"},
		{"like", map[string]any{"action": "like", "uri": "at://did:plc:x/app.bsky.feed.post/1"}, []string{session, post, `{"uri":"at://l"}`}, []string{"createSession", "getPosts", "createRecord"}, `"collection":"app.bsky.feed.like"`, "done"},
	} {
		cap := &capture{answer: tc.answers}
		cap.install(t)
		c, err := Build("bluesky", settings, "")
		if err != nil {
			t.Fatal(err)
		}
		out, err := c.Call(context.Background(), tc.args)
		if err != nil || !strings.Contains(out, tc.want) || len(cap.reqs) != len(tc.urls) {
			t.Errorf("%s: %q %v (%d requests)", tc.name, out, err, len(cap.reqs))
			continue
		}
		for i, u := range tc.urls {
			if !strings.Contains(cap.reqs[i].URL.String(), u) {
				t.Errorf("%s: request %d is %s, want %s", tc.name, i, cap.reqs[i].URL, u)
			}
		}
		if i := len(cap.reqs) - 1; i > 0 && cap.reqs[i].Header.Get("Authorization") != "Bearer jwt" || !strings.Contains(cap.bodies[len(cap.bodies)-1], tc.body) {
			t.Errorf("%s: last request %s body %s", tc.name, cap.reqs[len(cap.reqs)-1].Header, cap.bodies[len(cap.bodies)-1])
		}
	}
	// Refusals come before any login, and a failing login, an unknown post or an unknown action are told.
	for name, args := range map[string]map[string]any{"post needs text": {}, "reply needs text": {"action": "reply", "uri": "at://x"}, "reply needs a post": {"action": "reply", "text": "x"}, "like needs a post": {"action": "like"}, "unknown action": {"action": "delete"}} {
		cap := &capture{}
		cap.install(t)
		c, _ := Build("bluesky", settings, "")
		if _, err := c.Call(context.Background(), args); err == nil || len(cap.reqs) != 0 {
			t.Errorf("%s must be refused before a login: %v", name, err)
		}
	}
	for name, tc := range map[string]struct {
		answers []string
		code    int
		args    map[string]any
		want    string
	}{
		"login refused":    {[]string{`{"error":"AuthenticationRequired"}`}, 401, map[string]any{"text": "x"}, "bluesky login"},
		"no session token": {[]string{`{}`}, 200, map[string]any{"text": "x"}, "did not open a session"},
		"timeline fails":   {[]string{session, `boom`}, 0, map[string]any{"action": "timeline"}, ""},
		"unknown post":     {[]string{session, `{"posts":[]}`}, 0, map[string]any{"action": "like", "uri": "at://gone"}, "no such post"},
		"unreadable post":  {[]string{session, `nope`}, 0, map[string]any{"action": "like", "uri": "at://gone"}, "no such post"},
	} {
		cap := &capture{answer: tc.answers, code: tc.code}
		cap.install(t)
		if tc.code == 0 && name == "timeline fails" {
			old := httpDo
			n := 0
			httpDo = func(req *http.Request) (*http.Response, error) {
				n++
				if n == 2 {
					return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("down"))}, nil
				}
				return old(req)
			}
		}
		c, _ := Build("bluesky", settings, "")
		if _, err := c.Call(context.Background(), tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Notifications and the like fail the same way when the service does; Describe names the actions.
	for _, action := range []string{"notifications"} {
		cap := &capture{answer: []string{session}}
		cap.install(t)
		old := httpDo
		n := 0
		httpDo = func(req *http.Request) (*http.Response, error) {
			n++
			if n == 2 {
				return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("down"))}, nil
			}
			return old(req)
		}
		c, _ := Build("bluesky", settings, "")
		if _, err := c.Call(context.Background(), map[string]any{"action": action}); err == nil {
			t.Errorf("%s fails when the service does", action)
		}
		if d := c.Describe(); !strings.Contains(d, "reply(uri (at://…), text (what to say))") || !strings.Contains(d, "[the person approves it first]") {
			t.Errorf("describe = %s", d)
		}
	}
}
