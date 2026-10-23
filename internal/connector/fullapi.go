package connector

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// THE WHOLE API OF A SERVICE.
//
// The actions a kind lists are the common ones, said simply; they are not the limit. Every kind
// over an HTTP API also has "request", which calls any endpoint of that API with the account the
// connection has (its sign-in, keys or token): what the account may do is the service's to
// limit, through the permissions of the sign-in or of the key, not Kitties'. A full URL may only
// name the service's own hosts, so the account never goes anywhere else.

// apiSpec is where a kind's API lives and how its calls are made.
type apiSpec struct {
	// root is the base every path is relative to.
	root func(s Settings) string
	// hosts are the other hosts of the service a full URL may name (".example.com" for any
	// subdomain); the root's own host always may.
	hosts []string
	// headers are what every call carries besides the account (versions, formats); the
	// account's Authorization comes from the connector.
	headers func(s Settings) map[string]string
	// basic is the user and password of an account that signs with them.
	basic func(s Settings) [2]string
	// form sends a body form-encoded, as the service expects (Stripe).
	form bool
	// docs is the API reference, for the kitty.
	docs string
}

func tokenHeader(s Settings) map[string]string { return bearer(s["token"]) }

var apis = map[string]apiSpec{
	"x": {root: fixed("https://api.x.com"), headers: tokenHeader, docs: "docs.x.com/x-api"},
	"github": {root: fixed("https://api.github.com"), hosts: []string{"uploads.github.com"}, docs: "docs.github.com/rest", headers: func(s Settings) map[string]string {
		h := bearer(s["token"])
		h["Accept"] = "application/vnd.github+json"
		return h
	}},
	"gitlab":   {root: func(s Settings) string { return base(s, "https://gitlab.com") + "/api/v4" }, headers: tokenHeader, docs: "docs.gitlab.com/api"},
	"mastodon": {root: func(s Settings) string { return base(s, "") }, headers: tokenHeader, docs: "docs.joinmastodon.org/methods"},
	"notion": {root: fixed("https://api.notion.com"), docs: "developers.notion.com/reference", headers: func(s Settings) map[string]string {
		h := bearer(s["token"])
		h["Notion-Version"] = "2022-06-28"
		return h
	}},
	"todoist":  {root: fixed("https://api.todoist.com"), headers: tokenHeader, docs: "developer.todoist.com/rest/v2"},
	"linear":   {root: fixed("https://api.linear.app"), headers: func(s Settings) map[string]string { return keyHeader("Authorization", s["token"]) }, docs: "developers.linear.app (GraphQL: POST /graphql with {query, variables})"},
	"airtable": {root: fixed("https://api.airtable.com"), hosts: []string{"content.airtable.com"}, headers: tokenHeader, docs: "airtable.com/developers/web/api"},
	"figma":    {root: fixed("https://api.figma.com"), headers: func(s Settings) map[string]string { return keyHeader("X-Figma-Token", s["token"]) }, docs: "www.figma.com/developers/api"},
	"spotify":  {root: fixed("https://api.spotify.com"), headers: tokenHeader, docs: "developer.spotify.com/documentation/web-api"},
	"tidal":    {root: fixed("https://openapi.tidal.com"), headers: tokenHeader, docs: "developer.tidal.com/apiref"},
	"dropbox":  {root: fixed("https://api.dropboxapi.com"), hosts: []string{"content.dropboxapi.com"}, headers: tokenHeader, docs: "www.dropbox.com/developers/documentation/http/documentation"},
	"slack":    {root: fixed("https://slack.com/api"), headers: tokenHeader, docs: "api.slack.com/methods"},
	"google-workspace": {root: fixed("https://www.googleapis.com"), hosts: []string{".googleapis.com"}, headers: tokenHeader,
		docs: "developers.google.com (Gmail, Calendar, Drive, Sheets, Docs, People, Tasks REST APIs)"},
	"microsoft-365": {root: fixed("https://graph.microsoft.com"), headers: tokenHeader, docs: "learn.microsoft.com/graph/api/overview"},
	"jira": {root: func(s Settings) string { root, _ := jiraTarget(s); return root }, docs: "developer.atlassian.com/cloud/jira/platform/rest/v3",
		basic: func(s Settings) [2]string { _, b := jiraTarget(s); return b }},
	"zendesk": {root: func(s Settings) string { return "https://" + s["subdomain"] + ".zendesk.com/api/v2" }, docs: "developer.zendesk.com/api-reference",
		basic: func(s Settings) [2]string {
			if s["token"] == "" {
				return [2]string{}
			}
			return [2]string{s["email"] + "/token", s["token"]}
		}},
	"linkedin": {root: fixed("https://api.linkedin.com"), docs: "learn.microsoft.com/linkedin", headers: func(s Settings) map[string]string {
		h := bearer(s["token"])
		h["X-Restli-Protocol-Version"] = "2.0.0"
		return h
	}},
	"facebook":       {root: func(s Settings) string { return graphRoot(s["token"]) }, hosts: []string{"graph.facebook.com", "graph.instagram.com"}, headers: tokenHeader, docs: "developers.facebook.com/docs/graph-api"},
	"instagram":      {root: func(s Settings) string { return graphRoot(s["token"]) }, hosts: []string{"graph.facebook.com", "graph.instagram.com"}, headers: tokenHeader, docs: "developers.facebook.com/docs/instagram-platform"},
	"whatsapp":       {root: fixed("https://graph.facebook.com/" + graphVersion), headers: tokenHeader, docs: "developers.facebook.com/docs/whatsapp/cloud-api"},
	"home-assistant": {root: func(s Settings) string { return strings.TrimRight(s["url"], "/") + "/api" }, headers: tokenHeader, docs: "developers.home-assistant.io/docs/api/rest"},
	"stripe":         {root: fixed("https://api.stripe.com"), headers: stripeHeaders, form: true, docs: "docs.stripe.com/api"},
	"telegram":       {root: func(s Settings) string { return base(s, "https://api.telegram.org") + "/bot" + s["token"] }, docs: "core.telegram.org/bots/api"},
}

func fixed(u string) func(Settings) string { return func(Settings) string { return u } }

// RequestHelp says what request does, the same for every kind.
const RequestHelp = "anything else the service's API lets the account do"

// requestAction is the action that calls any endpoint of a kind's API.
func requestAction(kind string) (Action, bool) {
	spec, ok := apis[kind]
	if !ok {
		return Action{}, false
	}
	return Action{Name: "request", Help: RequestHelp,
		Args: []string{"method (GET by default; POST, PUT, PATCH, DELETE)", "path (relative to the API, or a full URL of the service; reference: " + spec.docs + ")", "query (object, optional)", "body (JSON, optional)"}}, true
}

// apiRequest builds a call to any endpoint of the kind's API.
func apiRequest(spec apiSpec) actionFn {
	return func(s Settings, a map[string]any) (request, error) {
		method := strings.ToUpper(str(a, "method"))
		if method == "" {
			method = "GET"
		}
		switch method {
		case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD":
		default:
			return request{}, fmt.Errorf("%q is not an HTTP method", method)
		}
		path, err := need(a, "path", "url", "endpoint")
		if err != nil {
			return request{}, err
		}
		root := strings.TrimRight(spec.root(s), "/")
		target := root + "/" + strings.TrimLeft(path, "/")
		if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
			if !spec.allows(root, path) {
				return request{}, errors.New("that address is not this service's: a request only goes to the service the connection is for")
			}
			target = path
		}
		u, err := url.Parse(target)
		if err != nil {
			return request{}, err
		}
		q := u.Query()
		if query, ok := a["query"].(map[string]any); ok {
			for k, v := range query {
				q.Set(k, fmt.Sprint(v))
			}
		}
		u.RawQuery = q.Encode()
		req := request{method: method, url: u.String()}
		if spec.headers != nil {
			req.headers = spec.headers(s)
		}
		if spec.basic != nil {
			req.basic = spec.basic(s)
		}
		if body, ok := a["body"]; ok && body != nil && method != "GET" && method != "HEAD" {
			if s, isText := body.(string); isText {
				var parsed any
				if json.Unmarshal([]byte(s), &parsed) == nil {
					body = parsed
				}
			}
			if spec.form {
				m, isMap := body.(map[string]any)
				if !isMap {
					return request{}, errors.New("this service takes its body as fields: give body as an object")
				}
				req.raw, req.ctype = []byte(formEncode(m)), "application/x-www-form-urlencoded"
			} else {
				req.body = body
			}
		}
		return req, nil
	}
}

// allows reports whether a full URL is the service's: the root's host, or one of its others.
func (spec apiSpec) allows(root, target string) bool {
	t, err := url.Parse(target)
	if err != nil || t.Scheme != "https" && !strings.HasPrefix(root, "http://") {
		return false
	}
	r, _ := url.Parse(root)
	if r != nil && strings.EqualFold(t.Host, r.Host) {
		return true
	}
	for _, h := range spec.hosts {
		if strings.EqualFold(t.Host, h) || strings.HasPrefix(h, ".") && strings.HasSuffix(strings.ToLower(t.Host), h) {
			return true
		}
	}
	return false
}

// formEncode writes fields the way form APIs read them: nested objects and lists as key[sub]
// and key[0].
func formEncode(m map[string]any) string {
	v := url.Values{}
	var add func(prefix string, x any)
	add = func(prefix string, x any) {
		switch val := x.(type) {
		case map[string]any:
			keys := make([]string, 0, len(val))
			for k := range val {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				add(prefix+"["+k+"]", val[k])
			}
		case []any:
			for i, item := range val {
				add(fmt.Sprintf("%s[%d]", prefix, i), item)
			}
		default:
			v.Add(prefix, fmt.Sprint(val))
		}
	}
	for k, x := range m {
		add(k, x)
	}
	return v.Encode()
}

// requestAnswer is what a request answers, for the kitty to read: the body as it came, clipped.
func requestAnswer(data []byte) string {
	s := strings.TrimSpace(string(data))
	if s == "" {
		return "done (no content)"
	}
	return clip(s, 12000)
}
