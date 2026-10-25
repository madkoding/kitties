# Configuration, connectors and sign-in

## Configuration

`kitties init` writes a commented file; the full one is
[`configs/kitties.example.yaml`](../configs/kitties.example.yaml). The essentials:

```yaml
providers:
  strong: { kind: anthropic, model: claude-sonnet-5-5 }
  local:  { kind: ollama, model: qwen3:8b }
default_provider: strong

kitties:
  - name: mimi
    soul: { persona: a patient researcher, traits: [curious, thorough] }
    connectors: [team-slack]
  - name: tom
    provider: local
    soul_file: souls/tom.yaml
    replicas: 3

containers: { runtime: auto, image: ghcr.io/madkoding/kitty-desktop:latest, memory_mb: 2048, cpus: 2 }
gateway:    { listen: 127.0.0.1:7488, token: "" }
motita:     { url: http://127.0.0.1:7477 }

connectors:
  team-slack: { kind: slack, url: https://hooks.slack.com/services/XXX }
  coder:      { kind: motita, description: runs a coding task in motita }
```

### The connector catalogue

| Category | Services |
| --- | --- |
| Messaging | Slack (webhook, bot token or OAuth sign-in; reads and replies), Discord, Telegram, WhatsApp (Cloud API), Email (SMTP), Matrix, Google Chat, Microsoft Teams, ntfy (push to your phone) |
| Social media | Facebook Pages (post, posts, comments, reply, like, Messenger inbox and answers), Instagram professional accounts (pictures and reels, posts, comments, replies), Mastodon and Bluesky (post, timeline, mentions, reply, like), X, LinkedIn |
| Calendar, reminders & contacts | CalDAV (iCloud, Fastmail, Nextcloud, Synology…: events, new events, reminders, ticking them off), CardDAV (find and add people), the Mac's own Calendar, Reminders and Contacts (when the gateway runs on the Mac), Google Contacts and Tasks, Outlook contacts and Microsoft To Do |
| Git | GitHub (issues, comments, pull requests, files; a token or OAuth sign-in), GitLab, Gitea/Forgejo, motita (coding tasks) |
| Productivity | Notion, Trello, Todoist, Linear, Jira, Zendesk (support tickets), Things and OmniFocus (on the Mac) |
| Finance | Stripe (balance, invoices, payments, refunds) |
| Home, news & weather | Home Assistant (states, acting on devices; its automations can post events), RSS and Atom feeds, the weather and forecast (Open-Meteo, no key) |
| Music | Spotify (what is playing, play, pause, skip and queue with Premium, search, playlists), TIDAL (search and playlists: its public API has no remote playback) |
| Office | Dropbox (list, search, read, save, share a link), Google Workspace (OAuth: Gmail inbox and send, Calendar, Drive, Sheets, Docs), Microsoft 365 (OAuth: Outlook inbox and send, Calendar, OneDrive), Airtable, Nextcloud (WebDAV files), Google Sheets (Apps Script) |
| Drawing | Diagrams through Kroki (mermaid, plantuml, graphviz, d2…) rendered to SVG artifacts, Figma, Excalidraw scenes |
| Generic | Webhook, any HTTP API with a bearer token, a program on the gateway's machine |

Each one is a button with its logo under **Settings → Connectors**: pick the service, fill the
fields it needs (secrets are kept on the gateway; OAuth services get a *Sign in* button), or
write it in the YAML with `kind` and `settings`. Signed-in tokens are refreshed in the
background ahead of their expiry, so a kitty's first call after a quiet night never waits or
fails on a stale token. In a chat, every call a kitty makes shows up as a chip with the
service's logo. Inside its
container a kitty also has LibreOffice, Inkscape, Mousepad (text editor), ImageMagick and mtPaint for office documents
and drawing on its own display.

For iCloud calendars and contacts use `https://caldav.icloud.com` and `https://contacts.icloud.com`
with an app-specific password from appleid.apple.com. iCloud reminders made with iOS 13 or later
are no longer served over CalDAV: on a Mac, the **Mac Calendar, Reminders & Contacts** connection
reaches them through the Reminders app (macOS asks once to allow it). Google and Microsoft accounts
signed in before contacts and tasks were added need to sign in once more for them.

**The actions are not the limit.** What a service lists (post, reply, search…) are the common
things, said simply. Every service with an HTTP API also has **Request**: any endpoint of that
API (method, path, query, body), called with the account the connection has. A kitty uses it for
whatever a listed action does not cover — DMs on X, bookmarks, labels in Gmail, workflows on
GitHub — and only the service decides what the account may do: the permissions of its sign-in,
its keys or its token. A full URL may only name the service's own hosts. Reading (GET) runs on
its own; other methods ask first only if you or a kitty set a limit for them. Sign-ins ask for
the broad permissions of each service (Dropbox gets whatever the app has enabled in its
console), so accounts signed in before need to sign in once more to get them.

### Signing in instead of pasting keys

Most connections sign in: **Sign in with …** opens the service, you allow Kitties, and the token
is kept and renewed on the gateway. The form asks first **how do you connect?** (the sign-in, an
API key or token, an app's keys, a webhook) and shows only that way's fields, each marked as
required.

| Signs in | How |
| --- | --- |
| Mastodon, Home Assistant | nothing to set up: Kitties registers itself on the instance, or uses the gateway's own address as its app |
| Google, Microsoft, Slack, GitHub, GitLab, Notion, Todoist, Linear, Jira, Airtable, Figma, Zendesk, X, LinkedIn, Facebook, Instagram, Spotify, TIDAL, Dropbox | through Kitties' own app when the build has one, else an app of yours (its redirect address is this gateway's `/v1/oauth/callback`) |

Signing in also fills what you would otherwise look up: your Facebook Page and its token (one
that does not expire), the Instagram account linked to it, your LinkedIn author URN, your X user
id, your Jira site. Telegram, WhatsApp, email, Stripe, Discord and the webhooks keep their keys:
those services have no sign-in for this.

**X** takes the sign-in (its OAuth 2.0 Client ID and Secret, with this gateway's callback under
*User authentication settings*), or, without a sign-in, the four **OAuth 1.0 keys** of *Keys and
tokens*: Consumer Key and Secret, Access Token and Secret (Read and write). Those do not expire,
and the account's id comes from the access token. The OAuth 2.0 access token the portal shows
lasts two hours, and the app-only Bearer Token cannot post.

A saved key is never shown again: **Clear** next to it forgets it on the next save, and **Sign
out** forgets an account. Signing in replaces a token typed before (it would win over the
account). Through the API, `PUT /v1/settings/connectors/{name}` takes `"clear": ["token",
"sign_in"]` for the same.

Each connection says what it uses (the sign-in, the OAuth 1.0 keys, a token or a webhook) and has
a **Test** that reads who the account is, without posting; a save tests it by itself. A webhook
cannot be tested without posting, so it says so. X's pasted OAuth 2.0 token counts down its two
hours on the list and in the form.

The apps a build signs in through come from `OAUTH_APPS` at build time
(`make OAUTH_APPS="spotify=<client id>,facebook=<id>:<secret>" dist`; the release reads the
`KITTIES_OAUTH_APPS` repository secret), or from `KITTIES_<SERVICE>_CLIENT_ID` and
`KITTIES_<SERVICE>_CLIENT_SECRET` on the gateway. An event created with `attendees` invites them (Google, Outlook and CalDAV
send the invitations), and counts as reaching people for a kitty in `approvals: ask`. Notion Calendar has no public API: it shows the Google
or iCloud calendars, which the kitties reach directly.

With **Settings → Notifications → Start it with my day**, the daily summary opens with what is
on the calendar, the reminders still to do and the weather. A Home Assistant automation tells the
kitties what happens with a `rest_command` that posts `{"entity_id": "{{ trigger.entity_id }}",
"state": "{{ trigger.to_state.state }}"}` to `/v1/hooks/<name>` with the connection's secret as
`X-Kitties-Secret`; the goals that listen to it (`on: [<name>]`) run.

Facebook and Instagram use Meta's Graph API with a token of your own app (developers.facebook.com):
for a Page, a long-lived Page token and the Page id; for Instagram, a professional (business or
creator) account linked to a Page, with a Page token, or an Instagram Login token (`IG…`), and the
account id. Instagram only publishes media from a public https URL (a JPEG, or an MP4 for a reel).
Publishing, replying, liking and messaging ask you first until you trust the kitty with them.

Keys come from the provider's usual variable (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`,
`GEMINI_API_KEY`, `GROQ_API_KEY`, `OPENROUTER_API_KEY`, `DEEPSEEK_API_KEY`, `DASHSCOPE_API_KEY`,
`MISTRAL_API_KEY`, `OLLAMA_API_KEY`) unless `api_key` or `api_key_env` says otherwise.

### A subscription instead of a key

Under **Settings → Models**, a provider that has one offers *API key* or the subscription you
already pay for:

| Subscription | How it signs in | Saved as |
| --- | --- | --- |
| Claude (Pro, Max) | Anthropic only allows it through Claude Code: install it, run `claude auth login` once, and Kitties runs the `claude` program for each answer (tools as text, no API key passed to it) | `kind: claude-code` |
| ChatGPT (Plus, Pro, Business) | *Sign in with ChatGPT* in the browser; the callback lands on `localhost:1455`, or paste its address back when the gateway runs elsewhere | `kind: codex, auth: login` |
| GitHub Copilot | a code typed on github.com/login/device | `kind: copilot, auth: login` |
| Google (Gemini) | the login of gcloud (`gcloud auth application-default login`) and a Cloud project | `kind: gemini, auth: login` |
| Qwen | ended by Qwen on 2026-04-15: a DashScope key is the way in | — |

The tokens are kept in `data_dir/accounts` (one file per account, readable by you only) and
renewed before they expire.
