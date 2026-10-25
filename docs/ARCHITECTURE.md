# Architecture

Kitties is a small set of Go packages with one direction of dependency: the CLI and the gateway
sit on top, the orchestrator in the middle, and the leaf packages (providers, souls, containers,
connectors) know nothing about each other.

```
cmd/kitties ─▶ gateway ─▶ chat ─▶ orchestrator ─▶ { provider, soul, container, connector }
                                                           ▲
                                              config ──────┘  (yamlx: the YAML parser)
```

## Overview

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
- **Containers** (`internal/container`): one container per kitty from `docker/kitty-desktop`: Debian, Xvfb,
  the Lumen desktop with its shelf, Chromium, a terminal, LibreOffice, Inkscape and the tools the kitty
  acts with. Nothing is published on the host; the orchestrator reaches a kitty with `docker exec`.
  What is in the image and how it looks: [KITTY-DESKTOP.md](KITTY-DESKTOP.md).
- **Orchestrator** (`internal/orchestrator`): the fleet, persisted in `~/.kitties/kitties.json`.
  Creates the kitties the configuration lists (`replicas: n` gives `name-1 … name-n`) and scales
  them on demand.
- **Chat** (`internal/chat`): one-to-one and group threads, persisted as JSON. In a group every
  kitty sees the others' turns labelled by name and answers in turn; `@name` addresses one.
- **Desktop tasks** (`internal/desktop`): a kitty with a container writes `@tool click {"x": 640,
  "y": 420}` lines; the runtime executes them (click, type, keys, scroll, drag, list and focus
  windows, open the browser, launch or install a program, run a command), takes a screenshot,
  shows it to the model and continues in the background until `@tool done`. Every step and
  picture lands in the thread; the chat can be cancelled. How it works, with diagrams:
  [DESKTOP.md](DESKTOP.md) ([en español](DESKTOP.es.md)).
- **Taking the desktop over**: the **Desktop** button of a chat (or *Live and take control* in a
  kitty's drawer) shows its display live, streamed as motion JPEG (a picture whenever it
  changes, about a quarter of a second behind). **Take control** sends your clicks, drags, wheel
  and keys to it, like a remote screen, for what the kitty cannot do alone (a password, a
  captcha); the clipboard goes both ways. The kitty's own actions wait until you give it back,
  and a hold left idle for five minutes ends by itself. A kitty that needs you says so with
  `ask_help`: the chat shows a **Take control** button and a notification goes out, and it goes
  on when you give the desktop back. Frames and input go through the gateway's API: no VNC port
  is opened.
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

## A kitty

| Part       | Package              | What it is                                                         |
| ---------- | -------------------- | ------------------------------------------------------------------ |
| Identity   | `internal/soul`      | The soul: persona, tone, traits, skills, language, instructions. Rendered to the system prompt with the context of the conversation (desktop or not, group members, connectors). |
| Brain      | `internal/provider`  | A client for one AI profile. Four API families cover every provider: chat completions, responses, Anthropic messages, Gemini. Retries on 429/5xx with backoff. |
| Body       | `internal/container` | One container from `docker/kitty-desktop`: Xvfb on `:99`, Chromium, xdotool, scrot. No port is published (Chromium's DevTools stays on the container's loopback); the orchestrator uses `exec`. `kitty-shot` prints a PNG of the display, `kitty-open <url>` opens the browser, `kitty-grab` streams the display as JPEGs through XShm, `kitty-ocr` reads its text and `kitty-page` reads and acts on the open page. |
| Hands      | `internal/connector` | External services from a catalogue (`catalog.go`): messaging, social media, git, productivity, office, drawing and generic kinds, each with its fields and actions. Most are tables of HTTP actions over one generic client; email is `net/smtp`; `shell` runs a fixed program; `motita` opens a session on a motita gateway, posts the task and reads the event stream to its `done`. A connector may answer with a file (`Filer`), which the chat keeps as an artifact. |
| Looks      | `internal/kitty`     | The avatar: a breed and a coat, picked from the name when none is chosen; the page draws and animates it (`desktop/frontend/src/PixelCat.tsx`). |
| Memory     | `internal/skills`, `internal/artifacts` | The library. Skills: markdown procedures (built-in ones embedded, the person's own in `<data_dir>/skills`, proposals in `skills/proposed`), listed in every prompt by name and summary, read and proposed with `@skill` lines. Artifacts: files in `<data_dir>/artifacts`, shared by the fleet, saved and read with `@artifact` lines and from the page. |

The kitty itself (`internal/kitty`) is a record: id, name, provider name, soul, connectors,
container name, status.

## The fleet (`internal/orchestrator`)

- `New` builds the provider registry and the connector registry from the configuration.
- `Create` validates the name, resolves the provider, loads and merges the soul
  (`default ← soul_file ← inline`), decides whether the kitty gets a container (a runtime is
  available and `desktop` is not false) and saves the record.
- `Start` creates the container the first time (workspace folder on the host, mounted at
  `/home/kitty/work`) or starts the stopped one. `Stop` keeps it, `Remove` deletes it.
- `Bootstrap` creates and starts what the configuration lists, `replicas: n` expanding to
  `name-1 … name-n`. `Scale` adds or removes replicas to reach a number.
- State lives in `<data_dir>/kitties.json`, written atomically.

Without Docker or Podman the orchestrator still works: every kitty is brain-only and the
commands that need a container say so.

## Conversations (`internal/chat`)

A session is a list of turns and a list of member kitty ids, saved as `<data_dir>/chats/<id>.json`.

`Send` appends the person's message and picks the responders: in a one-to-one chat the kitty;
in a group, the kitties the message mentions with `@name`, or every member when it mentions none.
Each responder answers in turn:

1. The transcript is rendered for that kitty: its own turns as `assistant`, everything else
   (the person and the other kitties) as `user` turns labelled `[name]: text`, so a kitty builds
   on what a teammate just said instead of repeating it.
2. The brain answers. With a provider that calls tools natively (the default), the desktop
   tools, the connectors (`connector_<name>`) and the library (`skill_read`, `artifact_save`,
   …) are offered as tools and the calls come back typed; with `tools: text` the same things
   are lines in the reply (`@tool`, `@connector`, `@skill`, `@artifact`). Both are parsed into
   one list of actions (`internal/chat/actions.go`), run the same way, and answered in the
   shape the model spoke: tool-result messages (with the screenshot as a following user turn)
   or one text message. This repeats up to `MaxRounds` times. A file a connector answered with
   is saved to the artifacts and named on the turn.
3. The turn is saved with the connector results and the token usage.

Turns of one session are serialised; different sessions run in parallel.

## The agenda (`internal/agenda`)

What makes a kitty work on its own. A `Goal` belongs to a kitty and has a title, the text the
kitty is told, a cron schedule (`every`, five fields or `@hourly`/`@daily`/`@weekly`/`@monthly`)
and the connectors whose events run it (`on`). The `Store` keeps them in `goals.json`; goals
from the configuration file are synced on every start (`Sync`, matched by kitty and title so
their run history survives) and the page cannot edit them. The `Scheduler` ticks every minute
and runs the goals that are due, `Fire` runs the ones listening to an event's source, `RunNow`
runs one by hand; a goal still busy with its previous run is skipped (and journaled), never
stacked. The `Runner` is the chat service: `RunGoal` finds or opens the goal's thread and posts
the scheduler's message (the goal, and why now); the result (`done`, `blocked`, `escalated`,
`delegated`) comes back through `Report` when the work ends.

The `Journal` is an append-only JSONL file per kitty: runs, events, retries, hand-overs,
escalations, recoveries and skips, newest first in the drawer. `Summarise` turns a webhook
body into the text a kitty reads: GitHub's action, issue or pull request and repository;
Slack's message, user and channel; a flat listing of fields otherwise.

Incoming events land on `POST /v1/hooks/{connector}` (open, rate limited): the connector's
`inbound_secret` must be presented as GitHub's `X-Hub-Signature-256`, as `X-Kitties-Secret`,
or as `?secret=`; the event's kind is `X-GitHub-Event` or `X-Kitties-Event`.

## Limits, retries, backup and delegation (`internal/chat/autonomy.go`)

A task runs within the kitty's `Limits` (tokens, seconds, rounds, retries; zero fields take the
chat service's defaults: 400k tokens, 30 minutes, 25 rounds, one retry). The round limit lets a
batch that calls `done` through. An attempt that fails for a reason worth retrying (an error
from the brain, the rounds running out) is tried again with the message "that attempt failed;
try a different approach"; a spent budget or a cancel is final. When the task is given up on
and a person did not cancel it, `giveUp` hands it to the kitty's `backup` (a thread
"mimi → luna", with the failure and the original request) or marks the turn `needs_help` and
journals an escalation.

`@delegate <name> {"task": ...}` (or the native `delegate` tool, offered when another kitty
exists) opens or reuses the thread between the two kitties (`Owner`, `Depth`), posts the task
in the delegating kitty's name, waits for the answer, including the background work the
other kitty may start, within that kitty's time limit, and feeds the answer back as the
action's result. `MaxDepth` (2) bounds how deep hand-overs go; at the limit the prompt names
no peers and the action is refused.

The orchestrator's `Watch` is the watchdog: every 30 seconds a running kitty whose container
stopped or vanished is started again and a kitty in error is tried once more; recoveries are
journaled.

## Memory, approvals and notifications

**Memory** (`internal/memory`). One JSON file per kitty under `<data_dir>/memory`: notes with a
scope (`user`, `work`, `project:<slug>`), an optional pin, and a profile with the person's
*demand* (1 to 5) and the verdict history. `chat.Service` renders `Prompt` into the system prompt
each turn. A thumb on a turn (`POST /v1/chats/{id}/turns/{i}/feedback`) records a verdict, moves
the demand, and every third verdict starts a distillation: the kitty's own brain turns the notes
of correction into lasting notes (an empty answer is dropped). `@memory` actions and the native
`memory_*` tools write notes from inside a reply.

**Approvals** (`internal/approvals`, `internal/connector/risk.go`). Every connector call has a
`Risk` (none, ask, always): by action name (`send`, `post`, `reply`, `like`... ask; `refund`,
`delete` always), by HTTP method for the generic API connector, and always for a shell command.
`chat.gate` asks `Store.Check(kitty, connector, action, risk, mode, demand)`: *allow* (reads,
`approvals: auto`, or a rule earned by `Threshold(demand)` yeses in a row: 2, 4, 6, 12, never),
or *hold*. A held action is not a blocking prompt: it is stored, the kitty is told it waits and
carries on, and `DecideApproval` runs it later and appends the result, or the person's reason, to
the thread as an `approval` turn. *Approve and stop asking* writes a rule that
`DELETE /v1/approvals/rules/{id}` removes. Pending requests, rules and a bounded history live in
`approvals.json`.

**Daily budget** (`internal/budget`, `chat/budget.go`). `Limits.DayTokens` and `DayActions`
cap a kitty's day. `budget.Store` counts tokens (every model reply in `answer` and in a desktop
task's rounds) and actions (a gated action that runs, or is approved) per kitty and day in
`budget.json`, keeping a fortnight. Out of tokens, `answer` and `attempt` refuse before asking
the model, and the `budget` notification goes out once a day. Past the actions, `gate` turns an
allowed action into a held one (even in `approvals: auto`), marked in its summary; the person's
yes runs it and counts it. `MemoryView.Spent` shows today's counts in the kitty's drawer.

**Notifications** (`internal/notify`). A `Notifier` sends through any connector that can send a
message (ntfy preferred, with a click link to the page), for the events `help`, `approval`,
`goal_done`, `goal_blocked`, `recovered` and `budget`. Events of the same kind within ten minutes fold into
one message. The choice is kept in `settings.json` over the YAML's `notify`; deleting the
connector clears it.

**Daily summary** (`internal/digest`). `Reporter` builds one message from the journal (what each
kitty finished, blocked, handed over, learned in the last 24 hours), today's spending and the
actions waiting for a yes, and sends it through `Notifier.Deliver` when the clock passes
`notify.digest` (gateway time) and today's was not sent; the day it last sent is kept in
`digest.json`, and a summary is only sent within 90 minutes of its time. `POST
/v1/settings/notify/digest` sends one now.

**Reading connectors** (`internal/connector/reads.go`). Kinds gain read, reply and like actions
(Mastodon, Bluesky, Gmail, Outlook, Slack, Telegram) and new kinds (Stripe, Zendesk, ntfy).
`listing` renders JSON arrays as one short line per item; `request.follow` lets one action
chain requests (Gmail lists ids, then fetches headers). Stripe events become goal inputs through
`POST /v1/hooks/<connector>` with `Stripe-Signature` verified.

**Thread compaction** (`chat.compact`). Past `CompactAfter` (40) turns since the last summary,
the earlier turns are summarised by the kitty's brain into `Session.Summary` (covering
`SummaryUpTo` turns) and only the last `CompactKeep` (16) go verbatim into the prompt; the
transcript the person reads is never cut.

**Persistent browser profile.** `kitty-open` starts Chromium with `--user-data-dir` in the
kitty's workspace (`/home/kitty/work/.browser`, `KITTY_PROFILE` overrides it) and removes a stale
profile lock, so sessions survive container restarts.

## Desktop tasks (`internal/desktop`)

A kitty with a container has, in its system prompt, a catalogue of tools: `screenshot`, `click`,
`double_click`, `move`, `type`, `key`, `scroll`, `open`, `run`, `wait`, `done`. It asks for them
with `@tool <name> <json>` lines. When a reply carries such lines:

1. `Send` returns at once with the turn marked `working`, and the loop continues in a goroutine
   (`chat.Service.runTask`), so the person keeps the interface and can cancel.
2. Each action becomes an argv (`xdotool mousemove x y click 1`, `kitty-open <url>`, the program
   of `run` as a list, never a shell) executed with `docker exec` in the kitty's container.
3. After the batch a screenshot is taken, saved under the chat's folder, attached to the turn's
   last step and shown to the model as an image together with the results. Every provider
   family carries images (data URLs for chat completions and responses, base64 blocks for
   Anthropic and Gemini).
4. The model continues, up to `MaxSteps` rounds, until it writes `@tool done` or stops asking
   for tools. The final words are appended to the same turn.

The demo (`internal/demo`) implements the same `Runner` with an in-memory desktop that draws its
screenshots, which is what the documentation's pictures show.

## Live events

`chat.Service.Subscribe(id)` hands a watcher every new or changed turn of a session (`turn`,
`update` while a task adds steps, `done`, `deleted`); a slow watcher loses events rather than
holding the chat. The gateway streams them at `GET /v1/chats/{id}/events` as server-sent events
with a heartbeat. The page uses an `EventSource` in a browser; inside Wails the Go side reads the
stream (`App.Watch`) and re-emits each event through the Wails runtime, since the page there
holds no token.

## OAuth (`internal/oauth`)

Google Workspace, Microsoft 365, Slack and GitHub sign in with the person's own OAuth app
(client id, optional secret) through the authorization-code flow with PKCE: `POST
/v1/oauth/{connector}/start` returns the consent URL, the browser lands on `GET
/v1/oauth/callback` (open, tied to the flow by its state), the code is exchanged and the tokens
are written into the connector's settings (`oauth_*` keys, never returned by the API). Slack
and GitHub keep their token field too: a typed token wins, a webhook needs no account, and
without either the sign-in's token signs the request (`needs_sign_in` in the settings view says
when a connector has neither). Slack and GitHub OAuth-app tokens last until revoked, so an
answer without `expires_in` is kept for good.

The connector's requests take their bearer token from a token source bound to the connector's
name, which refreshes it a minute before expiry and saves the new one. The gateway also runs
`Manager.KeepFresh` in the background (every five minutes, through the orchestrator's
`KeepTokensFresh`): a signed-in connector whose token expires within ten minutes is refreshed
ahead of time, so the first call after a quiet hour neither waits nor fails; a refresh that
fails is logged and tried again next round, and the on-use refresh still stands behind it.

## End to end (`desktop/frontend/e2e`)

Playwright drives the built page against a demo gateway (`kitties -demo`, ports 7498/4199 so
a gateway a person runs is left alone). The global setup seeds models, connectors (a Slack
webhook pointed at a stub of its own, so the post lands), a skill, an artifact, a proposed
skill and four chats through the API, waits for the scripted brain to answer, then each test
checks one screen (the catalogue has a logo on every button, the OAuth form offers the
sign-in and the consent URL comes back, the chip of a connector call carries its logo) and
photographs it into `docs/screenshots` under the names the docs use. `make e2e` runs it; CI runs it
on every push and uploads the screenshots and the report.

## Run-time settings (`internal/orchestrator/settings.go`)

The page edits models (provider profiles) and connectors. They are kept in
`<data_dir>/settings.json` and merged over the YAML at start: an entry edited on the page
shadows the file's, one deleted on the page stays deleted. Keys are stored there and never
returned by the API, which only says whether one is set. A profile that cannot be built (no key)
stays on the page with the reason instead of stopping the fleet.

## The gateway (`internal/gateway`)

A `net/http` server with a bearer token (generated when the configuration has none). It exposes
the fleet, the chats and the settings as JSON, serves screenshots as PNG, and ships a small
browser page at `/`. It binds to `127.0.0.1:7488` unless told otherwise and warns when it opens
on the network.

A local program never types the token: the gateway writes it to `<data_dir>/gateway.token`
(mode 0600), which the desktop app reads, and `GET /v1/local-token` hands it to a loopback client
whose request is not a cross-origin fetch (no `Origin` header, or the gateway's own). A page of
another site cannot read it, and a network peer gets 403.

## Serving it (`deploy/`, `docker/gateway`)

`docker/gateway/Dockerfile` builds the page, embeds it (`internal/web`) and ships the binary on
Alpine with the docker client; the gateway starts the kitties' containers through the host's
socket, so the data folder is mounted at the same path inside and outside (workspaces are
mounted by host path). The page signs in with `POST /v1/login` (the token for a `HttpOnly`,
`SameSite=Strict` session cookie, rate limited per address) and `GET /v1/session` tells it where
it stands; `cookieLogin()` in `api.ts` says when a page uses the cookie (served by the gateway,
not in Wails, no other address). The sign-in e2e (`login.spec.ts`) runs against a second demo
gateway with `local_token: false` and a `public_url`, as it would sit behind a proxy.
CI builds the image and checks its health and the 401 on every push, and publishes it on tags.
[DEPLOY.md](DEPLOY.md) is the operator's guide.

## The desktop app (`desktop/`)

The macOS download is a disk image (`desktop/dmg`): the signed-ad-hoc `Kitties.app`, an
Applications shortcut, a `READ ME FIRST.txt` with the `xattr` command for the first open, and a
background with an arrow, laid out by `dmgbuild` (no Finder needed in CI). If the styled image
cannot be built, CI falls back to a plain `hdiutil` image with the same three items, and always
mounts the result to check them. The icon and the background are drawn by
`frontend/scripts/appicon.mjs` from the interface's own tabby.

A Wails v2 application in its own Go module, so the framework's dependencies never reach the
CLI. The Go side (`app.go`) owns the connection and proxies the API (`App.Request`), so the page
never holds the token; the page (`desktop/frontend`, Vite + Preact + Tailwind) works both inside
Wails and in a browser against the gateway, with the Motita design system's tokens, fonts and
components (`src/index.css`, `src/themes.css`). Deep links: `#chat/<id>`, `#kitty/<id>`,
`#new-chat`, `#new-kitty`, `#new-settings`, `#new-connectors`, `#new-approvals`, `#new-notifications`; `?theme=light|dark` and
`?motion=off` for screenshots.

## Platforms

The binary is pure Go: Linux, macOS, Windows (amd64, arm64) and Android arm64 (Termux) are built
by `make dist`. Containers run wherever Docker or Podman does; on Android or a laptop without
Docker the CLI and the gateway still run brain-only, or point at a remote Docker with
`DOCKER_HOST`.

## Motita

Motita is the single-agent counterpart: an agent that proves a task is done. Kitties reuses its
provider design (one normalised message shape, SDK-free HTTP) and its dependency-free YAML parser
(`internal/yamlx`). A kitty delegates to motita through the `motita` connector:
`POST /v1/sessions`, `POST /v1/sessions/{id}/task`, then the `progress`/`done`/`error` events.
