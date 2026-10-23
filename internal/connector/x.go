package connector

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // OAuth 1.0a signs with HMAC-SHA1: X takes nothing else
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// X's developer portal shows several credentials, under "Keys and tokens" of an app:
//
//   - the Bearer Token (app-only): it reads public data and cannot post, so it is not asked for;
//   - the OAuth 1.0 keys (consumer key and secret, and the account's access token and secret,
//     "read and write"): they do not expire, so they are the keys to paste;
//   - the OAuth 2.0 client id and secret: for the sign-in, with the gateway's callback address
//     added under "User authentication settings";
//   - an OAuth 2.0 access token: it lasts two hours, so it is only a stopgap.

// xFields are the fields of the X connector, in the order the page shows them.
func xFields() []Field {
	app := fApp("developer.x.com")
	app[0].Label, app[1].Label = "OAuth 2.0 Client ID", "OAuth 2.0 Client Secret"
	app[0].Help = "developer.x.com → your app → User authentication settings: Web App, Read and write, the callback address above; then Keys and tokens → OAuth 2.0 Keys"
	app[1].Help = "shown with the Client ID when it is generated"
	oauth1 := func(name, label, help string, secret bool) Field {
		fl := f(name, label, help, false, secret)
		fl.Key = keyOAuth1
		return fl
	}
	token := f("token", "OAuth 2.0 access token", "Keys and tokens → OAuth 2.0 Keys → Access Token; it lasts two hours, so the keys above or the sign-in are better", false, true)
	token.Key = keyToken
	return append([]Field{
		oauth1("consumer_key", "Consumer Key", "Keys and tokens → OAuth 1.0 Keys → Consumer Key (the API key); with the three below, instead of signing in, and they do not expire", false),
		oauth1("consumer_secret", "Consumer Secret", "shown with the Consumer Key when it is generated", true),
		oauth1("access_token", "Access Token", "OAuth 1.0 Keys → Access Token, for the account, with Read and write permission", true),
		oauth1("access_secret", "Access Token Secret", "shown with the Access Token when it is generated", true),
		token,
	}, app...)
}

// xKeys are the OAuth 1.0a keys of the settings, when all four are there.
type xKeys struct{ consumer, consumerSecret, token, tokenSecret string }

func xKeysOf(s Settings) (xKeys, bool) {
	k := xKeys{
		strings.TrimSpace(s["consumer_key"]), strings.TrimSpace(s["consumer_secret"]),
		strings.TrimSpace(s["access_token"]), strings.TrimSpace(s["access_secret"]),
	}
	return k, k.consumer != "" && k.consumerSecret != "" && k.token != "" && k.tokenSecret != ""
}

// xUserID is the account's numeric id: the setting (a sign-in fills it), or the start of the
// OAuth 1.0a access token, which is "<id>-…".
func xUserID(s Settings) string {
	if id := strings.TrimSpace(s["user_id"]); id != "" {
		return id
	}
	if k, ok := xKeysOf(s); ok {
		if id, _, found := strings.Cut(k.token, "-"); found {
			if _, err := strconv.ParseUint(id, 10, 64); err == nil {
				return id
			}
		}
	}
	return ""
}

// oauthNonce and oauthNow make a signature; tests fix them.
var (
	oauthNonce = func() string {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		return hex.EncodeToString(b)
	}
	oauthNow = time.Now
)

// sign is the Authorization header of an OAuth 1.0a request (HMAC-SHA1). The query of the URL
// and form values are signed; a JSON body is not, as the protocol says.
func (k xKeys) sign(method, rawURL string, form url.Values) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	oauth := map[string]string{
		"oauth_consumer_key":     k.consumer,
		"oauth_nonce":            oauthNonce(),
		"oauth_signature_method": "HMAC-SHA1",
		"oauth_timestamp":        strconv.FormatInt(oauthNow().Unix(), 10),
		"oauth_token":            k.token,
		"oauth_version":          "1.0",
	}
	var pairs []string
	add := func(key, value string) { pairs = append(pairs, enc(key)+"="+enc(value)) }
	for key, value := range oauth {
		add(key, value)
	}
	for _, values := range []url.Values{u.Query(), form} {
		for key, vs := range values {
			for _, v := range vs {
				add(key, v)
			}
		}
	}
	sort.Strings(pairs)
	base := strings.ToUpper(method) + "&" + enc(strings.ToLower(u.Scheme)+"://"+strings.ToLower(u.Host)+u.EscapedPath()) + "&" + enc(strings.Join(pairs, "&"))
	mac := hmac.New(sha1.New, []byte(enc(k.consumerSecret)+"&"+enc(k.tokenSecret)))
	mac.Write([]byte(base))
	oauth["oauth_signature"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	keys := make([]string, 0, len(oauth))
	for key := range oauth {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, key := range keys {
		parts[i] = enc(key) + `="` + enc(oauth[key]) + `"`
	}
	return "OAuth " + strings.Join(parts, ", "), nil
}

// enc is the percent-encoding of RFC 3986 that OAuth 1.0a signs with.
func enc(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}
