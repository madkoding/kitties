#!/usr/bin/env bash
# Tests check-release-links.sh against a throwaway repository and release folder.
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/check-release-links.sh"
root="$(mktemp -d)"
dist="$(mktemp -d)"
trap 'rm -rf "$root" "$dist"' EXIT

mkdir -p "$root/docs" "$root/site"
echo '[a](https://github.com/o/r/releases/latest/download/app.zip)' >"$root/README.md"
echo '<a href="https://github.com/o/r/releases/latest/download/tool.exe">x</a>' >"$root/site/index.html"
echo 'no links here' >"$root/docs/A.md"

fails=0
expect() { # expect pass|fail description
  local want="$1" what="$2" got=pass
  "$script" "$dist" "$root" >/dev/null 2>&1 || got=fail
  if [ "$got" != "$want" ]; then
    echo "FAIL: $what: got $got, want $want"
    fails=$((fails + 1))
  fi
}

expect fail "empty release"
touch "$dist/app.zip"
expect fail "one asset missing"
touch "$dist/tool.exe"
expect pass "every linked asset present"

empty="$(mktemp -d)"
if "$script" "$dist" "$empty" >/dev/null 2>&1; then
  echo "FAIL: a tree without links passed"
  fails=$((fails + 1))
fi
rmdir "$empty"

[ "$fails" -eq 0 ] && echo "ok"
exit "$fails"
