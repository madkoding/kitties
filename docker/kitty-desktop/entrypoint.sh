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
echo "kitty ${KITTY_NAME:-?} ready on display $DISPLAY ($SCREEN)"
if [ "$#" -gt 0 ]; then
    exec "$@"
fi
wait "$XVFB"
