#!/bin/sh
# Acceptance tests for next-version.sh. Commits are fed on stdin; the expected values are relative to the latest local tag.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
failures=0
m="@@COMMIT@@"

check() {
    name=$1; expected=$2; input=$3
    actual=$(printf '%s\n' "$input" | sh "$here/next-version.sh" -)
    if [ "$actual" = "$expected" ]; then echo "ok   $name"; else echo "FAIL $name: expected '$expected', got '$actual'"; failures=$((failures + 1)); fi
}

base=$(git describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null || echo v0.0.0)
v=${base#v}; ma=${v%%.*}; r=${v#*.}; mi=${r%%.*}; pa=${r#*.}
minor="v${ma}.$((mi + 1)).0"; patch="v${ma}.${mi}.$((pa + 1))"; major="v$((ma + 1)).0.0"

check "feat bumps minor" "$minor" "$m
feat: add thing"
check "scoped feat bumps minor" "$minor" "$m
feat(ui): add thing"
check "fix bumps patch" "$patch" "$m
fix: repair thing"
check "scoped fix bumps patch" "$patch" "$m
fix(merge): repair thing"
check "perf bumps patch" "$patch" "$m
perf: faster"
check "bang bumps major" "$major" "$m
feat!: drop old api"
check "scoped bang bumps major" "$major" "$m
refactor(api)!: drop old api"
check "squash merge counts the commits listed in its body" "$minor" "$m
Feature/vim ide (#4)
* feat: add thing

- details
* docs: explain"
check "squash merge with only fixes gives patch" "$patch" "$m
Some title (#5)
* fix: repair
* chore: tidy"
check "squash merge without conventional lines gives no release" "" "$m
Some title (#6)
* update things
- feat: a bullet in a description is no commit"
check "squash merge with a breaking line gives major" "$major" "$m
Some title (#7)
* feat(x)!: drop old api"
check "conventional title and a listed fix keep the higher bump" "$minor" "$m
feat: add thing (#8)
* fix: repair"
check "BREAKING CHANGE footer bumps major" "$major" "$m
fix: x

BREAKING CHANGE: y"
check "feat after fix gives minor" "$minor" "$m
fix: a
$m
feat: b"
check "fix after major stays major" "$major" "$m
feat!: a
$m
fix: b"
check "mention of BREAKING CHANGE in docs gives no release" "" "$m
docs: mention BREAKING CHANGE handling"
check "bang later in subject is not breaking" "$patch" "$m
fix: handle foo!: bar"
check "chore and docs give no release" "" "$m
chore: tidy
$m
docs: words"

# Real git path: temporary repository with a tag and one feat commit.
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
(
    cd "$tmp"
    git init -q
    git config user.email t@example.com
    git config user.name t
    git commit -q --allow-empty -m "chore: start"
    git tag v1.2.3
    git commit -q --allow-empty -m "feat: new"
    actual=$(sh "$here/next-version.sh")
    if [ "$actual" = "v1.3.0" ]; then echo "ok   git log path"; else echo "FAIL git log path: got '$actual'"; exit 1; fi
) || failures=$((failures + 1))

[ "$failures" -eq 0 ] || exit 1
