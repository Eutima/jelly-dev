#!/bin/sh
# Tests for the dependency part of install.sh. Nothing is installed: the installer runs in dry-run mode with a fake
# machine (which commands exist, which package manager there is) and the printed plan is checked.
set -u
here=$(cd "$(dirname "$0")" && pwd)
failures=0

# plan runs install_dependencies on a fake machine.
#   plan <package manager> <commands that exist, space separated> [JD_DEPS]
# shellcheck disable=SC1091,SC2329,SC2034 # install.sh is sourced; the fakes replace functions of install.sh
plan() {
    (
        JD_INSTALL_NO_MAIN=1 JD_DRY_RUN=1
        [ -n "${3:-}" ] && JD_DEPS=$3
        export JD_INSTALL_NO_MAIN JD_DRY_RUN
        [ -n "${3:-}" ] && export JD_DEPS
        [ -n "${FAKE_LANGS:-}" ] && JD_LANGS=$FAKE_LANGS && export JD_LANGS
        . "$here/install.sh"
        unset WAYLAND_DISPLAY
        [ -n "${FAKE_WAYLAND:-}" ] && WAYLAND_DISPLAY=wayland-0
        fake_pm=$1
        fake_have=" $2 sudo "
        have() { case "$fake_have" in *" $1 "*) return 0 ;; esac; return 1; }
        rust_component() { return 1; } # the components of this machine's toolchain do not count either
        rustup_present() { have rustup; } # not the ~/.cargo/bin of this machine (a CI runner has rustup there)
        detect_pm() { pm=$fake_pm; }
        is_wsl() { return 1; }
        uname() { echo Linux; }
        id() { echo "${FAKE_UID:-1000}"; }
        can_sudo() { [ -z "${FAKE_NO_SUDO:-}" ]; }
        runtime_platform() { rt_os=${FAKE_OS:-linux} rt_arch=${FAKE_ARCH:-amd64} rt_libc=${FAKE_LIBC:-gnu}; }
        install_dependencies 2>&1
    )
}

expect() { # expect <name> <output> <pattern>
    if printf '%s\n' "$2" | grep -qF -- "$3"; then echo "ok   $1"; else echo "FAIL $1: '$3' not in:"; printf '%s\n' "$2"; failures=$((failures + 1)); fi
}
expect_not() { # expect_not <name> <output> <pattern>
    if printf '%s\n' "$2" | grep -qF -- "$3"; then echo "FAIL $1: '$3' found in:"; printf '%s\n' "$2"; failures=$((failures + 1)); else echo "ok   $1"; fi
}

out=$(plan apt-get "")
expect "apt updates once" "$out" "[dry-run] sudo -n apt-get update"
expect "apt installs git" "$out" "[dry-run] sudo -n apt-get install -y git"
expect "apt installs a clipboard tool" "$out" "install -y xclip"
expect "apt installs a terminal" "$out" "install -y xterm"
expect "apt installs go" "$out" "install -y golang-go"
expect "apt installs node and npm" "$out" "install -y nodejs npm"
expect "apt installs a jdk" "$out" "install -y default-jdk-headless"
expect "runtime missing means no language server install" "$out" "gopls needs Go, which is missing"
updates=$(printf '%s\n' "$out" | grep -c "apt-get update")
if [ "$updates" -eq 1 ]; then echo "ok   apt-get update runs once"; else echo "FAIL apt-get update ran $updates times"; failures=$((failures + 1)); fi

out=$(FAKE_WAYLAND=1 plan apt-get "git xterm go npm java")
expect "wayland gets wl-clipboard" "$out" "install -y wl-clipboard"

out=$(plan dnf "git xterm go npm javac java python3 pyright-langserver typescript-language-server jdtls gopls xclip vscode-html-language-server ngserver prettier black yaml-language-server docker-langserver docker-compose-langserver clangd clang-format rustup rust-analyzer rustfmt")
expect "everything present is reported" "$out" "All dependencies are in place."
expect_not "nothing is installed when everything is there" "$out" "[dry-run] sudo"

out=$(plan pacman "git xclip xterm" "runtimes")
expect "pacman go package" "$out" "pacman -S --noconfirm --needed go"
expect "pacman jdk package" "$out" "pacman -S --noconfirm --needed jdk-openjdk"
expect_not "only the chosen group runs" "$out" "gopls"
expect_not "other groups are skipped" "$out" "xterm"

out=$(plan apt-get "git xclip xterm go npm java python3" "lsp")
expect "gopls via go install" "$out" "[dry-run] go install golang.org/x/tools/gopls@latest"
expect "pyright via npm in the home prefix" "$out" "npm install -g --prefix $HOME/.local pyright"
expect "typescript server via npm" "$out" "typescript@5 typescript-language-server"
expect "angular server via npm" "$out" "npm install -g --prefix $HOME/.local @angular/language-server @angular/language-service typescript"
expect "html server via npm" "$out" "npm install -g --prefix $HOME/.local vscode-langservers-extracted"
expect "prettier via npm" "$out" "npm install -g --prefix $HOME/.local prettier"
expect "black via pip" "$out" "python3 -m pip install --user black"
expect "jdtls download is planned" "$out" "download https://download.eclipse.org/jdtls"
expect "jdtls is a pinned milestone" "$out" "jdtls/milestones/1.63.0/"
expect_not "jdtls is never a moving snapshot" "$out" "snapshots"

out=$(plan apt-get "" "lsp")
expect "the lsp group installs the runtimes it needs" "$out" "install -y golang-go"
expect "the lsp group installs node" "$out" "install -y nodejs npm"
expect "the lsp group installs python" "$out" "install -y python3 python3-pip pipx"
expect "the lsp group installs a jdk" "$out" "install -y default-jdk-headless"
expect_not "the lsp group alone does not install git" "$out" "install -y git"

out=$(plan apt-get "git xclip xterm go npm java python3 pipx" "lsp")
expect "black via pipx when there is pipx" "$out" "pipx install black"

out=$(plan apt-get "git xclip xterm go npm java" "lsp")
expect "no python means no black" "$out" "black (Python formatter) needs Python"

out=$(plan "" "")
expect "no package manager is reported, not fatal" "$out" "no supported package manager was found"
expect "problems are summed up" "$out" "some tools are missing, jd itself works without them"

# JD_LANGS limits the tools to the chosen languages: servers, formatters and the runtimes they need.
out=$(FAKE_LANGS="python,html" plan apt-get "git xclip xterm" "runtimes,lsp")
expect "python needs node and python" "$out" "install -y nodejs npm"
expect "python needs python" "$out" "python3"
expect_not "python and html need no go" "$out" "golang"
expect_not "python and html need no jdk" "$out" "jdk"
out=$(FAKE_LANGS="python,html" plan apt-get "git xclip xterm go npm java python3 pipx" "lsp")
expect "pyright for python" "$out" "pyright"
expect "black for python" "$out" "black"
expect "the html server" "$out" "vscode-langservers-extracted"
expect "prettier formats html" "$out" "prettier"
expect_not "no gopls without go" "$out" "gopls"
expect_not "no angular server" "$out" "@angular/language-server"
expect_not "no java server" "$out" "jdtls"
expect_not "no typescript server" "$out" "typescript-language-server"
out=$(FAKE_LANGS="python html;css" plan apt-get "git xclip xterm go npm java python3 pipx" "lsp")
expect "spaces and semicolons separate languages: html" "$out" "vscode-langservers-extracted"
expect "spaces and semicolons separate languages: python" "$out" "pyright"
out=$(FAKE_LANGS="yaml,docker,compose" plan apt-get "git xclip xterm go npm java python3 pipx" "lsp")
expect "the YAML server" "$out" "yaml-language-server"
expect "the Dockerfile server" "$out" "dockerfile-language-server-nodejs"
expect "the Docker Compose server" "$out" "@microsoft/compose-language-service"
expect "prettier formats YAML" "$out" "prettier"
expect_not "yaml, docker and compose need no python tool" "$out" "black"
expect_not "yaml, docker and compose need no pyright" "$out" "pyright"
out=$(FAKE_LANGS="docker" plan apt-get "git xclip xterm" "runtimes,lsp")
expect "docker needs node" "$out" "install -y nodejs npm"
expect_not "docker alone needs no prettier" "$out" "prettier"
out=$(FAKE_LANGS="c,cpp" plan apt-get "git xclip xterm python3 pipx" "runtimes,lsp")
expect "C and C++ get clangd from the package manager" "$out" "install -y clangd"
expect "C and C++ get clang-format with pipx" "$out" "pipx install clang-format"
expect_not "C and C++ need no Rust toolchain" "$out" "rustup"
expect_not "C and C++ need no npm tool" "$out" "npm install"
out=$(FAKE_LANGS="rust" plan apt-get "git xclip xterm" "runtimes,lsp")
expect "Rust gets rustup from the package manager" "$out" "install -y rustup"
expect_not "Rust needs no clangd" "$out" "clangd"
out=$(FAKE_LANGS="rust" plan apt-get "git xclip xterm rustup" "lsp")
expect "Rust adds the server and the formatter to the toolchain" "$out" "rustup component add rust-analyzer rustfmt"
out=$(FAKE_LANGS="go" plan apt-get "git xclip xterm go" "lsp")
expect "go gets gopls" "$out" "gopls"
expect_not "go needs no npm tool" "$out" "npm install"
expect_not "go needs no black" "$out" "black"
out=$(FAKE_LANGS="none" plan apt-get "git xclip xterm" "runtimes,lsp")
expect_not "no language installs no runtime" "$out" "install -y"
expect_not "no language installs no server" "$out" "pyright"
out=$(FAKE_LANGS="ALL" plan apt-get "git xclip xterm go npm java python3 pipx" "lsp")
expect "all means all (case ignored): jdtls" "$out" "jdtls"
expect "all means all: gopls" "$out" "gopls"
out=$(FAKE_LANGS="java" plan apt-get "git xclip xterm java python3" "lsp")
expect "java gets its server" "$out" "jdtls"
expect_not "java needs no node tool" "$out" "npm install"

# main with JD_BIN (the pip package runs the script on jd's first start): no download of jd, git is configured with the jd that
# is installed, and the PATH of new shells still gets the tool folders. The steps are replaced by fakes that record the calls.
# shellcheck disable=SC1091,SC2329,SC2034 # install.sh is sourced; the fakes replace functions of install.sh
main_calls() {
    (
        JD_INSTALL_NO_MAIN=1
        export JD_INSTALL_NO_MAIN
        . "$here/install.sh"
        calls=""
        install_jd() { calls="$calls install_jd"; }
        install_dependencies() { calls="$calls deps"; }
        configure_git() { calls="$calls git:${JD_BIN:-default}"; }
        add_to_path() { calls="$calls path"; }
        mark_setup_done() { calls="$calls mark"; }
        main
        echo "$calls"
    )
}
out=$(main_calls)
expect "the installer downloads jd, marks the setup done and sets the PATH" "$out" "install_jd deps git:default mark path"
out=$(JD_BIN=/opt/jd main_calls)
expect "with JD_BIN the dependencies and git are handled" "$out" "deps git:/opt/jd"
expect_not "with JD_BIN jd is not downloaded" "$out" "install_jd"
expect "with JD_BIN the tool folders still go on the PATH" "$out" "deps git:/opt/jd path"
expect_not "with JD_BIN jd marks the setup itself" "$out" "mark"

# mark_setup_done calls the installed jd, leaves nothing on a dry run and never fails the installation.
# shellcheck disable=SC1091,SC2329,SC2034 # install.sh is sourced
mark_run() { # mark_run <jd script body> [JD_DRY_RUN]
    (
        JD_INSTALL_NO_MAIN=1
        export JD_INSTALL_NO_MAIN
        [ -n "${2:-}" ] && JD_DRY_RUN=$2 && export JD_DRY_RUN
        . "$here/install.sh"
        install_dir=$(mktemp -d)
        printf '#!/bin/sh\n%s\n' "$1" >"$install_dir/jd"
        chmod 755 "$install_dir/jd"
        mark_setup_done
        echo "returned $?"
    )
}
log_file=$(mktemp)
out=$(mark_run "echo \"\$@\" > '$log_file'")
expect "the installer asks jd to record the setup" "$(cat "$log_file")" "setup --mark-done"
expect "marking succeeds" "$out" "returned 0"
: >"$log_file"
mark_run "echo \"\$@\" > '$log_file'" 1 >/dev/null
if [ -s "$log_file" ]; then echo "FAIL a dry run must not mark: $(cat "$log_file")"; failures=$((failures + 1)); else echo "ok   a dry run leaves no mark"; fi
out=$(mark_run "exit 3")
expect "a failing jd does not fail the installation" "$out" "returned 0"
rm -f "$log_file"

# ---- no root rights: the runtimes go below ~/.local, nothing asks for a password ------------------------------------------------
lsp_langs="go,javascript,java,rust,c"
out=$(FAKE_NO_SUDO=1 FAKE_LANGS=$lsp_langs plan apt-get "git xclip xterm python3 pipx" "basics,runtimes,lsp")
expect "no root: Go is downloaded" "$out" "download https://go.dev/dl/go1.27.4.linux-amd64.tar.gz"
expect "no root: Node.js is downloaded" "$out" "download https://nodejs.org/dist/v26.13.1/node-v26.13.1-linux-x64.tar.gz"
expect "no root: the JDK is downloaded" "$out" "OpenJDK21U-jdk_x64_linux_hotspot"
expect "no root: rustup is downloaded" "$out" "download https://static.rust-lang.org/rustup/dist/x86_64-unknown-linux-gnu/rustup-init"
expect "no root: the runtimes go below the home folder" "$out" "$HOME/.local/share/jd/runtimes/go"
expect "no root: the tools that follow find the runtimes" "$out" "go install golang.org/x/tools/gopls@latest"
expect "no root: npm tools are installed" "$out" "npm install -g --prefix $HOME/.local typescript@5 typescript-language-server"
expect "no root: jdtls is planned" "$out" "download https://download.eclipse.org/jdtls"
expect "no root: the skipped system packages are listed" "$out" "needs root rights: clangd"
expect "no root: the administrator gets the command" "$out" "an administrator installs them with: sudo apt-get update && sudo apt-get install -y"
expect_not "no root: sudo is never run" "$out" "[dry-run] sudo"
expect_not "no root: the package manager is never run" "$out" "[dry-run] apt-get"
expect_not "no root: no problem is reported for the runtimes" "$out" "is missing"

out=$(FAKE_UID=0 FAKE_NO_SUDO=1 FAKE_LANGS="go,javascript" plan apt-get "git xclip xterm" "runtimes")
expect "root installs Go with the package manager" "$out" "[dry-run] apt-get install -y golang-go"
expect_not "root downloads no Go" "$out" "go.dev/dl"
out=$(FAKE_UID=0 JD_USER_RUNTIMES=1 FAKE_LANGS="go" plan apt-get "git xclip xterm" "runtimes")
expect "JD_USER_RUNTIMES makes root download Go too" "$out" "download https://go.dev/dl/go1.27.4.linux-amd64.tar.gz"
out=$(FAKE_LANGS="go" plan apt-get "git xclip xterm" "runtimes")
expect "with passwordless sudo the package manager is used" "$out" "[dry-run] sudo -n apt-get install -y golang-go"
out=$(FAKE_NO_SUDO=1 FAKE_LANGS="go" plan brew "git" "runtimes")
expect "Homebrew is a user's tool: no download" "$out" "[dry-run] brew install go"
expect_not "Homebrew: nothing is skipped for root" "$out" "needs root rights"
out=$(FAKE_NO_SUDO=1 FAKE_LANGS="javascript,java" FAKE_LIBC=musl FAKE_ARCH=arm64 plan apk "git" "runtimes")
expect "musl: Node.js comes from the musl builds" "$out" "unofficial-builds.nodejs.org/download/release/v26.13.1/node-v26.13.1-linux-arm64-musl.tar.gz"
expect "musl: the JDK is the Alpine build" "$out" "OpenJDK21U-jdk_aarch64_alpine-linux_hotspot"
out=$(FAKE_NO_SUDO=1 FAKE_LANGS="go,java" FAKE_OS=darwin FAKE_ARCH=arm64 plan apt-get "git" "runtimes")
expect "macOS: the Go archive" "$out" "go1.27.4.darwin-arm64.tar.gz"
expect "macOS: the JDK archive" "$out" "OpenJDK21U-jdk_aarch64_mac_hotspot"

# install.ps1 is embedded in jd and also run by Windows PowerShell 5 from a file without a byte order mark: it must stay plain ASCII
if LC_ALL=C grep -q '[^ -~	]' "$here/install.ps1"; then
    echo "FAIL install.ps1 has characters that are not ASCII:"; LC_ALL=C grep -n '[^ -~	]' "$here/install.ps1" | head -3
    failures=$((failures + 1))
else
    echo "ok   install.ps1 is plain ASCII"
fi

# what jd runs when a language server needs Node.js and it is missing (JD_DEPS=runtimes JD_LANGS=javascript): Node.js and nothing else
out=$(FAKE_NO_SUDO=1 FAKE_LANGS=javascript plan apt-get "git" "runtimes")
expect "on demand: Node.js is downloaded" "$out" "download https://nodejs.org/dist/v26.13.1/node-v26.13.1-linux-x64.tar.gz"
expect_not "on demand: no Go" "$out" "go1.27.4"
expect_not "on demand: no JDK" "$out" "OpenJDK21U"
expect_not "on demand: no rustup" "$out" "rustup"
expect_not "on demand: no language server is installed here" "$out" "npm install -g"

# a machine without a pinned build is told so, nothing is downloaded
out=$(FAKE_NO_SUDO=1 FAKE_LANGS="go,rust" FAKE_ARCH=armv7l plan apt-get "git xclip xterm" "runtimes")
expect "an unknown CPU gets no build" "$out" "there is no build of it for linux/armv7l"
expect "an unknown CPU gets no rustup" "$out" "no installer for linux/armv7l"
expect_not "an unknown CPU downloads nothing" "$out" "download https"

# every pinned runtime has a well formed checksum and a URL for its platform
# shellcheck disable=SC1091,SC2329,SC2034,SC2154
pinned() {
    (
        JD_INSTALL_NO_MAIN=1
        export JD_INSTALL_NO_MAIN
        . "$here/install.sh"
        for name in go node jdk; do
            for combo in linux:amd64:gnu linux:arm64:gnu linux:amd64:musl linux:arm64:musl darwin:amd64:gnu darwin:arm64:gnu; do
                rt_os=${combo%%:*} rest=${combo#*:}
                rt_arch=${rest%%:*} rt_libc=${rest#*:}
                [ "$name" = go ] && [ "$rt_libc" = musl ] && continue # Go's archive is static: the same for both
                [ "$rt_os" = darwin ] && [ "$rt_libc" = musl ] && continue
                if ! runtime_asset "$name"; then echo "NOPIN $name $combo"; continue; fi
                case $asset_sha in *[!0-9a-f]* | "") echo "BADSHA $name $combo" ;; esac
                [ ${#asset_sha} -eq 64 ] || echo "BADSHA $name $combo"
                case $asset_url in https://*) ;; *) echo "BADURL $name $combo" ;; esac
            done
        done
        echo "done"
    )
}
out=$(pinned)
expect "every runtime is pinned for every platform" "$out" "done"
expect_not "no pin is missing" "$out" "NOPIN"
expect_not "every pinned checksum is 64 hex digits" "$out" "BADSHA"
expect_not "every pinned URL is https" "$out" "BADURL"

# HOME unset: a clear error instead of an unbound variable
out=$(env -u HOME -u INSTALL_DIR JD_INSTALL_NO_MAIN=1 sh -c ". '$here/install.sh'" 2>&1 || true)
expect "HOME unset is reported" "$out" "HOME is not set"

# add_to_path (also for the pip case): folders the shell lacks are added to the profiles, once, and the export line is printed
# shellcheck disable=SC1091,SC2329,SC2034,SC2154
path_run() { # path_run <PATH> [JD_BIN]
    (
        tmp=$(mktemp -d)
        HOME=$tmp SHELL=/bin/bash PATH=$1
        [ -n "${2:-}" ] && JD_BIN=$2 && export JD_BIN
        export HOME SHELL
        JD_INSTALL_NO_MAIN=1
        export JD_INSTALL_NO_MAIN
        . "$here/install.sh"
        add_to_path
        add_to_path
        echo "profile: $(cat "$HOME/.profile" 2>/dev/null)"
        echo "bashrc-lines: $(grep -c "added by the jd installer" "$HOME/.bashrc" 2>/dev/null)"
        echo "count: $(grep -c "export PATH" "$HOME/.profile")"
        rm -rf "$tmp"
    )
}
out=$(path_run "/usr/bin:/bin" /usr/local/bin/jd)
expect "the profile gets the install folder" "$out" ".local/bin:\$PATH\""
expect "bash gets it in .bashrc too" "$out" "bashrc-lines: 2"
expect "the pip case adds the folder of jd when it is not on the PATH" "$out" "/usr/local/bin:\$PATH\""
expect "the export line is printed for the current shell" "$out" "Use it in this shell now"
out=$(path_run "/usr/bin:/bin" | grep "^count:")
expect "a second run adds nothing twice" "$out" "count: 1"
out=$(tmp=$(mktemp -d); HOME=$tmp PATH="$tmp/.local/bin:/usr/bin" JD_INSTALL_NO_MAIN=1 JD_BIN=$tmp/.local/bin/jd sh -c ". '$here/install.sh'; add_to_path; ls -A '$tmp' | grep -c profile; rm -rf '$tmp'" 2>&1)
expect "nothing is written when the folder is on the PATH already" "$out" "0"

if [ "$failures" -gt 0 ]; then echo "$failures failure(s)"; exit 1; fi
echo "all install tests passed"
