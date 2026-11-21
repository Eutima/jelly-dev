#!/bin/sh
# Install the latest jelly-diff release (Linux and macOS).
#   curl -fsSL https://raw.githubusercontent.com/Eutima/jelly-diff/master/scripts/install.sh | sh
# Environment: JELLY_DIFF_VERSION (e.g. v0.2.0, default latest), INSTALL_DIR (default ~/.local/bin),
# JD_SKIP_GIT_CONFIG=1 (do not set jd as git's diff and merge tool).
set -eu

repo="Eutima/jelly-diff"
install_dir=${INSTALL_DIR:-$HOME/.local/bin}

fail() { echo "error: $*" >&2; exit 1; }

case $(uname -s) in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) fail "unsupported OS $(uname -s); on Windows use install.ps1" ;;
esac
case $(uname -m) in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) fail "unsupported architecture $(uname -m)" ;;
esac

if command -v curl >/dev/null 2>&1; then
    fetch() { curl -fsSL "$1" -o "$2"; }
    latest_url() { curl -fsSIL -o /dev/null -w '%{url_effective}' "$1"; }
elif command -v wget >/dev/null 2>&1; then
    fetch() { wget -q "$1" -O "$2"; }
    latest_url() { wget -q --max-redirect=10 -S --spider "$1" 2>&1 | sed -n 's/^ *Location: //p' | tail -n 1; }
else
    fail "curl or wget is required"
fi

tag=${JELLY_DIFF_VERSION:-}
if [ -z "$tag" ]; then
    tag=$(latest_url "https://github.com/$repo/releases/latest")
    tag=${tag##*/}
fi
case $tag in v[0-9]*.[0-9]*.[0-9]*) ;; *) fail "could not determine the latest release (got '$tag')" ;; esac
version=${tag#v}
case $version in *[!0-9.]*) fail "invalid version '$tag'" ;; esac

archive="jelly-diff_${version}_${os}_${arch}.tar.gz"
base_url="https://github.com/$repo/releases/download/$tag"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading jelly-diff $tag for $os/$arch"
fetch "$base_url/$archive" "$tmp/$archive"
fetch "$base_url/checksums.txt" "$tmp/checksums.txt"

expected=$(awk -v f="$archive" '$2 == f {print $1}' "$tmp/checksums.txt")
[ -n "$expected" ] || fail "no checksum listed for $archive"
if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$tmp/$archive" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
else
    fail "sha256sum or shasum is required to verify the download"
fi
[ "$expected" = "$actual" ] || fail "checksum mismatch for $archive"

tar -xzf "$tmp/$archive" -C "$tmp" jd
mkdir -p "$install_dir"
install -m 0755 "$tmp/jd" "$install_dir/jd"
rm -f "$install_dir/jelly-diff" # the command used to be called jelly-diff

echo "Installed $install_dir/jd"

# Make jd the global default diff and merge tool of git (opt out with JD_SKIP_GIT_CONFIG=1).
if [ -z "${JD_SKIP_GIT_CONFIG:-}" ]; then
    if command -v git >/dev/null 2>&1; then
        "$install_dir/jd" install-git --apply >/dev/null || fail "could not configure git; run: jd install-git --apply"
        echo "Configured git: jd is now the global diff.tool and merge.tool (git difftool / git mergetool)"
    else
        echo "note: git not found; later run: jd install-git --apply"
    fi
fi
case ":$PATH:" in
    *":$install_dir:"*) echo "Run: jd --version" ;;
    *)
        case ${SHELL##*/} in
            zsh) profile=$HOME/.zshrc ;;
            bash) if [ -f "$HOME/.bashrc" ]; then profile=$HOME/.bashrc; else profile=$HOME/.bash_profile; fi ;;
            *) profile=$HOME/.profile ;;
        esac
        line="export PATH=\"$install_dir:\$PATH\""
        if ! grep -qsF "$line" "$profile"; then
            printf '\n# added by the jd installer\n%s\n' "$line" >> "$profile"
            echo "Added $install_dir to PATH in $profile"
        fi
        echo "Reload your shell to use jd (and tab completion for it):  exec \"\$SHELL\" -l"
        ;;
esac
