#!/bin/sh
# `chromium` for the kitty. It adds the flags a container needs, decides once whether the sandbox
# can run, and writes what it did (and what Chromium said when it could not start) to
# ~/.cache/chromium-launch.log, so a browser that does not open leaves something to read:
#   cat ~/.cache/chromium-launch.log
#
# - --disable-gpu: the display is Xvfb, with no GPU and no compositor; Chromium paints in software
#   (a compositor like picom adds nothing to that: it only costs CPU on every frame voxl sends).
# - --disable-dev-shm-usage: Chromium keeps its shared memory in /tmp instead of /dev/shm, which a
#   container may have small.
# - The sandbox stays on whenever a real (headless) run with it works. It needs user namespaces,
#   which the container gets from the seccomp profile kitties passes when it creates it
#   (internal/container). Where that run fails (a container made before that profile, a runtime
#   that forbids them) the browser runs WITHOUT its sandbox, never silently: the log says so.
#   The verdict is kept in /tmp/chromium-sandbox-mode for the life of the container;
#   delete that file to test again.
real=/usr/bin/chromium
log="${KITTY_CHROMIUM_LOG:-$HOME/.cache/chromium-launch.log}"
mode=/tmp/chromium-sandbox-mode
mkdir -p "$(dirname "$log")" 2>/dev/null || true
# Keep the log small: the last 200 lines survive a launch.
if [ -f "$log" ] && [ "$(wc -l <"$log" 2>/dev/null || echo 0)" -gt 400 ]; then
    tail -n 200 "$log" >"$log.new" 2>/dev/null && mv "$log.new" "$log" 2>/dev/null || true
fi
note() { printf '%s %s\n' "$(date '+%F %T')" "$*" >>"$log" 2>/dev/null || true; }

# probe FLAGS: a headless run of the browser on a throwaway profile; its stderr goes to the log.
probe() {
    tmp="$(mktemp -d /tmp/chromium-probe.XXXXXX)" || return 1
    # shellcheck disable=SC2086 # $1 is a few flags
    timeout 40 "$real" $1 --headless --disable-gpu --disable-dev-shm-usage --no-first-run \
        --user-data-dir="$tmp" --dump-dom about:blank >"$tmp/out" 2>"$tmp/err"
    rc=$?
    note "probe [$1] exit $rc"
    [ "$rc" -eq 0 ] || tail -n 15 "$tmp/err" >>"$log" 2>/dev/null || true
    rm -rf "$tmp"
    return "$rc"
}

if [ ! -s "$mode" ]; then
    if probe ""; then
        echo sandbox >"$mode"
    else
        echo "kitties: Chromium cannot start with its sandbox here (see $log); trying it without." >&2
        if probe "--no-sandbox --test-type"; then
            echo nosandbox >"$mode"
        else
            # Nothing starts, not even headless: launch anyway so the error shows where it was asked.
            echo "kitties: Chromium does not start even without its sandbox: read $log" >&2
            echo nosandbox >"$mode"
        fi
    fi
fi

flags="--disable-gpu --disable-dev-shm-usage"
if [ "$(cat "$mode" 2>/dev/null)" = nosandbox ]; then
    echo "kitties: this container cannot make user namespaces, so Chromium runs WITHOUT its sandbox." >&2
    echo "kitties: recreate the kitty (restart it from kitties) to get the sandbox back." >&2
    flags="$flags --no-sandbox --test-type"
fi
note "launch: chromium $flags $*"
# shellcheck disable=SC2086 # $flags is a few flags
exec "$real" $flags "$@"
