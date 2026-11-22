#!/bin/sh
# Install the latest jelly-dev release (Linux and macOS) and the tools the IDE needs.
#   curl -fsSL https://raw.githubusercontent.com/Eutima/jelly-dev/master/scripts/install.sh | sh
# Environment:
#   JELLY_DEV_VERSION   release to install (e.g. v0.2.0, default latest)
#   INSTALL_DIR          where jd goes (default ~/.local/bin)
#   JD_SKIP_GIT_CONFIG=1 do not set jd as git's diff and merge tool
#   JD_SKIP_DEPS=1       do not check or install dependencies
#   JD_DEPS              dependency groups to handle, comma separated (default basics,terminal,runtimes,lsp)
#   JD_DRY_RUN=1         print what would be installed instead of installing it
#   JD_LANGS=<ids>       languages to install language servers and formatters for, comma separated: python html css json yaml docker compose
#                        javascript angular go rust c cpp java, all, or none (default: all; jd asks on its first start). csharp and
#                        powershell are installed only when they are named: all and the default leave them out
#   JD_BIN=<path>        jd is already installed there (pip, jd runs this script itself on its first start): do not download
#                        jd; the dependencies are installed, git is configured and the PATH of new shells gets the tool folders
#   JD_USER_RUNTIMES=1   install Go, Node.js, a JDK and Rust below ~/.local even when system packages could be installed (without
#                        root rights, sudo that needs a password and no Homebrew, this is what happens anyway: the installer
#                        never waits for a password)
set -eu

repo="Eutima/jelly-dev"
[ -n "${HOME:-}" ] || { echo "error: HOME is not set (the installer puts tools and shell settings there)" >&2; exit 1; }
install_dir=${INSTALL_DIR:-$HOME/.local/bin}
original_path=$PATH # what the user's shell has: add_to_path compares with this, not with the PATH the installer extends
failed=""
root_needed="" # system packages that were skipped because there are no root rights

fail() { echo "error: $*" >&2; exit 1; }
log() { printf '%s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
have() { command -v "$1" >/dev/null 2>&1; }
# problem records a dependency that could not be installed; the installer goes on and lists them at the end.
problem() { warn "$*"; failed="$failed
  - $*"; }

# run prints the command and runs it; with JD_DRY_RUN it only prints it.
run() {
    if [ -n "${JD_DRY_RUN:-}" ]; then
        printf '[dry-run] %s\n' "$*"
        return 0
    fi
    "$@"
}

# ---- dependencies --------------------------------------------------------------------------------------------

pm=""
apt_updated=""

detect_pm() {
    for candidate in apt-get dnf pacman zypper apk brew; do
        if have "$candidate"; then
            pm=$candidate
            return 0
        fi
    done
    pm=""
}

is_root() { [ "$(id -u)" -eq 0 ]; }

# can_sudo tells whether sudo works without asking for a password: the installer never waits for one.
can_sudo() { have sudo && sudo -n true >/dev/null 2>&1; }

# can_root tells whether system packages can be installed: as root, with a passwordless sudo, or with Homebrew (a user's tool).
can_root() { [ "$pm" = brew ] || is_root || can_sudo; }

as_root() {
    if is_root; then
        run "$@"
    else
        run sudo -n "$@"
    fi
}

# root_command prints the command an administrator runs to install the system packages that were skipped.
root_command() {
    # shellcheck disable=SC2086 # the list is split into words on purpose
    packages=$(printf '%s\n' $root_needed | sort -u | tr '\n' ' ')
    case $pm in
        apt-get) echo "sudo apt-get update && sudo apt-get install -y $packages" ;;
        dnf) echo "sudo dnf install -y $packages" ;;
        pacman) echo "sudo pacman -S --noconfirm --needed $packages" ;;
        zypper) echo "sudo zypper --non-interactive install $packages" ;;
        apk) echo "apk add $packages (as root)" ;;
        *) echo "install: $packages" ;;
    esac
}

# pm_install installs system packages with the package manager that was found.
pm_install() {
    case $pm in
        apt-get)
            if [ -z "$apt_updated" ]; then
                as_root apt-get update || return 1
                apt_updated=1
            fi
            as_root apt-get install -y "$@" ;;
        dnf) as_root dnf install -y "$@" ;;
        pacman) as_root pacman -S --noconfirm --needed "$@" ;;
        zypper) as_root zypper --non-interactive install "$@" ;;
        apk) as_root apk add "$@" ;;
        brew) run brew install "$@" ;;
        *) return 1 ;;
    esac
}

# pkg_for prints the package names of a tool for the package manager that was found.
pkg_for() {
    case "$1:$pm" in
        git:*) echo git ;;
        clipboard-wayland:*) echo wl-clipboard ;;
        clipboard-x11:*) echo xclip ;;
        terminal:*) echo xterm ;;
        go:apt-get) echo golang-go ;;
        go:dnf) echo golang ;;
        go:*) echo go ;;
        node:apt-get | node:dnf | node:pacman | node:zypper | node:apk) echo nodejs npm ;;
        node:brew) echo node ;;
        python:apt-get) echo python3 python3-pip pipx ;;
        python:dnf | python:zypper) echo python3 python3-pip pipx ;;
        python:pacman) echo python python-pip python-pipx ;;
        python:apk) echo python3 py3-pip pipx ;;
        python:brew) echo python pipx ;;
        clangd:apt-get) echo clangd ;;
        clangd:dnf | clangd:zypper) echo clang-tools-extra ;;
        clangd:pacman) echo clang ;;
        clangd:apk) echo clang-extra-tools ;;
        clangd:brew) echo llvm ;;
        rustup:*) echo rustup ;;
        jdk:apt-get) echo default-jdk-headless ;;
        jdk:dnf | jdk:zypper) echo java-17-openjdk-devel ;;
        jdk:pacman) echo jdk-openjdk ;;
        jdk:apk) echo openjdk17 ;;
        jdk:brew) echo openjdk ;;
        *) return 1 ;;
    esac
}

# ensure_tool installs the packages of a tool unless one of the commands is there already.
#   ensure_tool <tool> <label> <command>...
ensure_tool() {
    tool=$1 label=$2
    shift 2
    for cmd in "$@"; do
        if have "$cmd"; then
            log "ok       $label ($cmd)"
            return 0
        fi
    done
    if [ -z "$pm" ]; then
        problem "$label is missing and no supported package manager was found (apt-get, dnf, pacman, zypper, apk, brew)"
        return 1
    fi
    packages=$(pkg_for "$tool") || { problem "$label is missing and has no package for $pm"; return 1; }
    if ! can_root; then
        log "skip     $label (needs root rights: $packages)"
        root_needed="$root_needed $packages"
        return 1
    fi
    log "install  $label ($packages)"
    # shellcheck disable=SC2086 # the package list is meant to be split into words
    if pm_install $packages; then
        return 0
    fi
    problem "$label could not be installed (tried: $pm install $packages)"
    return 1
}

group_enabled() {
    case ",${JD_DEPS:-basics,terminal,runtimes,lsp}," in *",$1,"*) return 0 ;; esac
    return 1
}

is_wsl() { [ -n "${WSL_DISTRO_NAME:-}" ] || grep -qi microsoft /proc/version 2>/dev/null; }

deps_basics() {
    ensure_tool git "git" git || true
    if [ "$(uname -s)" = "Linux" ] && ! is_wsl; then
        if [ -n "${WAYLAND_DISPLAY:-}" ]; then
            ensure_tool clipboard-wayland "clipboard tool" wl-paste xclip xsel || true
        else
            ensure_tool clipboard-x11 "clipboard tool" wl-paste xclip xsel || true
        fi
    fi
}

# deps_terminal makes sure jd can open new windows.
deps_terminal() {
    if is_wsl; then
        if have wt.exe; then
            log "ok       Windows Terminal (wt.exe)"
        elif have winget.exe; then
            log "install  Windows Terminal (winget)"
            run winget.exe install --id Microsoft.WindowsTerminal -e --accept-package-agreements --accept-source-agreements ||
                problem "Windows Terminal could not be installed; install it from the Microsoft Store (jd opens new windows with it)"
        else
            problem "Windows Terminal (wt.exe) was not found; install it from the Microsoft Store (jd opens new windows with it)"
        fi
        return 0
    fi
    case $(uname -s) in
        Darwin) log "ok       Terminal.app (built in)" ;;
        Linux)
            ensure_tool terminal "terminal emulator" x-terminal-emulator gnome-terminal konsole xfce4-terminal kitty wezterm alacritty foot xterm || true ;;
    esac
}

# lang_enabled tells whether the tools of a language are wanted: JD_LANGS lists the languages, "none" means no language, all
# means every one; without it every language is wanted.
lang_enabled() {
    [ -n "${JD_LANGS:-}" ] || return 0
    case ",$(printf '%s' "$JD_LANGS" | tr ' ;' ',,' | tr '[:upper:]' '[:lower:]')," in *",$1,"* | *",all,"*) return 0 ;; esac
    return 1
}

# lang_explicit tells whether a language is named in JD_LANGS: "all", and no JD_LANGS at all, do not count. It is for the opt-in languages
# (C#, PowerShell), whose servers need a big runtime that most machines do not have.
lang_explicit() {
    case ",$(printf '%s' "${JD_LANGS:-}" | tr ' ;' ',,' | tr '[:upper:]' '[:lower:]')," in *",$1,"*) return 0 ;; esac
    return 1
}

# any_lang tells whether the tools of at least one of the languages are wanted.
any_lang() {
    for lang in "$@"; do
        ! lang_enabled "$lang" || return 0
    done
    return 1
}

# ---- runtimes in the home folder (no root rights) -------------------------------------------------------------

# user_runtimes tells whether Go, Node.js, the JDK and Rust are installed below ~/.local: when there are no root rights to install
# system packages (Homebrew, which works for a normal user, counts as having them), or when JD_USER_RUNTIMES asks for it.
user_runtimes() { [ -n "${JD_USER_RUNTIMES:-}" ] || ! can_root; }

runtimes_dir() { echo "${XDG_DATA_HOME:-$HOME/.local/share}/jd/runtimes"; }

# runtime_platform sets rt_os (linux, darwin), rt_arch (amd64, arm64, or the machine name when there is no build) and rt_libc (gnu, musl).
runtime_platform() {
    case $(uname -s) in
        Darwin) rt_os=darwin ;;
        *) rt_os=linux ;;
    esac
    case $(uname -m) in
        x86_64 | amd64) rt_arch=amd64 ;;
        aarch64 | arm64) rt_arch=arm64 ;;
        *) rt_arch=$(uname -m) ;; # no pinned build: runtime_asset and runtime_rust say so
    esac
    rt_libc=gnu
    if [ "$rt_os" = linux ] && { ls /lib/ld-musl-* >/dev/null 2>&1 || ldd --version 2>&1 | grep -qi musl; }; then
        rt_libc=musl
    fi
}

# The runtimes are pinned like jdtls: raise the version, the file names and the checksums together. The checksums come from
# go.dev/dl, nodejs.org (unofficial-builds.nodejs.org for musl) and api.adoptium.net.
GO_VERSION=1.27.4
NODE_VERSION=v26.13.1
JDK_VERSION=21.0.13.1_2 # the release folder and the file names carry the same number
JDK_URL=https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.13.1%2B2

# runtime_asset sets asset_url and asset_sha for a runtime (go, node, jdk) on rt_os, rt_arch and rt_libc; it fails when there is none.
runtime_asset() {
    asset_url="" asset_sha=""
    case "$1:$rt_os:$rt_arch:$rt_libc" in
        go:linux:amd64:*) asset_sha=7226736c2339719cda28fbf3a54d71aa63bcefe376e98cef00034268dab24840 ;;
        go:linux:arm64:*) asset_sha=3392582988a273a87ab5043ee1b2c9c17a2c3c00580bf9326c47a2e0aaaeac92 ;;
        go:darwin:amd64:*) asset_sha=2b799377704ef12db39bd254995d5c348c9cc8d86562ca817724390e56c38f43 ;;
        go:darwin:arm64:*) asset_sha=91a44a2b8a716600ad94a4f143e156116620db8828086a6e3602291be9bd0bb4 ;;
        node:linux:amd64:gnu) asset_sha=518674de2b621728a5e4486f5fb3b7d2900bb3cc4aa97969edaa463db99f9778 ;;
        node:linux:arm64:gnu) asset_sha=80a330b35415382878df18d41afbd106ea89bfb3be0087d6608e289a14c5cc7d ;;
        node:linux:amd64:musl) asset_sha=04bf9ed3e32ea139c9af52f6dd06879ff6f8880555a0aa1183ef2d156c0e02d3 ;;
        node:linux:arm64:musl) asset_sha=42684e7f1830ef9eaadd040672cba41bbaff9549d834600699f92ee1ee5b1fb1 ;;
        node:darwin:amd64:*) asset_sha=82bb96078a24ca50b9c9a38f2379e6be6d87171fcd400fc14c8934b3bce96e2f ;;
        node:darwin:arm64:*) asset_sha=aec2514ea1ceb2ba52455e9ec65986f6749a06dd759e5db2a72e6889668c9735 ;;
        jdk:linux:amd64:gnu) asset_sha=bb8a93f9ae6e7a693b2b721e85dbeff26ae1a77447985a8d7afcdd9c7cf6222e ;;
        jdk:linux:arm64:gnu) asset_sha=8c72816a6b308842d5ed1f13e5826b69c567da4784f7238d7e800efa71fe54b1 ;;
        jdk:linux:amd64:musl) asset_sha=87af8e1557222db2a29c9d055c34a317916b4ddbdb91474d04b35229d337193e ;;
        jdk:linux:arm64:musl) asset_sha=8e3ef290b2c182fed0f4318dfb928cde1f5b95bf5c28ff4bb236d38b0b177a74 ;;
        jdk:darwin:amd64:*) asset_sha=1b5eff1741bb83d32e2e19aade71452bb42e40f047c01d3e178d438304c9a26d ;;
        jdk:darwin:arm64:*) asset_sha=48067f2b7c958ed104a9d8a5749be7d25220b0a356b4d2d1bcfaab3e16ad0b14 ;;
        *) return 1 ;;
    esac
    case $1 in
        go) asset_url=https://go.dev/dl/go$GO_VERSION.$rt_os-$rt_arch.tar.gz ;;
        node)
            node_arch=x64
            [ "$rt_arch" = arm64 ] && node_arch=arm64
            if [ "$rt_libc" = musl ]; then
                asset_url=https://unofficial-builds.nodejs.org/download/release/$NODE_VERSION/node-$NODE_VERSION-linux-$node_arch-musl.tar.gz
            else
                asset_url=https://nodejs.org/dist/$NODE_VERSION/node-$NODE_VERSION-$rt_os-$node_arch.tar.gz
            fi ;;
        jdk)
            jdk_arch=x64
            [ "$rt_arch" = arm64 ] && jdk_arch=aarch64
            jdk_os=linux
            [ "$rt_os" = darwin ] && jdk_os=mac
            [ "$rt_libc" = musl ] && jdk_os=alpine-linux
            asset_url=$JDK_URL/OpenJDK21U-jdk_${jdk_arch}_${jdk_os}_hotspot_$JDK_VERSION.tar.gz ;;
    esac
}

# planned is the list of runtimes a dry run would have installed, so that the steps after them do not report them as missing.
planned=""
# usable tells whether a command is there (or, in a dry run, would be after the planned installs).
usable() { have "$1" || case " $planned " in *" $1 "*) [ -n "${JD_DRY_RUN:-}" ] ;; *) false ;; esac; }

# link_commands links commands of an unpacked runtime into the install folder.
#   link_commands <bin folder> <command>...
link_commands() {
    folder=$1
    shift
    mkdir_p "$install_dir"
    for cmd in "$@"; do
        run ln -sf "$folder/$cmd" "$install_dir/$cmd"
    done
}

# user_runtime downloads a runtime, checks its SHA-256 and unpacks it below ~/.local/share/jd/runtimes.
#   user_runtime <name> <label> <command> <bin folder in the archive> <command to link>...
user_runtime() {
    name=$1 label=$2 command=$3 bindir=$4
    shift 4
    if have "$command"; then log "ok       $label ($command)"; return 0; fi
    runtime_platform
    if ! runtime_asset "$name"; then
        problem "$label is missing and there is no build of it for $rt_os/$rt_arch ($rt_libc)"
        return 1
    fi
    dest=$(runtimes_dir)/$name
    log "install  $label (download to $dest)"
    if [ -n "${JD_DRY_RUN:-}" ]; then
        log "[dry-run] download $asset_url (sha256 $asset_sha) to $dest and link $* into $install_dir"
        planned="$planned $*"
        return 0
    fi
    archive=$(mktemp)
    if fetch "$asset_url" "$archive" &&
        [ "$(sha256_of "$archive")" = "$asset_sha" ] &&
        rm -rf "$dest.new" && mkdir -p "$dest.new" && tar -xzf "$archive" -C "$dest.new" --strip-components=1 &&
        [ -x "$dest.new/$bindir/$command" ] &&
        rm -rf "$dest" && mv "$dest.new" "$dest"; then
        link_commands "$dest/$bindir" "$@"
    else
        rm -rf "$dest.new"
        problem "$label could not be installed from $asset_url (or its checksum differs from the pinned one)"
        rm -f "$archive"
        return 1
    fi
    rm -f "$archive"
}

runtime_go() { user_runtime go "Go" go bin go gofmt; }
runtime_node() { user_runtime node "Node.js and npm" npm bin node npm npx; }
runtime_jdk() {
    runtime_platform
    jdk_bin=bin
    [ "$rt_os" = darwin ] && jdk_bin=Contents/Home/bin
    user_runtime jdk "Java JDK" javac "$jdk_bin" java javac jar
}

rustup_present() { have rustup || [ -x "$HOME/.cargo/bin/rustup" ]; }

# runtime_rust installs rustup (the Rust toolchain manager, which lives in ~/.cargo and ~/.rustup by design) with its own installer.
# The checksum is the one rustup publishes next to the installer (static.rust-lang.org): an integrity check, not a pin.
runtime_rust() {
    if rustup_present; then log "ok       Rust toolchain (rustup)"; return 0; fi
    runtime_platform
    case "$rt_os:$rt_arch:$rt_libc" in
        linux:amd64:gnu) triple=x86_64-unknown-linux-gnu ;;
        linux:amd64:musl) triple=x86_64-unknown-linux-musl ;;
        linux:arm64:gnu) triple=aarch64-unknown-linux-gnu ;;
        linux:arm64:musl) triple=aarch64-unknown-linux-musl ;;
        darwin:amd64:*) triple=x86_64-apple-darwin ;;
        darwin:arm64:*) triple=aarch64-apple-darwin ;;
        *) problem "Rust (rustup) has no installer for $rt_os/$rt_arch ($rt_libc)"; return 1 ;;
    esac
    url=https://static.rust-lang.org/rustup/dist/$triple/rustup-init
    log "install  Rust toolchain (rustup, download $url)"
    if [ -n "${JD_DRY_RUN:-}" ]; then
        log "[dry-run] download $url, check it against $url.sha256 and run it with -y --profile minimal --no-modify-path"
        planned="$planned rustup"
        return 0
    fi
    # rustup-init decides what it is by the name it is started under ("tmp.Ab12" from mktemp makes it think it is a proxy called tmp), so it
    # is downloaded as "rustup-init" into a folder of its own
    init_dir=$(mktemp -d)
    init=$init_dir/rustup-init
    expected=$init_dir/rustup-init.sha256
    if fetch "$url" "$init" && fetch "$url.sha256" "$expected" &&
        [ "$(sha256_of "$init")" = "$(cut -d' ' -f1 "$expected")" ] &&
        chmod +x "$init" && "$init" -y --profile minimal --no-modify-path; then
        PATH="$HOME/.cargo/bin:$PATH"
    else
        problem "Rust (rustup) could not be installed from $url"
    fi
    rm -rf "$init_dir"
}

deps_runtimes() {
    if user_runtimes; then
        lang_enabled go && { runtime_go || true; }
        # Node.js also runs the Python server (pyright)
        any_lang python html css json yaml docker compose javascript angular && { runtime_node || true; }
        any_lang java && { runtime_jdk || true; }
        lang_enabled rust && { runtime_rust || true; }
    else
        lang_enabled go && { ensure_tool go "Go" go || true; }
        any_lang python html css json yaml docker compose javascript angular && { ensure_tool node "Node.js and npm" npm || true; }
        any_lang java && { ensure_tool jdk "Java JDK" javac || true; }
        lang_enabled rust && { ensure_tool rustup "Rust toolchain (rustup)" rustup || true; }
    fi
    # Python runs black and clang-format and starts the Java server; there is no build of it (or of clangd) that needs no root
    any_lang python java c cpp && { ensure_tool python "Python 3 and pip" python3 || true; }
    any_lang c cpp && { ensure_tool clangd "clangd (C and C++ language server)" clangd || true; }
    return 0
}

# fetch downloads a URL to a file. Python is the last resort: it is there when jd came with pip, and plain Debian and Ubuntu images
# have neither curl nor wget.
fetch() {
    if have curl; then
        curl -fsSL "$1" -o "$2"
    elif have wget; then
        wget -q "$1" -O "$2"
    elif have python3; then
        python3 -c 'import sys, urllib.request; urllib.request.urlretrieve(sys.argv[1], sys.argv[2])' "$1" "$2"
    else
        return 1
    fi
}

lsp_gopls() {
    if have gopls; then log "ok       gopls (Go)"; return 0; fi
    usable go || { problem "gopls needs Go, which is missing"; return 0; }
    log "install  gopls (go install)"
    mkdir_p "$install_dir"
    GOBIN=$install_dir run go install golang.org/x/tools/gopls@latest || problem "gopls could not be installed (go install golang.org/x/tools/gopls@latest)"
}

mkdir_p() { run mkdir -p "$1"; }

# npm_tool installs npm packages below ~/.local so that no root rights are needed.
#   npm_tool <label> <command> <package>...
npm_tool() {
    label=$1 cmd=$2
    shift 2
    if have "$cmd"; then log "ok       $label (npm)"; return 0; fi
    usable npm || { problem "$label needs npm, which is missing"; return 0; }
    log "install  $label (npm)"
    run npm install -g --prefix "$HOME/.local" "$@" || problem "$label could not be installed (npm install -g --prefix ~/.local $*)"
}

# python_tool installs a Python command line tool: with pipx when there is one, else with pip for the user (and, when the system
# Python is managed by the distribution, with --break-system-packages, which only touches ~/.local).
#   python_tool <label> <command> <package>
python_tool() {
    label=$1 cmd=$2 package=$3
    if have "$cmd"; then log "ok       $label (python)"; return 0; fi
    if have pipx; then
        log "install  $label (pipx)"
        run pipx install "$package" || problem "$label could not be installed (pipx install $package)"
    elif have python3; then
        log "install  $label (pip)"
        run python3 -m pip install --user "$package" ||
            run python3 -m pip install --user --break-system-packages "$package" ||
            problem "$label could not be installed (python3 -m pip install --user $package)"
    else
        problem "$label needs Python (pipx or pip), which is missing"
    fi
}

# jdtls is pinned to one milestone; raise the version, file and checksum together (jdtlsVersion in internal/tools/run.go too)
JDTLS_URL=https://download.eclipse.org/jdtls/milestones/1.63.0/jdt-language-server-1.63.0-202610191322.tar.gz
JDTLS_SHA256=b1f2abe0b3cccb462cf64fb4a5587c7d09df60cf8be7748b152cbd89d018bdc4

# sha256_of FILE prints the SHA-256 of a file (sha256sum, or shasum on macOS)
sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1
    else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

lsp_jdtls() {
    if have jdtls; then log "ok       jdtls (Java)"; return 0; fi
    if [ "$pm" = brew ]; then
        log "install  jdtls (brew)"
        run brew install jdtls || problem "jdtls could not be installed (brew install jdtls)"
        return 0
    fi
    usable java || { problem "jdtls (Java language server) needs a JDK, which is missing"; return 0; }
    have python3 || { problem "jdtls needs python3 to start, which is missing"; return 0; }
    target=${XDG_DATA_HOME:-$HOME/.local/share}/jdtls
    log "install  jdtls (download to $target)"
    if [ -n "${JD_DRY_RUN:-}" ]; then
        log "[dry-run] download $JDTLS_URL to $target and link $install_dir/jdtls"
        return 0
    fi
    archive=$(mktemp)
    if fetch "$JDTLS_URL" "$archive" &&
        [ "$(sha256_of "$archive")" = "$JDTLS_SHA256" ] &&
        mkdir -p "$target" "$install_dir" && tar -xzf "$archive" -C "$target" && [ -f "$target/bin/jdtls" ]; then
        chmod +x "$target/bin/jdtls"
        ln -sf "$target/bin/jdtls" "$install_dir/jdtls"
    else
        problem "jdtls could not be downloaded from download.eclipse.org/jdtls (or the checksum of the pinned release 1.63.0 differs)"
    fi
    rm -f "$archive"
}

# lsp_rust adds the language server and the formatter to the Rust toolchain; without a toolchain the stable one is installed.
# rust_component tells whether the toolchain has a component: rustup keeps a proxy for it in ~/.cargo/bin either way.
rust_component() { have rustup && rustup which "$1" >/dev/null 2>&1; }

lsp_rust() {
    if rust_component rust-analyzer && rust_component rustfmt; then log "ok       rust-analyzer and rustfmt (rustup)"; return 0; fi
    usable rustup || { problem "rust-analyzer and rustfmt need rustup, which is missing"; return 0; }
    log "install  rust-analyzer and rustfmt (rustup)"
    run rustup component add rust-analyzer rustfmt ||
        { run rustup default stable && run rustup component add rust-analyzer rustfmt; } ||
        problem "rust-analyzer and rustfmt could not be installed (rustup component add rust-analyzer rustfmt)"
}

# lsp_opt_in lets jd install the language server of an opt-in language (jd install-lsp <language>): C# needs the .NET SDK (dotnet) and
# PowerShell needs PowerShell 7 (pwsh); jd says so when they are missing. Neither is installed by this script.
lsp_opt_in() {
    jd_cmd=${JD_BIN:-$install_dir/jd}
    if [ -n "${JD_DRY_RUN:-}" ]; then
        log "[dry-run] $jd_cmd install-lsp $1"
        return 0
    fi
    [ -x "$jd_cmd" ] || { problem "the $1 language server is installed by jd, which is not at $jd_cmd"; return 0; }
    log "install  $1 language server (jd install-lsp $1)"
    run "$jd_cmd" install-lsp "$1" || problem "the $1 language server could not be installed (jd install-lsp $1)"
}

deps_lsp() {
    lang_enabled go && lsp_gopls
    lang_enabled python && npm_tool "pyright (Python)" pyright-langserver pyright
    any_lang javascript angular && npm_tool "typescript-language-server (JavaScript, TypeScript)" typescript-language-server typescript@5 typescript-language-server
    any_lang html css json && npm_tool "HTML, CSS and JSON language servers" vscode-html-language-server vscode-langservers-extracted
    lang_enabled yaml && npm_tool "YAML language server" yaml-language-server yaml-language-server
    lang_enabled docker && npm_tool "Dockerfile language server" docker-langserver dockerfile-language-server-nodejs
    lang_enabled compose && npm_tool "Docker Compose language server" docker-compose-langserver @microsoft/compose-language-service
    lang_enabled angular && npm_tool "Angular language server" ngserver @angular/language-server @angular/language-service typescript@5
    lang_enabled java && lsp_jdtls
    any_lang html css json yaml javascript angular && npm_tool "prettier (HTML, CSS, JavaScript, TypeScript, JSON formatter)" prettier prettier
    lang_enabled python && python_tool "black (Python formatter)" black black
    any_lang c cpp && python_tool "clang-format (C and C++ formatter)" clang-format clang-format
    lang_enabled rust && lsp_rust
    ! lang_explicit csharp || lsp_opt_in csharp
    ! lang_explicit powershell || lsp_opt_in powershell
    return 0
}

install_dependencies() {
    detect_pm
    PATH="$install_dir:$HOME/.cargo/bin:$PATH" # what the installer puts there is found by the steps that follow
    export PATH
    log ""
    log "Checking the tools the IDE needs (skip with JD_SKIP_DEPS=1, choose groups with JD_DEPS)"
    group_enabled basics && deps_basics
    group_enabled terminal && deps_terminal
    # the language servers and formatters need Go, Node.js, Python and Java: choosing them installs the runtimes as well
    if group_enabled runtimes || group_enabled lsp; then
        deps_runtimes
    fi
    group_enabled lsp && deps_lsp
    if [ -n "$failed" ]; then
        warn "some tools are missing, jd itself works without them:$failed"
    elif [ -z "$root_needed" ]; then
        log "All dependencies are in place."
    fi
    if [ -n "$root_needed" ]; then
        warn "no root rights: these system packages were skipped (jd works without them, the language servers need some):$root_needed"
        warn "an administrator installs them with: $(root_command)"
    fi
}

# ---- jd ------------------------------------------------------------------------------------------------------

install_jd() {
    case $(uname -s) in
        Linux) os=linux ;;
        Darwin) os=darwin ;;
        *) fail "unsupported OS $(uname -s); on Windows use install.ps1" ;;
    esac
    case $(uname -m) in
        x86_64 | amd64) arch=amd64 ;;
        aarch64 | arm64) arch=arm64 ;;
        *) fail "unsupported architecture $(uname -m)" ;;
    esac

    if have curl; then
        download() { curl -fsSL "$1" -o "$2"; }
        latest_url() { curl -fsSIL -o /dev/null -w '%{url_effective}' "$1"; }
    elif have wget; then
        download() { wget -q "$1" -O "$2"; }
        latest_url() { wget -q --max-redirect=10 -S --spider "$1" 2>&1 | sed -n 's/^ *Location: //p' | tail -n 1; }
    else
        fail "curl or wget is required"
    fi

    tag=${JELLY_DEV_VERSION:-}
    if [ -z "$tag" ]; then
        tag=$(latest_url "https://github.com/$repo/releases/latest")
        tag=${tag##*/}
    fi
    case $tag in v[0-9]*.[0-9]*.[0-9]*) ;; *) fail "could not determine the latest release (got '$tag')" ;; esac
    version=${tag#v}
    case $version in *[!0-9.]*) fail "invalid version '$tag'" ;; esac

    archive="jelly-dev_${version}_${os}_${arch}.tar.gz"
    base_url="https://github.com/$repo/releases/download/$tag"

    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT

    echo "Downloading jelly-dev $tag for $os/$arch"
    download "$base_url/$archive" "$tmp/$archive"
    download "$base_url/checksums.txt" "$tmp/checksums.txt"

    expected=$(awk -v f="$archive" '$2 == f {print $1}' "$tmp/checksums.txt")
    [ -n "$expected" ] || fail "no checksum listed for $archive"
    if have sha256sum; then
        actual=$(sha256sum "$tmp/$archive" | awk '{print $1}')
    elif have shasum; then
        actual=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
    else
        fail "sha256sum or shasum is required to verify the download"
    fi
    [ "$expected" = "$actual" ] || fail "checksum mismatch for $archive"

    tar -xzf "$tmp/$archive" -C "$tmp" jd
    mkdir -p "$install_dir"
    install -m 0755 "$tmp/jd" "$install_dir/jd"
    rm -f "$install_dir/jelly-dev" # the command used to be called jelly-dev

    echo "Installed $install_dir/jd"
}

# configure_git makes jd the global default diff and merge tool of git (opt out with JD_SKIP_GIT_CONFIG=1).
configure_git() {
    [ -z "${JD_SKIP_GIT_CONFIG:-}" ] || return 0
    if have git; then
        "${JD_BIN:-$install_dir/jd}" install-git --apply >/dev/null || fail "could not configure git; run: jd install-git --apply"
        echo "Configured git: jd is now the global diff.tool and merge.tool (git difftool / git mergetool)"
    else
        echo "note: git not found; later run: jd install-git --apply"
    fi
}

# profile_files prints the files that set the PATH of the user's new shells, one per line: ~/.profile (login shells, also in
# containers), and the files of bash and zsh, where an interactive shell that is not a login shell does not read ~/.profile.
profile_files() {
    echo "$HOME/.profile"
    case ${SHELL:-} in
        */bash) echo "$HOME/.bashrc" ;;
        */zsh) echo "$HOME/.zshrc" ;;
    esac
    for file in "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.zshrc"; do
        [ ! -f "$file" ] || echo "$file"
    done
}

# add_to_path puts the folders the installer filled on the PATH of new shells: the install folder (jd, the language servers, Go,
# Node.js and the JDK in user space), ~/.cargo/bin, and the folder of jd when pip put it somewhere that is not on the PATH. It
# runs for the pip case too (JD_BIN): there the tools go to ~/.local/bin just the same. Folders the user's shell has already are
# skipped, and a line is never added twice.
add_to_path() {
    set -- "$install_dir"
    [ ! -d "$HOME/.cargo/bin" ] || set -- "$@" "$HOME/.cargo/bin"
    [ -z "${JD_BIN:-}" ] || set -- "$@" "$(dirname "$JD_BIN")"
    missing=""
    for dir in "$@"; do
        case ":$original_path:$missing:" in *":$dir:"*) ;; *) missing="${missing:+$missing:}$dir" ;; esac
    done
    if [ -z "$missing" ]; then
        [ -n "${JD_BIN:-}" ] || echo "Run: jd --version"
        return 0
    fi
    if [ -n "${JD_DRY_RUN:-}" ]; then
        echo "[dry-run] add $missing to the PATH in the shell profiles"
        return 0
    fi
    written=""
    profiles=$(profile_files | sort -u)
    while IFS= read -r profile; do
        old_ifs=$IFS IFS=:
        for dir in $missing; do
            line="export PATH=\"$dir:\$PATH\""
            if ! grep -qsF "$line" "$profile"; then
                printf '\n# added by the jd installer\n%s\n' "$line" >>"$profile"
                case " $written " in *" $profile "*) ;; *) written="$written $profile" ;; esac
            fi
        done
        IFS=$old_ifs
    done <<EOF
$profiles
EOF
    [ -z "$written" ] || echo "Added $missing to PATH in:$written"
    echo "Use it in this shell now (new shells have it):  export PATH=\"$missing:\$PATH\""
}

# mark_setup_done tells jd that this script did the setup, so jd does not run it again on its first start (the pip package
# does that; after this installer there is nothing left to do). A dry run changes nothing and leaves no mark.
mark_setup_done() {
    [ -z "${JD_DRY_RUN:-}" ] || return 0
    "$install_dir/jd" setup --mark-done >/dev/null 2>&1 || true
}

main() {
    if [ -z "${JD_BIN:-}" ]; then
        install_jd
    fi
    if [ -z "${JD_SKIP_DEPS:-}" ]; then
        install_dependencies
    fi
    configure_git # after the dependencies: git may have been installed just now
    if [ -z "${JD_BIN:-}" ]; then
        mark_setup_done
    fi
    add_to_path
}

# Tests source this file without running it.
if [ -z "${JD_INSTALL_NO_MAIN:-}" ]; then
    main "$@"
fi
