# The kitty's desktop: what it looks like and what it is made of

Every kitty with a body lives in a container built from [`docker/kitty-desktop`](../docker/kitty-desktop):
Debian, a headless X display (Xvfb, 1280×800) and a small desktop of its own. How the kitty *uses*
it (screenshots, actions, taking control) is in [DESKTOP.md](DESKTOP.md); this page is about the
desktop itself.

The live view is [voxl](https://github.com/tembo/voxl) (MIT, built into the image at a fixed commit
with x264, which makes the image GPL): it starts with the display, on the container's own loopback,
and the gateway reaches it with `docker exec ... socat` (see [DESKTOP.md](DESKTOP.md)).

**Sound.** The image runs PulseAudio with a virtual output; voxl encodes its monitor as Opus (10 ms
frames) and sends it over the same WebSocket as the video. Browsers only play sound after a click,
so the viewer starts muted and plays from the person's first click on it; voxl captures nothing
while nobody listens. A desktop of an older image just has no sound. The tunnel's `socat` has
`nodelay`, so small input events leave at once.

<p align="center">
<img src="screenshots/desktop-menu.png" alt="The kitty's desktop on Lumen: the apps menu open and the shelf at the bottom" width="100%">
</p>

> The pictures on this page were taken in a test environment (Xvfb with the same Lumen fork,
> theme, icons, fonts and configuration as the image, built from `main`), with sample data: the
> file names, the terminal text, the weather and the track in the player are made up.

## The shelf

The window manager is [Lumen Window Manager](https://github.com/madkoding/lumen-wm), a fork of [Openbox](http://openbox.org) (GPL-2.0+) that adds a **shelf**
(off by default upstream; Kitties turns it on in `docker/kitty-desktop/lumen/rc.xml`). The image
always builds the fork's `main`: CI resolves its commit and passes it as `LUMEN_COMMIT` (which is also the cache key, so a new commit rebuilds it). The commit used is in the build log, in `/usr/share/doc/lumen-wm/COMMIT` and in the image's `lumen-wm.commit` label. Local build: `docker build --no-cache-filter lumen docker/kitty-desktop`.

<img src="screenshots/desktop-shelf.png" alt="The shelf: the dock with the pinned apps, the player, the weather and the clock with status glyphs" width="100%">

- **A dock** of pinned apps (Chromium, Files, Terminal, Writer, Calc, Inkscape, Text editor). A click
  starts one or focuses its window, and a mark under the icon shows what is open. Apps can be
  **pinned and unpinned** from the dock and **dragged to reorder**; the choice is kept in
  `~/.config/kitties/pins`, which the backup of a kitty's desktop includes, so it survives an update.
- **Apps menu**: the first button opens the menu below, drawn in the shelf's own dark material with
  rounded corners (`menuRadius`, `menuPadding` in `rc.xml`; the colours are
  `lumen/menu-style.themerc`, added to both themes).
- **Player**: shows what the browser plays (MPRIS on the desktop's session bus) with the cover, title,
  artist and previous / play-pause / next. It hides itself when nothing plays.
- **Weather**: today and the next two days from [Open-Meteo](https://open-meteo.com) (no key). The place
  is `KITTY_WEATHER_PLACE` or the first line of `~/.config/kitties/weather-place`; with neither it is
  found from the host's address or time zone. Fahrenheit with `KITTY_WEATHER_UNITS=f`. It hides
  itself when there is no answer.
- **Clock and status**: the time and date, and the glyphs for network, volume, power and battery,
  each opening its own menu. The fork's own documentation has the details of the shelf:
  [madkoding/lumen-wm](https://github.com/madkoding/lumen-wm).

## Theme, fonts and icons

| | What the image ships |
| --- | --- |
| **Windows** | [Numix](https://github.com/numixproject/numix-gtk-theme) (dark), Openbox theme only, from Debian. Greybird-dark is the alternative: `kitty-theme Greybird-dark`, or pick it in the kitty's drawer; the choice is kept in the workspace. |
| **Programs** | GTK programs, Chromium (its face and the pages that offer a dark look), LibreOffice (on screen; a printed or exported document keeps its own colours) and the terminal are dark too. |
| **Fonts** | Sansation for the windows and the shelf, JetBrains Mono (Nerd Font) for the terminal, the same files as the app's page. |
| **Icons** | [Lucide](https://lucide.dev), drawn at build time as white glyphs with a soft shadow on rounded gradient squares, one colour per group of applications. |
| **Wallpaper** | Kitties' own picture, drawn by PCManFM; the desktop has no icons because the shelf holds the apps. PCManFM is also the file manager. |

The icon pack is generated, not drawn by hand: `docker/kitty-desktop/lucide/build-theme.py` reads
`groups.map` (the colours) and `icons.map` (which Lucide glyph and group each icon name gets) and
writes the SVGs, in a normal and a "small" set that stays legible at 16 px. The Lucide version and
checksum are pinned in the Dockerfile (`LUCIDE_VERSION`, `LUCIDE_SHA256`).

<p align="center">
<img src="screenshots/lucide-pack-dark.png" alt="The Lucide icon pack, by group, on a dark ground" width="70%">
</p>

## What is installed

Chromium, a terminal (xterm), PCManFM, Mousepad, LibreOffice (Writer, Calc, Impress), Inkscape,
ImageMagick and mtPaint, plus `xdotool`, `wmctrl` and `scrot` for the kitty's hands and eyes.
`kitty-install` adds programs from Debian (sudo only runs the installer, by package name) and
remembers them in the workspace, so a container made anew installs them again. Nothing is
published on the host: the orchestrator reaches a kitty with `docker exec`.

## Changing it

- The look is files: `lumen/rc.xml` (the shelf, fonts, theme and menus), `lumen/menu.xml`,
  `lumen/menu-style.themerc`, `lucide/*.map` and `wallpaper/`. `make image` rebuilds the image.
- Credits for everything in it: [README](../README.md#credits).

## Chromium and its sandbox

Chromium builds its sandbox from user namespaces, which Docker's default seccomp profile forbids;
that is why `chromium` used to need `--no-sandbox`. Kitties now creates the container with
[`seccomp-kitty.json`](../internal/container/seccomp-kitty.json): Docker's default profile plus
`clone`/`unshare` without `CAP_SYS_ADMIN` (no privileged mode, no extra capability, and the user is
the unprivileged `kitty`). `chromium` in the image is a wrapper: it adds `--disable-gpu` and
`--disable-dev-shm-usage` (Xvfb has no GPU; Chromium paints in software). It keeps the sandbox when the container
can make user namespaces and otherwise runs without it (`--no-sandbox`), with a warning; recreate
the kitty to get the sandbox back. `kitty-open` also retries without the sandbox when the sandboxed
browser does not come up. Everything it did, and what Chromium said when it failed, is in
`~/.cache/chromium-launch.log` (`kitty-open` adds the browser's own log there, and as a last resort
opens a temporary profile in `/tmp` when the workspace cannot hold one). Refresh the profile with
`scripts/update-seccomp.sh`.

Root has no password. The `kitty` user has passwordless `sudo` (`sudoers.d/kitty`), so it can install
and configure anything in its own computer (`sudo apt install …`, or `kitty-install <package>`). From
the host, `docker exec -u root -it <container> bash` also gives a root shell.

## Compositor (picom)

The desktop runs `picom` (xrender backend, [`picom.conf`](../docker/kitty-desktop/picom.conf)) so
translucent windows, menus and tooltips blend correctly on X11. It is deliberately light: no
shadows, fading or blur, and damage tracking on, so an idle desktop costs nothing and voxl's frames
cost little extra. Start the container with `KITTY_COMPOSITOR=0` to run without it. Its log is
`/tmp/picom.log`.
