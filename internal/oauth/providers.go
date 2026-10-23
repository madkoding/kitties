package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Providers are the services with an OAuth flow, by the name a connector kind gives (Kind.OAuth).
var Providers = map[string]Provider{
	"google": {
		Name: "google", Label: "Google", AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token",
		Scopes: []string{"https://www.googleapis.com/auth/gmail.send", "https://www.googleapis.com/auth/gmail.readonly", "https://www.googleapis.com/auth/calendar.events", "https://www.googleapis.com/auth/drive.file", "https://www.googleapis.com/auth/spreadsheets", "https://www.googleapis.com/auth/documents", "https://www.googleapis.com/auth/contacts", "https://www.googleapis.com/auth/tasks"},
		Extra:  map[string]string{"access_type": "offline", "prompt": "consent"},
	},
	"microsoft": {
		Name: "microsoft", Label: "Microsoft", AuthURL: "https://login.microsoftonline.com/common/oauth2/v2.0/authorize", TokenURL: "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		Scopes: []string{"offline_access", "Mail.Send", "Mail.Read", "Calendars.ReadWrite", "Files.ReadWrite", "User.Read", "Contacts.ReadWrite", "Tasks.ReadWrite"},
	},
	"slack": {
		Name: "slack", Label: "Slack", AuthURL: "https://slack.com/oauth/v2/authorize", TokenURL: "https://slack.com/api/oauth.v2.access",
		Scopes: []string{"chat:write", "chat:write.public", "channels:read"}, ScopeSep: ",", NoExpiry: true,
	},
	"github": {
		Name: "github", Label: "GitHub", AuthURL: "https://github.com/login/oauth/authorize", TokenURL: "https://github.com/login/oauth/access_token",
		Scopes: []string{"repo"}, NoExpiry: true,
	},
	"gitlab": {
		Name: "gitlab", Label: "GitLab", AuthURL: "{url}/oauth/authorize", TokenURL: "{url}/oauth/token",
		Scopes: []string{"api"}, Defaults: map[string]string{"url": "https://gitlab.com"},
	},
	"spotify": {
		Name: "spotify", Label: "Spotify", AuthURL: "https://accounts.spotify.com/authorize", TokenURL: "https://accounts.spotify.com/api/token",
		Scopes: []string{"user-read-playback-state", "user-modify-playback-state", "user-read-currently-playing", "playlist-read-private", "playlist-modify-private", "playlist-modify-public"},
	},
	"tidal": {
		Name: "tidal", Label: "TIDAL", AuthURL: "https://login.tidal.com/authorize", TokenURL: "https://auth.tidal.com/v1/oauth2/token",
		Scopes: []string{"search.read", "playlists.read", "playlists.write", "collection.read", "user.read"},
	},
	"dropbox": {
		Name: "dropbox", Label: "Dropbox", AuthURL: "https://www.dropbox.com/oauth2/authorize", TokenURL: "https://api.dropboxapi.com/oauth2/token",
		Scopes: []string{"files.metadata.read", "files.content.read", "files.content.write", "sharing.write"},
		Extra:  map[string]string{"token_access_type": "offline"},
	},
	"notion": {
		Name: "notion", Label: "Notion", AuthURL: "https://api.notion.com/v1/oauth/authorize", TokenURL: "https://api.notion.com/v1/oauth/token",
		Extra: map[string]string{"owner": "user"}, Basic: true, JSONBody: true, NoExpiry: true,
	},
	"todoist": {
		Name: "todoist", Label: "Todoist", AuthURL: "https://todoist.com/oauth/authorize", TokenURL: "https://todoist.com/oauth/access_token",
		Scopes: []string{"data:read_write"}, ScopeSep: ",", NoExpiry: true,
	},
	"linear": {
		Name: "linear", Label: "Linear", AuthURL: "https://linear.app/oauth/authorize", TokenURL: "https://api.linear.app/oauth/token",
		Scopes: []string{"read", "write"}, ScopeSep: ",",
	},
	"airtable": {
		Name: "airtable", Label: "Airtable", AuthURL: "https://airtable.com/oauth2/v1/authorize", TokenURL: "https://airtable.com/oauth2/v1/token",
		Scopes: []string{"data.records:read", "data.records:write", "schema.bases:read"}, Basic: true,
	},
	"figma": {
		Name: "figma", Label: "Figma", AuthURL: "https://www.figma.com/oauth", TokenURL: "https://api.figma.com/v1/oauth/token",
		Scopes: []string{"file_content:read", "file_comments:write", "current_user:read"},
	},
	"atlassian": {
		Name: "atlassian", Label: "Atlassian", AuthURL: "https://auth.atlassian.com/authorize", TokenURL: "https://auth.atlassian.com/oauth/token",
		Scopes: []string{"read:jira-work", "write:jira-work", "offline_access"}, JSONBody: true,
		Extra: map[string]string{"audience": "api.atlassian.com", "prompt": "consent"}, Finish: atlassianFinish,
	},
	"zendesk": {
		Name: "zendesk", Label: "Zendesk", AuthURL: "https://{subdomain}.zendesk.com/oauth/authorizations/new", TokenURL: "https://{subdomain}.zendesk.com/oauth/tokens",
		Scopes: []string{"read", "write"}, NoExpiry: true,
	},
	"x": {
		Name: "x", Label: "X", AuthURL: "https://x.com/i/oauth2/authorize", TokenURL: "https://api.x.com/2/oauth2/token",
		Scopes: []string{"tweet.read", "tweet.write", "users.read", "like.write", "offline.access"}, Basic: true, Finish: xFinish,
	},
	"linkedin": {
		Name: "linkedin", Label: "LinkedIn", AuthURL: "https://www.linkedin.com/oauth/v2/authorization", TokenURL: "https://www.linkedin.com/oauth/v2/accessToken",
		Scopes: []string{"openid", "profile", "w_member_social"}, Finish: linkedinFinish,
	},
	"facebook": {
		Name: "facebook", Label: "Facebook", AuthURL: "https://www.facebook.com/" + graphVersion + "/dialog/oauth", TokenURL: graphRoot + "/oauth/access_token",
		Scopes: []string{"pages_show_list", "pages_manage_posts", "pages_read_engagement", "pages_manage_engagement", "pages_messaging",
			"instagram_basic", "instagram_content_publish", "instagram_manage_comments", "business_management"},
		ScopeSep: ",", Finish: facebookFinish,
	},
	"mastodon": {
		Name: "mastodon", Label: "Mastodon", AuthURL: "{url}/oauth/authorize", TokenURL: "{url}/oauth/token",
		Scopes: []string{"read", "write"}, NoExpiry: true, Register: mastodonRegister, Finish: mastodonFinish,
	},
	"home-assistant": {
		Name: "home-assistant", Label: "Home Assistant", AuthURL: "{url}/auth/authorize", TokenURL: "{url}/auth/token", SelfClient: true,
	},
}

const (
	graphVersion = "v23.0"
	graphRoot    = "https://graph.facebook.com/" + graphVersion
)

// getJSON reads a JSON answer with an optional bearer token.
func getJSON(ctx context.Context, do HTTPDo, target, token string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return readAnswer(do, req, dest)
}

func readAnswer(do HTTPDo, req *http.Request, dest any) error {
	resp, err := do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data[:min(len(data), 200)])))
	}
	return json.Unmarshal(data, dest)
}

// mastodonRegister makes Kitties an app on the person's instance: Mastodon lets anyone.
func mastodonRegister(ctx context.Context, do HTTPDo, app App, redirect string) (string, string, error) {
	root := strings.TrimRight(app.Settings["url"], "/")
	if root == "" {
		return "", "", errors.New("the instance's address is missing")
	}
	form := url.Values{"client_name": {"Kitties"}, "redirect_uris": {redirect}, "scopes": {"read write"}, "website": {"https://github.com/madkoding/kitties"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, root+"/api/v1/apps", strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var r struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := readAnswer(do, req, &r); err != nil {
		return "", "", err
	}
	if r.ClientID == "" {
		return "", "", errors.New("the instance returned no app")
	}
	return r.ClientID, r.ClientSecret, nil
}

// mastodonFinish names the account that signed in.
func mastodonFinish(ctx context.Context, do HTTPDo, app App, c Credentials) (Credentials, map[string]string, error) {
	var me struct {
		Acct string `json:"acct"`
	}
	if getJSON(ctx, do, strings.TrimRight(app.Settings["url"], "/")+"/api/v1/accounts/verify_credentials", c.AccessToken, &me) == nil && me.Acct != "" {
		c.Account = "@" + me.Acct
	}
	return c, nil, nil
}

// xFinish learns the account's id, which mentions and likes need.
func xFinish(ctx context.Context, do HTTPDo, app App, c Credentials) (Credentials, map[string]string, error) {
	var me struct {
		Data struct {
			ID, Username string
		} `json:"data"`
	}
	if err := getJSON(ctx, do, "https://api.x.com/2/users/me", c.AccessToken, &me); err != nil || me.Data.ID == "" {
		return c, nil, nil // the token works for posting; mentions will ask for the id
	}
	c.Account = "@" + me.Data.Username
	return c, map[string]string{"user_id": me.Data.ID}, nil
}

// linkedinFinish learns the member who posts: their URN is the author of the posts.
func linkedinFinish(ctx context.Context, do HTTPDo, app App, c Credentials) (Credentials, map[string]string, error) {
	var me struct {
		Sub  string `json:"sub"`
		Name string `json:"name"`
	}
	if err := getJSON(ctx, do, "https://api.linkedin.com/v2/userinfo", c.AccessToken, &me); err != nil || me.Sub == "" {
		return c, nil, fmt.Errorf("LinkedIn did not say who signed in: %v", err)
	}
	c.Account = me.Name
	learned := map[string]string{}
	if app.Settings["author"] == "" {
		learned["author"] = "urn:li:person:" + me.Sub
	}
	return c, learned, nil
}

// atlassianFinish finds the Jira site the token reaches: its cloud id is where the calls go.
func atlassianFinish(ctx context.Context, do HTTPDo, app App, c Credentials) (Credentials, map[string]string, error) {
	var sites []struct {
		ID, URL, Name string
	}
	if err := getJSON(ctx, do, "https://api.atlassian.com/oauth/token/accessible-resources", c.AccessToken, &sites); err != nil {
		return c, nil, fmt.Errorf("Atlassian did not list the sites: %w", err)
	}
	if len(sites) == 0 {
		return c, nil, errors.New("the account reaches no Jira site")
	}
	site := sites[0]
	want := strings.TrimRight(app.Settings["url"], "/")
	for _, s := range sites {
		if want != "" && strings.EqualFold(strings.TrimRight(s.URL, "/"), want) {
			site = s
		}
	}
	c.Account = site.Name
	return c, map[string]string{"cloud_id": site.ID, "url": site.URL}, nil
}

// facebookFinish turns the person's Facebook token into a Page's: Kitties publishes as the Page,
// and a Page token from a long-lived user token does not expire. For Instagram it is the Page
// linked to the professional account, whose id it learns too.
func facebookFinish(ctx context.Context, do HTTPDo, app App, c Credentials) (Credentials, map[string]string, error) {
	user := c.AccessToken
	id, secret := app.ClientID, app.ClientSecret
	if id == "" {
		id, secret = Builtin("facebook")
	}
	q := url.Values{"grant_type": {"fb_exchange_token"}, "client_id": {id}, "client_secret": {secret}, "fb_exchange_token": {user}}
	var long struct {
		AccessToken string `json:"access_token"`
	}
	if getJSON(ctx, do, graphRoot+"/oauth/access_token?"+q.Encode(), "", &long) == nil && long.AccessToken != "" {
		user = long.AccessToken
	}
	var pages struct {
		Data []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			AccessToken string `json:"access_token"`
			Instagram   *struct {
				ID string `json:"id"`
			} `json:"instagram_business_account"`
		} `json:"data"`
	}
	if err := getJSON(ctx, do, graphRoot+"/me/accounts?fields=id,name,access_token,instagram_business_account&limit=100", user, &pages); err != nil {
		return c, nil, fmt.Errorf("Facebook did not list the Pages: %w", err)
	}
	instagram := app.Kind == "instagram"
	for _, p := range pages.Data {
		switch {
		case instagram && (p.Instagram == nil || (app.Settings["account_id"] != "" && p.Instagram.ID != app.Settings["account_id"])):
			continue
		case !instagram && app.Settings["page_id"] != "" && p.ID != app.Settings["page_id"]:
			continue
		}
		learned := map[string]string{"page_id": p.ID}
		account := p.Name
		if instagram {
			learned["account_id"] = p.Instagram.ID
			account = p.Name + " (Instagram)"
		}
		return Credentials{AccessToken: p.AccessToken, Expiry: time.Now().Add(forever), Account: account}, learned, nil
	}
	if instagram {
		return c, nil, errors.New("no Facebook Page of this account has an Instagram professional account linked: link it in the Instagram app (Settings → Account type) and sign in again")
	}
	return c, nil, errors.New("this Facebook account manages no Page (Kitties publishes as a Page): create one, or ask its admin for access, and sign in again")
}
