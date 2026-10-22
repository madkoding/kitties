# Architecture

Kitties is a small set of Go packages with one direction of dependency: the CLI and the gateway
sit on top, the orchestrator in the middle, and the leaf packages (providers, souls, containers,
connectors) know nothing about each other.

```
cmd/kitties ─▶ gateway ─▶ chat ─▶ orchestrator ─▶ { provider, soul, container, connector }
                                                           ▲
                                              config ──────┘  (yamlx: the YAML parser)
```

## A kitty

| Part       | Package              | What it is                                                         |
| ---------- | -------------------- | ------------------------------------------------------------------ |
| Identity   | `internal/soul`      | The soul: persona, tone, traits, skills, language, instructions. Rendered to the system prompt with the context of the conversation (desktop or not, group members, connectors). |
| Brain      | `internal/provider`  | A client for one AI profile. Four API families cover every provider: chat completions, responses, Anthropic messages, Gemini. Retries on 429/5xx with backoff. |
| Body       | `internal/container` | One container from `docker/kitty-desktop`: Xvfb on `:99`, Chromium, xdotool, scrot. No port is published; the orchestrator uses `exec`. `kitty-shot` prints a PNG of the display, `kitty-open <url>` opens the browser. |
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

Google Workspace and Microsoft 365 sign in with the person's own OAuth app (client id, optional
secret) through the authorization-code flow with PKCE: `POST /v1/oauth/{connector}/start`
returns the consent URL, the browser lands on `GET /v1/oauth/callback` (open, tied to the flow
by its state), the code is exchanged and the tokens are written into the connector's settings
(`oauth_*` keys, never returned by the API). The connector's requests take their bearer token
from a token source bound to the connector's name, which refreshes it a minute before expiry
and saves the new one.

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

## The desktop app (`desktop/`)

A Wails v2 application in its own Go module, so the framework's dependencies never reach the
CLI. The Go side (`app.go`) owns the connection and proxies the API (`App.Request`), so the page
never holds the token; the page (`desktop/frontend`, Vite + Preact + Tailwind) works both inside
Wails and in a browser against the gateway, with the Motita design system's tokens, fonts and
components (`src/index.css`, `src/themes.css`). Deep links: `#chat/<id>`, `#kitty/<id>`,
`#new-chat`, `#new-kitty`, `#new-settings`, `#new-connectors`; `?theme=light|dark` and
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
