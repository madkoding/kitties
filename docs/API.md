# The API

| Method and path                           | What it does                                   |
| ----------------------------------------- | ---------------------------------------------- |
| `GET /v1/health`                          | version, uptime, whether containers are available |
| `GET /v1/providers` · `GET /v1/connectors`| what the kitties can think with and reach      |
| `GET /v1/kitties` · `POST /v1/kitties`    | the fleet; create `{name, provider, soul, connectors, desktop, start}` |
| `POST /v1/kitties/{ref}/start` · `/stop` · `DELETE /v1/kitties/{ref}` | lifecycle, by id or name |
| `GET /v1/kitties/{ref}/power` · `POST /v1/kitties/{ref}/power` `{on, force}` | its machine: `{state: on\|starting\|off\|error, step, off_reason, auto_off_minutes, in_use, in_use_by, off_in_seconds…}`; `step: desktop` while its container is up and its desktop still coming up; turn it on (202, it comes up in the background) or off (409 when something uses it, unless `force`) |
| `PATCH /v1/kitties/{ref}` `{auto_off_minutes}` | minutes without use before its machine turns itself off (`0` never; `auto_off_default: true` goes back to the gateway's default) |
| `POST /v1/kitties/{ref}/exec` `{argv}`    | run a command in its container                 |
| `GET /v1/kitties/{ref}/screenshot`        | a PNG of its display                           |
| `POST /v1/kitties/{ref}/control` `{hold}` | take its display over, or give it back         |
| `POST /v1/kitties/{ref}/input` `{action, args}` | a click, drag, scroll, key or text of the person on it |
| `POST /v1/kitties/{ref}/voxl/ticket`      | a ticket for the H.264 live view (voxl): `{path}` to frame, `/v1/kitties/{ref}/voxl/t/{ticket}/` (404 when its desktop has no voxl) |
| `GET /v1/kitties/{ref}/stream`            | its display live, as motion JPEG (`?fps=1-30`) |
| `GET /v1/kitties/{ref}/rects`             | its display live as what changed on it, like VNC (`?fps=1-30`; `SCREEN`, `UPDATE`, `RECT` lines with JPEG bodies) |
| `GET/PUT /v1/kitties/{ref}/clipboard` `{text}` | its clipboard, both ways                  |
| `POST /v1/scale/{name}` `{replicas}`      | run n replicas of a configured kitty           |
| `GET/POST /v1/chats`                      | threads; create `{title, members: [names]}`    |
| `POST /v1/chats/{id}/members` · `DELETE /v1/chats/{id}/members/{ref}` | who is in the group |
| `POST /v1/chats/{id}/messages` `{text, files: [{name, mime, data}]}` | send a message; returns the kitties' turns (a desktop task goes on in the background: poll the chat). `files` is optional, `data` is base64; up to 6 files and 20 MB in all, checked by their real type (a program is refused with 400, a body over the limit with 413). The turn lists them in `attachments` |
| `GET /v1/chats/{id}/files/{file}`         | a file of the chat: a picture inline, anything else as a download |
| `POST /v1/chats/{id}/cancel` · `GET /v1/chats/{id}/shots/{name}` | stop a task; a step's screenshot |
| `GET /v1/settings` · `PUT/DELETE /v1/settings/providers/{name}` · `POST …/test` · `PUT/DELETE /v1/settings/connectors/{name}` | the models and the connectors (a connector that signs in needs its sign-in or a key; `?sign_in=1` saves it just before its sign-in); `POST …/connectors/{name}/test` checks its account with a request that only reads |
| `GET /v1/local-token`                     | the token, handed only to a client on the same machine |
| `GET /v1/chats/{id}/events`               | server-sent events: `turn`, `update`, `done`, `deleted` |
| `POST /v1/oauth/{connector}/start` · `GET /v1/oauth/callback` · `POST /v1/oauth/fragment` | the OAuth sign-in of a connector (`fragment`: the token of a flow that returns it in the address, Trello; open, tied to the flow by its `state`) |
| `PATCH /v1/kitties/{ref}` `{avatar, soul}` · `GET /v1/avatars` | change a kitty's looks; the breeds and coats |
| `GET/PUT/DELETE /v1/skills/{name}` · `POST …/accept` | the skills library and its proposals |
| `GET/PUT/DELETE /v1/artifacts/{name}`     | the artifacts library (raw file bodies) |
| `GET /v1/approvals?status=pending` · `POST /v1/approvals/{id}/approve` `{always}` · `…/reject` `{reason}` | the actions held for a yes, and the answer |
| `GET /v1/approvals/rules` · `POST /v1/approvals/rules` `{kitty, connector, action, mode}` · `DELETE /v1/approvals/rules/{id}` | the action limits (ask, never, auto; an empty action is all of the connection's), and taking one away |
| `GET/POST /v1/kitties/{ref}/memory` · `PUT/DELETE …/memory/{id}` · `PUT …/memory/profile` · `POST …/memory/distill` | a kitty's notes (correct one with `{"text": …}`), its picture of you, and a distillation now |
| `GET/POST /v1/memory/shared` · `PUT/DELETE /v1/memory/shared/{id}` | the notes every kitty knows |
| `POST /v1/chats/{id}/turns/{index}/feedback` `{rating, note}` | a thumb under an answer |
| `PUT /v1/settings/notify` · `POST /v1/settings/notify/test` · `POST …/notify/digest` | where and when kitties tell you; a test; the daily summary now |
| `PUT /v1/settings/autonomy` | `{"follow_ups": "10m 30m 2h", "brain_wait": "30m", "stand_ins": "3"}`: how long the kitties keep at a task on their own (empty: defaults, `off`: none) |
| `GET /v1/chats/{id}/turns/{n}/replay.gif` · `POST …/turns/{n}/help` `{text, secret, enter, save}` | a task as an animated GIF; the person's answer to a kitty that asked for help (a secret is typed on its desktop, not told) |
| `GET /v1/vault` · `PUT/DELETE /v1/vault/{name}` `{value, kitties}` | the secrets a kitty may type: names and who may use them, never values |
| `GET /v1/kitties/{ref}/desktop` · `POST …/backup` · `POST …/update` | its desktop's image and whether a newer one waits, the backup of its cookies, sessions and passwords, and moving it to the newest image |
| `GET /v1/shared` · `GET`/`DELETE /v1/kitties/{ref}/shared/{path}` (`?download=1`) · `POST /v1/kitties/{ref}/keep/{path}` | what every kitty left in its Shared folder (and the quota), one file (sandboxed), delete it, copy it into the library |
| `GET /v1/kitties/{ref}/export` · `POST /v1/kitties/{ref}/import` | a `.tar.gz` of its home, work and Shared folders without keys, passwords or browser sessions; put one back into its running desktop |
| `GET /v1/update` (`?refresh=1`) · `POST /v1/update/apply` | the version of Kitties and whether a newer one is out; install it and restart |
| `POST /v1/settings/accounts/{kind}/login` · `GET/DELETE /v1/settings/logins/{id}` · `POST …/paste` · `DELETE /v1/settings/accounts/{kind}` | sign a model in with a subscription, follow it, paste the browser's address, sign out |

`POST /mcp` is the [MCP connector](MCP.md) (off by default, its own bearer token). `GET /v1/mcp/clients` · `DELETE /v1/mcp/clients/{id}` list and revoke the OAuth clients that signed in. `GET`/`PUT /v1/mcp` read and change the connector's switches (`accept_risk` to turn on exec or approve; the gateway restarts); `GET /v1/mcp/token` returns its token (explicit, never cached) and `POST /v1/mcp/token` rotates it; `POST /v1/mcp/test` asks the connector for its tools and says whether it answers.

`GET`/`PUT /v1/whatsapp` read and change the [WhatsApp link](WHATSAPP.md) (on, the allow list, the kitty that answers, the groups it answers in and the ones that wrote lately, a kitty per number or group, the hourly limit, the quiet hours; `accept_risk` to turn it on) and give the QR code to scan while it waits; `POST /v1/whatsapp/link` asks for new codes, `POST /v1/whatsapp/reconnect` connects again with the kept session, `POST /v1/whatsapp/unlink` removes the device and forgets its session, and `POST /v1/whatsapp/test` writes to a number on the list.

Every other call needs `Authorization: Bearer <token>`; `/` and `/v1/health` are open.
