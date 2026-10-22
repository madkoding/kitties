#!/bin/sh
# Opens a URL in the browser on the virtual display: kitty-open https://example.com
#
# The browser profile lives in the kitty's workspace (a folder of the host, mounted at
# /home/kitty/work), so logins, cookies and settings survive a restart or a re-created
# container. KITTY_PROFILE moves it.
set -eu
: "${DISPLAY:=:99}"
: "${KITTY_PROFILE:=/home/kitty/work/.browser}"
mkdir -p "$KITTY_PROFILE"
chmod 700 "$KITTY_PROFILE" 2>/dev/null || true
# A container that was stopped leaves Chromium's lock behind, pointing at a process that no
# longer exists; Chromium would refuse to start on it. Only remove it when nothing runs.
if ! pgrep -x chromium >/dev/null 2>&1; then
    rm -f "$KITTY_PROFILE/SingletonLock" "$KITTY_PROFILE/SingletonSocket" "$KITTY_PROFILE/SingletonCookie"
fi
nohup chromium --no-sandbox --disable-gpu --disable-dev-shm-usage --no-first-run \
    --window-size=1280,800 --user-data-dir="$KITTY_PROFILE" "$@" >/tmp/chromium.log 2>&1 &
echo "opened $* on $DISPLAY (profile $KITTY_PROFILE)"
