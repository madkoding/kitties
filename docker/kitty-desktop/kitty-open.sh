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
    # Nor does it ask to restore the pages of a container that was stopped under it.
    if [ -f "$KITTY_PROFILE/Default/Preferences" ]; then
        sed -i 's/"exit_type":"Crashed"/"exit_type":"Normal"/' "$KITTY_PROFILE/Default/Preferences" 2>/dev/null || true
    fi
fi
# --force-dark-mode: Chromium's own face is dark, and pages that offer a dark look show it.
# --remote-debugging-port: the browser's DevTools on this container's own loopback (nothing
# outside reaches it), which kitty-page uses to read and act on a page from the inside.
# --password-store=basic: saved passwords are sealed with a key every container shares, so a
# profile kept in the workspace opens in a container made anew (no keyring to lose).
# --test-type keeps Chromium from covering the page with a bar about --no-sandbox, which a
# container needs (it has no user namespaces of its own).
nohup chromium --no-sandbox --test-type --hide-crash-restore-bubble --force-dark-mode --disable-gpu --disable-dev-shm-usage --no-first-run \
    --remote-debugging-port=9222 --remote-debugging-address=127.0.0.1 --password-store=basic \
    --window-size=1280,800 --user-data-dir="$KITTY_PROFILE" "$@" >/tmp/chromium.log 2>&1 &
echo "opened $* on $DISPLAY (profile $KITTY_PROFILE)"
