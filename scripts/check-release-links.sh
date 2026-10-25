#!/usr/bin/env bash
# Checks that every download link in the README, the docs and the landing page
# (releases/latest/download/<asset>) names a file the release actually has.
#
#   scripts/check-release-links.sh DIR [ROOT]
#
# DIR holds the release's files (dist/ in CI); ROOT is the repository (default: this one).
# Exits non-zero, listing the missing names, so a release that lacks an asset is not published
# with a broken link.
set -euo pipefail

dir="${1:?usage: check-release-links.sh DIR [ROOT]}"
root="${2:-$(cd "$(dirname "$0")/.." && pwd)}"

names="$(cd "$root" && grep -rhoE 'releases/latest/download/[A-Za-z0-9._-]+' README.md docs site \
  --include='*.md' --include='*.html' --include='*.js' | sed 's|.*/||' | sort -u)"
[ -n "$names" ] || { echo "::error::no download links found under $root"; exit 1; }

missing=0
for n in $names; do
  if [ ! -f "$dir/$n" ]; then
    echo "::error::linked as releases/latest/download/$n, but the release has no $n"
    missing=$((missing + 1))
  fi
done
[ "$missing" -eq 0 ] || exit 1
echo "all $(echo "$names" | wc -l | tr -d ' ') download links match a release asset"
