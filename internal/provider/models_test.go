package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type modelsRT func(*http.Request) (int, string)

func (f modelsRT) RoundTrip(r *http.Request) (*http.Response, error) {
	code, body := f(r)
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
}

func TestListModelsFamilies(t *testing.T) {
	cases := []struct {
		kind, url, header, body string
		want                    []string
	}{
		{"openai", "https://api.openai.com/v1/models", "Authorization", `{"data":[{"id":"b"},{"id":"a"},{"id":"a"}]}`, []string{"a", "b"}},
		{"anthropic", "https://api.anthropic.com/v1/models?limit=1000", "x-api-key", `{"data":[{"id":"claude-x"}]}`, []string{"claude-x"}},
		{"gemini", "https://generativelanguage.googleapis.com/v1beta/models?pageSize=1000", "x-goog-api-key",
			`{"models":[{"name":"models/g1","supportedGenerationMethods":["generateContent"]},{"name":"models/emb","supportedGenerationMethods":["embedContent"]}]}`, []string{"g1"}},
	}
	for _, c := range cases {
		got, err := ListModels(context.Background(), Options{Kind: c.kind, APIKey: "k"}, modelsRT(func(r *http.Request) (int, string) {
			if r.URL.String() != c.url || r.Header.Get(c.header) == "" {
				t.Errorf("%s: asked %s with %q", c.kind, r.URL, r.Header.Get(c.header))
			}
			return 200, c.body
		}))
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v %v, want %v", c.kind, got, err, c.want)
		}
	}
}

func TestListModelsInvalidKey(t *testing.T) {
	for kind, status := range map[string]int{"openai": 401, "anthropic": 403, "gemini": 400} {
		body := `{"error":"API_KEY_INVALID"}`
		_, err := ListModels(context.Background(), Options{Kind: kind, APIKey: "bad"}, modelsRT(func(*http.Request) (int, string) { return status, body }))
		if !errors.Is(err, ErrInvalidKey) {
			t.Errorf("%s: %v is not ErrInvalidKey", kind, err)
		}
	}
	_, err := ListModels(context.Background(), Options{Kind: "openai", APIKey: "k"}, modelsRT(func(*http.Request) (int, string) { return 500, "boom" }))
	var he *HTTPError
	if !errors.As(err, &he) || errors.Is(err, ErrInvalidKey) {
		t.Errorf("a server error is not an invalid key: %v", err)
	}
}

func TestListModelsNeedsKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := ListModels(context.Background(), Options{Kind: "openai"}, nil); !errors.Is(err, ErrNoKey) {
		t.Errorf("no key: %v", err)
	}
	if _, err := ListModels(context.Background(), Options{Kind: "claude-code"}, nil); err == nil {
		t.Error("the CLI has no model list")
	}
}

func TestListModelsRefusesOddAddresses(t *testing.T) {
	for _, base := range []string{"ftp://x/v1", "file:///etc", "http://u:p@host/v1", "http://169.254.169.254/latest", "http://[fe80::1]/v1", "http://0.0.0.0/v1", "https://"} {
		_, err := ListModels(context.Background(), Options{Kind: "openai", APIKey: "k", BaseURL: base}, modelsRT(func(*http.Request) (int, string) {
			t.Errorf("%s: a request was sent", base)
			return 200, "{}"
		}))
		if err == nil {
			t.Errorf("%s: accepted", base)
		}
	}
	if _, err := ListModels(context.Background(), Options{Kind: "ollama", BaseURL: "http://127.0.0.1:11434/v1"}, modelsRT(func(*http.Request) (int, string) { return 200, `{"data":[]}` })); err != nil {
		t.Errorf("a local server is fine: %v", err)
	}
}
