<div align="center">

# 🐱 Kitties

**A fleet of autonomous AI agents, each with its own soul and its own desktop.**

[![CI](https://img.shields.io/github/actions/workflow/status/madkoding/kitties/ci.yml?branch=main&style=flat-square&label=ci)](https://github.com/madkoding/kitties/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-stdlib%20only-00add8?style=flat-square&logo=go)](go.mod)
[![License](https://img.shields.io/badge/license-GPL--3.0--or--later-blue?style=flat-square)](LICENSE)

</div>

Kitties is a Go framework for running **NNN bots** at once. Every kitty has a **soul** (a
personality: helpful, kind and adaptable by default, yours to shape), a **brain** (the AI provider
you pick for it: OpenAI, Anthropic, Gemini, Ollama, Groq, OpenRouter, DeepSeek, Qwen, Mistral,
Copilot or any OpenAI-compatible server) and a **body** (its own Docker container with a headless
X display on Xvfb, a browser and the usual tools, so it can work on graphical applications without
touching your machine).

Talk to one kitty on its own, or open a **group chat**, add the kitties you want in it, and let
them work on the same thread as a team. Everything goes through one **gateway**: an HTTP API with
a small browser page, driven by the `kitties` CLI on Linux, macOS, Windows and Android (Termux).

A kitty does not just talk: ask it to open a site, fill a form or check an app and it **works on
its desktop in the background**, looking at a screenshot after every action (click, type, key,
scroll, open, run) until it is done, while the chat shows each step and its picture.

Kitties is the fleet counterpart of [motita](https://github.com/madkoding/motita): a kitty can
hand a coding task to a motita gateway through the `motita` connector and bring the result back
to the conversation, and the desktop app is drawn with motita's design system.

<p align="center">
<img src="docs/screenshots/04-desktop-task.png" alt="A kitty working on its desktop: the steps it took and a screenshot after each one" width="100%">
</p>

## Try it in two minutes, no key and no Docker

```sh
go install github.com/madkoding/kitties/cmd/kitties@latest   # or: make build
kitties -demo gateway        # three kitties with a scripted brain and a drawn desktop
```

Open `http://127.0.0.1:7488` (on the same machine no token is typed: the page gets it from the
gateway) or run the desktop app, then ask Tom to *open https://example.com*.

## Downloading a release

Each release has binaries for Linux, macOS, Windows and Android (Termux), the desktop app (a disk
image for macOS, a zip for Linux and Windows), the Android app (`kitties-android-app.apk`) and
`SHA256SUMS`. The desktop app carries its own
gateway and starts it when none is running, so it needs nothing else installed. They are not yet signed with an Apple Developer ID, so macOS blocks a file that
came through a browser ("Apple could not verify..."). It is the quarantine flag, not damage:

```sh
sha256sum -c SHA256SUMS --ignore-missing      # shasum -a 256 -c on macOS
xattr -d com.apple.quarantine kitties-darwin-arm64 && chmod +x kitties-darwin-arm64
# the desktop app: open kitties-desktop-macos-arm64.dmg, drag Kitties to Applications, then
xattr -dr com.apple.quarantine /Applications/Kitties.app
```

On Windows, SmartScreen asks once: *More info* then *Run anyway*.

## The real thing

```sh
kitties init                 # writes kitties.yaml and souls/tom.yaml
export ANTHROPIC_API_KEY=... # or OPENAI_API_KEY, GEMINI_API_KEY... (or a key or a subscription in Settings)
make image                   # builds the kitty-desktop image (needs Docker or Podman)
kitties gateway              # creates the kitties of the file, starts them, serves the API
```

Then open the desktop app or `http://127.0.0.1:7488`, or stay in the terminal:

```sh
kitties ls                                  # the fleet and its state
kitties chat mimi                           # one kitty
kitties chat mimi,tom-1,tom-2               # a group: @name picks who answers
kitties create luna -persona "a planner"    # a new kitty, started at once
kitties scale tom 5                         # tom-1 ... tom-5
kitties exec mimi kitty-open https://example.com
kitties shot mimi desk.png                  # what is on its display right now
```

Without a container runtime the kitties still run, brain-only.

## On a server

The same gateway serves the web interface from a server: open it from any browser, sign in with
the token (the browser keeps only a session cookie; *Sign out* ends it) and manage the whole fleet
from there. It runs as a container with Caddy for https, or as a binary under systemd.

```sh
sudo install -d -o 10001 /var/lib/kitties
cp deploy/.env.example deploy/.env      # your domain, a token and your keys
docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d
```

| | |
| --- | --- |
| ![Signing in to a gateway on a network](docs/screenshots/30-sign-in.png) | |

Compose, Caddy, nginx and systemd files are in [`deploy/`](deploy); the full guide, with what to
protect and how to update and back up, is [docs/DEPLOY.md](docs/DEPLOY.md). The image is published
as `ghcr.io/madkoding/kitties` with each release.

## The Android app

`kitties-android-app.apk` is the desktop app's page on the phone: a remote for a gateway that runs
on your computer or server (the kitties and their desktops stay there). Install it (Android asks to
allow installs from the browser or the file manager once), then type the gateway's address and
token:

```sh
KITTIES_LISTEN=0.0.0.0:7488 kitties gateway   # on the computer: listen on the home network
cat ~/.kitties/gateway.token                   # the token the app asks for
```

The address is the computer's on the network, like `http://192.168.1.20:7488`; over the internet,
put the gateway behind HTTPS ([docs/DEPLOY.md](docs/DEPLOY.md)). The gateway lets the app's page
call it with the token, and nothing else from other sites. `android/` is a plain WebView project:
`make android` builds the APK (Android SDK and JDK 17+). Releases are signed with the
`KITTIES_KEYSTORE_BASE64`, `KITTIES_KEYSTORE_PASSWORD` and `KITTIES_KEY_ALIAS` secrets when they
are set; without them each release gets a debug key, and updating means uninstalling first.

## The desktop app

`desktop/` is a [Wails](https://wails.io) application (Linux, macOS, Windows) with a Preact page
that uses the Motita design system: dark ground, neon edges, cut corners, frosted panels,
Sansation and JetBrains Mono. It talks to a gateway, local by default (the downloads carry one and start it for you), and
needs no token on the machine the gateway runs on: the gateway leaves its token in `~/.kitties/gateway.token` and the
app picks it up. **Settings** holds the models (pick a provider, paste its key: it is tested on save), the
connections (with the kitties that may use each one), notifications and the gateway address.
A first run is a short checklist: a model, a kitty, a chat.

```sh
make frontend     # builds the page (node 22)
make desktop      # wails build, with the Wails CLI installed
(cd desktop/frontend && npm run icon)   # redraw the app icon and the macOS disk image's background
cd desktop && wails dev   # live reload while working on it
```

| | |
| --- | --- |
| ![A chat with one kitty](docs/screenshots/02-chat.png) | ![A group chat](docs/screenshots/03-group.png) |
| ![A kitty's desktop](docs/screenshots/05-kitty-drawer.png) | ![Settings: the models](docs/screenshots/08-settings-models.png) |
| ![Settings: the connectors](docs/screenshots/09-settings-connectors.png) | ![The catalogue: one button per service](docs/screenshots/17-connector-catalogue.png) |
| ![Google Workspace: sign in with OAuth](docs/screenshots/18-connector-oauth.png) | ![A kitty's own display](docs/screenshots/13-tom-display.png) |
| ![The library: skills](docs/screenshots/15-library-skills.png) | ![The library: artifacts](docs/screenshots/16-library-artifacts.png) |
| ![New kitty: pick its pixel cat](docs/screenshots/07-new-kitty.png) | ![Light mode](docs/screenshots/11-light.png) |
| ![A first run is a checklist](docs/screenshots/31-welcome.png) | ![Adding a model: pick the provider](docs/screenshots/32-pick-provider.png) |
| ![Adding a model: the key and the suggested model](docs/screenshots/34-add-model.png) | ![A connection and the kitties that may use it](docs/screenshots/33-connection-form.png) |
| ![A Claude subscription, through Claude Code](docs/screenshots/35-claude-subscription.png) | ![GitHub Copilot: sign in with a code](docs/screenshots/36-copilot-code.png) |
| ![The Android app asks for the gateway](docs/screenshots/37-android-connect.png) | ![The Android app, connected](docs/screenshots/38-android-app.png) |
| ![A kitty posting through Slack: the chip carries the logo](docs/screenshots/19-connector-call.png) | ![The phone layout](docs/screenshots/12-phone.png) |

More in [docs/screenshots](docs/screenshots): the new chat and new kitty dialogs, the gateway
page, the phone layout and a kitty's own display.

## On their own

A kitty is not only something you talk to. Each one has an agenda:

- **Goals.** Standing tasks it pursues without being asked: on a schedule (cron, or
  *hourly*, *daily*, *weekdays at nine*), when a service reports something, or when you press
  *run now*. Each goal works in a thread of its own, so you can read along and chime in.
- **Incoming events.** GitHub, Slack, Discord, Jira and any service that can POST send their
  events to `POST /v1/hooks/<connector>`, signed with the connector's incoming events secret
  (GitHub's HMAC signature, or an `X-Kitties-Secret` header). The goals that listen to that
  connector run with the event.
- **Limits.** Per kitty: tokens, seconds and rounds per task, and how many times a failed
  attempt is tried another way. A task that hits a limit stops and says so instead of running
  on. A **daily budget** caps what a kitty spends around the clock: out of tokens it rests until
  tomorrow (and tells you once); past its actions for the day, every post, reply or refund waits
  for your yes even if it normally acts alone.
- **Recovery.** A failed attempt is retried with a different approach. When a kitty gives up,
  its **backup** kitty takes the task over; without one, the kitty asks you (the chat is
  marked *help* until you answer). A watchdog brings a stopped container back and retries a
  kitty in error.
- **Delegation.** A kitty can hand a subtask to another (`@delegate luna {"task": "..."}`,
  or the native `delegate` tool), waits for the answer and goes on. Hand-overs are bounded in
  depth so two kitties cannot bounce a task back and forth.
- **A journal.** Every run, event, retry, hand-over, cry for help and recovery is a line in the
  kitty's journal, in its drawer.

| | |
| --- | --- |
| ![A kitty's goals and journal](docs/screenshots/21-goals-journal.png) | ![A goal's thread: the scheduler's call and the answer](docs/screenshots/22-goal-chat.png) |
| ![A kitty hands a subtask to another](docs/screenshots/20-delegation.png) | |

In the YAML, under a kitty:

```yaml
  - name: mimi
    backup: luna                 # takes over what mimi gives up on
    limits: {max_tokens: 200000, max_seconds: 900, retries: 2, day_tokens: 2000000, day_actions: 20}
    goals:
      - title: Inbox check
        text: Look at the inbox and answer what is easy.
        every: "@hourly"
      - title: Triage new issues
        on: [github]             # the github connector's inbound_secret signs the hook
```

## Learning, trust and being told

Autonomy needs a memory, a leash that loosens with trust, and a way to reach you.

- **Memory.** Each kitty keeps what it knows about you and the work: notes in three scopes
  (*you*, *work* and one per project), pinned notes of your own, and a level of how demanding you
  are (1 relaxed to 5 strict) that it learns from the thumbs under its answers. Every few verdicts
  it distils the corrections into lasting notes with its own brain. It writes notes itself
  (`@memory remember {...}` or the native `memory_*` tools) when you say "remember that...". The
  memory sits in its prompt, so it answers the way you like.
- **Approvals.** Every connector action has a risk: reading is free, posting, replying, liking,
  sending mail or opening a pull request is *ask*, refunding or deleting is *always ask*. A kitty
  in `approvals: ask` holds an *ask* action and goes on with the rest; you answer in the
  Approvals screen (*approve*, *approve and stop asking*, *reject with a reason*) and the action
  runs or the reason reaches the kitty. Yeses in a row earn a kitty the right to do that kind of
  action alone, sooner for a relaxed person (2) than a strict one (12), never for a level 5.
  `approvals: auto` skips the questions.
- **Notifications.** A kitty that needs you does not wait for you to open the app: through any
  messaging connector (an [ntfy](https://ntfy.sh) topic pushes to your phone) it tells you when it
  needs help, when an action waits for a yes, and optionally when a goal finishes, gets blocked or
  a container recovers or a kitty spends its day's tokens. Bursts are folded into one message every
  ten minutes. Optionally a **daily summary** arrives at the hour you choose: what each kitty did
  in the last day, what it spent and what waits for your yes.
- **Community manager, support, finance.** Besides writing, connectors read and answer: Mastodon
  and Bluesky timelines, mentions, replies and likes; Gmail and Outlook inboxes; Slack and
  Telegram messages; Stripe balance, invoices and payments; Zendesk tickets. Results come back as
  one line per item, and Stripe's events (failed payments, disputes) can start a goal.
- **A browser that stays signed in.** Chromium keeps its profile in the kitty's workspace, so
  logins and cookies survive restarts and the kitty does not sign in again each morning.
- **Long threads.** When a thread passes forty turns the early ones are summarised and only the
  latest sixteen stay verbatim, so a goal that runs for weeks does not outgrow its context.

| | |
| --- | --- |
| ![An action waits for a yes](docs/screenshots/23-waiting-for-a-yes.png) | ![The Approvals screen](docs/screenshots/24-approvals.png) |
| ![Thumbs under the answers](docs/screenshots/25-feedback.png) | ![What a kitty knows about you](docs/screenshots/26-memory.png) |
| ![Notifications: a phone channel and its events](docs/screenshots/27-notifications.png) | ![Stripe: invoices and payments as one line each](docs/screenshots/28-stripe.png) |
| ![The daily summary, sent on demand](docs/screenshots/29-daily-summary.png) | |

In the YAML:

```yaml
kitties:
  - name: mimi
    approvals: ask               # ask (default) | auto
notify:
  connector: phone               # any messaging connector; ntfy pushes to your phone
  on: [help, approval, goal_done, goal_blocked, recovered, budget]   # default: help, approval
  digest: "08:30"                # a summary of the last day, every day (gateway's clock); empty: none
connectors:
  phone: { kind: ntfy, settings: { topic: my-kitties } }
```

## How it fits together

```
          kitties CLI / browser page / your program
                          │  HTTP + bearer token
                          ▼
                 ┌──────────────────┐
                 │     gateway      │  /v1/kitties  /v1/chats  /v1/providers
                 └────────┬─────────┘
                          ▼
                 ┌──────────────────┐      ┌───────────────┐
                 │   orchestrator   │──────│   providers   │ openai · anthropic · gemini
                 │  create · start  │      │ (one per name)│ ollama · groq · ... (motita-style)
                 │  stop · scale    │      └───────────────┘
                 └───┬─────────┬────┘
                     │         │
          ┌──────────┘         └──────────┐
          ▼                               ▼
 ┌─────────────────┐             ┌─────────────────┐
 │ kitty "mimi"    │             │ kitty "tom-1"   │   ... × NNN
 │ soul + brain    │             │ soul + brain    │
 │ container:      │             │ container:      │
 │  Xvfb :99       │             │  Xvfb :99       │
 │  chromium, …    │             │  chromium, …    │
 │  /home/kitty/work             │  /home/kitty/work
 └─────────────────┘             └─────────────────┘
          │  connectors: slack · discord · telegram · webhook · shell · motita
          ▼
     the outside world
```

- **Soul** (`internal/soul`): a YAML document (`persona`, `tone`, `traits`, `skills`, `language`,
  `instructions`) rendered into the system prompt. The default is helpful, kind and adaptable.
- **Providers** (`internal/provider`): dependency-free clients for the four API families
  (chat completions, responses, Anthropic messages, Gemini), with retries. Profiles are named in
  the configuration and each kitty picks one.
- **Containers** (`internal/container`): one container per kitty from `docker/kitty-desktop`
  (Debian, Xvfb, Chromium, xdotool, scrot). Nothing is published on the host; the orchestrator
  reaches a kitty with `docker exec`, and `kitty-shot` returns a PNG of its display.
- **Orchestrator** (`internal/orchestrator`): the fleet, persisted in `~/.kitties/kitties.json`.
  Creates the kitties the configuration lists (`replicas: n` gives `name-1 … name-n`) and scales
  them on demand.
- **Chat** (`internal/chat`): one-to-one and group threads, persisted as JSON. In a group every
  kitty sees the others' turns labelled by name and answers in turn; `@name` addresses one.
- **Desktop tasks** (`internal/desktop`): a kitty with a container writes `@tool click {"x": 640,
  "y": 420}` lines; the runtime executes them (xdotool, the browser, a program), takes a
  screenshot, shows it to the model and continues in the background until `@tool done`. Every
  step and picture lands in the thread; the chat can be cancelled.
- **Settings** (`internal/orchestrator/settings.go`): models and connectors edited from the app
  are kept in `settings.json` on top of the YAML, keys included and never shown again.
- **Demo** (`internal/demo`): a scripted brain and a drawn desktop, for `kitties -demo`.
- **Connectors** (`internal/connector`): a catalogue of services by category, each with its
  settings and actions. A kitty writes `@connector github {"action": "create_issue", "title":
  "..."}` in a reply; the runtime runs it and feeds the result back before the kitty answers.
  A connector that answers with a file (a rendered diagram) lands in the artifacts library.
- **Skills and artifacts** (`internal/skills`, `internal/artifacts`): a library like motita's.
  Skills are markdown procedures the kitties read with `@skill read <name>` and propose with
  `@skill save` (a person accepts them in the Library); three ship built in. Artifacts are the
  files they keep for later with `@artifact save` and read with `@artifact read`, shared by
  every kitty and every chat, downloadable from the page.
- **Avatars**: every kitty is an animated pixel cat (five breeds, ten coats) that reacts as a bot
  would: idle, thinking, talking, working, happy, error, sleeping.
- **Native tool calling**: with every provider family (OpenAI chat and responses, Anthropic,
  Gemini) the desktop tools, the connectors and the library are offered as real tools and the
  model calls them in its own format; `tools: text` on a profile keeps the `@tool` lines for a
  server without tool support. Both run through the same actions.
- **Live events**: the gateway streams every turn, step and finish of a chat as server-sent
  events (`GET /v1/chats/{id}/events`); the page and the desktop app follow a chat live instead
  of polling.
- **OAuth**: Google Workspace (Gmail, Calendar, Drive, Sheets, Docs) and Microsoft 365 (Outlook
  mail, Calendar, OneDrive) sign in with the person's own OAuth app from Settings; the gateway
  runs the authorization-code flow with PKCE, keeps the refresh token and renews access tokens.
- **Gateway** (`internal/gateway`): the HTTP API and the browser page.

## Configuration

`kitties init` writes a commented file; the full one is
[`configs/kitties.example.yaml`](configs/kitties.example.yaml). The essentials:

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
| Home, news & weather | Home Assistant (states, acting on devices: asks first; its automations can post events), RSS and Atom feeds, the weather and forecast (Open-Meteo, no key) |
| Music | Spotify (what is playing, play, pause, skip and queue with Premium, search, playlists), TIDAL (search and playlists: its public API has no remote playback) |
| Office | Dropbox (list, search, read, save, share a link: asks first), Google Workspace (OAuth: Gmail inbox and send, Calendar, Drive, Sheets, Docs), Microsoft 365 (OAuth: Outlook inbox and send, Calendar, OneDrive), Airtable, Nextcloud (WebDAV files), Google Sheets (Apps Script) |
| Drawing | Diagrams through Kroki (mermaid, plantuml, graphviz, d2…) rendered to SVG artifacts, Figma, Excalidraw scenes |
| Generic | Webhook, any HTTP API with a bearer token, a program on the gateway's machine |

Each one is a button with its logo under **Settings → Connectors**: pick the service, fill the
fields it needs (secrets are kept on the gateway; OAuth services get a *Sign in* button), or
write it in the YAML with `kind` and `settings`. Signed-in tokens are refreshed in the
background ahead of their expiry, so a kitty's first call after a quiet night never waits or
fails on a stale token. In a chat, every call a kitty makes shows up as a chip with the
service's logo. Inside its
container a kitty also has LibreOffice, Inkscape, ImageMagick and mtPaint for office documents
and drawing on its own display.

For iCloud calendars and contacts use `https://caldav.icloud.com` and `https://contacts.icloud.com`
with an app-specific password from appleid.apple.com. iCloud reminders made with iOS 13 or later
are no longer served over CalDAV: on a Mac, the **Mac Calendar, Reminders & Contacts** connection
reaches them through the Reminders app (macOS asks once to allow it). Google and Microsoft accounts
signed in before contacts and tasks were added need to sign in once more for them.

### Signing in instead of pasting keys

Most connections sign in: **Sign in with …** opens the service, you allow Kitties, and the token
is kept and renewed on the gateway. An API key or a token stays available under *Use an API key
or a token instead*.

| Signs in | How |
| --- | --- |
| Mastodon, Home Assistant | nothing to set up: Kitties registers itself on the instance, or uses the gateway's own address as its app |
| Google, Microsoft, Slack, GitHub, GitLab, Notion, Todoist, Linear, Jira, Airtable, Figma, Zendesk, X, LinkedIn, Facebook, Instagram, Spotify, TIDAL, Dropbox | through Kitties' own app when the build has one, else an app of yours (its redirect address is this gateway's `/v1/oauth/callback`) |

Signing in also fills what you would otherwise look up: your Facebook Page and its token (one
that does not expire), the Instagram account linked to it, your LinkedIn author URN, your X user
id, your Jira site. Telegram, WhatsApp, email, Stripe, Discord and the webhooks keep their keys:
those services have no sign-in for this.

The apps a build signs in through come from `OAUTH_APPS` at build time
(`make OAUTH_APPS="spotify=<client id>,facebook=<id>:<secret>" dist`; the release reads the
`KITTIES_OAUTH_APPS` repository secret), or from `KITTIES_<SERVICE>_CLIENT_ID` and
`KITTIES_<SERVICE>_CLIENT_SECRET` on the gateway. An event created with `attendees` invites them (Google, Outlook and CalDAV
send the invitations), so it asks first. Notion Calendar has no public API: it shows the Google
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

## The API

| Method and path                           | What it does                                   |
| ----------------------------------------- | ---------------------------------------------- |
| `GET /v1/health`                          | version, uptime, whether containers are available |
| `GET /v1/providers` · `GET /v1/connectors`| what the kitties can think with and reach      |
| `GET /v1/kitties` · `POST /v1/kitties`    | the fleet; create `{name, provider, soul, connectors, desktop, start}` |
| `POST /v1/kitties/{ref}/start` · `/stop` · `DELETE /v1/kitties/{ref}` | lifecycle, by id or name |
| `POST /v1/kitties/{ref}/exec` `{argv}`    | run a command in its container                 |
| `GET /v1/kitties/{ref}/screenshot`        | a PNG of its display                           |
| `POST /v1/scale/{name}` `{replicas}`      | run n replicas of a configured kitty           |
| `GET/POST /v1/chats`                      | threads; create `{title, members: [names]}`    |
| `POST /v1/chats/{id}/members` · `DELETE /v1/chats/{id}/members/{ref}` | who is in the group |
| `POST /v1/chats/{id}/messages` `{text}`   | send a message; returns the kitties' turns (a desktop task goes on in the background: poll the chat) |
| `POST /v1/chats/{id}/cancel` · `GET /v1/chats/{id}/shots/{name}` | stop a task; a step's screenshot |
| `GET /v1/settings` · `PUT/DELETE /v1/settings/providers/{name}` · `POST …/test` · `PUT/DELETE /v1/settings/connectors/{name}` | the models and the connectors |
| `GET /v1/local-token`                     | the token, handed only to a client on the same machine |
| `GET /v1/chats/{id}/events`               | server-sent events: `turn`, `update`, `done`, `deleted` |
| `POST /v1/oauth/{connector}/start` · `GET /v1/oauth/callback` | the OAuth sign-in of a connector |
| `PATCH /v1/kitties/{ref}` `{avatar, soul}` · `GET /v1/avatars` | change a kitty's looks; the breeds and coats |
| `GET/PUT/DELETE /v1/skills/{name}` · `POST …/accept` | the skills library and its proposals |
| `GET/PUT/DELETE /v1/artifacts/{name}`     | the artifacts library (raw file bodies) |
| `GET /v1/approvals?status=pending` · `POST /v1/approvals/{id}/approve` `{always}` · `…/reject` `{reason}` | the actions held for a yes, and the answer |
| `GET /v1/approvals/rules` · `DELETE /v1/approvals/rules/{id}` | what kitties may do alone, and taking it back |
| `GET/POST /v1/kitties/{ref}/memory` · `DELETE …/memory/{id}` · `PUT …/memory/profile` · `POST …/memory/distill` | a kitty's notes, its picture of you, and a distillation now |
| `POST /v1/chats/{id}/turns/{index}/feedback` `{rating, note}` | a thumb under an answer |
| `PUT /v1/settings/notify` · `POST /v1/settings/notify/test` · `POST …/notify/digest` | where and when kitties tell you; a test; the daily summary now |
| `POST /v1/settings/accounts/{kind}/login` · `GET/DELETE /v1/settings/logins/{id}` · `POST …/paste` · `DELETE /v1/settings/accounts/{kind}` | sign a model in with a subscription, follow it, paste the browser's address, sign out |

Every other call needs `Authorization: Bearer <token>`; `/` and `/v1/health` are open.

## Development

```sh
make check    # gofmt, go vet, go test -race
make cover    # coverage per package (gate: 90 %)
make dist     # linux, macOS, windows and android binaries
make image    # the kitty-desktop container image
make desktop-check   # the Wails module and its page
make e2e             # the desktop app end to end, in a browser; rewrites docs/screenshots
```

`cmd/kitties/smoke_test.go` runs the real gateway against a stand-in language model that speaks the
OpenAI protocol with native tool calls, and proves the chain that needs no real model: a kitty
asks to post, the action is held, the phone is told, a yes sends it exactly once, and the
memory and the budget move.

The end-to-end run (`desktop/frontend/e2e`, Playwright) starts a demo gateway and the built
page, seeds chats, connectors and the library through the API, checks every screen and
photographs it into `docs/screenshots`: the pictures in this README are regenerated by the
test that proves them, and CI uploads them as an artifact on every run.

On a tag CI also publishes the `kitty-desktop` image to `ghcr.io/madkoding/kitty-desktop`
(amd64 and arm64) and attaches the Wails desktop builds for Linux, Windows and macOS to the
release next to the CLI binaries.

The project builds with the Go standard library only. Commits follow
[Conventional Commits](https://www.conventionalcommits.org/); releases are cut from tags.
See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the design in more depth.

Free software under [GPL-3.0-or-later](LICENSE) · © 2026 madkoding
