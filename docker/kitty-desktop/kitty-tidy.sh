#!/bin/sh
# Keeps the desktop's scratch space from filling the disk: the container's own layer grows with
# what is written outside the folders of the host (~/work, ~/Shared, the home), and it lives as
# long as the container, through every restart.
#
#   kitty-tidy           trims the logs the desktop keeps appending to (/tmp/*.log, ~/.cache/*.log)
#                        to their last KITTY_LOG_KEEP_KB once they pass KITTY_LOG_MAX_KB
#   kitty-tidy --boot    the same, and deletes what a start before left in /tmp untouched for
#                        KITTY_TMP_DAYS days (the display's own files stay)
#
# The entrypoint runs it at the start and then every quarter of an hour.
set -u
tmp="${KITTY_TMP:-/tmp}"
cache="${KITTY_CACHE:-$HOME/.cache}"
max_kb="${KITTY_LOG_MAX_KB:-5120}"
keep_kb="${KITTY_LOG_KEEP_KB:-1024}"
days="${KITTY_TMP_DAYS:-7}"

# A log is trimmed in place, not moved: the program writing it keeps its file open, appending,
# and goes on at the new end.
for f in "$tmp"/*.log "$cache"/*.log; do
    [ -f "$f" ] || continue
    size_kb=$(($(wc -c <"$f" 2>/dev/null || echo 0) / 1024))
    if [ "$size_kb" -gt "$max_kb" ]; then
        tail -c "$((keep_kb * 1024))" "$f" >"$f.tidy" 2>/dev/null && cat "$f.tidy" >"$f" 2>/dev/null
        rm -f "$f.tidy"
    fi
done

if [ "${1:-}" = "--boot" ]; then
    # Only the top level, and never the display's sockets and locks (.X11-unix, .X99-lock, .ICE-unix).
    find "$tmp" -mindepth 1 -maxdepth 1 ! -name '.X*' ! -name '.ICE*' ! -name '.font-unix' \
        -mtime +"$days" -exec rm -rf {} + 2>/dev/null || true
fi
exit 0
