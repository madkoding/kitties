package connector

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOAuth1Signature(t *testing.T) {
	// The example of X's documentation ("Creating a signature"): same keys, nonce and time,
	// same signature.
	oldNonce, oldNow := oauthNonce, oauthNow
	t.Cleanup(func() { oauthNonce, oauthNow = oldNonce, oldNow })
	oauthNonce = func() string { return "kYjzVBB8Y0ZFabxSWbWovY3uYSQ2pTgmZeNu2VS4cg" }
	oauthNow = func() time.Time { return time.Unix(1318622958, 0) }
	k := xKeys{
		consumer: "xvz1evFS4wEEPTGEFPHBog", consumerSecret: "kAcSOqF21Fu85e7zjz7ZN2U4ZRhfV3WpwPAoE3Z7kBw",
		token: "370773112-GmHxMAgYyLbNEtIKZeRNFsMKPR9EyMZeS9weJAEb", tokenSecret: "LswwdoUaIvS8ltyTt5jkRh4J50vUPVVHtR2YPi5kE",
	}
	auth, err := k.sign("post", "https://api.twitter.com/1.1/statuses/update.json?include_entities=true",
		url.Values{"status": {"Hello Ladies + Gentlemen, a signed OAuth request!"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(auth, "OAuth ") || !strings.Contains(auth, `oauth_signature="hCtSmYh%2BiHYCEqBWrE7C7hYmtUk%3D"`) || !strings.Contains(auth, `oauth_consumer_key="xvz1evFS4wEEPTGEFPHBog"`) {
		t.Errorf("header = %s", auth)
	}
	if _, err := k.sign("GET", "://bad", nil); err == nil {
		t.Error("a bad URL fails")
	}
	if enc("a b~*") != "a%20b~%2A" {
		t.Errorf("enc = %s", enc("a b~*"))
	}
}

func TestXKeys(t *testing.T) {
	keys := Settings{"consumer_key": "ck", "consumer_secret": "cs", "access_token": "42-abc", "access_secret": "as"}
	x, _ := LookupKind("x")
	if !x.KeySet(keys) || x.KeySet(Settings{"consumer_key": "ck", "access_token": "42-abc"}) || !x.KeySet(Settings{"token": "t"}) || x.KeySet(Settings{"client_id": "c"}) {
		t.Error("KeySet: the four OAuth 1.0 keys, or the token, are an account; part of them or an app is not")
	}
	if xUserID(keys) != "42" || xUserID(Settings{"user_id": "7"}) != "7" || xUserID(Settings{"consumer_key": "ck", "consumer_secret": "cs", "access_token": "abc", "access_secret": "as"}) != "" {
		t.Error("the user id comes from the setting or the access token")
	}
	// The page shows where each key is in the portal, the sign-in's app last.
	names := []string{}
	for _, f := range x.Fields {
		names = append(names, f.Name+":"+f.Key)
	}
	if got := strings.Join(names, " "); !strings.HasPrefix(got, "consumer_key:oauth1 consumer_secret:oauth1 access_token:oauth1 access_secret:oauth1 token:token client_id: client_secret:") {
		t.Errorf("fields = %s", got)
	}

	// Posting and liking with the keys signs with OAuth 1.0a, never a bearer token, and the
	// id of the account comes from the access token.
	cap := &capture{}
	cap.install(t)
	c, err := Build("x", keys, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(context.Background(), map[string]any{"text": "hello"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(context.Background(), map[string]any{"action": "like", "id": "9"}); err != nil {
		t.Fatal(err)
	}
	for _, r := range cap.reqs {
		if a := r.Header.Get("Authorization"); !strings.HasPrefix(a, "OAuth ") || !strings.Contains(a, `oauth_token="42-abc"`) {
			t.Errorf("%s %s: Authorization = %q", r.Method, r.URL, a)
		}
	}
	if len(cap.reqs) != 2 || cap.reqs[1].URL.Path != "/2/users/42/likes" {
		t.Errorf("requests = %v", cap.reqs)
	}
}
