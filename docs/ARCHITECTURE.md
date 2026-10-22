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
| Hands      | `internal/connector` | External services: `webhook`, `slack`, `discord`, `telegram`, `shell` (a fixed command chosen by the operator), `motita` (opens a session on a motita gateway, posts the task, reads the event stream to its `done`). |

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
2. The brain answers. Lines of the form `@connector <name> <json>` are parsed out and run (only
   the connectors the kitty is allowed), their results are fed back as a message and the brain is
   asked again, up to `MaxRounds` times.
3. The turn is saved with the connector results and the token usage.

Turns of one session are serialised; different sessions run in parallel.

## The gateway (`internal/gateway`)

A `net/http` server with a bearer token (generated and printed when the configuration has none).
It exposes the fleet and the chats as JSON, serves screenshots as PNG, and ships a single-page
browser client at `/`. It binds to `127.0.0.1:7488` unless told otherwise and warns when it opens
on the network.

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
