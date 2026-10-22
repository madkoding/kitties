package connector

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Settings are the values a connector was configured with, by field name (url, token, ...).
type Settings map[string]string

// Field is one setting a kind needs.
type Field struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Help        string `json:"help,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// Action is one thing a kind can do, as the model and the page see it.
type Action struct {
	Name string   `json:"name"`
	Help string   `json:"help"`
	Args []string `json:"args,omitempty"`
	// Risk is "ask" for an action that reaches other people or spends money (the person
	// approves it until trusted) or "always" for one that is never done unasked; empty for
	// the ones that only read or touch the person's own tools.
	Risk string `json:"risk,omitempty"`
}

// Kind describes one connector of the catalogue.
type Kind struct {
	Name     string   `json:"name"`
	Category string   `json:"category"`
	Label    string   `json:"label"`
	Help     string   `json:"help"`
	Fields   []Field  `json:"fields"`
	Actions  []Action `json:"actions"`
	// OAuth names the identity service the kind signs in with ("google", "microsoft", "slack",
	// "github"), empty for a kind that only takes a key or a token. A kind with a token field
	// takes either: the token when typed, the sign-in otherwise.
	OAuth string `json:"oauth,omitempty"`
	// Inbound marks a kind whose service can post events to the gateway (inbound_secret).
	Inbound bool `json:"inbound,omitempty"`
	// make builds the connector; nil for the kinds built elsewhere (shell, motita, webhook).
	make  func(s Settings) Connector
	oauth string
}

// The categories, in the order the page shows them.
const (
	CatMessaging    = "messaging"
	CatSocial       = "social media"
	CatGit          = "git"
	CatProductivity = "productivity"
	CatOffice       = "office"
	CatDrawing      = "drawing"
	CatFinance      = "finance"
	CatGeneric      = "generic"
)

// Categories lists them in order.
var Categories = []string{CatMessaging, CatSocial, CatGit, CatProductivity, CatOffice, CatFinance, CatDrawing, CatGeneric}

func f(name, label, help string, required, secret bool) Field {
	return Field{Name: name, Label: label, Help: help, Required: required, Secret: secret}
}

var (
	fURL     = f("url", "URL", "", true, false)
	fToken   = f("token", "Token", "", true, true)
	fOptTok  = f("token", "Token", "", false, true)
	fText    = "text (what to say)"
	fTitle   = "title"
	fBody    = "body"
	fRepo    = "repo (owner/name; the default when empty)"
	fContent = "content (the file's text)"
)

// Catalog is every kind, by category. It is filled by init: the kinds name their builders and
// the builders look kinds up, which a static initialiser would call a cycle.
var Catalog []Kind

func init() {
	Catalog = catalog()
}

func catalog() []Kind {
	kinds := withReads(withInbound(append(moreKinds(), []Kind{
		// ── messaging ──────────────────────────────────────────────────────────────────────────
		{Name: "slack", Category: CatMessaging, Label: "Slack", Help: "posts messages to a Slack channel, through a webhook, a bot token or a sign-in",
			Fields: []Field{f("url", "Incoming webhook URL", "from Slack's app settings", false, true), f("token", "Bot token", "xoxb-…, instead of a webhook", false, true), f("channel", "Channel", "#general, used with a bot token or a sign-in", false, false),
				f("client_id", "OAuth client id", "to sign in instead of pasting a token: a Slack app of your own", false, false), f("client_secret", "OAuth client secret", "", false, true)},
			Actions: []Action{{Name: "send", Help: "post a message", Args: []string{fText}}}, make: makeSlack, oauth: "slack"},
		{Name: "discord", Category: CatMessaging, Label: "Discord", Help: "posts messages to a Discord channel",
			Fields: []Field{f("url", "Webhook URL", "", true, true)}, Actions: []Action{{Name: "send", Help: "post a message", Args: []string{fText}}}, make: makeDiscord},
		{Name: "telegram", Category: CatMessaging, Label: "Telegram", Help: "sends messages through a bot",
			Fields:  []Field{f("token", "Bot token", "from @BotFather", true, true), f("chat_id", "Chat id", "", true, false)},
			Actions: []Action{{Name: "send", Help: "send a message", Args: []string{fText}}}, make: makeTelegram},
		{Name: "whatsapp", Category: CatMessaging, Label: "WhatsApp", Help: "sends messages with the WhatsApp Cloud API",
			Fields:  []Field{fToken, f("phone_id", "Phone number id", "", true, false), f("to", "Default recipient", "+5491122334455", false, false)},
			Actions: []Action{{Name: "send", Help: "send a text message", Args: []string{fText, "to (phone, optional)"}}}, make: makeWhatsApp},
		{Name: "email", Category: CatMessaging, Label: "Email", Help: "sends mail through an SMTP server",
			Fields:  []Field{f("host", "SMTP host:port", "smtp.gmail.com:587", true, false), f("user", "User", "", true, false), f("password", "Password", "", true, true), f("from", "From", "", true, false), f("to", "Default recipient", "", false, false)},
			Actions: []Action{{Name: "send", Help: "send a mail", Args: []string{"subject", fText, "to (optional)"}}}, make: makeEmail},
		{Name: "matrix", Category: CatMessaging, Label: "Matrix", Help: "sends messages to a Matrix room",
			Fields:  []Field{f("url", "Homeserver", "https://matrix.org", true, false), fToken, f("room", "Room id", "!abc:matrix.org", true, false)},
			Actions: []Action{{Name: "send", Help: "send a message", Args: []string{fText}}}, make: makeMatrix},
		// ── social media ───────────────────────────────────────────────────────────────────────
		{Name: "mastodon", Category: CatSocial, Label: "Mastodon", Help: "posts to a Mastodon account",
			Fields:  []Field{f("url", "Instance", "https://mastodon.social", true, false), f("token", "Access token", "", true, true)},
			Actions: []Action{{Name: "post", Help: "publish a status", Args: []string{fText}}}, make: makeMastodon},
		{Name: "bluesky", Category: CatSocial, Label: "Bluesky", Help: "posts to a Bluesky account",
			Fields:  []Field{f("handle", "Handle", "name.bsky.social", true, false), f("password", "App password", "", true, true)},
			Actions: []Action{{Name: "post", Help: "publish a post", Args: []string{fText}}}, make: makeBluesky},
		{Name: "x", Category: CatSocial, Label: "X (Twitter)", Help: "posts to an X account",
			Fields:  []Field{f("token", "OAuth 2.0 user token", "with tweet.write", true, true)},
			Actions: []Action{{Name: "post", Help: "publish a post", Args: []string{fText}}}, make: makeX},
		{Name: "linkedin", Category: CatSocial, Label: "LinkedIn", Help: "shares a post from a LinkedIn member",
			Fields:  []Field{fToken, f("author", "Author URN", "urn:li:person:…", true, false)},
			Actions: []Action{{Name: "post", Help: "share a post", Args: []string{fText}}}, make: makeLinkedIn},
		// ── git ────────────────────────────────────────────────────────────────────────────────
		{Name: "github", Category: CatGit, Label: "GitHub", Help: "issues, pull requests and files on GitHub, with a token or a sign-in",
			Fields: []Field{f("token", "Token", "a fine-grained or classic token; leave empty to sign in", false, true), f("repo", "Default repository", "owner/name", false, false),
				f("client_id", "OAuth client id", "to sign in instead of pasting a token: an OAuth app of your own", false, false), f("client_secret", "OAuth client secret", "", false, true)},
			Actions: []Action{
				{Name: "create_issue", Help: "open an issue", Args: []string{fRepo, fTitle, fBody}},
				{Name: "comment", Help: "comment on an issue or pull request", Args: []string{fRepo, "number", fText}},
				{Name: "create_pr", Help: "open a pull request", Args: []string{fRepo, fTitle, "head", "base", fBody}},
				{Name: "get_file", Help: "read a file of the repository", Args: []string{fRepo, "path", "ref (optional)"}},
			}, make: makeGitHub, oauth: "github"},
		{Name: "gitlab", Category: CatGit, Label: "GitLab", Help: "issues and merge requests on GitLab",
			Fields: []Field{f("url", "Instance", "https://gitlab.com when empty", false, false), f("token", "Token", "", true, true), f("project", "Default project", "group/name", false, false)},
			Actions: []Action{
				{Name: "create_issue", Help: "open an issue", Args: []string{"project", fTitle, "description"}},
				{Name: "comment", Help: "comment on an issue", Args: []string{"project", "iid", fText}},
			}, make: makeGitLab},
		{Name: "gitea", Category: CatGit, Label: "Gitea / Forgejo", Help: "issues on a Gitea or Forgejo server",
			Fields:  []Field{fURL, fToken, f("repo", "Default repository", "owner/name", false, false)},
			Actions: []Action{{Name: "create_issue", Help: "open an issue", Args: []string{fRepo, fTitle, fBody}}}, make: makeGitea},
		{Name: "motita", Category: CatGit, Label: "Motita", Help: "runs a coding task in motita (an agent that proves it is done) and returns the result",
			Fields:  []Field{f("url", "Gateway", "http://127.0.0.1:7477", false, false), fOptTok},
			Actions: []Action{{Name: "run", Help: "run a task", Args: []string{"task"}}}},
		// ── productivity ───────────────────────────────────────────────────────────────────────
		{Name: "notion", Category: CatProductivity, Label: "Notion", Help: "pages in a Notion workspace",
			Fields: []Field{f("token", "Integration token", "", true, true), f("parent", "Default parent page id", "", false, false)},
			Actions: []Action{
				{Name: "create_page", Help: "create a page with a paragraph", Args: []string{fTitle, fText, "parent (page id, optional)"}},
				{Name: "append", Help: "append a paragraph to a page", Args: []string{"page", fText}},
			}, make: makeNotion},
		{Name: "trello", Category: CatProductivity, Label: "Trello", Help: "cards on a Trello board",
			Fields:  []Field{f("key", "API key", "", true, true), fToken, f("list", "Default list id", "", false, false)},
			Actions: []Action{{Name: "create_card", Help: "add a card", Args: []string{"name", "desc", "list (optional)"}}}, make: makeTrello},
		{Name: "todoist", Category: CatProductivity, Label: "Todoist", Help: "tasks in Todoist",
			Fields:  []Field{f("token", "API token", "", true, true), f("project", "Default project id", "", false, false)},
			Actions: []Action{{Name: "add_task", Help: "add a task", Args: []string{"content", "due (natural language, optional)"}}}, make: makeTodoist},
		{Name: "linear", Category: CatProductivity, Label: "Linear", Help: "issues in Linear",
			Fields:  []Field{f("token", "API key", "", true, true), f("team", "Team id", "", true, false)},
			Actions: []Action{{Name: "create_issue", Help: "create an issue", Args: []string{fTitle, "description"}}}, make: makeLinear},
		{Name: "jira", Category: CatProductivity, Label: "Jira", Help: "issues in Jira Cloud",
			Fields:  []Field{f("url", "Site", "https://acme.atlassian.net", true, false), f("email", "Email", "", true, false), f("token", "API token", "", true, true), f("project", "Project key", "KIT", true, false)},
			Actions: []Action{{Name: "create_issue", Help: "create a task", Args: []string{"summary", "description"}}}, make: makeJira},
		{Name: "google-chat", Category: CatMessaging, Label: "Google Chat", Help: "posts to a Google Chat space through a webhook",
			Fields: []Field{f("url", "Webhook URL", "", true, true)}, Actions: []Action{{Name: "send", Help: "post a message", Args: []string{fText}}}, make: makeGoogleChat},
		{Name: "teams", Category: CatMessaging, Label: "Microsoft Teams", Help: "posts to a Teams channel through a workflow webhook",
			Fields: []Field{f("url", "Webhook URL", "", true, true)}, Actions: []Action{{Name: "send", Help: "post a message", Args: []string{fText}}}, make: makeTeams},
		// ── office ─────────────────────────────────────────────────────────────────────────────
		{Name: "google-workspace", Category: CatOffice, Label: "Google Workspace", Help: "Gmail, Calendar, Drive, Sheets and Docs of a Google account, signed in with OAuth",
			Fields: []Field{f("client_id", "OAuth client id", "a Desktop or Web client of your Google Cloud project", true, false), f("client_secret", "OAuth client secret", "", false, true)},
			Actions: []Action{
				{Name: "send_mail", Help: "send a mail from the account", Args: []string{"to", "subject", fText}},
				{Name: "create_event", Help: "add a calendar event", Args: []string{"summary", "start (RFC 3339)", "end (RFC 3339)", "description (optional)"}},
				{Name: "upload", Help: "create a text file in Drive", Args: []string{"name", fContent}},
				{Name: "append_row", Help: "append a row to a spreadsheet", Args: []string{"spreadsheet (id)", "values (a list)", "range (optional, Sheet1!A1)"}},
				{Name: "create_doc", Help: "create a document with text", Args: []string{fTitle, fText}},
			}, make: makeGoogleWorkspace, oauth: "google"},
		{Name: "microsoft-365", Category: CatOffice, Label: "Microsoft 365", Help: "Outlook mail, Calendar and OneDrive of a Microsoft account, signed in with OAuth",
			Fields: []Field{f("client_id", "Application (client) id", "an app registration in Entra ID with a public client or a secret", true, false), f("client_secret", "Client secret", "", false, true)},
			Actions: []Action{
				{Name: "send_mail", Help: "send a mail from the account", Args: []string{"to", "subject", fText}},
				{Name: "create_event", Help: "add a calendar event", Args: []string{"subject", "start (RFC 3339)", "end (RFC 3339)", "body (optional)"}},
				{Name: "upload", Help: "create a text file in OneDrive", Args: []string{"name", fContent}},
			}, make: makeMicrosoft365, oauth: "microsoft"},
		{Name: "airtable", Category: CatOffice, Label: "Airtable", Help: "records in an Airtable table",
			Fields:  []Field{f("token", "Personal access token", "", true, true), f("base", "Base id", "app…", true, false), f("table", "Table", "", true, false)},
			Actions: []Action{{Name: "add_record", Help: "add a record", Args: []string{"fields (an object of column: value)"}}}, make: makeAirtable},
		{Name: "nextcloud", Category: CatOffice, Label: "Nextcloud", Help: "files in a Nextcloud (WebDAV) folder",
			Fields:  []Field{f("url", "Server", "https://cloud.example.com", true, false), f("user", "User", "", true, false), f("password", "App password", "", true, true), f("folder", "Folder", "Kitties", false, false)},
			Actions: []Action{{Name: "upload", Help: "write a text file", Args: []string{"name", fContent}}, {Name: "download", Help: "read a file", Args: []string{"name"}}}, make: makeNextcloud},
		{Name: "google-sheets", Category: CatOffice, Label: "Google Sheets", Help: "appends rows to a sheet through an Apps Script web app URL (deploy a doPost that appends e.values)",
			Fields:  []Field{f("url", "Web app URL", "https://script.google.com/macros/s/…/exec", true, true)},
			Actions: []Action{{Name: "append_row", Help: "append a row", Args: []string{"values (a list)"}}}, make: makeSheets},
		// ── drawing ────────────────────────────────────────────────────────────────────────────
		{Name: "kroki", Category: CatDrawing, Label: "Diagrams (Kroki)", Help: "renders a diagram (mermaid, plantuml, graphviz, d2, excalidraw…) to an SVG artifact",
			Fields:  []Field{f("url", "Server", "https://kroki.io (the default)", false, false)},
			Actions: []Action{{Name: "render", Help: "render a diagram", Args: []string{"type (mermaid, plantuml, graphviz, d2, …)", "source", "name (file name, optional)"}}}, make: makeKroki},
		{Name: "figma", Category: CatDrawing, Label: "Figma", Help: "reads and comments on Figma files",
			Fields:  []Field{f("token", "Personal access token", "", true, true)},
			Actions: []Action{{Name: "comment", Help: "comment on a file", Args: []string{"file (key)", fText}}, {Name: "get_file", Help: "read a file's structure (names of pages and frames)", Args: []string{"file (key)"}}}, make: makeFigma},
		{Name: "excalidraw", Category: CatDrawing, Label: "Excalidraw", Help: "saves an Excalidraw scene (JSON elements) as an artifact to open at excalidraw.com",
			Fields: []Field{}, Actions: []Action{{Name: "save", Help: "save a scene", Args: []string{"name", "elements (the scene's elements, a list)"}}}, make: makeExcalidraw},
		// ── generic ────────────────────────────────────────────────────────────────────────────
		{Name: "webhook", Category: CatGeneric, Label: "Webhook", Help: "posts the arguments as JSON to a URL",
			Fields: []Field{fURL, f("headers", "Headers", "Name: value, one per line", false, true)}, Actions: []Action{{Name: "post", Help: "post the arguments", Args: []string{"anything"}}}},
		{Name: "http", Category: CatGeneric, Label: "HTTP API", Help: "calls any HTTP API with a bearer token",
			Fields:  []Field{f("url", "Base URL", "https://api.example.com", true, false), fOptTok},
			Actions: []Action{{Name: "call", Help: "make a request", Args: []string{"method", "path", "body (optional JSON)"}}}, make: makeHTTP},
		{Name: "shell", Category: CatGeneric, Label: "Program", Help: "runs a fixed program on the gateway's machine with the text as its argument",
			Fields: []Field{f("command", "Command", "/usr/local/bin/notify", true, false)}, Actions: []Action{{Name: "run", Help: "run it", Args: []string{fText}}}},
	}...)))
	return kinds
}

// withReads adds to the kinds that wrote only the actions that read, reply and like.
func withReads(kinds []Kind) []Kind {
	for i := range kinds {
		kinds[i].Actions = append(kinds[i].Actions, readActions(kinds[i].Name)...)
		kinds[i].Fields = append(kinds[i].Fields, readFields(kinds[i].Name)...)
		for j := range kinds[i].Actions {
			if r := RiskOf(kinds[i].Name, kinds[i].Actions[j].Name); r != RiskNone {
				kinds[i].Actions[j].Risk = r.String()
			}
		}
	}
	return kinds
}

// withInbound adds the incoming events field to the kinds whose services report events.
func withInbound(kinds []Kind) []Kind {
	for i := range kinds {
		if inboundCategories[kinds[i].Category] && kinds[i].Name != "shell" {
			kinds[i].Fields = append(kinds[i].Fields, fInbound)
			kinds[i].Inbound = true
		}
	}
	return kinds
}

// fInbound is the field a kind that receives events takes: the secret the service presents
// when it posts to the connector's hook (POST /v1/hooks/<name>), so a kitty can act on
// what arrives.
var fInbound = f("inbound_secret", "Incoming events secret", "to receive events at /v1/hooks/<name>: GitHub's webhook secret, or any secret sent as X-Kitties-Secret", false, true)

// inboundCategories are the categories whose services report events.
var inboundCategories = map[string]bool{CatMessaging: true, CatGit: true, CatProductivity: true, CatFinance: true, CatGeneric: true}

// Kinds lists the catalogue, by category then name.
func Kinds() []Kind {
	out := append([]Kind(nil), Catalog...)
	for i := range out {
		out[i].OAuth = out[i].oauth
	}
	sort.SliceStable(out, func(i, j int) bool {
		ci, cj := indexOf(Categories, out[i].Category), indexOf(Categories, out[j].Category)
		if ci != cj {
			return ci < cj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func indexOf(list []string, s string) int {
	for i, x := range list {
		if x == s {
			return i
		}
	}
	return len(list)
}

// LookupKind returns the catalogue entry called name.
func LookupKind(name string) (Kind, bool) {
	for _, k := range Catalog {
		if k.Name == name {
			k.OAuth = k.oauth
			return k, true
		}
	}
	return Kind{}, false
}

// TokenSource hands a connector a valid bearer token for its account, refreshing it when needed.
type TokenSource func(ctx context.Context) (string, error)

// tokenKey is the settings entry a token source is passed through: not a setting a person
// types, but what Build reads to wire OAuth kinds (see WithTokenSource).
const tokenKey = "\x00token_source"

// sources are the token sources handed to Build, by a key Build removes again.
var (
	sourcesMu sync.Mutex
	sources   = map[string]TokenSource{}
)

// WithTokenSource returns settings that carry the token source for an OAuth kind.
func WithTokenSource(s Settings, src TokenSource) Settings {
	out := Settings{}
	for k, v := range s {
		out[k] = v
	}
	key := fmt.Sprintf("src-%p", &src)
	sourcesMu.Lock()
	sources[key] = src
	sourcesMu.Unlock()
	out[tokenKey] = key
	return out
}

// tokenSourceOf takes the token source out of the settings, if one was given.
func tokenSourceOf(s Settings) TokenSource {
	key, ok := s[tokenKey]
	if !ok {
		return nil
	}
	delete(s, tokenKey)
	sourcesMu.Lock()
	defer sourcesMu.Unlock()
	src := sources[key]
	delete(sources, key)
	return src
}

// Validate checks that the settings hold what the kind requires.
func (k Kind) Validate(s Settings) error {
	for _, fl := range k.Fields {
		if fl.Required && strings.TrimSpace(s[fl.Name]) == "" {
			return fmt.Errorf("%s needs %s", k.Name, strings.ToLower(fl.Label))
		}
	}
	// A Slack connector takes a webhook, or a channel to post to with a bot token or a sign-in.
	if k.Name == "slack" && s["url"] == "" && s["channel"] == "" {
		return errors.New("slack needs a webhook url, or a channel with a bot token or a sign-in")
	}
	return nil
}

// TokenSet reports whether the settings carry a token or webhook of their own, so an OAuth
// kind with one works without a sign-in.
func (k Kind) TokenSet(s Settings) bool {
	for _, f := range k.Fields {
		if (f.Name == "token" || f.Name == "url") && strings.TrimSpace(s[f.Name]) != "" {
			return true
		}
	}
	return false
}

// Kinder is a connector that knows its kind in the catalogue (the page shows its logo).
type Kinder interface {
	Kind() string
}

// Build creates the connector of a kind from its settings. shell, motita and webhook are built
// by the caller (they need nothing from the catalogue beyond validation).
func Build(kind string, s Settings, description string) (Connector, error) {
	k, ok := LookupKind(kind)
	if !ok {
		return nil, fmt.Errorf("%q is not a connector kind", kind)
	}
	if err := k.Validate(s); err != nil {
		return nil, err
	}
	var plain Connector
	switch kind {
	case "shell":
		plain = Shell{Command: s["command"], Description: description}
	case "motita":
		plain = Motita{URL: s["url"], Token: s["token"], Description: description}
	case "webhook":
		plain = Webhook{URL: s["url"], Headers: parseHeaders(s["headers"]), Description: description}
	}
	if plain != nil {
		return described{Connector: plain, text: plain.Describe(), kind: kind}, nil
	}
	src := tokenSourceOf(s)
	c := k.make(s)
	if a, ok := c.(*api); ok && src != nil {
		a.token = src
	}
	if description != "" {
		return described{Connector: c, text: description + " " + actionsHelp(k), kind: kind}, nil
	}
	return described{Connector: c, text: k.Help + " " + actionsHelp(k), kind: kind}, nil
}

// described wraps a connector with the text the model reads and its kind.
type described struct {
	Connector
	text string
	kind string
}

func (d described) Describe() string { return d.text }

// Kind implements Kinder.
func (d described) Kind() string { return d.kind }

// CallFile forwards to the wrapped connector's file answer, or its plain one.
func (d described) CallFile(ctx context.Context, args map[string]any) (string, string, []byte, error) {
	if f, ok := d.Connector.(Filer); ok {
		return f.CallFile(ctx, args)
	}
	out, err := d.Connector.Call(ctx, args)
	return out, "", nil, err
}

// actionsHelp renders the actions for the prompt: the model picks one with "action".
func actionsHelp(k Kind) string {
	if len(k.Actions) == 0 {
		return ""
	}
	parts := make([]string, 0, len(k.Actions))
	for _, a := range k.Actions {
		part := a.Name + "(" + strings.Join(a.Args, ", ") + ")"
		switch a.Risk {
		case "ask":
			part += " [the person approves it first]"
		case "always":
			part += " [always needs the person's approval]"
		}
		parts = append(parts, part)
	}
	return "(actions, chosen with \"action\": " + strings.Join(parts, "; ") + ")"
}

func parseHeaders(raw string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) != "" {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

// ── the generic API connector ──────────────────────────────────────────────────────────────

// request is one HTTP call an action builds.
type request struct {
	method  string
	url     string
	headers map[string]string
	body    any    // JSON unless raw is set
	raw     []byte // a body sent as is
	ctype   string
	// basic is a user:password pair for basic auth.
	basic [2]string
	// file names the answer as a file for the artifacts (the answer's bytes are the file).
	file string
	// plain marks a request that takes no account token (a webhook): an OAuth kind leaves it.
	plain bool
	// follow, when set, turns the answer into the text for the model, with more requests if it
	// needs them (it gets the headers, the account's token included).
	follow func(ctx context.Context, raw []byte, headers map[string]string) (string, error)
}

type actionFn func(s Settings, args map[string]any) (request, error)

// api is a connector made of actions over an HTTP service.
type api struct {
	kind    Kind
	s       Settings
	actions map[string]actionFn
	first   string
	// render turns a successful answer into what the model reads.
	render func(action string, data []byte) string
	// token, when set, signs every request with the account's bearer token (OAuth kinds).
	token TokenSource
}

func newAPI(kind string, s Settings, actions map[string]actionFn, first string) *api {
	k, _ := LookupKind(kind)
	for name, fn := range extraActions(kind) {
		if _, taken := actions[name]; !taken {
			actions[name] = fn
		}
	}
	return &api{kind: k, s: s, actions: actions, first: first}
}

// Describe implements Connector.
func (a *api) Describe() string { return a.kind.Help + " " + actionsHelp(a.kind) }

// Call implements Connector.
func (a *api) Call(ctx context.Context, args map[string]any) (string, error) {
	out, _, _, err := a.call(ctx, args)
	return out, err
}

// CallFile implements Filer: an action whose answer is a file returns it.
func (a *api) CallFile(ctx context.Context, args map[string]any) (string, string, []byte, error) {
	return a.call(ctx, args)
}

func (a *api) call(ctx context.Context, args map[string]any) (text, name string, data []byte, err error) {
	action := strings.ToLower(str(args, "action"))
	if action == "" {
		action = a.first
	}
	fn, ok := a.actions[action]
	if !ok {
		return "", "", nil, fmt.Errorf("%s has no action %q; %s", a.kind.Name, action, actionsHelp(a.kind))
	}
	req, err := fn(a.s, args)
	if err != nil {
		return "", "", nil, err
	}
	// An OAuth kind signs the request with the account's token, unless the request is plain
	// (a webhook) or already carries a token the person typed.
	if a.kind.oauth != "" && !req.plain && strings.TrimSpace(strings.TrimPrefix(req.headers["Authorization"], "Bearer")) == "" {
		if a.token == nil {
			return "", "", nil, errors.New("not signed in: connect the account from Settings")
		}
		tok, err := a.token(ctx)
		if err != nil {
			return "", "", nil, err
		}
		if req.headers == nil {
			req.headers = map[string]string{}
		}
		req.headers["Authorization"] = "Bearer " + tok
	}
	raw, err := do(ctx, req)
	if err != nil {
		return "", "", nil, err
	}
	if err := readCheck(a.kind.Name, raw); err != nil {
		return "", "", nil, err
	}
	if req.file != "" {
		return "saved as " + req.file, req.file, raw, nil
	}
	if req.follow != nil {
		out, err := req.follow(ctx, raw, req.headers)
		return out, "", nil, err
	}
	if out, ok := readRender(a.kind.Name, action, raw); ok {
		return out, "", nil, nil
	}
	if a.render != nil {
		return a.render(action, raw), "", nil, nil
	}
	return summary(raw), "", nil, nil
}

// Filer is a connector whose call may answer with a file, kept as an artifact.
type Filer interface {
	CallFile(ctx context.Context, args map[string]any) (text, name string, data []byte, err error)
}

// do performs a request and returns the answer's bytes, or an error naming the status.
func do(ctx context.Context, r request) ([]byte, error) {
	var body io.Reader
	ctype := r.ctype
	switch {
	case r.raw != nil:
		body = strings.NewReader(string(r.raw))
	case r.body != nil:
		data, err := json.Marshal(r.body)
		if err != nil {
			return nil, err
		}
		body = strings.NewReader(string(data))
		if ctype == "" {
			ctype = "application/json"
		}
	}
	req, err := http.NewRequestWithContext(ctx, r.method, r.url, body)
	if err != nil {
		return nil, err
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	req.Header.Set("User-Agent", "kitties")
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	if r.basic[0] != "" {
		req.SetBasicAuth(r.basic[0], r.basic[1])
	}
	resp, err := httpDo(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, clip(string(data), 300))
	}
	return data, nil
}

// summary says what an answer was, briefly: a JSON object's id or url when it has one.
func summary(data []byte) string {
	var m map[string]any
	if json.Unmarshal(data, &m) == nil {
		parts := []string{"done"}
		for _, k := range []string{"html_url", "url", "web_url", "id", "ts", "number", "iid", "key"} {
			if v, ok := m[k]; ok && v != nil {
				parts = append(parts, k+": "+fmt.Sprint(v))
			}
		}
		return strings.Join(parts, ", ")
	}
	if s := strings.TrimSpace(string(data)); s != "" {
		return clip(s, 500)
	}
	return "done"
}

// str takes the first of keys that holds text (or a number) in args.
func str(args map[string]any, keys ...string) string {
	for _, k := range keys {
		switch v := args[k].(type) {
		case string:
			if v != "" {
				return v
			}
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		case bool:
			return strconv.FormatBool(v)
		}
	}
	return ""
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func need(args map[string]any, keys ...string) (string, error) {
	v := str(args, keys...)
	if v == "" {
		return "", fmt.Errorf("%s is missing", keys[0])
	}
	return v, nil
}

func pick(args map[string]any, s Settings, key string) string {
	if v := str(args, key); v != "" {
		return v
	}
	return s[key]
}

func repoOf(args map[string]any, s Settings, key string) (string, error) {
	repo := pick(args, s, key)
	if repo == "" {
		return "", fmt.Errorf("%s is missing (owner/name)", key)
	}
	return repo, nil
}

func base(s Settings, def string) string {
	u := strings.TrimRight(s["url"], "/")
	if u == "" {
		return def
	}
	return u
}

// ── messaging ──────────────────────────────────────────────────────────────────────────────

func makeSlack(s Settings) Connector {
	return newAPI("slack", s, map[string]actionFn{"send": func(s Settings, a map[string]any) (request, error) {
		text, err := need(a, "text", "message")
		if err != nil {
			return request{}, err
		}
		if s["url"] != "" {
			return request{method: "POST", url: s["url"], body: map[string]any{"text": text}, plain: true}, nil
		}
		return request{method: "POST", url: "https://slack.com/api/chat.postMessage", headers: bearer(s["token"]),
			body: map[string]any{"channel": pick(a, s, "channel"), "text": text}}, nil
	}}, "send")
}

func makeDiscord(s Settings) Connector {
	return newAPI("discord", s, map[string]actionFn{"send": func(s Settings, a map[string]any) (request, error) {
		text, err := need(a, "text", "message")
		if err != nil {
			return request{}, err
		}
		return request{method: "POST", url: s["url"], body: map[string]any{"content": text}}, nil
	}}, "send")
}

func makeTelegram(s Settings) Connector {
	return newAPI("telegram", s, map[string]actionFn{"send": func(s Settings, a map[string]any) (request, error) {
		text, err := need(a, "text", "message")
		if err != nil {
			return request{}, err
		}
		return request{method: "POST", url: base(s, "https://api.telegram.org") + "/bot" + s["token"] + "/sendMessage",
			body: map[string]any{"chat_id": pick(a, s, "chat_id"), "text": text}}, nil
	}}, "send")
}

func makeWhatsApp(s Settings) Connector {
	return newAPI("whatsapp", s, map[string]actionFn{"send": func(s Settings, a map[string]any) (request, error) {
		text, err := need(a, "text", "message")
		if err != nil {
			return request{}, err
		}
		to := pick(a, s, "to")
		if to == "" {
			return request{}, errors.New("to is missing (a phone number)")
		}
		return request{method: "POST", url: "https://graph.facebook.com/v20.0/" + s["phone_id"] + "/messages", headers: bearer(s["token"]),
			body: map[string]any{"messaging_product": "whatsapp", "to": to, "type": "text", "text": map[string]any{"body": text}}}, nil
	}}, "send")
}

func makeGoogleChat(s Settings) Connector {
	return newAPI("google-chat", s, map[string]actionFn{"send": func(s Settings, a map[string]any) (request, error) {
		text, err := need(a, "text", "message")
		if err != nil {
			return request{}, err
		}
		return request{method: "POST", url: s["url"], body: map[string]any{"text": text}}, nil
	}}, "send")
}

func makeTeams(s Settings) Connector {
	return newAPI("teams", s, map[string]actionFn{"send": func(s Settings, a map[string]any) (request, error) {
		text, err := need(a, "text", "message")
		if err != nil {
			return request{}, err
		}
		return request{method: "POST", url: s["url"], body: map[string]any{"type": "message", "attachments": []map[string]any{{
			"contentType": "application/vnd.microsoft.card.adaptive",
			"content":     map[string]any{"type": "AdaptiveCard", "version": "1.4", "body": []map[string]any{{"type": "TextBlock", "text": text, "wrap": true}}},
		}}}}, nil
	}}, "send")
}

func makeMatrix(s Settings) Connector {
	return newAPI("matrix", s, map[string]actionFn{"send": func(s Settings, a map[string]any) (request, error) {
		text, err := need(a, "text", "message")
		if err != nil {
			return request{}, err
		}
		txn := strconv.FormatInt(time.Now().UnixNano(), 36)
		return request{method: "PUT", url: base(s, "") + "/_matrix/client/v3/rooms/" + url.PathEscape(s["room"]) + "/send/m.room.message/" + txn,
			headers: bearer(s["token"]), body: map[string]any{"msgtype": "m.text", "body": text}}, nil
	}}, "send")
}

// Email is the one connector without HTTP: net/smtp with STARTTLS where the server offers it.
type emailConnector struct {
	s Settings
}

var sendMail = smtp.SendMail

func makeEmail(s Settings) Connector { return &emailConnector{s: s} }

func (e *emailConnector) Describe() string {
	k, _ := LookupKind("email")
	return k.Help + " " + actionsHelp(k)
}

func (e *emailConnector) Call(_ context.Context, args map[string]any) (string, error) {
	text := str(args, "text", "body", "message")
	if text == "" {
		return "", errors.New("text is missing")
	}
	to := pick(args, e.s, "to")
	if to == "" {
		return "", errors.New("to is missing")
	}
	subject := str(args, "subject")
	if subject == "" {
		subject = "A message from your kitty"
	}
	host, _, _ := strings.Cut(e.s["host"], ":")
	msg := "From: " + e.s["from"] + "\r\nTo: " + to + "\r\nSubject: " + subject + "\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + text + "\r\n"
	auth := smtp.PlainAuth("", e.s["user"], e.s["password"], host)
	if err := sendMail(e.s["host"], auth, e.s["from"], strings.Split(to, ","), []byte(msg)); err != nil {
		return "", fmt.Errorf("send the mail: %w", err)
	}
	return "sent to " + to, nil
}

// ── social media ───────────────────────────────────────────────────────────────────────────

func makeMastodon(s Settings) Connector {
	return newAPI("mastodon", s, map[string]actionFn{"post": func(s Settings, a map[string]any) (request, error) {
		text, err := need(a, "text", "status")
		if err != nil {
			return request{}, err
		}
		return request{method: "POST", url: base(s, "") + "/api/v1/statuses", headers: bearer(s["token"]), body: map[string]any{"status": text}}, nil
	}}, "post")
}

// bluesky works in two steps: a session for the app password, then the call.
type blueskyConnector struct{ s Settings }

func makeBluesky(s Settings) Connector { return &blueskyConnector{s: s} }

func (b *blueskyConnector) Describe() string {
	k, _ := LookupKind("bluesky")
	return k.Help + " " + actionsHelp(k)
}

const bsky = "https://bsky.social/xrpc/"

// session opens a session with the app password and returns its token and the account's did.
func (b *blueskyConnector) session(ctx context.Context) (jwt, did string, err error) {
	raw, err := do(ctx, request{method: "POST", url: bsky + "com.atproto.server.createSession",
		body: map[string]any{"identifier": b.s["handle"], "password": b.s["password"]}})
	if err != nil {
		return "", "", fmt.Errorf("bluesky login: %w", err)
	}
	var session struct {
		Jwt string `json:"accessJwt"`
		Did string `json:"did"`
	}
	if json.Unmarshal(raw, &session) != nil || session.Jwt == "" {
		return "", "", errors.New("bluesky did not open a session")
	}
	return session.Jwt, session.Did, nil
}

func (b *blueskyConnector) Call(ctx context.Context, args map[string]any) (string, error) {
	action := strings.ToLower(str(args, "action"))
	if action == "" {
		action = "post"
	}
	switch action {
	case "post", "timeline", "notifications", "reply", "like":
	default:
		k, _ := LookupKind("bluesky")
		return "", fmt.Errorf("bluesky has no action %q; %s", action, actionsHelp(k))
	}
	// Validate before logging in: a missing argument should not cost a session.
	var text, uri string
	var err error
	switch action {
	case "post":
		text, err = need(args, "text")
	case "reply":
		if text, err = need(args, "text"); err == nil {
			uri, err = need(args, "uri", "id", "to")
		}
	case "like":
		uri, err = need(args, "uri", "id", "to")
	}
	if err != nil {
		return "", err
	}
	jwt, did, err := b.session(ctx)
	if err != nil {
		return "", err
	}
	auth := bearer(jwt)
	now := time.Now().UTC().Format(time.RFC3339)
	record := func(collection string, rec map[string]any) (string, error) {
		out, err := do(ctx, request{method: "POST", url: bsky + "com.atproto.repo.createRecord", headers: auth,
			body: map[string]any{"repo": did, "collection": collection, "record": rec}})
		if err != nil {
			return "", err
		}
		return summary(out), nil
	}
	switch action {
	case "timeline":
		out, err := do(ctx, request{method: "GET", url: bsky + "app.bsky.feed.getTimeline?limit=" + strconv.Itoa(limitOf(args)), headers: auth})
		if err != nil {
			return "", err
		}
		return listing(out, "feed", 20, "post.uri", "post.author.handle", "post.record.text"), nil
	case "notifications":
		out, err := do(ctx, request{method: "GET", url: bsky + "app.bsky.notification.listNotifications?limit=" + strconv.Itoa(limitOf(args)), headers: auth})
		if err != nil {
			return "", err
		}
		return listing(out, "notifications", 20, "uri", "reason", "author.handle", "record.text"), nil
	case "post":
		return record("app.bsky.feed.post", map[string]any{"$type": "app.bsky.feed.post", "text": text, "createdAt": now})
	}
	// A reply and a like point at a post: its uri and its cid, which only the service knows.
	out, err := do(ctx, request{method: "GET", url: bsky + "app.bsky.feed.getPosts?uris=" + url.QueryEscape(uri), headers: auth})
	if err != nil {
		return "", err
	}
	var posts struct {
		Posts []struct {
			URI    string `json:"uri"`
			CID    string `json:"cid"`
			Record struct {
				Reply *struct {
					Root struct{ URI, CID string } `json:"root"`
				} `json:"reply"`
			} `json:"record"`
		} `json:"posts"`
	}
	if json.Unmarshal(out, &posts) != nil || len(posts.Posts) == 0 {
		return "", fmt.Errorf("no such post %q", uri)
	}
	target := posts.Posts[0]
	ref := map[string]any{"uri": target.URI, "cid": target.CID}
	if action == "like" {
		return record("app.bsky.feed.like", map[string]any{"$type": "app.bsky.feed.like", "subject": ref, "createdAt": now})
	}
	root := ref
	if target.Record.Reply != nil {
		root = map[string]any{"uri": target.Record.Reply.Root.URI, "cid": target.Record.Reply.Root.CID}
	}
	return record("app.bsky.feed.post", map[string]any{"$type": "app.bsky.feed.post", "text": text, "createdAt": now, "reply": map[string]any{"root": root, "parent": ref}})
}

func makeX(s Settings) Connector {
	return newAPI("x", s, map[string]actionFn{"post": func(s Settings, a map[string]any) (request, error) {
		text, err := need(a, "text")
		if err != nil {
			return request{}, err
		}
		return request{method: "POST", url: "https://api.x.com/2/tweets", headers: bearer(s["token"]), body: map[string]any{"text": text}}, nil
	}}, "post")
}

func makeLinkedIn(s Settings) Connector {
	return newAPI("linkedin", s, map[string]actionFn{"post": func(s Settings, a map[string]any) (request, error) {
		text, err := need(a, "text")
		if err != nil {
			return request{}, err
		}
		headers := bearer(s["token"])
		headers["X-Restli-Protocol-Version"] = "2.0.0"
		return request{method: "POST", url: "https://api.linkedin.com/v2/ugcPosts", headers: headers, body: map[string]any{
			"author": s["author"], "lifecycleState": "PUBLISHED",
			"specificContent": map[string]any{"com.linkedin.ugc.ShareContent": map[string]any{"shareCommentary": map[string]any{"text": text}, "shareMediaCategory": "NONE"}},
			"visibility":      map[string]any{"com.linkedin.ugc.MemberNetworkVisibility": "PUBLIC"},
		}}, nil
	}}, "post")
}

// ── git ────────────────────────────────────────────────────────────────────────────────────

func makeGitHub(s Settings) Connector {
	gh := func(s Settings) map[string]string {
		h := bearer(s["token"])
		h["Accept"] = "application/vnd.github+json"
		return h
	}
	c := newAPI("github", s, map[string]actionFn{
		"create_issue": func(s Settings, a map[string]any) (request, error) {
			repo, err := repoOf(a, s, "repo")
			if err != nil {
				return request{}, err
			}
			title, err := need(a, "title")
			if err != nil {
				return request{}, err
			}
			return request{method: "POST", url: "https://api.github.com/repos/" + repo + "/issues", headers: gh(s), body: map[string]any{"title": title, "body": str(a, "body", "text")}}, nil
		},
		"comment": func(s Settings, a map[string]any) (request, error) {
			repo, err := repoOf(a, s, "repo")
			if err != nil {
				return request{}, err
			}
			number, err := need(a, "number", "issue")
			if err != nil {
				return request{}, err
			}
			text, err := need(a, "text", "body")
			if err != nil {
				return request{}, err
			}
			return request{method: "POST", url: "https://api.github.com/repos/" + repo + "/issues/" + number + "/comments", headers: gh(s), body: map[string]any{"body": text}}, nil
		},
		"create_pr": func(s Settings, a map[string]any) (request, error) {
			repo, err := repoOf(a, s, "repo")
			if err != nil {
				return request{}, err
			}
			title, err := need(a, "title")
			if err != nil {
				return request{}, err
			}
			head, err := need(a, "head")
			if err != nil {
				return request{}, err
			}
			b := str(a, "base")
			if b == "" {
				b = "main"
			}
			return request{method: "POST", url: "https://api.github.com/repos/" + repo + "/pulls", headers: gh(s), body: map[string]any{"title": title, "head": head, "base": b, "body": str(a, "body", "text")}}, nil
		},
		"get_file": func(s Settings, a map[string]any) (request, error) {
			repo, err := repoOf(a, s, "repo")
			if err != nil {
				return request{}, err
			}
			path, err := need(a, "path")
			if err != nil {
				return request{}, err
			}
			h := gh(s)
			h["Accept"] = "application/vnd.github.raw+json"
			u := "https://api.github.com/repos/" + repo + "/contents/" + strings.TrimPrefix(path, "/")
			if ref := str(a, "ref"); ref != "" {
				u += "?ref=" + url.QueryEscape(ref)
			}
			return request{method: "GET", url: u, headers: h}, nil
		},
	}, "create_issue")
	c.render = func(action string, data []byte) string {
		if action == "get_file" {
			return clip(string(data), 4000)
		}
		return summary(data)
	}
	return c
}

func makeGitLab(s Settings) Connector {
	return newAPI("gitlab", s, map[string]actionFn{
		"create_issue": func(s Settings, a map[string]any) (request, error) {
			project, err := repoOf(a, s, "project")
			if err != nil {
				return request{}, err
			}
			title, err := need(a, "title")
			if err != nil {
				return request{}, err
			}
			return request{method: "POST", url: base(s, "https://gitlab.com") + "/api/v4/projects/" + url.PathEscape(project) + "/issues",
				headers: map[string]string{"PRIVATE-TOKEN": s["token"]}, body: map[string]any{"title": title, "description": str(a, "description", "body", "text")}}, nil
		},
		"comment": func(s Settings, a map[string]any) (request, error) {
			project, err := repoOf(a, s, "project")
			if err != nil {
				return request{}, err
			}
			iid, err := need(a, "iid", "number")
			if err != nil {
				return request{}, err
			}
			text, err := need(a, "text", "body")
			if err != nil {
				return request{}, err
			}
			return request{method: "POST", url: base(s, "https://gitlab.com") + "/api/v4/projects/" + url.PathEscape(project) + "/issues/" + iid + "/notes",
				headers: map[string]string{"PRIVATE-TOKEN": s["token"]}, body: map[string]any{"body": text}}, nil
		},
	}, "create_issue")
}

func makeGitea(s Settings) Connector {
	return newAPI("gitea", s, map[string]actionFn{"create_issue": func(s Settings, a map[string]any) (request, error) {
		repo, err := repoOf(a, s, "repo")
		if err != nil {
			return request{}, err
		}
		title, err := need(a, "title")
		if err != nil {
			return request{}, err
		}
		return request{method: "POST", url: base(s, "") + "/api/v1/repos/" + repo + "/issues", headers: map[string]string{"Authorization": "token " + s["token"]},
			body: map[string]any{"title": title, "body": str(a, "body", "text")}}, nil
	}}, "create_issue")
}

// ── productivity ───────────────────────────────────────────────────────────────────────────

func notionParagraph(text string) map[string]any {
	return map[string]any{"object": "block", "type": "paragraph", "paragraph": map[string]any{"rich_text": []map[string]any{{"type": "text", "text": map[string]any{"content": text}}}}}
}

func makeNotion(s Settings) Connector {
	nh := func(s Settings) map[string]string {
		h := bearer(s["token"])
		h["Notion-Version"] = "2022-06-28"
		return h
	}
	return newAPI("notion", s, map[string]actionFn{
		"create_page": func(s Settings, a map[string]any) (request, error) {
			title, err := need(a, "title")
			if err != nil {
				return request{}, err
			}
			parent := pick(a, s, "parent")
			if parent == "" {
				return request{}, errors.New("parent is missing (a page id)")
			}
			body := map[string]any{"parent": map[string]any{"page_id": parent},
				"properties": map[string]any{"title": map[string]any{"title": []map[string]any{{"text": map[string]any{"content": title}}}}}}
			if text := str(a, "text", "body"); text != "" {
				body["children"] = []map[string]any{notionParagraph(text)}
			}
			return request{method: "POST", url: "https://api.notion.com/v1/pages", headers: nh(s), body: body}, nil
		},
		"append": func(s Settings, a map[string]any) (request, error) {
			page, err := need(a, "page", "page_id")
			if err != nil {
				return request{}, err
			}
			text, err := need(a, "text", "body")
			if err != nil {
				return request{}, err
			}
			return request{method: "PATCH", url: "https://api.notion.com/v1/blocks/" + page + "/children", headers: nh(s), body: map[string]any{"children": []map[string]any{notionParagraph(text)}}}, nil
		},
	}, "create_page")
}

func makeTrello(s Settings) Connector {
	return newAPI("trello", s, map[string]actionFn{"create_card": func(s Settings, a map[string]any) (request, error) {
		name, err := need(a, "name", "title", "text")
		if err != nil {
			return request{}, err
		}
		list := pick(a, s, "list")
		if list == "" {
			return request{}, errors.New("list is missing (a list id)")
		}
		q := url.Values{"key": {s["key"]}, "token": {s["token"]}, "idList": {list}, "name": {name}, "desc": {str(a, "desc", "body")}}
		return request{method: "POST", url: "https://api.trello.com/1/cards?" + q.Encode()}, nil
	}}, "create_card")
}

func makeTodoist(s Settings) Connector {
	return newAPI("todoist", s, map[string]actionFn{"add_task": func(s Settings, a map[string]any) (request, error) {
		content, err := need(a, "content", "text", "title")
		if err != nil {
			return request{}, err
		}
		body := map[string]any{"content": content}
		if due := str(a, "due"); due != "" {
			body["due_string"] = due
		}
		if project := pick(a, s, "project"); project != "" {
			body["project_id"] = project
		}
		return request{method: "POST", url: "https://api.todoist.com/rest/v2/tasks", headers: bearer(s["token"]), body: body}, nil
	}}, "add_task")
}

func makeLinear(s Settings) Connector {
	return newAPI("linear", s, map[string]actionFn{"create_issue": func(s Settings, a map[string]any) (request, error) {
		title, err := need(a, "title")
		if err != nil {
			return request{}, err
		}
		return request{method: "POST", url: "https://api.linear.app/graphql", headers: map[string]string{"Authorization": s["token"]}, body: map[string]any{
			"query":     "mutation($input: IssueCreateInput!) { issueCreate(input: $input) { success issue { id identifier url } } }",
			"variables": map[string]any{"input": map[string]any{"teamId": s["team"], "title": title, "description": str(a, "description", "body", "text")}},
		}}, nil
	}}, "create_issue")
}

func makeJira(s Settings) Connector {
	return newAPI("jira", s, map[string]actionFn{"create_issue": func(s Settings, a map[string]any) (request, error) {
		summary, err := need(a, "summary", "title", "text")
		if err != nil {
			return request{}, err
		}
		fields := map[string]any{"project": map[string]any{"key": pick(a, s, "project")}, "summary": summary, "issuetype": map[string]any{"name": "Task"}}
		if d := str(a, "description", "body"); d != "" {
			fields["description"] = map[string]any{"type": "doc", "version": 1, "content": []map[string]any{{"type": "paragraph", "content": []map[string]any{{"type": "text", "text": d}}}}}
		}
		return request{method: "POST", url: base(s, "") + "/rest/api/3/issue", basic: [2]string{s["email"], s["token"]}, body: map[string]any{"fields": fields}}, nil
	}}, "create_issue")
}

// ── office ─────────────────────────────────────────────────────────────────────────────────

func makeAirtable(s Settings) Connector {
	return newAPI("airtable", s, map[string]actionFn{"add_record": func(s Settings, a map[string]any) (request, error) {
		fields, ok := a["fields"].(map[string]any)
		if !ok || len(fields) == 0 {
			return request{}, errors.New("fields is missing (an object of column: value)")
		}
		return request{method: "POST", url: "https://api.airtable.com/v0/" + s["base"] + "/" + url.PathEscape(pick(a, s, "table")), headers: bearer(s["token"]), body: map[string]any{"fields": fields}}, nil
	}}, "add_record")
}

func makeNextcloud(s Settings) Connector {
	path := func(s Settings, a map[string]any) (string, error) {
		name, err := need(a, "name", "path")
		if err != nil {
			return "", err
		}
		if strings.Contains(name, "..") {
			return "", errors.New("name cannot climb folders")
		}
		folder := strings.Trim(s["folder"], "/")
		if folder != "" {
			folder += "/"
		}
		return base(s, "") + "/remote.php/dav/files/" + url.PathEscape(s["user"]) + "/" + folder + url.PathEscape(name), nil
	}
	c := newAPI("nextcloud", s, map[string]actionFn{
		"upload": func(s Settings, a map[string]any) (request, error) {
			p, err := path(s, a)
			if err != nil {
				return request{}, err
			}
			content := str(a, "content", "text")
			return request{method: "PUT", url: p, raw: []byte(content), ctype: "application/octet-stream", basic: [2]string{s["user"], s["password"]}}, nil
		},
		"download": func(s Settings, a map[string]any) (request, error) {
			p, err := path(s, a)
			if err != nil {
				return request{}, err
			}
			return request{method: "GET", url: p, basic: [2]string{s["user"], s["password"]}}, nil
		},
	}, "upload")
	c.render = func(action string, data []byte) string {
		if action == "download" {
			return clip(string(data), 4000)
		}
		return "uploaded"
	}
	return c
}

func makeSheets(s Settings) Connector {
	return newAPI("google-sheets", s, map[string]actionFn{"append_row": func(s Settings, a map[string]any) (request, error) {
		values, ok := a["values"].([]any)
		if !ok || len(values) == 0 {
			return request{}, errors.New("values is missing (a list)")
		}
		return request{method: "POST", url: s["url"], body: map[string]any{"values": values}}, nil
	}}, "append_row")
}

// mailRFC822 renders a plain mail for Gmail's raw send.
func mailRFC822(from, to, subject, text string) string {
	head := ""
	if from != "" {
		head += "From: " + from + "\r\n"
	}
	return head + "To: " + to + "\r\nSubject: " + subject + "\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + text
}

func makeGoogleWorkspace(s Settings) Connector {
	c := newAPI("google-workspace", s, map[string]actionFn{
		"send_mail": func(s Settings, a map[string]any) (request, error) {
			to, err := need(a, "to")
			if err != nil {
				return request{}, err
			}
			text, err := need(a, "text", "body")
			if err != nil {
				return request{}, err
			}
			raw := base64.RawURLEncoding.EncodeToString([]byte(mailRFC822("", to, str(a, "subject"), text)))
			return request{method: "POST", url: "https://gmail.googleapis.com/gmail/v1/users/me/messages/send", body: map[string]any{"raw": raw}}, nil
		},
		"create_event": func(s Settings, a map[string]any) (request, error) {
			summary, err := need(a, "summary", "title", "text")
			if err != nil {
				return request{}, err
			}
			start, err := need(a, "start")
			if err != nil {
				return request{}, err
			}
			end := str(a, "end")
			if end == "" {
				end = start
			}
			return request{method: "POST", url: "https://www.googleapis.com/calendar/v3/calendars/primary/events", body: map[string]any{
				"summary": summary, "description": str(a, "description"), "start": map[string]any{"dateTime": start}, "end": map[string]any{"dateTime": end}}}, nil
		},
		"upload": func(s Settings, a map[string]any) (request, error) {
			name, err := need(a, "name")
			if err != nil {
				return request{}, err
			}
			content := str(a, "content", "text")
			meta, _ := json.Marshal(map[string]any{"name": name})
			boundary := "kitties-boundary"
			body := "--" + boundary + "\r\nContent-Type: application/json; charset=UTF-8\r\n\r\n" + string(meta) +
				"\r\n--" + boundary + "\r\nContent-Type: text/plain\r\n\r\n" + content + "\r\n--" + boundary + "--"
			return request{method: "POST", url: "https://www.googleapis.com/upload/drive/v3/files?uploadType=multipart", raw: []byte(body), ctype: "multipart/related; boundary=" + boundary}, nil
		},
		"append_row": func(s Settings, a map[string]any) (request, error) {
			id, err := need(a, "spreadsheet", "id")
			if err != nil {
				return request{}, err
			}
			values, ok := a["values"].([]any)
			if !ok || len(values) == 0 {
				return request{}, errors.New("values is missing (a list)")
			}
			rng := str(a, "range")
			if rng == "" {
				rng = "Sheet1!A1"
			}
			return request{method: "POST", url: "https://sheets.googleapis.com/v4/spreadsheets/" + url.PathEscape(id) + "/values/" + url.PathEscape(rng) + ":append?valueInputOption=USER_ENTERED",
				body: map[string]any{"values": []any{values}}}, nil
		},
		"create_doc": func(s Settings, a map[string]any) (request, error) {
			title, err := need(a, "title", "name")
			if err != nil {
				return request{}, err
			}
			return request{method: "POST", url: "https://docs.googleapis.com/v1/documents", body: map[string]any{"title": title}}, nil
		},
	}, "send_mail")
	return c
}

func makeMicrosoft365(s Settings) Connector {
	return newAPI("microsoft-365", s, map[string]actionFn{
		"send_mail": func(s Settings, a map[string]any) (request, error) {
			to, err := need(a, "to")
			if err != nil {
				return request{}, err
			}
			text, err := need(a, "text", "body")
			if err != nil {
				return request{}, err
			}
			var recipients []map[string]any
			for _, addr := range strings.Split(to, ",") {
				recipients = append(recipients, map[string]any{"emailAddress": map[string]any{"address": strings.TrimSpace(addr)}})
			}
			return request{method: "POST", url: "https://graph.microsoft.com/v1.0/me/sendMail", body: map[string]any{"message": map[string]any{
				"subject": str(a, "subject"), "body": map[string]any{"contentType": "Text", "content": text}, "toRecipients": recipients}}}, nil
		},
		"create_event": func(s Settings, a map[string]any) (request, error) {
			subject, err := need(a, "subject", "summary", "title", "text")
			if err != nil {
				return request{}, err
			}
			start, err := need(a, "start")
			if err != nil {
				return request{}, err
			}
			end := str(a, "end")
			if end == "" {
				end = start
			}
			return request{method: "POST", url: "https://graph.microsoft.com/v1.0/me/events", body: map[string]any{
				"subject": subject, "body": map[string]any{"contentType": "Text", "content": str(a, "body", "description")},
				"start": map[string]any{"dateTime": start, "timeZone": "UTC"}, "end": map[string]any{"dateTime": end, "timeZone": "UTC"}}}, nil
		},
		"upload": func(s Settings, a map[string]any) (request, error) {
			name, err := need(a, "name")
			if err != nil {
				return request{}, err
			}
			if strings.Contains(name, "..") || strings.Contains(name, "/") {
				return request{}, errors.New("name is a plain file name")
			}
			return request{method: "PUT", url: "https://graph.microsoft.com/v1.0/me/drive/root:/Kitties/" + url.PathEscape(name) + ":/content",
				raw: []byte(str(a, "content", "text")), ctype: "text/plain"}, nil
		},
	}, "send_mail")
}

// ── drawing ────────────────────────────────────────────────────────────────────────────────

func makeKroki(s Settings) Connector {
	return newAPI("kroki", s, map[string]actionFn{"render": func(s Settings, a map[string]any) (request, error) {
		kind := strings.ToLower(str(a, "type", "kind"))
		if kind == "" {
			kind = "mermaid"
		}
		source, err := need(a, "source", "text", "diagram")
		if err != nil {
			return request{}, err
		}
		name := str(a, "name")
		if name == "" {
			name = kind + "-" + strconv.FormatInt(time.Now().Unix(), 36)
		}
		if !strings.HasSuffix(name, ".svg") {
			name += ".svg"
		}
		return request{method: "POST", url: base(s, "https://kroki.io") + "/" + url.PathEscape(kind) + "/svg", raw: []byte(source), ctype: "text/plain", file: name}, nil
	}}, "render")
}

func makeFigma(s Settings) Connector {
	fh := func(s Settings) map[string]string { return map[string]string{"X-Figma-Token": s["token"]} }
	c := newAPI("figma", s, map[string]actionFn{
		"comment": func(s Settings, a map[string]any) (request, error) {
			file, err := need(a, "file", "key")
			if err != nil {
				return request{}, err
			}
			text, err := need(a, "text", "message")
			if err != nil {
				return request{}, err
			}
			return request{method: "POST", url: "https://api.figma.com/v1/files/" + url.PathEscape(file) + "/comments", headers: fh(s), body: map[string]any{"message": text}}, nil
		},
		"get_file": func(s Settings, a map[string]any) (request, error) {
			file, err := need(a, "file", "key")
			if err != nil {
				return request{}, err
			}
			return request{method: "GET", url: "https://api.figma.com/v1/files/" + url.PathEscape(file) + "?depth=2", headers: fh(s)}, nil
		},
	}, "comment")
	c.render = func(action string, data []byte) string {
		if action != "get_file" {
			return summary(data)
		}
		// The names of the pages and their frames, which is what a kitty can act on.
		var doc struct {
			Name     string `json:"name"`
			Document struct {
				Children []struct {
					Name     string `json:"name"`
					Children []struct {
						Name string `json:"name"`
						Type string `json:"type"`
					} `json:"children"`
				} `json:"children"`
			} `json:"document"`
		}
		if json.Unmarshal(data, &doc) != nil || (doc.Name == "" && len(doc.Document.Children) == 0) {
			return summary(data)
		}
		var b strings.Builder
		b.WriteString(doc.Name + "\n")
		for _, page := range doc.Document.Children {
			b.WriteString("- " + page.Name + "\n")
			for _, fr := range page.Children {
				b.WriteString("  - " + fr.Name + " (" + fr.Type + ")\n")
			}
		}
		return clip(b.String(), 4000)
	}
	return c
}

// excalidraw needs no service: the scene is written as a file to open at excalidraw.com.
type excalidrawConnector struct{}

func makeExcalidraw(Settings) Connector { return excalidrawConnector{} }

func (excalidrawConnector) Describe() string {
	k, _ := LookupKind("excalidraw")
	return k.Help + " " + actionsHelp(k)
}

func (excalidrawConnector) Call(ctx context.Context, args map[string]any) (string, error) {
	text, _, _, err := excalidrawConnector{}.CallFile(ctx, args)
	return text, err
}

func (excalidrawConnector) CallFile(_ context.Context, args map[string]any) (string, string, []byte, error) {
	elements, ok := args["elements"].([]any)
	if !ok {
		return "", "", nil, errors.New("elements is missing (the scene's elements, a list)")
	}
	name := str(args, "name")
	if name == "" {
		name = "scene-" + strconv.FormatInt(time.Now().Unix(), 36)
	}
	if !strings.HasSuffix(name, ".excalidraw") {
		name += ".excalidraw"
	}
	data, err := json.MarshalIndent(map[string]any{"type": "excalidraw", "version": 2, "source": "kitties", "elements": elements, "appState": map[string]any{"viewBackgroundColor": "#ffffff"}}, "", "  ")
	if err != nil {
		return "", "", nil, err
	}
	return "saved as " + name + " (open it at https://excalidraw.com)", name, data, nil
}

// ── generic ────────────────────────────────────────────────────────────────────────────────

func makeHTTP(s Settings) Connector {
	c := newAPI("http", s, map[string]actionFn{"call": func(s Settings, a map[string]any) (request, error) {
		method := strings.ToUpper(str(a, "method"))
		if method == "" {
			method = "GET"
		}
		path := str(a, "path")
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		req := request{method: method, url: base(s, "") + path}
		if s["token"] != "" {
			req.headers = bearer(s["token"])
		}
		if body, ok := a["body"]; ok && body != nil {
			req.body = body
		}
		return req, nil
	}}, "call")
	c.render = func(_ string, data []byte) string { return clip(string(data), 4000) }
	return c
}
