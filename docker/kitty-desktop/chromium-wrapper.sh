#!/bin/sh
# `chromium` for the kitty. It adds the flags a container needs, keeps the sandbox on when the
# container can make user namespaces, and writes what it did to ~/.cache/chromium-launch.log, so a
# browser that does not open leaves something to read:   cat ~/.cache/chromium-launch.log
#
# - --disable-gpu: the display is Xvfb, with no GPU and no compositor; Chromium paints in software
#   (a compositor like picom adds nothing to that: it only costs CPU on every frame voxl sends).
# - --disable-dev-shm-usage: Chromium keeps its shared memory in /tmp instead of /dev/shm, which a
#   container may have small.
# - The sandbox needs user namespaces, which the container gets from the seccomp profile kitties
#   passes when it creates it (internal/container). A container made before that profile (or run
#   by hand with Docker's default one) forbids them; there the browser would not start at all, so
#   it runs without its sandbox, scoped to this container (whose only user is the unprivileged
#   `kitty`), and never silently. kitty-open also sets KITTY_NO_SANDBOX=1 to retry that way when
#   the sandboxed browser did not come up for any other reason.
real=/usr/bin/chromium
log="${KITTY_CHROMIUM_LOG:-$HOME/.cache/chromium-launch.log}"
mkdir -p "$(dirname "$log")" 2>/dev/null || true
# Keep the log small: the last 200 lines survive a launch.
if [ -f "$log" ] && [ "$(wc -l <"$log" 2>/dev/null || echo 0)" -gt 400 ]; then
    tail -n 200 "$log" >"$log.new" 2>/dev/null && mv "$log.new" "$log" 2>/dev/null || true
fi
note() { printf '%s %s\n' "$(date '+%F %T')" "$*" >>"$log" 2>/dev/null || true; }

flags="--disable-gpu --disable-dev-shm-usage"
if [ -n "${KITTY_NO_SANDBOX:-}" ]; then
    note "sandbox off: asked for (the sandboxed launch did not come up)"
    flags="$flags --no-sandbox --test-type"
elif ! unshare --user --pid --net --fork true >/dev/null 2>&1; then
    note "sandbox off: this container cannot make user namespaces"
    echo "kitties: this container cannot make user namespaces, so Chromium runs WITHOUT its sandbox." >&2
    echo "kitties: recreate the kitty (restart it from kitties) to get the sandbox back." >&2
    flags="$flags --no-sandbox --test-type"
fi
note "launch: chromium $flags $*"
# shellcheck disable=SC2086 # $flags is a few flags
exec "$real" $flags "$@"
