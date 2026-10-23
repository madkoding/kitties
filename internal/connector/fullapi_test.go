package connector

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

func TestRequestReachesTheWholeAPI(t *testing.T) {
	ctx := context.Background()
	// Any endpoint, with the account and the query, and its answer as it came.
	cap := &capture{answer: []string{`{"data":[{"id":"1","username":"ana"}]}`}}
	cap.install(t)
	x, _ := Build("x", Settings{"token": "t"}, "")
	out, err := x.Call(ctx, map[string]any{"action": "request", "path": "/2/users/42/following", "query": map[string]any{"max_results": 5.0}})
	if err != nil || !strings.Contains(out, `"username":"ana"`) {
		t.Fatalf("x request = %q %v", out, err)
	}
	r := cap.reqs[0]
	if r.Method != "GET" || r.URL.Host != "api.x.com" || r.URL.Path != "/2/users/42/following" || r.URL.Query().Get("max_results") != "5" || r.Header.Get("Authorization") != "Bearer t" {
		t.Errorf("request = %s %s %s", r.Method, r.URL, r.Header.Get("Authorization"))
	}
	// A JSON body, given as an object or as text.
	cap = &capture{}
	cap.install(t)
	gh, _ := Build("github", Settings{"token": "g"}, "")
	if _, err := gh.Call(ctx, map[string]any{"action": "request", "method": "post", "path": "repos/a/b/labels", "body": `{"name":"bug"}`}); err != nil {
		t.Fatal(err)
	}
	if r := cap.reqs[0]; r.Method != "POST" || r.URL.String() != "https://api.github.com/repos/a/b/labels" || cap.bodies[0] != `{"name":"bug"}` || r.Header.Get("Accept") != "application/vnd.github+json" {
		t.Errorf("github = %s %s %q", r.Method, r.URL, cap.bodies[0])
	}
	// Another host of the service may be named in full; any other host never.
	cap = &capture{}
	cap.install(t)
	g, _ := Build("google-workspace", Settings{"token": "y"}, "")
	if _, err := g.Call(ctx, map[string]any{"action": "request", "path": "https://gmail.googleapis.com/gmail/v1/users/me/labels"}); err != nil || cap.reqs[0].URL.Host != "gmail.googleapis.com" {
		t.Errorf("a host of the service: %v", err)
	}
	for _, bad := range []string{"https://evil.example/steal", "http://www.googleapis.com/x", "https://googleapis.com.evil.example/x"} {
		if _, err := g.Call(ctx, map[string]any{"action": "request", "path": bad}); err == nil || !strings.Contains(err.Error(), "not this service's") {
			t.Errorf("%s = %v", bad, err)
		}
	}
	// Stripe takes fields, nested the way it reads them.
	cap = &capture{}
	cap.install(t)
	st, _ := Build("stripe", Settings{"token": "sk"}, "")
	if _, err := st.Call(ctx, map[string]any{"action": "request", "method": "POST", "path": "/v1/customers", "body": map[string]any{"email": "a@x", "metadata": map[string]any{"plan": "pro"}, "tags": []any{"a", "b"}}}); err != nil {
		t.Fatal(err)
	}
	form, _ := url.ParseQuery(cap.bodies[0])
	if form.Get("email") != "a@x" || form.Get("metadata[plan]") != "pro" || form.Get("tags[1]") != "b" || cap.reqs[0].Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		t.Errorf("stripe form = %q", cap.bodies[0])
	}
	if _, err := st.Call(ctx, map[string]any{"action": "request", "method": "POST", "path": "/v1/x", "body": []any{1.0}}); err == nil {
		t.Error("a form body is an object")
	}
	// Jira signs with its email and token, X with its OAuth 1.0 keys.
	cap = &capture{}
	cap.install(t)
	j, _ := Build("jira", Settings{"project": "K", "url": "https://acme.atlassian.net", "email": "a@x", "token": "j"}, "")
	if _, err := j.Call(ctx, map[string]any{"action": "request", "path": "/rest/api/3/myself"}); err != nil {
		t.Fatal(err)
	}
	if u, p, ok := cap.reqs[0].BasicAuth(); !ok || u != "a@x" || p != "j" || cap.reqs[0].URL.Host != "acme.atlassian.net" {
		t.Errorf("jira = %s %v", cap.reqs[0].URL, ok)
	}
	cap = &capture{}
	cap.install(t)
	xk, _ := Build("x", Settings{"consumer_key": "ck", "consumer_secret": "cs", "access_token": "1-a", "access_secret": "as"}, "")
	if _, err := xk.Call(ctx, map[string]any{"action": "request", "path": "/2/users/me"}); err != nil || !strings.HasPrefix(cap.reqs[0].Header.Get("Authorization"), "OAuth ") {
		t.Errorf("x keys = %v", err)
	}
	// Nonsense is refused; an empty answer says so.
	for _, bad := range []map[string]any{{"action": "request"}, {"action": "request", "path": "/x", "method": "BREW"}, {"action": "request", "path": "%zz"}} {
		if _, err := x.Call(ctx, bad); err == nil {
			t.Errorf("%v is refused", bad)
		}
	}
	cap = &capture{answer: []string{""}}
	cap.install(t)
	if out, _ := x.Call(ctx, map[string]any{"action": "request", "method": "DELETE", "path": "/2/users/1/likes/2"}); out != "done (no content)" {
		t.Errorf("empty answer = %q", out)
	}
}

func TestEveryAPIKindHasRequest(t *testing.T) {
	for kind := range apis {
		k, ok := LookupKind(kind)
		if !ok {
			t.Errorf("%s is not in the catalogue", kind)
			continue
		}
		found := false
		for _, a := range Kinds() {
			if a.Name != kind {
				continue
			}
			for _, act := range a.Actions {
				found = found || act.Name == "request"
			}
		}
		if !found || k.Name == "" {
			t.Errorf("%s lists no request action", kind)
		}
	}
	if _, ok := requestAction("shell"); ok {
		t.Error("a program has no API")
	}
	// It is judged by its method: reading is free, writing reaches out.
	if RiskOfCall("x", map[string]any{"action": "request"}) != RiskNone || RiskOfCall("x", map[string]any{"action": "request", "method": "POST"}) != RiskAsk {
		t.Error("RiskOfCall of a request")
	}
}
