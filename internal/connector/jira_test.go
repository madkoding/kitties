package connector

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestJiraSite(t *testing.T) {
	for in, want := range map[string]string{
		"":                              "",
		"acme.atlassian.net":            "https://acme.atlassian.net",
		" https://acme.atlassian.net/ ": "https://acme.atlassian.net",
		"http://Acme.atlassian.net":     "https://acme.atlassian.net",
		"https://acme.atlassian.net/jira/your-work":    "https://acme.atlassian.net",
		"https://acme.atlassian.net/browse/KIT-1":      "https://acme.atlassian.net",
		"https://jira.corp.example/jira/browse/K-1":    "https://jira.corp.example/jira",
		"jira.corp.example:8443/secure/Dashboard.jspa": "https://jira.corp.example:8443",
		"http://jira.lan/rest/api/2/myself":            "http://jira.lan",
	} {
		if got := jiraSite(in); got != want {
			t.Errorf("jiraSite(%q) = %q, want %q", in, got, want)
		}
	}
}

// jiraStub answers like Jira: the site refuses a scoped token, the gateway takes it.
func jiraStub(t *testing.T, siteTakesToken bool) *[]string {
	t.Helper()
	var seen []string
	old := httpDo
	httpDo = func(req *http.Request) (*http.Response, error) {
		seen = append(seen, req.Method+" "+req.URL.String())
		answer, code := `{"displayName":"Mad","emailAddress":"a@x"}`, 200
		user, pass, ok := req.BasicAuth()
		switch {
		case req.URL.Path == "/_edge/tenant_info":
			answer = `{"cloudId":"c-9"}`
		case !ok || user != "a@x" || pass != "tok":
			answer, code = `{"errorMessages":["unauthorized"]}`, 401
		case req.URL.Host == "acme.atlassian.net" && !siteTakesToken:
			answer, code = `{"errorMessages":["scoped token"]}`, 401
		}
		return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(answer))}, nil
	}
	t.Cleanup(func() { httpDo = old })
	return &seen
}

func TestJiraCheckTrimsAndNormalizes(t *testing.T) {
	seen := jiraStub(t, true)
	c, err := Build("jira", Settings{"project": "K", "url": "acme.atlassian.net/jira/your-work", "email": " a@x ", "token": "tok\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	who, err := c.(Checker).Check(context.Background())
	if err != nil || who != "a@x" {
		t.Fatalf("check = %q %v", who, err)
	}
	if (*seen)[0] != "GET https://acme.atlassian.net/rest/api/3/myself" {
		t.Errorf("asked %v", *seen)
	}
}

func TestJiraScopedTokenGoesThroughGateway(t *testing.T) {
	seen := jiraStub(t, false)
	c, _ := Build("jira", Settings{"project": "K", "url": "https://acme.atlassian.net", "email": "a@x", "token": "tok"}, "")
	if _, err := c.(Checker).Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET https://acme.atlassian.net/rest/api/3/myself",
		"GET https://acme.atlassian.net/_edge/tenant_info",
		"GET https://api.atlassian.com/ex/jira/c-9/rest/api/3/myself",
	}
	if strings.Join(*seen, "|") != strings.Join(want, "|") {
		t.Fatalf("asked %v", *seen)
	}
	// The cloud id is kept: the next call goes straight to the gateway.
	*seen = nil
	if _, err := c.Call(context.Background(), map[string]any{"summary": "S"}); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 || (*seen)[0] != "POST https://api.atlassian.com/ex/jira/c-9/rest/api/3/issue" {
		t.Errorf("asked %v", *seen)
	}
}

func TestJiraErrorsSayWhatFailed(t *testing.T) {
	jiraStub(t, true)
	c, _ := Build("jira", Settings{"project": "K", "url": "https://acme.atlassian.net", "email": "a@x", "token": "wrong"}, "")
	_, err := c.(Checker).Check(context.Background())
	var st *StatusError
	if err == nil || !errors.As(err, &st) || st.Code != 401 || !strings.Contains(err.Error(), "refused the email and API token") {
		t.Errorf("401 = %v", err)
	}

	old := httpDo
	t.Cleanup(func() { httpDo = old })
	httpDo = func(req *http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: "Get", URL: req.URL.String(), Err: &net.DNSError{Err: "no such host", Name: req.URL.Host, IsNotFound: true}}
	}
	_, err = c.(Checker).Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "cannot find acme.atlassian.net") || !strings.Contains(err.Error(), "Jira at https://acme.atlassian.net") {
		t.Errorf("dns = %v", err)
	}
}

func TestJiraDataCenterTokenIsBearer(t *testing.T) {
	var auth string
	old := httpDo
	httpDo = func(req *http.Request) (*http.Response, error) {
		auth = req.Header.Get("Authorization")
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"displayName":"Mad"}`))}, nil
	}
	t.Cleanup(func() { httpDo = old })
	c, _ := Build("jira", Settings{"project": "K", "url": "https://jira.corp.example", "token": "pat"}, "")
	if who, err := c.(Checker).Check(context.Background()); err != nil || who != "Mad" || auth != "Bearer pat" {
		t.Errorf("check = %q %v, auth %q", who, err, auth)
	}
}

func TestNetErrorWithoutScheme(t *testing.T) {
	err := netError(&url.Error{Op: "Get", URL: "acme/rest", Err: errors.New(`unsupported protocol scheme ""`)})
	if !strings.Contains(err.Error(), "is not a web address") {
		t.Errorf("err = %v", err)
	}
}
