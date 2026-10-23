package connector

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// READING, REPLYING AND WATCHING.
//
// A kitty that watches something (a mailbox, mentions, unpaid invoices, support tickets) needs
// to read it, and one that answers needs to reply. The actions here add that to the kinds that
// only wrote before, and listing renders what comes back as one short line per item for the
// model to read, instead of a wall of JSON.

// extraActions are the actions added to a kind besides the ones its builder lists: its reads,
// and the request that reaches the rest of its API.
func extraActions(kind string) map[string]actionFn {
	m := readFns(kind)
	if spec, ok := apis[kind]; ok {
		if m == nil {
			m = map[string]actionFn{}
		}
		m["request"] = apiRequest(spec)
	}
	return m
}

// readFns are the reading actions of a kind.
func readFns(kind string) map[string]actionFn {
	switch kind {
	case "slack":
		return map[string]actionFn{"history": slackHistory}
	case "telegram":
		return map[string]actionFn{"updates": telegramUpdates}
	case "mastodon":
		return mastodonReads()
	case "x":
		return xReads()
	case "linkedin":
		return linkedinReads()
	case "github":
		return githubReads()
	case "jira":
		return map[string]actionFn{"search": jiraSearch}
	case "google-workspace":
		return merged(googleReads(), googlePersonal())
	case "microsoft-365":
		return merged(microsoftReads(), microsoftPersonal())
	}
	return nil
}

func merged(a, b map[string]actionFn) map[string]actionFn {
	for k, v := range b {
		a[k] = v
	}
	return a
}

// readRender says an answer of a reading action as lines; ok is false for the actions that
// render the usual way.
func readRender(kind, action string, data []byte) (string, bool) {
	if action == "request" {
		return requestAnswer(data), true
	}
	switch kind + "." + action {
	case "slack.history":
		return listing(data, "messages", 20, "user", "text", "ts"), true
	case "telegram.updates":
		return listing(data, "result", 20, "message.chat.id", "message.from.username", "message.text"), true
	case "mastodon.timeline":
		return listing(data, "", 20, "id", "account.acct", "content"), true
	case "mastodon.mentions":
		return listing(data, "", 20, "status.id", "account.acct", "status.content"), true
	case "x.mentions":
		return listing(data, "data", 20, "id", "author_id", "text"), true
	case "github.list_issues":
		return listing(data, "", 20, "number", "state", "title", "user.login", "labels.0.name"), true
	case "github.get_issue":
		return issueText(data), true
	case "jira.search":
		return listing(data, "issues", 20, "key", "fields.status.name", "fields.summary", "fields.assignee.displayName"), true
	case "google-workspace.read_mail":
		return gmailRead(data), true
	case "google-workspace.list_events":
		return listing(data, "items", 20, "id", "start.dateTime", "start.date", "summary"), true
	case "microsoft-365.list_mail":
		return listing(data, "value", 20, "id", "from.emailAddress.address", "subject", "receivedDateTime", "bodyPreview"), true
	case "microsoft-365.read_mail":
		return outlookRead(data), true
	case "microsoft-365.list_events":
		return listing(data, "value", 20, "id", "start.dateTime", "subject"), true
	case "google-workspace.tasks":
		return listing(data, "items", 30, "id", "title", "due", "notes"), true
	case "stripe.balance":
		return stripeBalance(data), true
	case "stripe.list_invoices":
		return listing(data, "data", 20, "id", "number", "status", "customer_email", "amount_due:cents", "currency", "due_date:date"), true
	case "stripe.list_payments":
		return listing(data, "data", 20, "id", "status", "amount:cents", "currency", "receipt_email"), true
	case "zendesk.list_tickets":
		return listing(data, "results", 20, "id", "status", "priority", "subject"), true
	case "zendesk.read_ticket":
		return listing(data, "comments", 20, "author_id", "plain_body"), true
	}
	return "", false
}

// readCheck turns an answer that says it failed (Slack answers 200 with ok: false) into an error.
func readCheck(kind string, data []byte) error {
	if kind != "slack" {
		return nil
	}
	var m struct {
		OK    *bool  `json:"ok"`
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &m) == nil && m.OK != nil && !*m.OK {
		return errors.New("slack: " + m.Error)
	}
	return nil
}

// ── listing ────────────────────────────────────────────────────────────────────────────────

var tagRe = regexp.MustCompile(`<[^>]*>`)

// getPath follows a dotted path through decoded JSON; a number steps into an array.
func getPath(v any, path string) any {
	for _, p := range strings.Split(path, ".") {
		switch t := v.(type) {
		case map[string]any:
			v = t[p]
		case []any:
			i, err := strconv.Atoi(p)
			if err != nil || i < 0 || i >= len(t) {
				return nil
			}
			v = t[i]
		default:
			return nil
		}
	}
	return v
}

// cell renders one value: text without markup, money in cents, dates from unix seconds.
func cell(v any, mod string) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return clip(strings.Join(strings.Fields(html.UnescapeString(tagRe.ReplaceAllString(t, " "))), " "), 160)
	case float64:
		switch mod {
		case "cents":
			return strconv.FormatFloat(t/100, 'f', 2, 64)
		case "date":
			return time.Unix(int64(t), 0).UTC().Format("2006-01-02")
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	}
	data, _ := json.Marshal(v)
	return clip(string(data), 120)
}

// listing renders a JSON list as lines, one per item, from the dotted paths in cols (a path may
// end in :cents or :date). root names where the list is in an object; empty when the answer is
// the list itself.
func listing(data []byte, root string, max int, cols ...string) string {
	var v any
	if json.Unmarshal(data, &v) != nil {
		return clip(string(data), 500)
	}
	if root != "" {
		v = getPath(v, root)
	}
	items, _ := v.([]any)
	if len(items) == 0 {
		return "(nothing)"
	}
	var lines []string
	for i, it := range items {
		if i == max {
			lines = append(lines, fmt.Sprintf("… and %d more", len(items)-max))
			break
		}
		var parts []string
		for _, c := range cols {
			path, mod, _ := strings.Cut(c, ":")
			if s := cell(getPath(it, path), mod); s != "" {
				parts = append(parts, s)
			}
		}
		lines = append(lines, "- "+strings.Join(parts, " | "))
	}
	return strings.Join(lines, "\n")
}

// limitOf is how many items a reading action asks for: limit, 10 by default, at most 30.
func limitOf(a map[string]any) int {
	n, err := strconv.Atoi(str(a, "limit"))
	if err != nil || n < 1 {
		return 10
	}
	if n > 30 {
		return 30
	}
	return n
}

// ── messaging ──────────────────────────────────────────────────────────────────────────────

func slackHistory(s Settings, a map[string]any) (request, error) {
	if s["url"] != "" && s["token"] == "" && s["client_id"] == "" {
		return request{}, errors.New("reading needs a bot token or a sign-in; a webhook only posts")
	}
	ch := pick(a, s, "channel")
	if ch == "" {
		return request{}, errors.New("channel is missing")
	}
	return request{method: "GET", url: "https://slack.com/api/conversations.history?channel=" + url.QueryEscape(ch) + "&limit=" + strconv.Itoa(limitOf(a)), headers: bearer(s["token"])}, nil
}

func telegramUpdates(s Settings, a map[string]any) (request, error) {
	return request{method: "GET", url: base(s, "https://api.telegram.org") + "/bot" + s["token"] + "/getUpdates?limit=" + strconv.Itoa(limitOf(a))}, nil
}

// ── social media ───────────────────────────────────────────────────────────────────────────

func mastodonReads() map[string]actionFn {
	status := func(verb string) actionFn {
		return func(s Settings, a map[string]any) (request, error) {
			id, err := need(a, "id", "to", "status")
			if err != nil {
				return request{}, err
			}
			return request{method: "POST", url: base(s, "") + "/api/v1/statuses/" + url.PathEscape(id) + "/" + verb, headers: bearer(s["token"])}, nil
		}
	}
	return map[string]actionFn{
		"timeline": func(s Settings, a map[string]any) (request, error) {
			return request{method: "GET", url: base(s, "") + "/api/v1/timelines/home?limit=" + strconv.Itoa(limitOf(a)), headers: bearer(s["token"])}, nil
		},
		"mentions": func(s Settings, a map[string]any) (request, error) {
			return request{method: "GET", url: base(s, "") + "/api/v1/notifications?types%5B%5D=mention&limit=" + strconv.Itoa(limitOf(a)), headers: bearer(s["token"])}, nil
		},
		"reply": func(s Settings, a map[string]any) (request, error) {
			id, err := need(a, "id", "to")
			if err != nil {
				return request{}, err
			}
			text, err := need(a, "text", "status")
			if err != nil {
				return request{}, err
			}
			return request{method: "POST", url: base(s, "") + "/api/v1/statuses", headers: bearer(s["token"]), body: map[string]any{"status": text, "in_reply_to_id": id}}, nil
		},
		"like":  status("favourite"),
		"boost": status("reblog"),
	}
}

func xReads() map[string]actionFn {
	user := func(s Settings) (string, error) {
		id := xUserID(s)
		if id == "" {
			return "", errors.New("this needs the account's numeric user id in the connector's settings (user_id); signing in, or the OAuth 1.0 keys, fill it")
		}
		return id, nil
	}
	return map[string]actionFn{
		"mentions": func(s Settings, a map[string]any) (request, error) {
			id, err := user(s)
			if err != nil {
				return request{}, err
			}
			return request{method: "GET", url: "https://api.x.com/2/users/" + url.PathEscape(id) + "/mentions?tweet.fields=author_id,created_at&max_results=" + strconv.Itoa(max(5, limitOf(a))), headers: bearer(s["token"])}, nil
		},
		"reply": func(s Settings, a map[string]any) (request, error) {
			id, err := need(a, "id", "to")
			if err != nil {
				return request{}, err
			}
			text, err := need(a, "text")
			if err != nil {
				return request{}, err
			}
			return request{method: "POST", url: "https://api.x.com/2/tweets", headers: bearer(s["token"]), body: map[string]any{"text": text, "reply": map[string]any{"in_reply_to_tweet_id": id}}}, nil
		},
		"like": func(s Settings, a map[string]any) (request, error) {
			uid, err := user(s)
			if err != nil {
				return request{}, err
			}
			id, err := need(a, "id", "to")
			if err != nil {
				return request{}, err
			}
			return request{method: "POST", url: "https://api.x.com/2/users/" + url.PathEscape(uid) + "/likes", headers: bearer(s["token"]), body: map[string]any{"tweet_id": id}}, nil
		},
	}
}

func linkedinReads() map[string]actionFn {
	social := func(s Settings, a map[string]any, verb string, body func(urn string) map[string]any) (request, error) {
		urn, err := need(a, "post", "id", "to")
		if err != nil {
			return request{}, err
		}
		headers := bearer(s["token"])
		headers["X-Restli-Protocol-Version"] = "2.0.0"
		return request{method: "POST", url: "https://api.linkedin.com/v2/socialActions/" + url.PathEscape(urn) + "/" + verb, headers: headers, body: body(urn)}, nil
	}
	return map[string]actionFn{
		"comment": func(s Settings, a map[string]any) (request, error) {
			text, err := need(a, "text")
			if err != nil {
				return request{}, err
			}
			return social(s, a, "comments", func(string) map[string]any {
				return map[string]any{"actor": s["author"], "message": map[string]any{"text": text}}
			})
		},
		"like": func(s Settings, a map[string]any) (request, error) {
			return social(s, a, "likes", func(urn string) map[string]any {
				return map[string]any{"actor": s["author"], "object": urn}
			})
		},
	}
}

// ── git and tickets ────────────────────────────────────────────────────────────────────────

func githubReads() map[string]actionFn {
	gh := func(s Settings) map[string]string {
		h := bearer(s["token"])
		h["Accept"] = "application/vnd.github+json"
		return h
	}
	return map[string]actionFn{
		"list_issues": func(s Settings, a map[string]any) (request, error) {
			repo, err := repoOf(a, s, "repo")
			if err != nil {
				return request{}, err
			}
			state := str(a, "state")
			if state != "closed" && state != "all" {
				state = "open"
			}
			return request{method: "GET", url: "https://api.github.com/repos/" + repo + "/issues?state=" + state + "&per_page=" + strconv.Itoa(limitOf(a)), headers: gh(s)}, nil
		},
		"get_issue": func(s Settings, a map[string]any) (request, error) {
			repo, err := repoOf(a, s, "repo")
			if err != nil {
				return request{}, err
			}
			n, err := need(a, "number")
			if err != nil {
				return request{}, err
			}
			return request{method: "GET", url: "https://api.github.com/repos/" + repo + "/issues/" + url.PathEscape(n), headers: gh(s)}, nil
		},
	}
}

// issueText renders one issue or pull request.
func issueText(data []byte) string {
	var v any
	if json.Unmarshal(data, &v) != nil {
		return clip(string(data), 2000)
	}
	head := fmt.Sprintf("#%s %s (%s) by %s", cell(getPath(v, "number"), ""), cell(getPath(v, "title"), ""), cell(getPath(v, "state"), ""), cell(getPath(v, "user.login"), ""))
	body, _ := getPath(v, "body").(string)
	return head + "\n" + clip(body, 3000)
}

func jiraSearch(s Settings, a map[string]any) (request, error) {
	jql := str(a, "jql", "query")
	if jql == "" {
		jql = "project = " + pick(a, s, "project") + " AND statusCategory != Done ORDER BY updated DESC"
	}
	root, basic := jiraTarget(s)
	return request{method: "GET", url: root + "/rest/api/3/search/jql?fields=summary,status,assignee&maxResults=" + strconv.Itoa(limitOf(a)) + "&jql=" + url.QueryEscape(jql), basic: basic}, nil
}

// ── mail and calendar ──────────────────────────────────────────────────────────────────────

const gmail = "https://gmail.googleapis.com/gmail/v1/users/me/messages"

func googleReads() map[string]actionFn {
	return map[string]actionFn{
		"list_mail": func(s Settings, a map[string]any) (request, error) {
			u := gmail + "?maxResults=" + strconv.Itoa(limitOf(a))
			if q := str(a, "query", "q"); q != "" {
				u += "&q=" + url.QueryEscape(q)
			}
			return request{method: "GET", url: u, follow: gmailHeaders}, nil
		},
		"read_mail": func(s Settings, a map[string]any) (request, error) {
			id, err := need(a, "id")
			if err != nil {
				return request{}, err
			}
			return request{method: "GET", url: gmail + "/" + url.PathEscape(id) + "?format=full"}, nil
		},
		"list_events": func(s Settings, a map[string]any) (request, error) {
			from := time.Now().UTC().Format(time.RFC3339)
			if v := str(a, "from"); v != "" {
				from = v
			}
			return request{method: "GET", url: "https://www.googleapis.com/calendar/v3/calendars/primary/events?singleEvents=true&orderBy=startTime&timeMin=" + url.QueryEscape(from) + "&maxResults=" + strconv.Itoa(limitOf(a))}, nil
		},
	}
}

// gmailHeaders turns the ids Gmail lists into one line per message (from, subject, date and
// the start of the text): the list itself says nothing but ids.
func gmailHeaders(ctx context.Context, raw []byte, headers map[string]string) (string, error) {
	var list struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if json.Unmarshal(raw, &list) != nil || len(list.Messages) == 0 {
		return "(no mail)", nil
	}
	var lines []string
	for i, m := range list.Messages {
		if i == 10 {
			lines = append(lines, fmt.Sprintf("… and %d more", len(list.Messages)-10))
			break
		}
		data, err := do(ctx, request{method: "GET", headers: headers, url: gmail + "/" + url.PathEscape(m.ID) + "?format=metadata&metadataHeaders=From&metadataHeaders=Subject&metadataHeaders=Date"})
		if err != nil {
			lines = append(lines, "- "+m.ID+" | (unreadable: "+err.Error()+")")
			continue
		}
		var msg struct {
			Snippet string `json:"snippet"`
			Payload struct {
				Headers []struct{ Name, Value string } `json:"headers"`
			} `json:"payload"`
		}
		_ = json.Unmarshal(data, &msg)
		h := map[string]string{}
		for _, kv := range msg.Payload.Headers {
			h[strings.ToLower(kv.Name)] = kv.Value
		}
		lines = append(lines, "- "+m.ID+" | "+cell(h["from"], "")+" | "+cell(h["subject"], "")+" | "+cell(h["date"], "")+" | "+cell(msg.Snippet, ""))
	}
	return strings.Join(lines, "\n"), nil
}

// gmailRead renders one message: its headers and its text.
func gmailRead(data []byte) string {
	var msg struct {
		Snippet string `json:"snippet"`
		Payload mailPart
	}
	if json.Unmarshal(data, &msg) != nil {
		return clip(string(data), 2000)
	}
	h := map[string]string{}
	for _, kv := range msg.Payload.Headers {
		h[strings.ToLower(kv.Name)] = kv.Value
	}
	body := msg.Payload.text()
	if body == "" {
		body = msg.Snippet
	}
	return "from: " + h["from"] + "\nto: " + h["to"] + "\nsubject: " + h["subject"] + "\ndate: " + h["date"] + "\n\n" + clip(body, 6000)
}

// mailPart is a part of a Gmail message.
type mailPart struct {
	MimeType string                         `json:"mimeType"`
	Headers  []struct{ Name, Value string } `json:"headers"`
	Body     struct {
		Data string `json:"data"`
	} `json:"body"`
	Parts []mailPart `json:"parts"`
}

// text finds the plain text of a message: its own, or the first text/plain part below it.
func (p mailPart) text() string {
	if strings.HasPrefix(p.MimeType, "text/plain") && p.Body.Data != "" {
		if b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(p.Body.Data, "=")); err == nil {
			return string(b)
		}
	}
	for _, c := range p.Parts {
		if t := c.text(); t != "" {
			return t
		}
	}
	return ""
}

func microsoftReads() map[string]actionFn {
	graph := "https://graph.microsoft.com/v1.0/me"
	return map[string]actionFn{
		"list_mail": func(s Settings, a map[string]any) (request, error) {
			u := graph + "/messages?$select=id,from,subject,receivedDateTime,bodyPreview&$orderby=receivedDateTime%20desc&$top=" + strconv.Itoa(limitOf(a))
			if q := str(a, "query", "q"); q != "" {
				u = graph + "/messages?$select=id,from,subject,receivedDateTime,bodyPreview&$top=" + strconv.Itoa(limitOf(a)) + "&$search=" + url.QueryEscape(`"`+q+`"`)
			}
			return request{method: "GET", url: u}, nil
		},
		"read_mail": func(s Settings, a map[string]any) (request, error) {
			id, err := need(a, "id")
			if err != nil {
				return request{}, err
			}
			return request{method: "GET", url: graph + "/messages/" + url.PathEscape(id) + "?$select=from,subject,receivedDateTime,body", headers: map[string]string{"Prefer": `outlook.body-content-type="text"`}}, nil
		},
		"list_events": func(s Settings, a map[string]any) (request, error) {
			return request{method: "GET", url: graph + "/events?$select=id,subject,start&$orderby=start/dateTime&$top=" + strconv.Itoa(limitOf(a))}, nil
		},
	}
}

func outlookRead(data []byte) string {
	var v any
	if json.Unmarshal(data, &v) != nil {
		return clip(string(data), 2000)
	}
	body, _ := getPath(v, "body.content").(string)
	return "from: " + cell(getPath(v, "from.emailAddress.address"), "") + "\nsubject: " + cell(getPath(v, "subject"), "") + "\ndate: " + cell(getPath(v, "receivedDateTime"), "") + "\n\n" + clip(body, 6000)
}

// ── finance ────────────────────────────────────────────────────────────────────────────────

// form encodes key, value pairs as a form body, the way Stripe wants them.
func form(kv ...string) []byte {
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] != "" {
			v.Add(kv[i], kv[i+1])
		}
	}
	return []byte(v.Encode())
}

func stripeHeaders(s Settings) map[string]string { return bearer(s["token"]) }

func makeStripe(s Settings) Connector {
	return newAPI("stripe", s, map[string]actionFn{
		"balance": func(s Settings, a map[string]any) (request, error) {
			return request{method: "GET", url: "https://api.stripe.com/v1/balance", headers: stripeHeaders(s)}, nil
		},
		"list_invoices": func(s Settings, a map[string]any) (request, error) {
			status := str(a, "status")
			if status == "" {
				status = "open"
			}
			u := "https://api.stripe.com/v1/invoices?status=" + url.QueryEscape(status) + "&limit=" + strconv.Itoa(limitOf(a))
			if str(a, "overdue") == "true" {
				u += "&due_date%5Blt%5D=" + strconv.FormatInt(time.Now().Unix(), 10)
			}
			return request{method: "GET", url: u, headers: stripeHeaders(s)}, nil
		},
		"list_payments": func(s Settings, a map[string]any) (request, error) {
			return request{method: "GET", url: "https://api.stripe.com/v1/payment_intents?limit=" + strconv.Itoa(limitOf(a)), headers: stripeHeaders(s)}, nil
		},
		"send_invoice": func(s Settings, a map[string]any) (request, error) {
			id, err := need(a, "id", "invoice")
			if err != nil {
				return request{}, err
			}
			return request{method: "POST", url: "https://api.stripe.com/v1/invoices/" + url.PathEscape(id) + "/send", headers: stripeHeaders(s), raw: []byte{}, ctype: "application/x-www-form-urlencoded"}, nil
		},
		"refund": func(s Settings, a map[string]any) (request, error) {
			pi, err := need(a, "payment", "payment_intent", "id")
			if err != nil {
				return request{}, err
			}
			return request{method: "POST", url: "https://api.stripe.com/v1/refunds", headers: stripeHeaders(s), raw: form("payment_intent", pi, "amount", str(a, "amount")), ctype: "application/x-www-form-urlencoded"}, nil
		},
	}, "balance")
}

// stripeBalance says what is available and what is pending, per currency.
func stripeBalance(data []byte) string {
	var v any
	if json.Unmarshal(data, &v) != nil {
		return clip(string(data), 500)
	}
	var lines []string
	for _, kind := range []string{"available", "pending"} {
		items, _ := getPath(v, kind).([]any)
		for _, it := range items {
			lines = append(lines, kind+": "+cell(getPath(it, "amount"), "cents")+" "+strings.ToUpper(cell(getPath(it, "currency"), "")))
		}
	}
	if len(lines) == 0 {
		return "(no balance)"
	}
	return strings.Join(lines, "\n")
}

// ── support ────────────────────────────────────────────────────────────────────────────────

func makeZendesk(s Settings) Connector {
	root := "https://" + s["subdomain"] + ".zendesk.com/api/v2"
	// An agent's email and API token, or the signed-in account (its token goes as a bearer).
	var basic [2]string
	if s["token"] != "" {
		basic = [2]string{s["email"] + "/token", s["token"]}
	}
	return newAPI("zendesk", s, map[string]actionFn{
		"list_tickets": func(s Settings, a map[string]any) (request, error) {
			q := str(a, "query")
			if q == "" {
				q = "type:ticket status<solved"
			}
			return request{method: "GET", url: root + "/search.json?sort_by=updated_at&sort_order=desc&query=" + url.QueryEscape(q), basic: basic}, nil
		},
		"read_ticket": func(s Settings, a map[string]any) (request, error) {
			id, err := need(a, "id", "ticket")
			if err != nil {
				return request{}, err
			}
			return request{method: "GET", url: root + "/tickets/" + url.PathEscape(id) + "/comments.json", basic: basic}, nil
		},
		"reply": func(s Settings, a map[string]any) (request, error) {
			id, err := need(a, "id", "ticket")
			if err != nil {
				return request{}, err
			}
			text, err := need(a, "text", "body")
			if err != nil {
				return request{}, err
			}
			public := str(a, "public") != "false"
			return request{method: "PUT", url: root + "/tickets/" + url.PathEscape(id) + ".json", basic: basic, body: map[string]any{"ticket": map[string]any{"comment": map[string]any{"body": text, "public": public}}}}, nil
		},
	}, "list_tickets")
}

// ── push notifications ─────────────────────────────────────────────────────────────────────

func makeNtfy(s Settings) Connector {
	return newAPI("ntfy", s, map[string]actionFn{"send": func(s Settings, a map[string]any) (request, error) {
		text, err := need(a, "text", "message")
		if err != nil {
			return request{}, err
		}
		body := map[string]any{"topic": s["topic"], "message": text}
		if t := str(a, "title"); t != "" {
			body["title"] = t
		}
		if p := str(a, "priority"); p != "" {
			if n, err := strconv.Atoi(p); err == nil && n >= 1 && n <= 5 {
				body["priority"] = n
			}
		}
		if c := str(a, "click"); c != "" {
			body["click"] = c
		}
		headers := map[string]string{}
		if s["token"] != "" {
			headers = bearer(s["token"])
		}
		return request{method: "POST", url: base(s, "https://ntfy.sh"), headers: headers, body: body}, nil
	}}, "send")
}

// moreKinds are the kinds of the catalogue that came with reading and watching.
func moreKinds() []Kind {
	return []Kind{
		{Name: "stripe", Category: CatFinance, Label: "Stripe", Help: "balance, invoices and payments in Stripe; watch what is owed and what failed",
			Fields: []Field{f("token", "Secret or restricted key", "sk_… or rk_…; a read-only restricted key is enough to watch", true, true)},
			Actions: []Action{
				{Name: "balance", Help: "available and pending balance per currency"},
				{Name: "list_invoices", Help: "invoices by status (open by default), overdue ones with overdue: true", Args: []string{"status (open, paid, void, draft, uncollectible)", "overdue (true)", fLimit}},
				{Name: "list_payments", Help: "the latest payments", Args: []string{fLimit}},
				{Name: "send_invoice", Help: "email an open invoice to its customer", Args: []string{"id (in_…)"}},
				{Name: "refund", Help: "refund a payment, all of it or an amount in cents", Args: []string{"payment (pi_…)", "amount (cents, optional)"}},
			}, make: makeStripe},
		{Name: "zendesk", Category: CatProductivity, Label: "Zendesk", Help: "support tickets in Zendesk: watch what customers write and answer them",
			Fields: signIn([]Field{f("subdomain", "Subdomain", "acme for acme.zendesk.com", true, false), f("email", "Agent email", "with an API token", false, false)},
				f("token", "API token", "", false, true), "Admin Center → Apps and integrations → APIs → OAuth clients"),
			Actions: []Action{
				{Name: "list_tickets", Help: "tickets matching a search (the unsolved ones by default)", Args: []string{"query (Zendesk search, optional)"}},
				{Name: "read_ticket", Help: "the comments of a ticket", Args: []string{"id"}},
				{Name: "reply", Help: "answer a ticket", Args: []string{"id", fText, "public (false for an internal note)"}},
			}, make: makeZendesk, oauth: "zendesk"},
		{Name: "ntfy", Category: CatMessaging, Label: "ntfy (push)", Help: "push notifications to a phone or a browser through ntfy.sh or a server of your own",
			Fields:  []Field{f("topic", "Topic", "a hard-to-guess name: anyone who knows it can read it", true, false), f("url", "Server", "https://ntfy.sh when empty", false, false), f("token", "Access token", "for a protected topic", false, true)},
			Actions: []Action{{Name: "send", Help: "push a message", Args: []string{fText, "title (optional)", "priority (1-5, optional)", "click (a URL to open, optional)"}}}, make: makeNtfy},
	}
}

// readActions are the actions the reading kinds add to the ones they had.
func readActions(kind string) []Action {
	switch kind {
	case "slack":
		return []Action{{Name: "history", Help: "the latest messages of a channel", Args: []string{"channel (optional)", fLimit}}}
	case "telegram":
		return []Action{{Name: "updates", Help: "the latest messages the bot received", Args: []string{fLimit}}}
	case "mastodon":
		return []Action{
			{Name: "timeline", Help: "the home timeline", Args: []string{fLimit}},
			{Name: "mentions", Help: "who mentioned the account", Args: []string{fLimit}},
			{Name: "reply", Help: "answer a status", Args: []string{"id", fText}},
			{Name: "like", Help: "favourite a status", Args: []string{"id"}},
			{Name: "boost", Help: "boost a status", Args: []string{"id"}},
		}
	case "bluesky":
		return []Action{
			{Name: "timeline", Help: "the home timeline", Args: []string{fLimit}},
			{Name: "notifications", Help: "mentions, replies and likes", Args: []string{fLimit}},
			{Name: "reply", Help: "answer a post", Args: []string{"uri (at://…)", fText}},
			{Name: "like", Help: "like a post", Args: []string{"uri (at://…)"}},
		}
	case "x":
		return []Action{
			{Name: "mentions", Help: "recent mentions of the account", Args: []string{fLimit}},
			{Name: "reply", Help: "answer a post", Args: []string{"id", fText}},
			{Name: "like", Help: "like a post", Args: []string{"id"}},
		}
	case "linkedin":
		return []Action{
			{Name: "comment", Help: "comment on a post", Args: []string{"post (urn:li:share:…)", fText}},
			{Name: "like", Help: "like a post", Args: []string{"post (urn:li:share:…)"}},
		}
	case "github":
		return []Action{
			{Name: "list_issues", Help: "issues and pull requests", Args: []string{fRepo, "state (open, closed, all)", fLimit}},
			{Name: "get_issue", Help: "read one issue or pull request", Args: []string{fRepo, "number"}},
		}
	case "jira":
		return []Action{{Name: "search", Help: "issues by JQL (the open ones of the project by default)", Args: []string{"jql (optional)", fLimit}}}
	case "google-workspace":
		return []Action{
			{Name: "list_mail", Help: "the latest mail (from, subject, date, start of the text), searchable like Gmail", Args: []string{"query (optional, Gmail search)", fLimit}},
			{Name: "read_mail", Help: "read one message", Args: []string{"id"}},
			{Name: "list_events", Help: "upcoming calendar events", Args: []string{"from (RFC 3339, optional)", fLimit}},
			{Name: "contacts", Help: "find people in Google Contacts", Args: []string{"query (optional)", fLimit}},
			{Name: "add_contact", Help: "add a person to Google Contacts", Args: []string{"name", "email (optional)", "phone (optional)", "company (optional)"}},
			{Name: "tasks", Help: "the Google Tasks still to do, with their ids", Args: []string{fLimit}},
			{Name: "add_task", Help: "add a task", Args: []string{fTitle, "due (optional)", "notes (optional)"}},
			{Name: "complete_task", Help: "tick a task off", Args: []string{"id"}},
		}
	case "microsoft-365":
		return []Action{
			{Name: "list_mail", Help: "the latest mail, searchable", Args: []string{"query (optional)", fLimit}},
			{Name: "read_mail", Help: "read one message", Args: []string{"id"}},
			{Name: "list_events", Help: "upcoming calendar events", Args: []string{fLimit}},
			{Name: "contacts", Help: "find people in Outlook contacts", Args: []string{"query (optional)", fLimit}},
			{Name: "add_contact", Help: "add a person to Outlook contacts", Args: []string{"name", "email (optional)", "phone (optional)", "company (optional)"}},
			{Name: "tasks", Help: "the Microsoft To Do tasks still to do, with their ids", Args: []string{fLimit}},
			{Name: "add_task", Help: "add a task to To Do", Args: []string{fTitle, "due (optional)", "notes (optional)"}},
			{Name: "complete_task", Help: "tick a task off", Args: []string{"id"}},
		}
	}
	return nil
}

// readFields are the settings the reading actions need besides the ones a kind had.
func readFields(kind string) []Field {
	if kind == "x" {
		return []Field{f("user_id", "User id", "the account's numeric id, for mentions and likes; signing in or the OAuth 1.0 keys fill it", false, false)}
	}
	return nil
}

const fLimit = "limit (how many, 10 by default)"
