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

Kitties is the fleet counterpart of [motita](https://github.com/madkoding/motita): a kitty can
hand a coding task to a motita gateway through the `motita` connector and bring the result back
to the conversation.

## Quick start

```sh
go install github.com/madkoding/kitties/cmd/kitties@latest   # or: make build
kitties init                 # writes kitties.yaml and souls/tom.yaml
export ANTHROPIC_API_KEY=... # or OPENAI_API_KEY, GEMINI_API_KEY, GROQ_API_KEY...
make image                   # builds the kitty-desktop image (needs Docker or Podman)
kitties gateway              # creates the kitties of the file, starts them, serves the API
```

Then open `http://127.0.0.1:7488` with the token the gateway printed, or stay in the terminal:

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
- **Connectors** (`internal/connector`): a kitty writes `@connector slack {"text": "..."}` in a
  reply; the runtime runs it and feeds the result back before the kitty answers the person.
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

Keys come from the provider's usual variable (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`,
`GEMINI_API_KEY`, `GROQ_API_KEY`, `OPENROUTER_API_KEY`, `DEEPSEEK_API_KEY`, `DASHSCOPE_API_KEY`,
`MISTRAL_API_KEY`, `OLLAMA_API_KEY`) unless `api_key` or `api_key_env` says otherwise.

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
| `POST /v1/chats/{id}/messages` `{text}`   | send a message; returns the kitties' turns     |

Every call except `/` and `/v1/health` needs `Authorization: Bearer <token>`.

## Development

```sh
make check    # gofmt, go vet, go test -race
make cover    # coverage per package (gate: 90 %)
make dist     # linux, macOS, windows and android binaries
make image    # the kitty-desktop container image
```

The project builds with the Go standard library only. Commits follow
[Conventional Commits](https://www.conventionalcommits.org/); releases are cut from tags.
See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the design in more depth.

Free software under [GPL-3.0-or-later](LICENSE) · © 2026 madkoding
