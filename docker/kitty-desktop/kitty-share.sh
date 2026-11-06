#!/bin/sh
# Shares files with the kitty's person: they are copied to ~/Shared, which the person sees in the
# app (Library > Shared) and can preview and download.
#   kitty-share report.pdf chart.png      copies them into ~/Shared
#   kitty-share -d drafts notes.md        into ~/Shared/drafts
#   kitty-share --list                    what is shared, and how much room is left
set -eu
shared="${KITTY_SHARED:-$HOME/Shared}"
quota_mb="${KITTY_SHARED_QUOTA_MB:-0}"
used() { du -sk "$shared" 2>/dev/null | cut -f1; }
if [ "${1:-}" = "--list" ]; then
    (cd "$shared" && find . -type f ! -path '*/.*' | sed 's,^\./,,' | sort)
    if [ "$quota_mb" -gt 0 ]; then
        echo "used $(($(used) / 1024)) MB of $quota_mb MB"
    fi
    exit 0
fi
sub=""
if [ "${1:-}" = "-d" ]; then
    sub="${2:?usage: kitty-share -d folder file...}"
    shift 2
    case "$sub" in /* | *..* | .*) echo "the folder must be a plain name inside ~/Shared" >&2; exit 2 ;; esac
fi
if [ "$#" -eq 0 ]; then
    echo "usage: kitty-share [-d folder] file..." >&2
    exit 2
fi
dest="$shared${sub:+/$sub}"
mkdir -p "$dest"
for f in "$@"; do
    if [ ! -f "$f" ]; then
        echo "not a file: $f" >&2
        exit 2
    fi
    if [ "$quota_mb" -gt 0 ]; then
        need=$(($(du -sk "$f" | cut -f1) + $(used)))
        if [ "$need" -gt $((quota_mb * 1024)) ]; then
            echo "no room: ~/Shared holds up to $quota_mb MB; delete something there first" >&2
            exit 1
        fi
    fi
    cp -- "$f" "$dest/"
    echo "shared ${sub:+$sub/}$(basename -- "$f")"
done
