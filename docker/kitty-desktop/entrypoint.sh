#!/bin/sh
# Starts the virtual display and keeps the container alive. Everything else happens through
# `docker exec` from the orchestrator.
set -eu
# The WhatsApp kitty that works on its own (KITTY_NO_LAN=1) reaches the internet but not the local
# network: the rules go in first, before anything of the kitty runs, and its sudo is cut down.
if [ "${KITTY_NO_LAN:-}" = 1 ]; then
    # The log is the kitty's own (/tmp), written after sudo returns.
    nolan_ok=true
    nolan="$(sudo -n /usr/local/sbin/kitty-nolan 2>&1)" || nolan_ok=false
    printf '%s\n' "$nolan" >/tmp/nolan.log
    "$nolan_ok" || echo "kitty-nolan failed: see /tmp/nolan.log" >&2
fi
# The home may be a folder of the host (it outlives the container): it gets what the image brings.
kitty-home || echo "kitty-home: the home could not be seeded" >&2
# What a start before left in /tmp, and logs that grew too big, go; then the logs are kept small
# while the desktop runs (kitty-tidy).
kitty-tidy --boot
(
    while sleep 900; do
        kitty-tidy
    done
) &
: "${DISPLAY:=:99}"
: "${SCREEN:=1280x800x24}"
# A container stopped hard (the host or Docker shut down, `docker restart`) keeps its /tmp, and
# with it the display's lock: Xvfb reads the PID in it, finds a process of this start with that
# number and refuses ("Server is already active for display 99"), so the desktop never came up
# again. Nothing runs yet at this point, so the display's lock and socket are stale: they go.
display_n="${DISPLAY#:}"
display_n="${display_n%%.*}"
rm -f "/tmp/.X$display_n-lock" "/tmp/.X11-unix/X$display_n" 2>/dev/null || true
# The browser's profile lock of the start before, too (kitty-open does it again before a launch);
# kitty-profile also moves a profile kept in the workspace to where it now lives.
profile="$(kitty-profile 2>/dev/null || echo "$HOME/work/.browser")"
rm -f "$profile/SingletonLock" "$profile/SingletonSocket" "$profile/SingletonCookie" 2>/dev/null || true
# On a stop (SIGTERM from `docker stop`/`restart`, the host shutting down) the browser is closed in
# an orderly way first, so it writes its cookies, sessions and saved passwords (it only flushes
# them every 30 s): without this the whole container was killed at once and a login made just
# before was lost. Docker waits 10 s before it kills; the browser gets up to 8 of them.
stop_desktop() {
    trap - TERM INT
    if pgrep -x chromium >/dev/null 2>&1; then
        kitty-page quit >/dev/null 2>&1 || pkill -TERM -x chromium 2>/dev/null || true
        for _ in $(seq 1 80); do
            pgrep -x chromium >/dev/null 2>&1 || break
            sleep 0.1
        done
    fi
    kill "$XVFB" 2>/dev/null || true
    exit 0
}
# The mark of a desktop that has come up (see kitty-ready): /dev/shm is new on every start, and it
# is removed anyway, so a mark of the start before is never taken for this one's.
READY=/dev/shm/kitty-desktop.ready
rm -f "$READY" 2>/dev/null || true
# -noreset: without it the display forgets the resources (xrdb) when its last client leaves.
Xvfb "$DISPLAY" -screen 0 "$SCREEN" -nolisten tcp -ac -noreset -shmem +extension RANDR >>/tmp/xvfb.log 2>&1 &
XVFB=$!
trap stop_desktop TERM INT
# Wait until the display answers, then hand the process over so signals stop the container.
for _ in $(seq 1 50); do
    if xdpyinfo -display "$DISPLAY" >/dev/null 2>&1; then
        break
    fi
    sleep 0.1
done
# The live view: voxl (the display as H.264 over a WebSocket), on the container's own loopback with
# a password made up for this start; the gateway reaches it with `docker exec ... socat` and
# signs in for the person (no port is published). A desktop of an older image has no voxl and is
# followed as pictures. It starts again if it ends. The viewer, and the sound when the image has
# PulseAudio: a virtual output whose monitor voxl sends as Opus over the same WebSocket (the page
# only plays it after the person's first click, so it starts muted). No files.
# Light on purpose so the sound does not stutter: 30 fps (VOXL_FPS), video quality CRF 30
# (VOXL_CRF) between 0.5 and 4 Mbps (VOXL_MIN_KBPS, VOXL_MAX_KBPS) and Opus at 48 kbps (VOXL_AUDIO_BPS).
audio=false
if command -v pulseaudio >/dev/null 2>&1; then
    mkdir -p /tmp/kitty-pulse
    if pulseaudio -n --daemonize=yes --exit-idle-time=-1 --disallow-exit --log-target=file:/tmp/pulse.log \
        --load="module-native-protocol-unix socket=/tmp/kitty-pulse/native auth-anonymous=1" \
        --load="module-null-sink sink_name=output sink_properties=device.description=Kitty" \
        >/dev/null 2>&1; then
        audio=true
        # the shelf's volume (pactl @DEFAULT_SINK@) and every program use this output
        PULSE_SERVER=unix:/tmp/kitty-pulse/native pactl set-default-sink output >/dev/null 2>&1 || true
    fi
fi
if command -v voxl >/dev/null 2>&1; then
    (
        umask 077
        head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n' >/tmp/voxl.pw
    )
    (
        while :; do
            VOXL_PASSWORD="$(cat /tmp/voxl.pw)" voxl --addr=0.0.0.0 --port=6901 --framerate="${VOXL_FPS:-30}" \
                --crf="${VOXL_CRF:-30}" --max-kbps="${VOXL_MAX_KBPS:-4000}" --min-kbps="${VOXL_MIN_KBPS:-500}" \
                --audio-enabled="$audio" --audio-bitrate="${VOXL_AUDIO_BPS:-48000}" \
                --file-transfers=none --title="Kitty desktop" >>/tmp/voxl.log 2>&1
            sleep 1
        done
    ) &
fi
# The compositor, for the transparencies (menus, translucent windows). Light on purpose: xrender,
# no shadows, fading or blur (see picom.conf), so voxl's frames cost no more than before. A kitty
# whose CPU matters more than its transparencies starts with KITTY_COMPOSITOR=0.
if [ "${KITTY_COMPOSITOR:-1}" != 0 ] && command -v picom >/dev/null 2>&1; then
    (
        while :; do
            picom --config "$HOME/.config/picom/picom.conf" >>/tmp/picom.log 2>&1
            sleep 1
        done
    ) &
fi
# The desktop: Lumen with the shelf (the pinned apps, the open windows and the clock) in the dark
# Numix theme, and the terminal in Kitties' colours. Its autostart brings up PCManFM, which
# draws the wallpaper and the icons on it.
xrdb -nocpp -merge "$HOME/.Xresources" 2>/dev/null || true
# The theme picked for this kitty, kept in its workspace (kitty-theme).
if [ -s "$HOME/work/.desktop/theme" ]; then
    theme="$(head -n 1 "$HOME/work/.desktop/theme" | tr -cd 'A-Za-z0-9@._-')"
    sed -i "s|<name>[A-Za-z0-9@._-]*</name>\(<!--theme-->\)|<name>$theme</name>\1|" "$HOME/.config/lumen/rc.xml"
fi
# The dock's saved pins replace the defaults of rc.xml; an old file may lack the browser, which a
# kitty needs, so it is put back at the front.
pins="$HOME/.config/kitties/pins"
if [ -s "$pins" ] && ! grep -q '|kitty-open|' "$pins"; then
    { echo 'Chromium|web-browser|kitty-open|Chromium'; cat "$pins"; } >"$pins.new" && mv "$pins.new" "$pins"
fi
# Under a session bus: the browser announces what it plays there (MPRIS), and the shelf's player
# island reads it.
dbus-launch --exit-with-session openbox-session >>/tmp/lumen.log 2>&1 &
# The open browser's logins are kept alive: its tabs are visited again every so often
# (KITTY_BROWSER_REFRESH minutes, default 20, 0 off; ~/.config/kitties/browser-refresh wins), without
# reloading what someone is using or typing in (kitty-refresh).
(
    while :; do
        kitty-refresh >>/tmp/kitty-refresh.log 2>&1
        sleep 30
    done
) &
# The programs the kitty installed come back on a container made anew (they are listed in its
# workspace, which outlives the container).
if [ -s "$HOME/work/.desktop/packages" ]; then
    # shellcheck disable=SC2046 # one package name per line, checked by kitty-apt
    nohup sudo -n /usr/local/sbin/kitty-apt $(cat "$HOME/work/.desktop/packages") >/tmp/install.log 2>&1 &
fi
# Ready once the live view answers (any HTTP answer: it listens), at most 30 s after this point;
# without voxl, now. Until then the gateway shows the machine as starting its desktop.
(
    if command -v voxl >/dev/null 2>&1; then
        for _ in $(seq 1 60); do
            code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 1 http://127.0.0.1:6901/healthz || true)"
            [ "$code" != 000 ] && [ -n "$code" ] && break
            sleep 0.5
        done
    fi
    date +%s >"$READY"
) &
echo "kitty ${KITTY_NAME:-?} ready on display $DISPLAY ($SCREEN)"
if [ "$#" -gt 0 ]; then
    exec "$@"
fi
# `wait` returns early when a signal comes (the trap runs then); it is the display's end otherwise.
wait "$XVFB"
