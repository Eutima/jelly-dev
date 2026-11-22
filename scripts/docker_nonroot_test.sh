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

# check_install IMAGE NAME [ENV=VALUE]... : the installer script runs as uid 1000 with a writable home and no sudo
install_script=$(cd "$(dirname "$0")" && pwd)/install.sh
check_install() {
    image=$1 name=$2
    shift 2
    # shellcheck disable=SC2016 # the inner shell expands the variables
    if out=$(docker run --rm --user 1000:1000 --tmpfs /tmp --tmpfs /home/u:exec,mode=1777 -e HOME=/home/u -e SHELL=/bin/sh \
        -e JD_BIN=/usr/local/bin/jd -e JD_SKIP_GIT_CONFIG=1 -e JD_DEPS=runtimes,lsp -e JD_LANGS=go "$@" \
        -v "$work/jd:/usr/local/bin/jd:ro" -v "$install_script:/install.sh:ro" "$image" sh -c 'sh /install.sh && sh -lc "go version && gopls version"' 2>&1); then
        echo "ok   $image: $name"
    else
        echo "FAIL $image: $name" >&2
        echo "$out" | sed 's/^/     /' >&2
        failures=$((failures + 1))
    fi
}

# check_plan IMAGE NAME: a dry run as uid 1000 plans downloads into the home folder, never asks for root rights or a password
check_plan() {
    image=$1 name=$2
    if out=$(docker run --rm --user 1000:1000 --tmpfs /tmp --tmpfs /home/u:exec,mode=1777 -e HOME=/home/u -e SHELL=/bin/sh \
        -e JD_BIN=/usr/local/bin/jd -e JD_SKIP_GIT_CONFIG=1 -e JD_DRY_RUN=1 -e JD_LANGS=go,javascript,java \
        -v "$work/jd:/usr/local/bin/jd:ro" -v "$install_script:/install.sh:ro" "$image" sh /install.sh 2>&1) &&
        echo "$out" | grep -q "go.dev/dl" && echo "$out" | grep -q "download https://nodejs.org\|unofficial-builds.nodejs.org" &&
        ! echo "$out" | grep -q "sudo -n\|\[dry-run\] sudo"; then
        echo "ok   $image: $name"
    else
        echo "FAIL $image: $name" >&2
        echo "$out" | sed 's/^/     /' >&2
        failures=$((failures + 1))
    fi
}

for image in $images; do
    check_plan "$image" "the installer plans user-space runtimes without sudo"
    # shellcheck disable=SC2016 # the inner shell expands $(id -u), not this one
    check "$image" "runs as a normal user" sh -c 'test "$(id -u)" != 0'
    check "$image" "jd --version without HOME" sh -c 'unset HOME; jd --version'
    check "$image" "jd --version with a HOME that cannot be written" sh -c 'HOME=/nonexistent jd --version'
    check "$image" "jd install-lsp --list reads without writing" sh -c 'HOME=/nonexistent jd install-lsp --list'
    check "$image" "jd setup --mark-done writes into a config folder in /tmp" sh -c 'JD_CONFIG_DIR=/tmp/jd jd setup --mark-done && test -f /tmp/jd/setup-done'
    check_fails "$image" "jd setup --mark-done reports an unwritable home with an error, not a crash" sh -c 'HOME=/nonexistent jd setup --mark-done'
done
# a real install needs the network and a download tool: Alpine has wget, the others use Python (not in the slim images), so one image
case " $images " in *" alpine:3.20 "*) check_install alpine:3.20 "the installer installs Go and gopls without root and a new login shell finds them" ;; esac

if [ "$failures" -ne 0 ]; then
    echo "$failures container check(s) failed" >&2
    exit 1
fi
echo "all container checks passed"
