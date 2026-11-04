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
# --disable-gpu and --disable-dev-shm-usage come from the wrapper. The sandbox stays on: `chromium` is the wrapper (chromium-wrapper.sh) that only drops it, loudly,
# when the container cannot make user namespaces.
start() {
    nohup chromium --hide-crash-restore-bubble --force-dark-mode --no-first-run \
        --remote-debugging-port=9222 --remote-debugging-address=127.0.0.1 --password-store=basic \
        --window-size=1280,800 --user-data-dir="$KITTY_PROFILE" "$@" >/tmp/chromium.log 2>&1 &
    pid=$!
    # It is up once its DevTools port answers, and it has failed once its process is gone.
    for _ in $(seq 1 60); do
        if curl -fsS -m 1 http://127.0.0.1:9222/json/version >/dev/null 2>&1; then
            return 0
        fi
        if ! kill -0 "$pid" 2>/dev/null && ! pgrep -x chromium >/dev/null 2>&1; then
            return 1
        fi
        sleep 0.5
    done
    return 1
}

if start "$@"; then
    echo "opened $* on $DISPLAY (profile $KITTY_PROFILE)"
    exit 0
fi
# Maybe the sandbox: it can look available (user namespaces) and still not come up. Try once
# without it (the wrapper logs that it did), keeping the profile.
pkill -x chromium 2>/dev/null || true
export KITTY_NO_SANDBOX=1
if start "$@"; then
    echo "opened $* on $DISPLAY WITHOUT the browser's sandbox (profile $KITTY_PROFILE); see ~/.cache/chromium-launch.log" >&2
    exit 0
fi
# A profile the browser cannot open (made by a newer version, or damaged) keeps it from starting:
# put it aside (nothing is deleted) and start again with a new one.
if [ -z "$(ls -A "$KITTY_PROFILE" 2>/dev/null)" ]; then
    aside=""
else
    aside="$KITTY_PROFILE.$(date +%Y%m%d%H%M%S).bak"
    echo "the browser did not start; its profile is moved to $aside and a new one is used:" >&2
    tail -n 5 /tmp/chromium.log >&2 || true
    pkill -x chromium 2>/dev/null || true
    mv "$KITTY_PROFILE" "$aside" && mkdir -p "$KITTY_PROFILE" && chmod 700 "$KITTY_PROFILE"
fi
if [ -n "$aside" ] && start "$@"; then
    echo "opened $* on $DISPLAY (new profile $KITTY_PROFILE; the old one is in $aside)"
    exit 0
fi
# The profile may be what fails (a workspace on a host folder whose file system cannot hold
# Chromium's locks and databases, as on some Windows mounts): last, a profile in /tmp, which is
# lost with the container but opens.
pkill -x chromium 2>/dev/null || true
tmp_profile="/tmp/kitty-browser-profile"
mkdir -p "$tmp_profile" && chmod 700 "$tmp_profile"
KITTY_PROFILE="$tmp_profile"
if start "$@"; then
    echo "opened $* on $DISPLAY with a temporary profile ($tmp_profile): the workspace cannot hold the browser's profile, so logins are lost when the container is" >&2
    exit 0
fi
{
    echo "$(date '+%F %T') kitty-open: the browser did not start; /tmp/chromium.log:"
    tail -n 40 /tmp/chromium.log
} >>"$HOME/.cache/chromium-launch.log" 2>/dev/null || true
echo "the browser did not start on $DISPLAY; its log (/tmp/chromium.log):" >&2
tail -n 20 /tmp/chromium.log >&2 || true
echo "more in ~/.cache/chromium-launch.log" >&2
exit 1
