package connector

import (
	"testing"
	"time"
)

func TestAuthOf(t *testing.T) {
	x, _ := LookupKind("x")
	sl, _ := LookupKind("slack")
	gh, _ := LookupKind("github")
	tg, _ := LookupKind("telegram")
	keys := Settings{"consumer_key": "ck", "consumer_secret": "cs", "access_token": "1-a", "access_secret": "as", "token": "t"}
	for _, c := range []struct {
		kind     Kind
		s        Settings
		signedIn bool
		want     string
	}{
		{x, keys, true, AuthKeys},
		{x, Settings{"token": "t"}, true, AuthToken},
		{x, Settings{}, true, AuthSignIn},
		{x, Settings{}, false, ""},
		{sl, Settings{"url": "https://hooks/x", "token": "t"}, false, AuthWebhook},
		{gh, Settings{"token": "t"}, false, AuthToken},
		{tg, Settings{"token": "t"}, false, ""},
	} {
		if got := c.kind.AuthOf(c.s, c.signedIn); got != c.want {
			t.Errorf("%s %v %v = %q, want %q", c.kind.Name, c.s, c.signedIn, got, c.want)
		}
	}
	if x.TokenLifetime() != 2*time.Hour || gh.TokenLifetime() != 0 {
		t.Error("only X's token has a known lifetime")
	}
	if at, ok := x.TokenExpiry(Settings{"token": "t", TokenSavedAt: "2026-10-23T10:00:00Z"}); !ok || !at.Equal(time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("expiry = %v %v", at, ok)
	}
	if _, ok := x.TokenExpiry(Settings{TokenSavedAt: "2026-10-23T10:00:00Z"}); ok {
		t.Error("no token, no expiry")
	}
	if _, ok := gh.TokenExpiry(Settings{"token": "t", TokenSavedAt: "2026-10-23T10:00:00Z"}); ok {
		t.Error("a lifetime unknown, no expiry")
	}
}
