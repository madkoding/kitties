#!/bin/sh
# Regenerates internal/container/seccomp-kitty.json: Docker's default seccomp profile plus a rule
# that allows clone and unshare without CAP_SYS_ADMIN (user namespaces, for Chromium's sandbox).
set -eu
out="$(dirname "$0")/../internal/container/seccomp-kitty.json"
curl -fsS https://raw.githubusercontent.com/moby/profiles/main/seccomp/default.json |
    jq '.syscalls += [{"names":["clone","unshare"],"action":"SCMP_ACT_ALLOW","comment":"Chromium sandbox: create a user namespace without CAP_SYS_ADMIN (the rest of the default profile is untouched)"}]' >"$out"
echo "wrote $out"
