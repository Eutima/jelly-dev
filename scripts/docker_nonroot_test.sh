#!/bin/sh
# Checks that jd runs as a normal user (not root) in the common container images. It builds a static jd, mounts it into one
# container per image and runs a few commands that need no terminal. Needs a working docker daemon:
#   sh scripts/docker_nonroot_test.sh [image ...]      (default: alpine:3.20 debian:12-slim ubuntu:24.04)
set -eu

images=${*:-alpine:3.20 debian:12-slim ubuntu:24.04}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
chmod 755 "$work" # the container user is not the host user: it must be able to reach the binary
CGO_ENABLED=0 GOOS=linux go build -trimpath -o "$work/jd" ./cmd/jelly-dev
failures=0

# check IMAGE NAME COMMAND...: runs COMMAND as uid 1000 with a read-only root file system
check() {
    image=$1 name=$2
    shift 2
    if out=$(docker run --rm --user 1000:1000 --read-only --tmpfs /tmp -v "$work/jd:/usr/local/bin/jd:ro" "$image" "$@" 2>&1); then
        echo "ok   $image: $name"
    else
        echo "FAIL $image: $name" >&2
        echo "$out" | sed 's/^/     /' >&2
        failures=$((failures + 1))
    fi
}

# check_fails IMAGE NAME COMMAND...: the command must exit non-zero with a message and no Go panic
check_fails() {
    image=$1 name=$2
    shift 2
    if out=$(docker run --rm --user 1000:1000 --read-only --tmpfs /tmp -v "$work/jd:/usr/local/bin/jd:ro" "$image" "$@" 2>&1); then
        echo "FAIL $image: $name (it succeeded)" >&2
        failures=$((failures + 1))
    elif [ -z "$out" ] || echo "$out" | grep -q "^goroutine \|panic:"; then
        echo "FAIL $image: $name" >&2
        echo "$out" | sed 's/^/     /' >&2
        failures=$((failures + 1))
    else
        echo "ok   $image: $name"
    fi
}

for image in $images; do
    # shellcheck disable=SC2016 # the inner shell expands $(id -u), not this one
    check "$image" "runs as a normal user" sh -c 'test "$(id -u)" != 0'
    check "$image" "jd --version without HOME" sh -c 'unset HOME; jd --version'
    check "$image" "jd --version with a HOME that cannot be written" sh -c 'HOME=/nonexistent jd --version'
    check "$image" "jd install-lsp --list reads without writing" sh -c 'HOME=/nonexistent jd install-lsp --list'
    check "$image" "jd setup --mark-done writes into a config folder in /tmp" sh -c 'JD_CONFIG_DIR=/tmp/jd jd setup --mark-done && test -f /tmp/jd/setup-done'
    check_fails "$image" "jd setup --mark-done reports an unwritable home with an error, not a crash" sh -c 'HOME=/nonexistent jd setup --mark-done'
done

if [ "$failures" -ne 0 ]; then
    echo "$failures container check(s) failed" >&2
    exit 1
fi
echo "all container checks passed"
