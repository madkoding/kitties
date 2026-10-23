package chat

import (
	"strings"
	"testing"
)

func TestPlain(t *testing.T) {
	for msg, want := range map[string]string{
		"HTTP 401: {\"title\":\"Unauthorized\"}":           "did not accept the key",
		"HTTP 403: forbidden":                              "not allowed to do that",
		"HTTP 404: not found":                              "did not find it",
		"HTTP 429: rate limit":                             "wait a moment",
		"HTTP 503: unavailable":                            "having trouble",
		"not signed in: connect the account from Settings": "not connected any more",
		"the google sign-in could not be renewed":          "not connected any more",
		"slack: invalid_auth":                              "did not accept the key",
		"context canceled":                                 "it was stopped",
		"Post \"https://x\": context deadline exceeded":    "took too long",
		"dial tcp: lookup api.x.com: no such host":         "could not be reached",
		"something nobody planned for":                     "something nobody planned for",
		"HTTP 302: a redirect nobody expected":             "HTTP 302",
	} {
		if got := Plain(msg); !strings.Contains(got, want) {
			t.Errorf("Plain(%q) = %q, want %q", msg, got, want)
		}
	}
}
