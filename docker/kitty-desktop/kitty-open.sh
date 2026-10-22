#!/bin/sh
# Opens a URL in the browser on the virtual display: kitty-open https://example.com
set -eu
: "${DISPLAY:=:99}"
nohup chromium --no-sandbox --disable-gpu --disable-dev-shm-usage --no-first-run \
    --window-size=1280,800 --user-data-dir=/tmp/chromium "$@" >/tmp/chromium.log 2>&1 &
echo "opened $* on $DISPLAY"
