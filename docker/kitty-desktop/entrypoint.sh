#!/bin/sh
# Starts the virtual display and keeps the container alive. Everything else happens through
# `docker exec` from the orchestrator.
set -eu
: "${DISPLAY:=:99}"
: "${SCREEN:=1280x800x24}"
# -noreset: without it the display forgets the resources (xrdb) when its last client leaves.
Xvfb "$DISPLAY" -screen 0 "$SCREEN" -nolisten tcp -ac -noreset -shmem +extension RANDR >/tmp/xvfb.log 2>&1 &
XVFB=$!
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
            VOXL_PASSWORD="$(cat /tmp/voxl.pw)" voxl --addr=0.0.0.0 --port=6901 --framerate="${VOXL_FPS:-30}" --audio-enabled="$audio" \
                --file-transfers=none --title="Kitty desktop" >>/tmp/voxl.log 2>&1
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
dbus-launch --exit-with-session openbox-session >/tmp/lumen.log 2>&1 &
# The programs the kitty installed come back on a container made anew (they are listed in its
# workspace, which outlives the container).
if [ -s "$HOME/work/.desktop/packages" ]; then
    # shellcheck disable=SC2046 # one package name per line, checked by kitty-apt
    nohup sudo -n /usr/local/sbin/kitty-apt $(cat "$HOME/work/.desktop/packages") >/tmp/install.log 2>&1 &
fi
echo "kitty ${KITTY_NAME:-?} ready on display $DISPLAY ($SCREEN)"
if [ "$#" -gt 0 ]; then
    exec "$@"
fi
wait "$XVFB"
