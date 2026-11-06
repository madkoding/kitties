# Claude as the orchestrator: the MCP connector

The gateway can speak the [Model Context Protocol](https://modelcontextprotocol.io) so Claude
(Claude Code, Claude Desktop, any MCP client) runs the fleet for you: list, create, start and
stop kitties, give them tasks, look at their desktops. It is **off by default** and has a token
of its own.

## Turn it on

```yaml
gateway:
  mcp:
    enabled: true      # off by default
    # token: ...       # optional; else one is made once and kept in <data>/mcp.token
    allow_exec: false  # true adds kitty_exec (commands inside a kitty's container)
```

Or by environment: `KITTIES_MCP=true`, `KITTIES_MCP_TOKEN`, `KITTIES_MCP_EXEC=true`. The gateway
prints the address and where the token is when it starts (`MCP connector: http://127.0.0.1:7488/mcp`).

## Connect Claude

```sh
claude mcp add --transport http kitties http://127.0.0.1:7488/mcp \
  --header "Authorization: Bearer $(cat ~/.kitties/mcp.token)"
```

The token file is `mcp.token` in the data directory (`~/.kitties` by default, or `data_dir`). For a gateway on another machine put
https in front (`public_url`, TLS or a proxy): the token travels in a header.

## Tools

| Tool | What it does |
| ---- | ------------ |
| `fleet_status`, `provider_list` | version, uptime, containers; the models (never keys) |
| `kitty_list`, `kitty_get` | the fleet and one kitty |
| `kitty_create`, `kitty_start`, `kitty_stop` | lifecycle |
| `kitty_remove` | delete a kitty; needs `confirm: true` |
| `kitty_screenshot` | its desktop as a picture |
| `kitty_exec` | a command in its container (only with `allow_exec`) |
| `chat_create`, `chat_list`, `chat_get` | threads; `chat_get` returns the last turns |
| `chat_send`, `chat_cancel` | give a task and read the answer; stop it |

A desktop task goes on after `chat_send` returns: follow it with `chat_get`.

## Safety

- The connector accepts only its own token (not the gateway's, not a cookie, not `?token=`), so
  it can be revoked alone: delete `mcp.token` and restart.
- A request that names another web origin is refused, and wrong tokens are rate-limited.
- No tool returns provider keys, connector secrets or vault values.
- Transport: Streamable HTTP, one JSON answer per POST; no sessions, no streams.
