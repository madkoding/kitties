#!/bin/sh
# `chromium` for the kitty: the sandbox stays on. It needs user namespaces, which the container
# gets from the seccomp profile kitties passes when it creates it (internal/container).
# A container made before that profile (or run by hand with Docker's default one) forbids them;
# there the browser would not start at all, so it runs without its sandbox and says so — scoped
# to this container, whose only user is the unprivileged `kitty`, and never silently.
real=/usr/bin/chromium
if unshare --user --pid --net --fork true >/dev/null 2>&1; then
    exec "$real" "$@"
fi
echo "kitties: this container cannot make user namespaces, so Chromium runs WITHOUT its sandbox." >&2
echo "kitties: recreate the kitty (restart it from kitties) to get the sandbox back." >&2
exec "$real" --no-sandbox --test-type "$@"
