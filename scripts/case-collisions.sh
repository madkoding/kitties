#!/usr/bin/env bash
# Fails when two tracked paths are one on a file system that ignores case (macOS, Windows):
# the same path in another case, or two scripts an import names the same way ("./Library" is both
# Library.tsx and library.ts there). Checks git ls-files, or the paths on stdin with "-".
set -euo pipefail
paths() { if [ "${1:-}" = "-" ]; then cat; else git ls-files; fi; }
dup="$(paths "$@" | sed -E 's/\.(ts|tsx|js|jsx|mjs|cjs)$//' | sort -u | sort -f | uniq -Di)"
if [ -n "$dup" ]; then
  echo "::error::paths that are one on macOS and Windows (they differ only in case):"
  echo "$dup"
  exit 1
fi
