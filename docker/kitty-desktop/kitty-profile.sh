#!/bin/sh
# Prints the folder of the browser's profile (cookies, sessions, local storage),
# ready to use:   kitty-profile
#
# Where it lives, first that applies:
# - KITTY_PROFILE: the gateway sets it to /var/lib/kitty-browser, a Docker volume of the kitty's
#   own that outlives restarts and updates of the desktop, on the runtime's Linux disk.
# - ~/work/.browser, the kitty's workspace, when that folder can hold a profile.
# - /var/lib/kitty-browser in the container, when the workspace cannot: Chromium locks its
#   profile with a symbolic link (SingletonLock), and a folder of a Windows or macOS host mounted by
#   Docker Desktop cannot make one, so Chromium did not start on it and the logins went to a
#   temporary profile that was lost on every restart. This one survives restarts.
#
# A profile that is not in the workspace starts as a copy of the one there, when there is one
# (a profile kept before, or put back by kitty-restore): nothing is logged out by the move. The
# workspace's copy is removed then, so no cookies or passwords stay behind on the host's disk.
set -u
work="$HOME/work/.browser"
local_dir="${KITTY_LOCAL_PROFILE:-/var/lib/kitty-browser}"

# can_hold DIR: DIR can be written to and can hold a symbolic link.
can_hold() {
    mkdir -p "$1" 2>/dev/null || return 1
    probe="$1/.kitty-probe.$$"
    rm -f "$probe" 2>/dev/null
    ln -s kitty-probe "$probe" 2>/dev/null || return 1
    rm -f "$probe" 2>/dev/null
    return 0
}

if [ -n "${KITTY_PROFILE:-}" ]; then
    profile="$KITTY_PROFILE"
elif can_hold "$work"; then
    profile="$work"
else
    profile="$local_dir"
    printf '%s kitty-profile: %s cannot hold the browser profile (no symbolic links: a Windows or macOS folder); using %s\n' \
        "$(date '+%F %T')" "$work" "$profile" >>"$HOME/.cache/chromium-launch.log" 2>/dev/null || true
fi
mkdir -p "$profile" 2>/dev/null || true
chmod 700 "$profile" 2>/dev/null || true

if [ "$profile" != "$work" ] && [ ! -e "$profile/Default" ] && [ -d "$work/Default" ]; then
    if cp -a "$work/." "$profile/" 2>/dev/null; then
        rm -f "$profile/SingletonLock" "$profile/SingletonSocket" "$profile/SingletonCookie" "$profile"/.kitty-probe.* 2>/dev/null
        printf '%s kitty-profile: the profile in %s was copied to %s\n' "$(date '+%F %T')" "$work" "$profile" \
            >>"$HOME/.cache/chromium-launch.log" 2>/dev/null || true
    fi
fi
# A copy in the workspace (a folder of the host) would keep the cookies and saved passwords
# readable there for good: once the profile lives in the kitty's volume (which outlives the
# container), it goes. That is the one copied above, or one an older kitty-restore put there on
# every update. The container's own folder (no volume) is lost with the container, so there the
# workspace's copy stays as the way back.
if [ -n "${KITTY_PROFILE:-}" ] && [ "$profile" != "$work" ] && [ -e "$profile/Default" ] && [ -e "$work" ] && rm -rf "$work" 2>/dev/null; then
    printf '%s kitty-profile: the old copy in %s was removed (the profile is in %s)\n' "$(date '+%F %T')" "$work" "$profile" \
        >>"$HOME/.cache/chromium-launch.log" 2>/dev/null || true
fi
echo "$profile"
