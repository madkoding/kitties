#!/bin/sh
# Starts the virtual display and keeps the container alive. Everything else happens through
# `docker exec` from the orchestrator.
set -eu
: "${DISPLAY:=:99}"
: "${SCREEN:=1280x800x24}"
# -noreset: without it the display forgets the resources (xrdb) when its last client leaves.
Xvfb "$DISPLAY" -screen 0 "$SCREEN" -nolisten tcp -ac -noreset +extension RANDR >/tmp/xvfb.log 2>&1 &
XVFB=$!
# Wait until the display answers, then hand the process over so signals stop the container.
for _ in $(seq 1 50); do
    if xdpyinfo -display "$DISPLAY" >/dev/null 2>&1; then
        break
    fi
    sleep 0.1
done
# The desktop: Openbox with the shelf (the pinned apps, the open windows and the clock) in the dark
# Numix theme, and the terminal in Kitties' colours. Its autostart brings up PCManFM, which
# draws the wallpaper and the icons on it.
xrdb -nocpp -merge "$HOME/.Xresources" 2>/dev/null || true
# The theme picked for this kitty, kept in its workspace (kitty-theme).
if [ -s "$HOME/work/.desktop/theme" ]; then
    theme="$(head -n 1 "$HOME/work/.desktop/theme" | tr -cd 'A-Za-z0-9@._-')"
    sed -i "s|<name>[A-Za-z0-9@._-]*</name>\(<!--theme-->\)|<name>$theme</name>\1|" "$HOME/.config/openbox/rc.xml"
fi
openbox-session >/tmp/openbox.log 2>&1 &
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
