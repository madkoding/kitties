# Install and first steps

Four ways in: a demo with no key and no Docker, one line on a Linux server, a release you download, or
the real thing with containers. For a server, see [DEPLOY.md](DEPLOY.md).

## Try it in two minutes, no key and no Docker

```sh
go install github.com/madkoding/kitties/cmd/kitties@latest   # or: make build
kitties -demo gateway        # three kitties with a scripted brain and a drawn desktop
```

Open `http://127.0.0.1:7488` (on the same machine no token is typed: the page gets it from the
gateway) or run the desktop app, then ask Tom to *open https://example.com*.

## On a Linux server, one line

```sh
curl -fsSL https://raw.githubusercontent.com/madkoding/kitties/main/scripts/install.sh | sudo sh
```

It picks the release for the machine (amd64, arm64 or arm), checks it against the release's
`SHA256SUMS`, installs `/usr/local/bin/kitties`, creates the `kitties` user (in the `docker` group
when there is one), writes `/etc/kitties/kitties.env` with a generated token and installs and
starts the `kitties` systemd service ([DEPLOY.md](DEPLOY.md) is the same by hand). At the end it
prints the token. The gateway listens on `127.0.0.1:7488`: reach it with
`ssh -L 7488:127.0.0.1:7488 you@server`, or put nginx or Caddy with https in front.

Options go after `sh -s --`:

```sh
curl -fsSL .../install.sh | sudo sh -s -- --version v0.5.0 --public-url https://kitties.example.com
```

| Option | What it does |
| --- | --- |
| `--version vX.Y.Z` | a release instead of the latest |
| `--listen ADDR` | another address (`0.0.0.0:7488` for the network) |
| `--public-url URL` | where the page is reached from, behind https |
| `--pubkey KEY` | the release signing key: the Ed25519 signature of `SHA256SUMS` must then check, or nothing is installed |
| `--no-service` | the binary only, without user or systemd |
| `--prefix DIR` | another folder for the binary |

Run it again to update: it replaces the binary and the unit, restarts only if something changed,
and keeps `kitties.env` and the token. The service cannot replace its own binary (the unit makes
the system read-only), so updates on a server are this one line, not `updates.gateway: auto`.
Read the script before piping it if you like: [`scripts/install.sh`](../scripts/install.sh).

## Downloading a release

| System | Desktop app | CLI |
| --- | --- | --- |
| Windows | [zip](https://github.com/madkoding/kitties/releases/latest/download/kitties-desktop-windows-amd64.zip) | [x64](https://github.com/madkoding/kitties/releases/latest/download/kitties-windows-amd64.exe) · [arm64](https://github.com/madkoding/kitties/releases/latest/download/kitties-windows-arm64.exe) |
| macOS | [dmg, Apple silicon](https://github.com/madkoding/kitties/releases/latest/download/kitties-desktop-macos-arm64.dmg) | [arm64](https://github.com/madkoding/kitties/releases/latest/download/kitties-darwin-arm64) · [Intel](https://github.com/madkoding/kitties/releases/latest/download/kitties-darwin-amd64) |
| Linux | [zip, x64](https://github.com/madkoding/kitties/releases/latest/download/kitties-desktop-linux-amd64.zip) | [x64](https://github.com/madkoding/kitties/releases/latest/download/kitties-linux-amd64) · [arm64](https://github.com/madkoding/kitties/releases/latest/download/kitties-linux-arm64) · [arm](https://github.com/madkoding/kitties/releases/latest/download/kitties-linux-arm) |
| Android | [apk](https://github.com/madkoding/kitties/releases/latest/download/kitties-android-app.apk) | [Termux, arm64](https://github.com/madkoding/kitties/releases/latest/download/kitties-android-arm64) |

There is no desktop app for Intel Macs, or for Windows and Linux on arm, yet: use the CLI there.

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

## With containers

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

Without a container runtime the kitties still run, brain-only. The gateway keeps looking for
Docker or Podman every half minute, so opening Docker Desktop after Kitties is enough: the
kitties that wanted a desktop get one without a restart. On a Mac, the folders Docker Desktop,
Homebrew, OrbStack, Rancher Desktop and Podman install to are searched even when the app is
opened from the Finder, whose PATH lacks them.

## Keeping Kitties up to date

`updates.gateway` in the configuration (or *Settings*) picks what a gateway does when a newer
release is out: `off`, `notify` (the default: it tells you and you update from Settings) or `auto`
(it downloads the binary for the system, checks it against the release's `SHA256SUMS` and its
Ed25519 signature, runs it once to see it is the version it claims to be, replaces the running one
and restarts when nothing is in use). A version that does not hold is gone back from, and is not
offered again. `updates.window` limits automatic updates to a time of day. Only release builds
run from a writable folder update themselves: a container is updated with its image, and the
desktop app's bundle is signed, so those are told instead. How the releases are signed:
[RELEASING.md](RELEASING.md).

The kitties' desktop image updates on its own too (`containers.auto_update`): a new image is
downloaded and idle desktops are made anew with their sessions kept
([DESKTOP.md](DESKTOP.md#updating-a-desktop-without-losing-what-it-knows)).
