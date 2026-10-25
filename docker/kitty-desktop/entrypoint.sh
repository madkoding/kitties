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
# The desktop: IceWM with the dark Ceres Pro theme (or the one picked) and its taskbar (Chromium, a terminal and the
# files on it), and the terminal in Kitties' colours.
xrdb -nocpp -merge "$HOME/.Xresources" 2>/dev/null || true
# The theme picked for this kitty, kept in its workspace (kitty-theme).
kitty-theme --boot || true
icewm-session >/tmp/icewm.log 2>&1 &
# The wallpaper and the icons on it (PCManFM draws them over IceWM's background).
pcmanfm --desktop >/tmp/pcmanfm.log 2>&1 &
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
