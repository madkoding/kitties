#!/bin/sh
# Starts the virtual display and keeps the container alive. Everything else happens through
# `docker exec` from the orchestrator.
set -eu
: "${DISPLAY:=:99}"
: "${SCREEN:=1280x800x24}"
Xvfb "$DISPLAY" -screen 0 "$SCREEN" -nolisten tcp -ac +extension RANDR >/tmp/xvfb.log 2>&1 &
XVFB=$!
# Wait until the display answers, then hand the process over so signals stop the container.
for _ in $(seq 1 50); do
    if xdpyinfo -display "$DISPLAY" >/dev/null 2>&1; then
        break
    fi
    sleep 0.1
done
# The desktop: IceWM with its taskbar (Chromium and a terminal on it), on a plain background so
# screenshots read clearly (no icewmbg, whose picture would cover it).
xsetroot -solid '#1e222b'
icewm >/tmp/icewm.log 2>&1 &
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
