#!/usr/bin/env -S -u BASH_ENV bash
# -S -u BASH_ENV (GNU env >= 8.30) keeps WSL's BASH_ENV hook from printing an nsenter error; `bash smoke.sh` still reads it.
# Smoke test for the playground: runs setup.sh and checks the promised states of the generated repositories.
# Usage: playground/smoke.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
sandbox="$here/sandbox"
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null

fail() { echo "FAIL: $*" >&2; exit 1; }
expect() { # expect <description> <expected> <actual>
  [ "$2" = "$3" ] || fail "$1: expected '$2', got '$3'"
  echo "ok   $1"
}

"$here/setup.sh" >/dev/null

merge_state="$(git -C "$sandbox/repo-merge" status --short | LC_ALL=C sort | tr '\n' ';')"
expect "repo-merge conflicts" "M  config.json;M  notes.txt;UU README.md;UU app.go;UU docs/changelog.md;" "$merge_state"
[ -f "$sandbox/repo-merge/.git/MERGE_HEAD" ] || fail "repo-merge is not in the middle of a merge"
echo "ok   repo-merge is in the middle of a merge"
expect "conflict sections in app.go" "2" "$(grep -c '^<<<<<<<' "$sandbox/repo-merge/app.go")"

changes="$(git -C "$sandbox/repo-changes" diff --name-status HEAD | cut -f1 | cut -c1 | LC_ALL=C sort | uniq -c | tr -s ' ' | tr '\n' ';')"
expect "repo-changes tracked changes" " 1 A; 1 D; 3 M; 1 R;" "$changes"
expect "repo-changes untracked files" "2" "$(git -C "$sandbox/repo-changes" ls-files --others --exclude-standard | wc -l | tr -d ' ')"
expect "repo-changes tags" "v0.1.0;v0.2.0;" "$(git -C "$sandbox/repo-changes" tag | LC_ALL=C sort | tr '\n' ';')"
git -C "$sandbox/repo-changes" rev-parse --verify --quiet feature/login >/dev/null || fail "branch feature/login is missing"
echo "ok   repo-changes has branch feature/login"
for dir in left right; do [ -d "$sandbox/dirs/$dir" ] || fail "sandbox/dirs/$dir is missing"; done
echo "ok   sandbox/dirs has left and right"
echo "playground smoke test passed"
