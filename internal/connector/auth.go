package connector

import (
	"strings"
	"time"
)

// TokenSavedAt is the setting that keeps when a token of a known lifetime was typed (RFC 3339).
const TokenSavedAt = "token_saved_at"

// Ways a connector that signs in reaches its account, as AuthOf names them.
const (
	AuthSignIn  = "sign-in"
	AuthKeys    = "keys"
	AuthToken   = "token"
	AuthWebhook = "webhook"
)

// AuthOf says how a connector that signs in reaches its account, in the order its requests
// take them: X's OAuth 1.0 keys, Slack's webhook, a typed token, then the sign-in. Empty for a
// kind that does not sign in, or one with no account.
func (k Kind) AuthOf(s Settings, signedIn bool) string {
	_, keys := xKeysOf(s)
	switch {
	case k.oauth == "":
		return ""
	case k.Name == "x" && keys:
		return AuthKeys
	case k.Name == "slack" && strings.TrimSpace(s["url"]) != "":
		return AuthWebhook
	case strings.TrimSpace(s["token"]) != "":
		return AuthToken
	case signedIn:
		return AuthSignIn
	}
	return ""
}

// TokenLifetime is how long a token typed in the kind's token field lasts; zero when unknown.
func (k Kind) TokenLifetime() time.Duration {
	for _, f := range k.Fields {
		if f.Name == "token" {
			return time.Duration(f.Lifetime) * time.Second
		}
	}
	return 0
}

// TokenExpiry is when the typed token of the settings stops working, when the kind knows its
// lifetime and when it was typed.
func (k Kind) TokenExpiry(s Settings) (time.Time, bool) {
	life := k.TokenLifetime()
	at, err := time.Parse(time.RFC3339, s[TokenSavedAt])
	if life == 0 || err != nil || strings.TrimSpace(s["token"]) == "" {
		return time.Time{}, false
	}
	return at.Add(life), true
}
