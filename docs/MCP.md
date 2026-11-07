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
    allow_approve: false  # true adds approval_approve (Claude may say yes, once, to a held action)
```

Or by environment: `KITTIES_MCP=true`, `KITTIES_MCP_TOKEN`, `KITTIES_MCP_EXEC=true`, `KITTIES_MCP_OAUTH=true`, `KITTIES_MCP_APPROVE=true`. The gateway
prints the address and where the token is when it starts (`MCP connector: http://127.0.0.1:7488/mcp`).

## From Settings

**Settings → Preferences → Claude (MCP)** shows and changes all of this without editing files:

![The Claude (MCP) section of Settings: the switches, the claude mcp add command and the signed-in clients](screenshots/59-settings-mcp.png)

- Switches: the connector, `kitty_exec`, `approval_approve` and OAuth sign-in. Turning on exec or
  approve asks for an explicit yes. The choice is kept in `<data>/mcp-settings.json`, wins over the
  file and the environment, and the gateway restarts to apply it.
- The `claude mcp add` command, ready to copy. It reads the token from its file: **the token is
  never drawn on the page.** *Copy token* and *Rotate token* are explicit buttons (rotating cuts off
  every connection; a token set in the configuration or `KITTIES_MCP_TOKEN` is not rotated here).
- *Test connection* asks the connector for its tools, with its own token, and says how many Claude gets.
- With OAuth on, the clients that signed in (and when each was last used), each with a *Revoke* button.

The same through the API (gateway token): `GET`/`PUT /v1/mcp`, `GET`/`POST /v1/mcp/token`, `POST /v1/mcp/test`.

## Connect Claude

```sh
claude mcp add --transport http kitties http://127.0.0.1:7488/mcp \
  --header "Authorization: Bearer $(cat ~/.kitties/mcp.token)"
```

The token file is `mcp.token` in the data directory (`~/.kitties` by default, or `data_dir`). For a gateway on another machine put
https in front (`public_url`, TLS or a proxy): the token travels in a header.

Without sending a header, Claude Code can also start the bridge as a command; it reads the token
itself and forwards to the running gateway:

```sh
claude mcp add kitties -- kitties mcp            # or: kitties mcp -url https://k.example/mcp -token ...
```

### claude.ai (remote connector)

claude.ai reaches the gateway from the internet and signs in with OAuth. Put https in front
(`public_url`, TLS or a proxy), then:

```yaml
gateway:
  public_url: https://kitties.example.com
  mcp: { enabled: true, oauth: true }
```

Add `https://kitties.example.com/mcp` as a custom connector in claude.ai. It registers itself,
opens the gateway's consent page and you type the contents of `mcp.token` once. The gateway
issues one-hour access tokens and 30-day refresh tokens, signed with a key derived from
`mcp.token`, so replacing that token revokes every connection. To cut off one client only, with
the gateway's own token:

```sh
curl -H "Authorization: Bearer $(kitties token)" http://127.0.0.1:7488/v1/mcp/clients          # who signed in
curl -X DELETE -H "Authorization: Bearer $(kitties token)" http://127.0.0.1:7488/v1/mcp/clients/<id>
```

Its tokens stop working at once (the list is kept in `<data>/mcp-clients.json`); to come back it
signs in again.

## Tools

| Tool | What it does |
| ---- | ------------ |
| `fleet_status`, `provider_list` | version, uptime, containers; the models (never keys) |
| `kitty_list`, `kitty_get` | the fleet and one kitty |
| `kitty_create`, `kitty_start`, `kitty_stop` | lifecycle |
| `kitty_remove` | delete a kitty; needs `confirm: true` |
| `kitty_screenshot` | its desktop as a picture |
| `kitty_exec` | a command in its container (only with `allow_exec`) |
| `chat_create`, `chat_list`, `chat_get` | threads (`chat_create` with no members opens one with the lead kitty); `chat_get` returns the last turns |
| `chat_send`, `chat_cancel` | give a task and read the answer; stop it |
| `kitty_power`, `kitty_compute` | turn a machine on or off; where it runs (local or cloud) and its state |
| `goal_list`, `goal_add`, `goal_run`, `goal_remove`, `kitty_journal` | goals on a schedule or event, run one now, read the journal |
| `memory_list`, `memory_remember`, `memory_edit`, `memory_forget` | what a kitty knows about you and the work (the Memory panel), including the `shared` notes every kitty sees; a note written here is pinned |
| `fleet_lead`, `fleet_lead_set`, `lead_ask` | the lead kitty and its Reports thread; make another the lead (or none); give the lead a task for the whole fleet and read its report ([the lead kitty](AUTONOMY.md#the-lead-kitty)) |
| `approval_list`, `approval_reject` | see and reject actions held for a yes |
| `approval_approve` | say yes, this once, to a held action; **only with `allow_approve`** (by default approving stays with you) |

A desktop task goes on after `chat_send` returns: follow it with `chat_get`.

`kitty_compute` only reports where a desktop runs. Local or cloud follows the configuration
(`containers.runtime` and each kitty's `compute`); switching it per kitty from the API waits
for the cloud desktop channel (see [design/cloud-compute.md](design/cloud-compute.md)).

## Safety

- The connector accepts only its own token (not the gateway's, not a cookie, not `?token=`), so
  it can be revoked alone: delete `mcp.token` and restart.
- A request that names another web origin is refused, and wrong tokens are rate-limited.
- No tool returns provider keys, connector secrets or vault values.
- Transport: Streamable HTTP, one JSON answer per POST; no sessions, no streams.
