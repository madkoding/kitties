# WhatsApp: a number of the kitties' own

[En español](WHATSAPP.es.md)

Kitties can link to a WhatsApp number the way WhatsApp Web does: as a **linked device** of a phone,
with a QR code shown in Settings. Then the people you put on the list write to that number and a
kitty of yours answers them, and the kitties can write to those people with the `whatsapp-web`
connector.

![WhatsApp in Settings, waiting for the QR code](screenshots/60-settings-whatsapp.png)

> [!WARNING]
> **This is not an official API.** WhatsApp's terms of service do not allow unofficial or automated
> clients, and WhatsApp may ban a number used this way, for a while or for good. Use a **spare
> number** (a second SIM, a prepaid line), never your personal one, and keep the volume low and
> personal (no bulk messages, no strangers).
>
> **The official alternative** is the [WhatsApp Business Cloud API](https://developers.facebook.com/docs/whatsapp/cloud-api)
> from Meta: the `whatsapp` connection of the catalogue (Settings, Connections, Add a connection,
> WhatsApp) sends messages with it. It needs a Meta Business account and a number registered
> there, and messages outside a 24-hour window must use approved templates; in exchange it is
> allowed and is not banned for being automated.

## Turning it on

1. Settings, **Connections**, the **WhatsApp (linked device)** section at the end.
2. Choose **the kitty that answers**, and add the **numbers that may write** with their country
   code (`+56 9 1234 5678`). The list starts empty: until you add someone, every message is
   ignored. Tick **Answer in groups when someone mentions the kitty** to let it talk in groups too,
   and pick the groups: the ones that wrote lately show up there (WhatsApp gives a linked device no
   list of groups until they write). With more than one kitty, **Who answers each one** gives a
   number or a group a kitty of its own; **Messages sent per hour** caps what goes out (60 when
   left empty); **Quiet hours** hold the answers until they end.
3. Tick **Turn on WhatsApp**, then **I understand that WhatsApp may ban this number**, and **Save**.
4. A QR code appears. On the phone of the kitties' number: WhatsApp, Settings, **Linked devices**,
   **Link a device**, and point it at the code. The code changes every few seconds; if they run out,
   **Show a new code**.
5. Once it says **Connected · +number**, **Send a test** writes to a number on the list.

The phone must stay on and online from time to time (WhatsApp unlinks a device whose phone has been
off for about two weeks). When the connection drops, **Reconnect** opens it again with the kept
session, without a new code. **Unlink** removes the device from the phone and forgets the session.

## How it works

- **One chat per number.** The first message of a number opens a chat titled `WhatsApp: <name>`
  with the chosen kitty; the next ones go to the same chat, so you see the whole conversation on the
  page too. The kitty's answer goes back to WhatsApp, with its Markdown turned into WhatsApp's own
  marks (`*bold*`, `_italics_`). A desktop task is followed: the first words go at once and the
  result when it is done.
- **Read, and reactions.** A message is marked read (blue ticks) when the kitty starts on it. An
  answer that is only an emoji goes as a reaction to the message, and the kitties can react with
  the connector.
- **Quotes.** In a group, and in a chat where newer messages came in the meantime, the answer quotes
  the message it answers.
- **Quiet hours.** From one time to another (the gateway's clock, `TZ`), a message gets one short
  note in the kitty's language ("🌙 luna is resting until 08:00…") and its answer when they end; the
  kitties' own sends and reactions wait too. Messages waiting are kept sealed (`queue.bin`), so a
  restart does not lose them: they are answered on the next connection after the quiet hours.
- **A kitty per number or group.** Each number or group may have its own kitty; the others go to
  the kitty that answers. Moving one to another kitty starts a new chat with it.
- **An hourly limit.** Every message, file and reaction that goes out counts; past the limit a
  reply is held (and counted in Settings) and a kitty's `send` fails with a clear error. It keeps
  the number from writing like a bot.
- **"typing…" while it thinks.** The chat on the phone shows the kitty typing until the answer goes.
  A message that comes while it is still busy gets a short note in the kitty's language (its soul's
  language, else the language of the message: English, Spanish or Portuguese).
- **Groups on their own list.** With groups on, the kitty answers only in the groups you picked,
  and there anyone may talk to it by @mentioning the kitties' number, answering one of its
  messages, or naming the kitty. Each group has its own chat (`WhatsApp group: <name>`), with every
  line signed by its sender; the answer goes to the group. A mention in a group not on the list is
  ignored and counted.
- **Pictures, videos, documents and audio.** They reach the kitty as files of the chat (up to 25 MB
  each), with their caption as the text. A **voice note** is turned into text with the speech API
  (Whisper) of an `openai` or `groq` profile with a key; without one it comes as an audio file.
- **Stickers, locations and contacts.** A sticker comes as its picture (with its emoji), a location
  as its name and address with a map link, a shared contact as its name and numbers.
- **Files back.** A file the kitty saves to the library while it answers goes to the person (or the
  group) by itself, a picture as a picture and anything else as a document (up to 16 MB).
- **The allow list works both ways.** A message from a number that is not on it is ignored (and
  counted in Settings), without an answer; the kitties can only write to numbers on it.
- **The kitties write with the connector.** While the link is connected, `whatsapp-web` is offered
  to the kitties (those whose connections list is empty or names it):

  ```
  @connector whatsapp-web {"action": "send", "to": "+56912345678", "text": "The report is ready."}
  @connector whatsapp-web {"action": "send", "to": "Familia", "text": "Here it is.", "file": "report.pdf"}
  @connector whatsapp-web {"action": "react", "to": "+56912345678", "emoji": "👍"}
  @connector whatsapp-web {"action": "status"}
  ```

  `to` is a number or a group on the list (its name or its id); `file` is a file of the library,
  sent with `text` as its caption. `to` left out writes to whoever wrote last (or to the only number on the list). A send reaches
  other people, so like every `send` it is **held for your approval** until you trust the kitty with
  it ([AUTONOMY.md](AUTONOMY.md)). The answers to a person who wrote are not held: they asked.

## From the file

The `whatsapp:` section of `kitties.yaml` sets the same while nothing was saved from Settings (on a
server, before opening the page). Settings then keeps its own in `settings.json`.

```yaml
whatsapp:
  enabled: true                  # turning it on here is accepting the risk
  allow: ["+56 9 1234 5678"]
  kitty: mimi                    # empty: the first kitty
  groups: true
  allow_groups: ["120363012345678901@g.us"]   # the ids Settings lists (GET /v1/whatsapp, seen_groups)
  kitties: {"+1 555 000 1111": luna, "120363012345678901@g.us": tom}
  max_per_hour: 30               # 0: 60
  quiet: {from: "22:00", to: "08:00"}
  transcribe: ""                 # the openai or groq profile for voice notes; empty: the first; off: none
```

The QR code is still scanned from Settings, or read with `GET /v1/whatsapp` (`qr_png`).

## Where things are kept

| What | Where | How |
| --- | --- | --- |
| On or off, the list, the kitty, the chat of each number | `<data>/whatsapp/settings.json` | mode 0600, no secrets |
| The messages waiting for the quiet hours to end | `<data>/whatsapp/queue.bin` | **sealed** like the session; gone once they are answered |
| The session of the linked device (its keys, the Signal sessions, the contacts) | `<data>/whatsapp/session.bin` | **sealed** with the vault's key (AES-256-GCM, bound to its use); never in the clear |

The session is a blob of the library's state kept in memory and sealed as a whole: the gateway uses
no SQLite file or any other plain store for it. The key is the vault's (`vault.key` beside it, or
`KITTIES_VAULT_KEY`); without a vault (a read-only data folder) WhatsApp cannot be turned on and
Settings says why. A session that cannot be opened with the key (the key changed) is not used: the
phone is linked again with a new code.

## The API

| Route | What it does |
| --- | --- |
| `GET /v1/whatsapp` | The link: `enabled`, `allow`, `kitty`, `state` (`off`, `starting`, `qr`, `qr_expired`, `connected`, `disconnected`, `logged_out`, `error`), `qr` and `qr_png` (a PNG data URL) while it waits, `phone`, `linked`, `ignored`, `seen_groups` (the groups that wrote lately), `sent_last_hour`, `held`, `quiet`, `waiting` |
| `PUT /v1/whatsapp` | `{"enabled", "allow", "kitty", "groups", "allow_groups", "kitties", "max_per_hour", "quiet_from", "quiet_to"}`; turning it on needs `"accept_risk": true` |
| `POST /v1/whatsapp/link` | New QR codes (after they expired or the phone logged the device out) |
| `POST /v1/whatsapp/reconnect` | Connects again with the kept session (no new code) |
| `POST /v1/whatsapp/unlink` | Removes the device from the phone and forgets the session |
| `POST /v1/whatsapp/test` | `{"to": "+56912345678"}` sends a short message to a number on the list |

## Under the hood

The protocol is [whatsmeow](https://github.com/tulir/whatsmeow) (MPL-2.0), the WhatsApp Web
multi-device protocol in Go. `internal/whatsapp` is the bridge (settings, list, chats, connector) and
knows nothing of the protocol (`transcribe.go` sends voice notes to `/audio/transcriptions`);
`internal/whatsapp/wameow` is the connection, the media downloads and the in-memory store that is
exported, sealed and read back. The bridge is tested with a fake client, the store and the
connection with a fake of whatsmeow's client.
