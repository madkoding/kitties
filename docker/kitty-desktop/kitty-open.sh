#!/bin/sh
# Opens a URL in the browser on the virtual display: kitty-open https://example.com
#
# The browser profile is kept where it survives a restart and an update (kitty-profile says
# where: the kitty's own Docker volume, or its workspace), so logins, cookies and settings stay. KITTY_PROFILE moves it.
set -eu
: "${DISPLAY:=:99}"
note() { printf '%s %s\n' "$(date '+%F %T')" "$*" >>"$HOME/.cache/chromium-launch.log" 2>/dev/null || true; }
KITTY_PROFILE="$(kitty-profile)"
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
# The pages in the host's language (KITTY_LANG, which the gateway sets from the host, like the
# clock): the browser asks for them in it (Accept-Language), so Google or a bank sees the usual
# language and takes the kitty for a new device less often. A choice made in the browser's own
# settings is kept: the language is only written when the profile has none, or the one written
# here before.
if [ -n "${KITTY_LANG:-}" ] && ! pgrep -x chromium >/dev/null 2>&1; then
    mkdir -p "$KITTY_PROFILE/Default"
    python3 - "$KITTY_PROFILE" "$KITTY_LANG" <<'PY' 2>/dev/null || note "kitty-open: the browser's language could not be set"
import json, os, sys
profile, tag = sys.argv[1], sys.argv[2]
base = tag.split("-")[0]
langs = [tag] + ([base] if base != tag else []) + (["en-US", "en"] if base != "en" else [])
want = ",".join(langs)
prefs_path = os.path.join(profile, "Default", "Preferences")
mark_path = os.path.join(profile, ".kitty-lang")
try:
    with open(prefs_path) as f:
        prefs = json.load(f)
except (OSError, ValueError):
    prefs = {}
intl = prefs.setdefault("intl", {})
try:
    with open(mark_path) as f:
        mine = f.read().strip()
except OSError:
    mine = ""
now = intl.get("accept_languages", "")
if now and now != mine:
    sys.exit(0)  # chosen in the browser: kept
if now != want:
    intl["accept_languages"] = want
    intl["selected_languages"] = want
    tmp = prefs_path + ".kitty"
    with open(tmp, "w") as f:
        json.dump(prefs, f)
    os.replace(tmp, prefs_path)
with open(mark_path, "w") as f:
    f.write(want + "\n")
PY
fi
# --force-dark-mode: Chromium's own face is dark, and pages that offer a dark look show it.
# --remote-debugging-port: the browser's DevTools on this container's own loopback (nothing
# outside reaches it), which kitty-page uses to read and act on a page from the inside.
# --password-store=basic: no keyring (there is none to unlock here, and one would be lost with the
# container); the profile opens in a container made anew. That store is only obfuscated, which is
# why the browser saves no passwords (chromium-policy.json): they live in the gateway's vault.
# --disable-gpu and --disable-dev-shm-usage come from the wrapper. The sandbox stays on: `chromium` is the wrapper (chromium-wrapper.sh) that only drops it, loudly,
# when the container cannot make user namespaces.
start() {
    nohup chromium --hide-crash-restore-bubble --force-dark-mode --no-first-run \
        --remote-debugging-port=9222 --remote-debugging-address=127.0.0.1 --password-store=basic \
        --window-size=1280,800 --user-data-dir="$KITTY_PROFILE" "$@" >/tmp/chromium.log 2>&1 &
    pid=$!
    # It is up once its DevTools port answers, and it has failed once its process is gone. One
    # that is still there after the wait is slow (a big profile, a busy machine), not broken: 2.
    for _ in $(seq 1 "${KITTY_OPEN_WAIT:-60}"); do
        if curl -fsS -m 1 http://127.0.0.1:9222/json/version >/dev/null 2>&1; then
            return 0
        fi
        if ! kill -0 "$pid" 2>/dev/null && ! pgrep -x chromium >/dev/null 2>&1; then
            return 1
        fi
        sleep 0.5
    done
    pgrep -x chromium >/dev/null 2>&1 && return 2
    return 1
}
# A browser that is slow to come up is left to finish: killing it and putting its profile aside
# (as this did before) logged the kitty out of everything on a slow start.
slow() {
    note "kitty-open: the browser is still starting after the wait; it is left to finish (profile $KITTY_PROFILE)"
    echo "the browser is still starting on $DISPLAY (profile $KITTY_PROFILE); give it a moment" >&2
    exit 0
}

# A profile the kitty cannot write to (a host folder owned by another user) cannot keep a login:
# say so plainly instead of failing later.
if [ ! -w "$KITTY_PROFILE" ]; then
    note "kitty-open: $KITTY_PROFILE is not writable by $(id -un); logins cannot be kept there"
    echo "kitties: the browser's profile ($KITTY_PROFILE) is not writable, so logins cannot be kept: fix its owner on the host" >&2
fi

st=0
start "$@" || st=$?
if [ "$st" -eq 0 ]; then
    echo "opened $* on $DISPLAY (profile $KITTY_PROFILE)"
    exit 0
fi
[ "$st" -eq 2 ] && slow
# Maybe the sandbox: it can look available (user namespaces) and still not come up. Try once
# without it (the wrapper logs that it did), keeping the profile.
pkill -x chromium 2>/dev/null || true
export KITTY_NO_SANDBOX=1
st=0
start "$@" || st=$?
if [ "$st" -eq 0 ]; then
    echo "opened $* on $DISPLAY WITHOUT the browser's sandbox (profile $KITTY_PROFILE); see ~/.cache/chromium-launch.log" >&2
    exit 0
fi
[ "$st" -eq 2 ] && slow
# A profile the browser cannot open (made by a newer version, or damaged) keeps it from starting:
# put it aside (nothing is deleted) and start again with a new one.
if [ -z "$(ls -A "$KITTY_PROFILE" 2>/dev/null)" ]; then
    aside=""
else
    aside="$KITTY_PROFILE.$(date +%Y%m%d%H%M%S).bak"
    echo "the browser did not start; its profile is moved to $aside and a new one is used:" >&2
    tail -n 5 /tmp/chromium.log >&2 || true
    pkill -x chromium 2>/dev/null || true
    note "kitty-open: the browser exited twice on its profile; it is moved to $aside"
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
note "kitty-open: falling back to a temporary profile ($tmp_profile); logins will not survive a restart"
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
