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
#   JD_BIN=<path>        jd is already installed there (pip, jd runs this script itself on its first start): do not download
#                        jd and do not change the PATH, only install the dependencies and configure git
set -eu

repo="Eutima/jelly-dev"
install_dir=${INSTALL_DIR:-$HOME/.local/bin}
failed=""

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

as_root() {
    if [ "$(id -u)" -eq 0 ]; then
        run "$@"
    elif have sudo; then
        run sudo "$@"
    else
        warn "root rights are needed for: $* (install sudo or run the installer as root)"
        return 1
    fi
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

deps_runtimes() {
    ensure_tool go "Go" go || true
    ensure_tool node "Node.js and npm" npm || true
    ensure_tool jdk "Java JDK" javac || true
    ensure_tool python "Python 3 and pip" python3 || true
}

# fetch downloads a URL to a file.
fetch() {
    if have curl; then
        curl -fsSL "$1" -o "$2"
    elif have wget; then
        wget -q "$1" -O "$2"
    else
        return 1
    fi
}

lsp_gopls() {
    if have gopls; then log "ok       gopls (Go)"; return 0; fi
    have go || { problem "gopls needs Go, which is missing"; return 0; }
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
    have npm || { problem "$label needs npm, which is missing"; return 0; }
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

lsp_jdtls() {
    if have jdtls; then log "ok       jdtls (Java)"; return 0; fi
    if [ "$pm" = brew ]; then
        log "install  jdtls (brew)"
        run brew install jdtls || problem "jdtls could not be installed (brew install jdtls)"
        return 0
    fi
    have java || { problem "jdtls (Java language server) needs a JDK, which is missing"; return 0; }
    have python3 || { problem "jdtls needs python3 to start, which is missing"; return 0; }
    target=${XDG_DATA_HOME:-$HOME/.local/share}/jdtls
    log "install  jdtls (download to $target)"
    if [ -n "${JD_DRY_RUN:-}" ]; then
        log "[dry-run] download https://download.eclipse.org/jdtls/snapshots/jdt-language-server-latest.tar.gz to $target and link $install_dir/jdtls"
        return 0
    fi
    archive=$(mktemp)
    if fetch "https://download.eclipse.org/jdtls/snapshots/jdt-language-server-latest.tar.gz" "$archive" &&
        mkdir -p "$target" "$install_dir" && tar -xzf "$archive" -C "$target" && [ -f "$target/bin/jdtls" ]; then
        chmod +x "$target/bin/jdtls"
        ln -sf "$target/bin/jdtls" "$install_dir/jdtls"
    else
        problem "jdtls could not be downloaded from download.eclipse.org/jdtls"
    fi
    rm -f "$archive"
}

lsp_groovy() {
    if have groovy-language-server; then log "ok       groovy-language-server"; return 0; fi
    have java || { problem "groovy-language-server needs a JDK, which is missing"; return 0; }
    jar=${XDG_DATA_HOME:-$HOME/.local/share}/groovy-language-server/groovy-language-server-all.jar
    log "install  groovy-language-server (download to $jar)"
    if [ -n "${JD_DRY_RUN:-}" ]; then
        log "[dry-run] download the groovy-language-server release jar to $jar and write $install_dir/groovy-language-server"
        return 0
    fi
    if mkdir -p "$(dirname "$jar")" "$install_dir" &&
        fetch "https://github.com/GroovyLanguageServer/groovy-language-server/releases/latest/download/groovy-language-server-all.jar" "$jar"; then
        printf '#!/bin/sh\nexec java -jar "%s" "$@"\n' "$jar" >"$install_dir/groovy-language-server"
        chmod +x "$install_dir/groovy-language-server"
    else
        problem "groovy-language-server could not be downloaded from github.com/GroovyLanguageServer/groovy-language-server"
    fi
}

deps_lsp() {
    lsp_gopls
    npm_tool "pyright (Python)" pyright-langserver pyright
    npm_tool "typescript-language-server (JavaScript, TypeScript)" typescript-language-server typescript typescript-language-server
    npm_tool "HTML, CSS and JSON language servers" vscode-html-language-server vscode-langservers-extracted
    npm_tool "Angular language server" ngserver @angular/language-server @angular/language-service typescript
    lsp_jdtls
    lsp_groovy
    npm_tool "prettier (HTML, CSS, JavaScript, TypeScript, JSON formatter)" prettier prettier
    python_tool "black (Python formatter)" black black
}

install_dependencies() {
    detect_pm
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
    else
        log "All dependencies are in place."
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

# add_to_path puts the install directory on the PATH of new shells.
add_to_path() {
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
                printf '\n# added by the jd installer\n%s\n' "$line" >>"$profile"
                echo "Added $install_dir to PATH in $profile"
            fi
            echo "Reload your shell to use jd (and tab completion for it):  exec \"\$SHELL\" -l"
            ;;
    esac
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
        add_to_path
    fi
}

# Tests source this file without running it.
if [ -z "${JD_INSTALL_NO_MAIN:-}" ]; then
    main "$@"
fi
