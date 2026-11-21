#!/bin/sh
# Print the next release tag (vX.Y.Z) derived from Conventional Commits since the latest v* tag.
# Prints nothing (exit 0) when no commit warrants a release. Counted: the subject of every commit and the "* subject" lines
# of a squash merge body.
# With "-" the commits are read from stdin instead of git, in the format of COMMIT_FORMAT below (used by tests).
set -eu

marker="@@COMMIT@@"
last_tag=$(git describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null || true)
base=${last_tag:-v0.0.0}

if [ "${1:-}" = "-" ]; then
    log=$(cat)
elif [ -n "$last_tag" ]; then
    log=$(git log "${last_tag}..HEAD" --format="${marker}%n%B")
else
    log=$(git log --format="${marker}%n%B")
fi

bump="none"
raise() {
    # Only ever raise the bump level: none < patch < minor < major.
    case "$1:$bump" in
        major:*) bump="major" ;;
        minor:major) ;;
        minor:*) bump="minor" ;;
        patch:none) bump="patch" ;;
    esac
}

# classify raises the bump for one commit subject written as a Conventional Commit; anything else is ignored.
classify() {
    subject=$1
    prefix=${subject%%:*}
    [ "$prefix" != "$subject" ] || return 0
    case $prefix in
        *[!a-z0-9\(\)\!,\ -]*) return 0 ;;
    esac
    case $prefix in
        *"!") raise major ;;
        feat|feat\(*\)) raise minor ;;
        fix|fix\(*\)|perf|perf\(*\)) raise patch ;;
    esac
}

expect_subject=0
while IFS= read -r line; do
    if [ "$line" = "$marker" ]; then
        expect_subject=1
        continue
    fi
    if [ "$expect_subject" -eq 1 ]; then
        [ -n "$line" ] || continue
        expect_subject=0
        classify "$line"
        continue
    fi
    # A squash merge lists the squashed commits as "* subject" lines in the body, and its own title is often no
    # Conventional Commit (for example "Feature/x (#4)"): the listed ones count. (So does a "* feat: ..." bullet in the
    # body of an ordinary commit; write such examples with a dash.)
    case $line in
        "BREAKING CHANGE:"*|"BREAKING-CHANGE:"*) raise major ;;
        "* "*) classify "${line#\* }" ;;
    esac
done <<EOT
$log
EOT

[ "$bump" != none ] || exit 0

version=${base#v}
major=${version%%.*}
rest=${version#*.}
minor=${rest%%.*}
patch=${rest#*.}

case $bump in
    major) major=$((major + 1)); minor=0; patch=0 ;;
    minor) minor=$((minor + 1)); patch=0 ;;
    patch) patch=$((patch + 1)) ;;
esac
echo "v${major}.${minor}.${patch}"
