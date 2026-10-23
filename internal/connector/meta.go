package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// FACEBOOK AND INSTAGRAM.
//
// Both speak Meta's Graph API with a token: a Facebook Page's, or an Instagram professional
// (business or creator) account's. A kitty publishes, reads what was published and what people
// commented, answers comments and, on Facebook, the Page's Messenger inbox. Publishing,
// replying, liking and messaging reach people, so they wait for a yes until the kitty is trusted.

// graphVersion is the Graph API version the connectors speak.
const graphVersion = "v23.0"

// graphRoot is where a token's calls go: an Instagram Login token (IG…) has its own host, a
// Facebook one (Page or user token, EAA…) the usual one.
func graphRoot(token string) string {
	if strings.HasPrefix(token, "IG") {
		return "https://graph.instagram.com/" + graphVersion
	}
	return "https://graph.facebook.com/" + graphVersion
}

// graphNode is the URL of a node (a page, a post, a comment) and an edge below it.
func graphNode(s Settings, id, edge string) string {
	u := graphRoot(s["token"]) + "/" + url.PathEscape(id)
	if edge != "" {
		u += "/" + edge
	}
	return u
}

// metaKinds are the Facebook and Instagram kinds of the catalogue.
func metaKinds() []Kind {
	return []Kind{
		{Name: "facebook", Category: CatSocial, Label: "Facebook Page", Help: "publishes on a Facebook Page, reads its posts and comments, answers them and the Page's Messenger inbox",
			Fields: signIn([]Field{f("page_id", "Page id", "signing in picks your Page (the first one, or this one)", false, false)},
				f("token", "Page access token", "a long-lived Page token (pages_manage_posts, pages_read_engagement, pages_manage_engagement, pages_messaging)", false, true), "developers.facebook.com"),
			Actions: []Action{
				{Name: "post", Help: "publish on the Page: text, a link, or a picture from a public image URL", Args: []string{fText, "link (optional)", "image_url (optional, a public https URL)"}},
				{Name: "posts", Help: "the Page's latest posts", Args: []string{fLimit}},
				{Name: "comments", Help: "the comments on a post", Args: []string{"id (the post's)", fLimit}},
				{Name: "reply", Help: "comment on a post, or answer a comment", Args: []string{"id (a post's or a comment's)", fText}},
				{Name: "like", Help: "like a post or a comment as the Page", Args: []string{"id"}},
				{Name: "inbox", Help: "the latest Messenger conversations: who wrote and their last message", Args: []string{fLimit}},
				{Name: "send", Help: "answer someone in Messenger (within 24 hours of their last message)", Args: []string{"to (their id, from inbox)", fText}},
			}, make: makeFacebook, oauth: "facebook"},
		{Name: "instagram", Category: CatSocial, Label: "Instagram", Help: "publishes pictures and reels on an Instagram professional account, reads its posts and comments and answers them",
			Fields: signIn([]Field{f("account_id", "Instagram account id", "signing in with Facebook finds the professional account linked to your Page", false, false)},
				f("token", "Access token", "a Facebook Page token with instagram_basic and instagram_content_publish, or an Instagram Login token (IG…)", false, true), "developers.facebook.com"),
			Actions: []Action{
				{Name: "post", Help: "publish a picture (or a reel) from a public URL with a caption", Args: []string{"image_url (a public https JPEG) or video_url (a reel)", "text (the caption)"}},
				{Name: "posts", Help: "the account's latest posts, with likes and comments", Args: []string{fLimit}},
				{Name: "comments", Help: "the comments on a post", Args: []string{"id (the post's)", fLimit}},
				{Name: "reply", Help: "answer a comment", Args: []string{"id (the comment's)", fText}},
			}, make: makeInstagram, oauth: "facebook"},
	}
}

// metaList renders the reading actions of both kinds as one line per item.
func metaList(kind, action string, data []byte) string {
	switch kind + "." + action {
	case "facebook.posts":
		return listing(data, "data", 30, "id", "created_time", "message", "permalink_url")
	case "facebook.comments":
		return listing(data, "data", 30, "id", "from.name", "message", "created_time")
	case "facebook.inbox":
		return listing(data, "data", 30, "messages.data.0.from.id", "messages.data.0.from.name", "messages.data.0.message", "updated_time")
	case "instagram.posts":
		return listing(data, "data", 30, "id", "timestamp", "media_type", "caption", "like_count", "comments_count", "permalink")
	case "instagram.comments":
		return listing(data, "data", 30, "id", "username", "text", "timestamp")
	}
	return summary(data)
}

func limitQuery(a map[string]any) string { return "limit=" + strconv.Itoa(limitOf(a)) }

// pageOf is the Facebook Page the connector acts as; signing in fills it.
func pageOf(s Settings) (string, error) {
	if s["page_id"] == "" {
		return "", errors.New("the Page is not set: sign in with Facebook (it picks your Page), or type the Page id")
	}
	return s["page_id"], nil
}

func makeFacebook(s Settings) Connector {
	auth := bearer(s["token"])
	c := newAPI("facebook", s, map[string]actionFn{
		"post": func(s Settings, a map[string]any) (request, error) {
			if _, err := pageOf(s); err != nil {
				return request{}, err
			}
			text := str(a, "text", "message")
			if img := str(a, "image_url", "image"); img != "" {
				return request{method: "POST", url: graphNode(s, s["page_id"], "photos"), headers: auth, body: map[string]any{"url": img, "caption": text}}, nil
			}
			link := str(a, "link", "url")
			if text == "" && link == "" {
				return request{}, errors.New("text is missing (or a link, or an image_url)")
			}
			body := map[string]any{"message": text}
			if link != "" {
				body["link"] = link
			}
			return request{method: "POST", url: graphNode(s, s["page_id"], "feed"), headers: auth, body: body}, nil
		},
		"posts": func(s Settings, a map[string]any) (request, error) {
			if _, err := pageOf(s); err != nil {
				return request{}, err
			}
			return request{method: "GET", url: graphNode(s, s["page_id"], "posts") + "?fields=id,message,created_time,permalink_url&" + limitQuery(a), headers: auth}, nil
		},
		"comments": func(s Settings, a map[string]any) (request, error) {
			id, err := need(a, "id", "post")
			if err != nil {
				return request{}, err
			}
			return request{method: "GET", url: graphNode(s, id, "comments") + "?fields=id,from,message,created_time&order=reverse_chronological&" + limitQuery(a), headers: auth}, nil
		},
		"reply": func(s Settings, a map[string]any) (request, error) {
			id, err := need(a, "id", "to")
			if err != nil {
				return request{}, err
			}
			text, err := need(a, "text", "message")
			if err != nil {
				return request{}, err
			}
			return request{method: "POST", url: graphNode(s, id, "comments"), headers: auth, body: map[string]any{"message": text}}, nil
		},
		"like": func(s Settings, a map[string]any) (request, error) {
			id, err := need(a, "id", "to")
			if err != nil {
				return request{}, err
			}
			return request{method: "POST", url: graphNode(s, id, "likes"), headers: auth}, nil
		},
		"inbox": func(s Settings, a map[string]any) (request, error) {
			if _, err := pageOf(s); err != nil {
				return request{}, err
			}
			fields := url.QueryEscape("updated_time,messages.limit(1){message,from}")
			return request{method: "GET", url: graphNode(s, s["page_id"], "conversations") + "?platform=messenger&fields=" + fields + "&" + limitQuery(a), headers: auth}, nil
		},
		"send": func(s Settings, a map[string]any) (request, error) {
			if _, err := pageOf(s); err != nil {
				return request{}, err
			}
			to, err := need(a, "to", "id")
			if err != nil {
				return request{}, err
			}
			text, err := need(a, "text", "message")
			if err != nil {
				return request{}, err
			}
			return request{method: "POST", url: graphNode(s, s["page_id"], "messages"), headers: auth, body: map[string]any{
				"recipient": map[string]any{"id": to}, "messaging_type": "RESPONSE", "message": map[string]any{"text": text}}}, nil
		},
	}, "post")
	c.render = func(action string, data []byte) string { return metaList("facebook", action, data) }
	return c
}

// errAccount is an Instagram connection that does not know its account yet.
var errAccount = errors.New("the Instagram account is not set: sign in with Facebook (it finds the account linked to your Page), or type its id")

// igPoll is how often a reel's upload is checked while Instagram processes it.
var igPoll = 3 * time.Second

func makeInstagram(s Settings) Connector {
	auth := bearer(s["token"])
	account := s["account_id"]
	if account == "" && strings.HasPrefix(s["token"], "IG") {
		account = "me" // an Instagram Login token speaks for its own account
	}
	c := newAPI("instagram", s, map[string]actionFn{
		"post": func(s Settings, a map[string]any) (request, error) {
			if account == "" {
				return request{}, errAccount
			}
			body := map[string]any{"caption": str(a, "text", "caption")}
			switch img, video := str(a, "image_url", "image"), str(a, "video_url", "video"); {
			case video != "":
				body["media_type"], body["video_url"] = "REELS", video
			case img != "":
				body["image_url"] = img
			default:
				return request{}, errors.New("image_url is missing: Instagram publishes a picture (a public https JPEG) or a reel (video_url)")
			}
			// Publishing is two steps: a container with the media, then publishing it once
			// Instagram has fetched (and, for a reel, processed) the file.
			return request{method: "POST", url: graphNode(s, account, "media"), headers: auth, body: body,
				follow: func(ctx context.Context, raw []byte, headers map[string]string) (string, error) {
					return igPublish(ctx, s, account, raw, headers)
				}}, nil
		},
		"posts": func(s Settings, a map[string]any) (request, error) {
			if account == "" {
				return request{}, errAccount
			}
			return request{method: "GET", url: graphNode(s, account, "media") + "?fields=id,caption,media_type,permalink,timestamp,like_count,comments_count&" + limitQuery(a), headers: auth}, nil
		},
		"comments": func(s Settings, a map[string]any) (request, error) {
			id, err := need(a, "id", "post")
			if err != nil {
				return request{}, err
			}
			return request{method: "GET", url: graphNode(s, id, "comments") + "?fields=id,username,text,timestamp&" + limitQuery(a), headers: auth}, nil
		},
		"reply": func(s Settings, a map[string]any) (request, error) {
			id, err := need(a, "id", "to")
			if err != nil {
				return request{}, err
			}
			text, err := need(a, "text", "message")
			if err != nil {
				return request{}, err
			}
			return request{method: "POST", url: graphNode(s, id, "replies"), headers: auth, body: map[string]any{"message": text}}, nil
		},
	}, "post")
	c.render = func(action string, data []byte) string { return metaList("instagram", action, data) }
	return c
}

// igPublish waits for the container to be ready, publishes it and says where the post is.
func igPublish(ctx context.Context, s Settings, account string, raw []byte, headers map[string]string) (string, error) {
	var container struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &container) != nil || container.ID == "" {
		return "", errors.New("instagram did not take the media: " + summary(raw))
	}
	for {
		out, err := do(ctx, request{method: "GET", url: graphNode(s, container.ID, "") + "?fields=status_code,status", headers: headers})
		if err != nil {
			return "", err
		}
		var st struct {
			Code   string `json:"status_code"`
			Status string `json:"status"`
		}
		_ = json.Unmarshal(out, &st)
		if st.Code == "FINISHED" || st.Code == "PUBLISHED" || st.Code == "" {
			break
		}
		if st.Code == "ERROR" || st.Code == "EXPIRED" {
			return "", fmt.Errorf("instagram could not use the media (%s): check that the URL is public and the file is a JPEG or an MP4 it accepts", strings.TrimSpace(st.Code+" "+st.Status))
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("instagram is still processing the media: %w", ctx.Err())
		case <-time.After(igPoll):
		}
	}
	out, err := do(ctx, request{method: "POST", url: graphNode(s, account, "media_publish"), headers: headers, body: map[string]any{"creation_id": container.ID}})
	if err != nil {
		return "", err
	}
	var post struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(out, &post) != nil || post.ID == "" {
		return summary(out), nil
	}
	// The link is a nicety: the post is out even when it cannot be read back.
	if link, err := do(ctx, request{method: "GET", url: graphNode(s, post.ID, "") + "?fields=permalink", headers: headers}); err == nil {
		var p struct {
			Permalink string `json:"permalink"`
		}
		if json.Unmarshal(link, &p) == nil && p.Permalink != "" {
			return "published: " + p.Permalink + " (id " + post.ID + ")", nil
		}
	}
	return "published, id " + post.ID, nil
}
