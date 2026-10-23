#!/usr/bin/env bash
# Tests next-version.sh in a throwaway repository: each case is commits since a tag, and the
# version they call for.
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/next-version.sh"
fails=0

# check makes a repository tagged $1, adds a commit per message, and expects $2.
check() {
  local tag="$1" want="$2"
  shift 2
  local dir
  dir="$(mktemp -d)"
  (
    cd "$dir"
    git init -q
    git -c user.name=t -c user.email=t@t commit -q --allow-empty -m "chore: start"
    if [ -n "$tag" ]; then
      git tag "$tag"
    fi
    for msg in "$@"; do
      git -c user.name=t -c user.email=t@t commit -q --allow-empty -m "$msg"
    done
    got="$("$script")"
    if [ "$got" != "$want" ]; then
      echo "FAIL: from ${tag:-no tag} with ${*:-no commits}: got '$got', want '$want'"
      exit 1
    fi
  ) || fails=$((fails + 1))
  rm -rf "$dir"
}

check v0.1.11 v0.1.12 "fix(desktop): a close button"
check v0.1.11 v0.1.12 "perf: faster" "docs: words"
check v0.1.11 v0.2.0 "fix: a" "feat(chat): b"
check v0.1.11 v0.2.0 "feat!: breaking while 0.x bumps the minor"
check v0.1.11 v0.2.0 "refactor: x" $'fix: y\n\nBREAKING CHANGE: the default changed'
check v1.4.2 v2.0.0 "feat(api)!: a new shape"
check v1.4.2 v1.5.0 "feat: more"
check v0.1.11 "" "docs: readme" "ci: workflow" "chore: deps" "test: more" "style: fmt"
check v0.1.11 "" "featuring: not a type" "fixes the thing"
check v0.1.11 ""
check "" v0.1.0 "feat: first"
check "" v0.0.1 "fix: first"

if [ "$fails" -gt 0 ]; then
  echo "$fails failed"
  exit 1
fi
echo "ok  next-version"
