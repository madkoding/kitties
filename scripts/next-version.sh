#!/usr/bin/env bash
# next-version prints the version the semantic commits since the last v* tag call for, or
# nothing when none of them is a release:
#   a breaking change (type!: or a BREAKING CHANGE: footer) bumps the major, the minor while the
#   major is 0; feat bumps the minor; fix and perf bump the patch; the rest (docs, test, ci,
#   chore, style, build, refactor) is no release on its own.
# Usage: scripts/next-version.sh [commit]   (HEAD by default)
set -euo pipefail

head="${1:-HEAD}"
last="$(git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' "$head" 2>/dev/null || true)"
range="$head"
if [ -n "$last" ]; then
  range="$last..$head"
else
  last="v0.0.0"
fi

bump=0 # 0 none, 1 patch, 2 minor, 3 major
while IFS= read -r -d $'\x1e' msg; do
  msg="${msg#$'\n'}"
  subject="${msg%%$'\n'*}"
  level=0
  if printf '%s\n' "$msg" | grep -qE '^BREAKING[ -]CHANGE: ' ||
    printf '%s\n' "$subject" | grep -qE '^[a-z]+(\([^)]*\))?!: '; then
    level=3
  elif printf '%s\n' "$subject" | grep -qE '^feat(\([^)]*\))?: '; then
    level=2
  elif printf '%s\n' "$subject" | grep -qE '^(fix|perf)(\([^)]*\))?: '; then
    level=1
  fi
  if [ "$level" -gt "$bump" ]; then
    bump=$level
  fi
done < <(git log --format='%B%x1e' "$range")

if [ "$bump" -eq 0 ]; then
  exit 0
fi
IFS=. read -r major minor patch <<<"${last#v}"
if [ "$bump" -eq 3 ] && [ "$major" -eq 0 ]; then
  bump=2
fi
case "$bump" in
3) major=$((major + 1)) minor=0 patch=0 ;;
2) minor=$((minor + 1)) patch=0 ;;
1) patch=$((patch + 1)) ;;
esac
echo "v$major.$minor.$patch"
