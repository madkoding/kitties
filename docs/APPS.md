# The desktop app and the Android app

## The desktop app

`desktop/` is a [Wails](https://wails.io) application (Linux, macOS, Windows) with a Preact page
that uses the Motita design system: dark ground, neon edges, cut corners, frosted panels,
Sansation and JetBrains Mono. It talks to a gateway, local by default (the downloads carry one and start it for you), and
needs no token on the machine the gateway runs on: the gateway leaves its token in `~/.kitties/gateway.token` and the
app picks it up. **Settings** holds the models (pick a provider, paste its key: it is tested on save), the
connections (with the kitties that may use each one), notifications and the gateway address.
A first run is a short checklist: a model, a kitty, a chat.

```sh
make frontend     # builds the page (node 24)
make desktop      # wails build, with the Wails CLI installed
(cd desktop/frontend && npm run icon)   # redraw the app icon and the macOS disk image's background
cd desktop && wails dev   # live reload while working on it
```

| | |
| --- | --- |
| ![A chat with one kitty](screenshots/02-chat.png) | ![A group chat](screenshots/03-group.png) |
| ![A kitty's desktop](screenshots/05-kitty-drawer.png) | ![Settings: the models](screenshots/08-settings-models.png) |
| ![Settings: the connectors](screenshots/09-settings-connectors.png) | ![The catalogue: one button per service](screenshots/17-connector-catalogue.png) |
| ![Google Workspace: sign in with OAuth](screenshots/18-connector-oauth.png) | ![A kitty's own display](screenshots/13-tom-display.png) |
| ![The library: skills](screenshots/15-library-skills.png) | ![The library: artifacts](screenshots/16-library-artifacts.png) |
| ![New kitty: pick its pixel cat](screenshots/07-new-kitty.png) | ![Light mode](screenshots/11-light.png) |
| ![A real kitty's desktop, live: Chromium and a terminal on Lumen](screenshots/48-live-desktop.png) | ![Taking control: a command typed in the kitty's terminal](screenshots/49-live-desktop-control.png) |
| ![A first run is a checklist](screenshots/31-welcome.png) | ![Adding a model: pick the provider](screenshots/32-pick-provider.png) |
| ![Adding a model: the key and the suggested model](screenshots/34-add-model.png) | ![A connection and the kitties that may use it](screenshots/33-connection-form.png) |
| ![A Claude subscription, through Claude Code](screenshots/35-claude-subscription.png) | ![GitHub Copilot: sign in with a code](screenshots/36-copilot-code.png) |
| ![The Android app asks for the gateway](screenshots/37-android-connect.png) | ![The Android app, connected](screenshots/38-android-app.png) |
| ![A kitty posting through Slack: the chip carries the logo](screenshots/19-connector-call.png) | ![The phone layout](screenshots/12-phone.png) |

More in [docs/screenshots](screenshots): the new chat and new kitty dialogs, the gateway
page, the phone layout and a kitty's own display.

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
put the gateway behind HTTPS ([DEPLOY.md](DEPLOY.md)). The gateway lets the app's page
call it with the token, and nothing else from other sites. `android/` is a plain WebView project:
`make android` builds the APK (Android SDK and JDK 17+). Releases are signed with the
`KITTIES_KEYSTORE_BASE64`, `KITTIES_KEYSTORE_PASSWORD` and `KITTIES_KEY_ALIAS` secrets when they
are set; without them each release gets a debug key, and updating means uninstalling first.
