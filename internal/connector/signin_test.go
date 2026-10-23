package connector

import (
	"context"
	"strings"
	"testing"
)

// signedIn builds a connector that signs in (no key), as the orchestrator does.
func signedIn(t *testing.T, kind string, s Settings) (Connector, *capture) {
	t.Helper()
	c := &capture{}
	c.install(t)
	conn, err := Build(kind, WithTokenSource(s, func(context.Context) (string, error) { return "acct", nil }), "")
	if err != nil {
		t.Fatal(err)
	}
	return conn, c
}

func TestSignedInConnectors(t *testing.T) {
	cases := []struct {
		kind     string
		settings Settings
		args     map[string]any
		host     string
		noHeader string
	}{
		{"gitlab", Settings{"project": "g/p"}, map[string]any{"title": "x"}, "gitlab.com", "PRIVATE-TOKEN"},
		{"figma", Settings{}, map[string]any{"file": "F", "text": "nice"}, "api.figma.com", "X-Figma-Token"},
		{"notion", Settings{"parent": "p"}, map[string]any{"title": "x"}, "api.notion.com", ""},
		{"todoist", Settings{}, map[string]any{"text": "milk"}, "api.todoist.com", ""},
		{"linear", Settings{"team": "T"}, map[string]any{"title": "x"}, "api.linear.app", ""},
		{"airtable", Settings{"base": "app1", "table": "t"}, map[string]any{"fields": map[string]any{"a": 1.0}}, "api.airtable.com", ""},
		{"x", Settings{}, map[string]any{"text": "hi"}, "api.x.com", ""},
		{"mastodon", Settings{"url": "https://m.social"}, map[string]any{"text": "hi"}, "m.social", ""},
		{"home-assistant", Settings{"url": "http://ha:8123"}, map[string]any{"action": "call_service", "service": "light.turn_on"}, "ha:8123", ""},
		{"zendesk", Settings{"subdomain": "acme"}, map[string]any{}, "acme.zendesk.com", ""},
		{"jira", Settings{"project": "K", "cloud_id": "c9"}, map[string]any{"summary": "x"}, "api.atlassian.com", ""},
		{"facebook", Settings{"page_id": "P1"}, map[string]any{"text": "hi"}, "graph.facebook.com", ""},
		{"linkedin", Settings{"author": "urn:li:person:a"}, map[string]any{"text": "hi"}, "api.linkedin.com", ""},
		{"dropbox", Settings{}, map[string]any{}, "api.dropboxapi.com", ""},
	}
	for _, c := range cases {
		conn, cap := signedIn(t, c.kind, c.settings)
		if _, err := conn.Call(context.Background(), c.args); err != nil {
			t.Errorf("%s: %v", c.kind, err)
			continue
		}
		r := cap.reqs[0]
		if r.URL.Host != c.host || r.Header.Get("Authorization") != "Bearer acct" {
			t.Errorf("%s: %s %v", c.kind, r.URL, r.Header)
		}
		if _, _, ok := r.BasicAuth(); ok {
			t.Errorf("%s: no basic auth when signed in", c.kind)
		}
		if c.noHeader != "" && r.Header.Get(c.noHeader) != "" {
			t.Errorf("%s: %s is sent empty", c.kind, c.noHeader)
		}
	}
	if conn, cap := signedIn(t, "jira", Settings{"project": "K", "cloud_id": "c9"}); conn != nil {
		_, _ = conn.Call(context.Background(), map[string]any{"action": "search"})
		if !strings.HasPrefix(cap.reqs[0].URL.String(), "https://api.atlassian.com/ex/jira/c9/rest/api/3/search/jql") {
			t.Errorf("jira search = %s", cap.reqs[0].URL)
		}
	}
}

func TestTypedKeyWins(t *testing.T) {
	// A key typed into the connection is used even though it could sign in.
	for kind, s := range map[string]Settings{
		"gitlab":  {"token": "glpat", "project": "g/p"},
		"zendesk": {"subdomain": "acme", "email": "a@b.c", "token": "zd"},
		"jira":    {"url": "https://acme.atlassian.net", "email": "a@b.c", "token": "jt", "project": "K"},
	} {
		conn, cap := signedIn(t, kind, s)
		args := map[string]any{"title": "x", "summary": "x"}
		if _, err := conn.Call(context.Background(), args); err != nil {
			t.Errorf("%s: %v", kind, err)
			continue
		}
		r := cap.reqs[0]
		if strings.Contains(r.Header.Get("Authorization"), "acct") {
			t.Errorf("%s: the account's token replaced the typed key", kind)
		}
	}
}

func TestSignInFillsWhatIsMissing(t *testing.T) {
	for kind, want := range map[string]string{"facebook": "sign in with Facebook", "instagram": "sign in with Facebook", "linkedin": "sign in with LinkedIn"} {
		conn, _ := signedIn(t, kind, Settings{})
		if _, err := conn.Call(context.Background(), map[string]any{"text": "hi", "image_url": "https://x/a.jpg"}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s = %v", kind, err)
		}
	}
	for _, action := range []string{"posts", "inbox", "send"} {
		conn, _ := signedIn(t, "facebook", Settings{})
		if _, err := conn.Call(context.Background(), map[string]any{"action": action, "to": "x", "text": "y"}); err == nil {
			t.Errorf("facebook %s without a page passes", action)
		}
	}
	conn, _ := signedIn(t, "instagram", Settings{})
	if _, err := conn.Call(context.Background(), map[string]any{"action": "posts"}); err == nil {
		t.Error("instagram posts without an account passes")
	}
	// An Instagram Login token speaks for its own account.
	conn, cap := signedIn(t, "instagram", Settings{"token": "IGAAx"})
	_, _ = conn.Call(context.Background(), map[string]any{"action": "posts"})
	if !strings.HasPrefix(cap.reqs[0].URL.String(), "https://graph.instagram.com/v23.0/me/media") {
		t.Errorf("ig login = %s", cap.reqs[0].URL)
	}
	if k, _ := LookupKind("google-workspace"); len(k.Fields) != 2 || k.Fields[0].Required {
		t.Error("google signs in without an app of the person's when Kitties has one")
	}
}
