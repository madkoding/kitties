package connector

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// Checker is a connector that can say whether its account works without doing anything: one
// request that only reads (who the account is), answered with the account's name when the
// service says it.
type Checker interface {
	Check(ctx context.Context) (string, error)
}

// ErrNoCheck says a connector has no request that tests it without acting (a webhook only
// posts): it is tried on its first use.
var ErrNoCheck = errors.New("this connection cannot be tested without acting: it is tried on its first use")

// check is the reading request of a kind, and where its answer names the account (dotted
// paths into the JSON, the first one found wins).
type check struct {
	req  func(s Settings) request
	name []string
}

func get(u string, headers map[string]string) request {
	return request{method: "GET", url: u, headers: headers}
}

// checks are the kinds that can be tested, by name.
var checks = map[string]check{
	"x":      {func(s Settings) request { return get("https://api.x.com/2/users/me", bearer(s["token"])) }, []string{"data.username"}},
	"github": {func(s Settings) request { return get("https://api.github.com/user", bearer(s["token"])) }, []string{"login"}},
	"gitlab": {func(s Settings) request { return get(base(s, "https://gitlab.com")+"/api/v4/user", bearer(s["token"])) }, []string{"username"}},
	"mastodon": {func(s Settings) request {
		return get(base(s, "")+"/api/v1/accounts/verify_credentials", bearer(s["token"]))
	}, []string{"acct"}},
	"notion": {func(s Settings) request {
		h := bearer(s["token"])
		h["Notion-Version"] = "2022-06-28"
		return get("https://api.notion.com/v1/users/me", h)
	}, []string{"bot.workspace_name", "name"}},
	"todoist":  {func(s Settings) request { return get("https://api.todoist.com/rest/v2/projects", bearer(s["token"])) }, nil},
	"airtable": {func(s Settings) request { return get("https://api.airtable.com/v0/meta/whoami", bearer(s["token"])) }, []string{"email", "id"}},
	"figma": {func(s Settings) request {
		return get("https://api.figma.com/v1/me", keyHeader("X-Figma-Token", s["token"]))
	}, []string{"handle", "email"}},
	"spotify":       {func(s Settings) request { return get("https://api.spotify.com/v1/me", bearer(s["token"])) }, []string{"display_name", "id"}},
	"linkedin":      {func(s Settings) request { return get("https://api.linkedin.com/v2/userinfo", bearer(s["token"])) }, []string{"name"}},
	"microsoft-365": {func(s Settings) request { return get("https://graph.microsoft.com/v1.0/me", bearer(s["token"])) }, []string{"userPrincipalName", "displayName"}},
	"google-workspace": {func(s Settings) request {
		return get("https://gmail.googleapis.com/gmail/v1/users/me/profile", bearer(s["token"]))
	}, []string{"emailAddress"}},
	"home-assistant": {func(s Settings) request { return get(strings.TrimRight(s["url"], "/")+"/api/", bearer(s["token"])) }, nil},
	"telegram": {func(s Settings) request {
		return request{method: "GET", url: base(s, "https://api.telegram.org") + "/bot" + s["token"] + "/getMe", plain: true}
	}, []string{"result.username"}},
	// A Discord webhook says what it is to a GET, without posting.
	"discord": {func(s Settings) request { return request{method: "GET", url: s["url"], plain: true} }, []string{"name"}},
	"stripe":  {func(s Settings) request { return get("https://api.stripe.com/v1/balance", stripeHeaders(s)) }, nil},
	"dropbox": {func(s Settings) request {
		return request{method: "POST", url: "https://api.dropboxapi.com/2/users/get_current_account", headers: bearer(s["token"]), raw: []byte("null"), ctype: "application/json"}
	}, []string{"email", "name.display_name"}},
	"linear": {func(s Settings) request {
		return request{method: "POST", url: "https://api.linear.app/graphql", headers: map[string]string{"Authorization": s["token"]}, body: map[string]any{"query": "{ viewer { name } }"}}
	}, []string{"data.viewer.name"}},
	"zendesk": {func(s Settings) request {
		r := get("https://"+s["subdomain"]+".zendesk.com/api/v2/users/me.json", nil)
		if s["token"] != "" {
			r.basic = [2]string{s["email"] + "/token", s["token"]}
		}
		return r
	}, []string{"user.email"}},
	"jira": {func(s Settings) request {
		root, basic := jiraTarget(s)
		r := get(root+"/rest/api/3/myself", nil)
		r.basic = basic
		return r
	}, []string{"emailAddress", "displayName"}},
	"facebook": {func(s Settings) request {
		return get(graphRoot(s["token"])+"/me?fields=name", bearer(s["token"]))
	}, []string{"name"}},
	"instagram": {func(s Settings) request {
		return get(graphRoot(s["token"])+"/me?fields=username,name", bearer(s["token"]))
	}, []string{"username", "name"}},
	"slack": {func(s Settings) request {
		return request{method: "POST", url: "https://slack.com/api/auth.test", headers: bearer(s["token"])}
	}, []string{"user", "team"}},
}

// Check runs the kind's reading request with the account the connector has (a sign-in, keys
// or a token), and names the account when the answer does.
func (a *api) Check(ctx context.Context) (string, error) {
	c, ok := checks[a.kind.Name]
	// A Slack webhook only posts, and when there is one it is what the messages go through:
	// there is nothing to read with it.
	if !ok || (a.kind.Name == "slack" && strings.TrimSpace(a.s["url"]) != "") {
		return "", ErrNoCheck
	}
	req := c.req(a.s)
	raw, err := a.send(ctx, &req)
	if err != nil {
		return "", err
	}
	if err := readCheck(a.kind.Name, raw); err != nil {
		return "", err
	}
	// Slack and Linear answer 200 with the error inside.
	var answer map[string]any
	_ = json.Unmarshal(raw, &answer)
	if ok, isBool := answer["ok"].(bool); isBool && !ok {
		return "", errors.New(a.kind.Label + " refused: " + strings.TrimSpace(jsonText(answer["error"])))
	}
	if errs, has := answer["errors"].([]any); has && len(errs) > 0 {
		return "", errors.New(a.kind.Label + " refused: " + jsonText(errs[0]))
	}
	for _, path := range c.name {
		if v := lookup(answer, path); v != "" {
			return v, nil
		}
	}
	return "", nil
}

// lookup reads a dotted path of a JSON answer as text.
func lookup(v any, path string) string {
	for _, key := range strings.Split(path, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return ""
		}
		v = m[key]
	}
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	}
	return jsonText(v)
}

func jsonText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if m, ok := v.(map[string]any); ok {
		if msg, ok := m["message"].(string); ok {
			return msg
		}
	}
	data, _ := json.Marshal(v)
	return string(data)
}

// Check opens a session with the app password, which is all Bluesky's account needs.
func (b *blueskyConnector) Check(ctx context.Context) (string, error) {
	if _, _, err := b.session(ctx); err != nil {
		return "", err
	}
	return b.s["handle"], nil
}

// Check passes on the check of the connector inside; one without a check says so.
func (d described) Check(ctx context.Context) (string, error) {
	if c, ok := d.Connector.(Checker); ok {
		return c.Check(ctx)
	}
	return "", ErrNoCheck
}
