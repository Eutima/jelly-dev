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
        detect_pm() { pm=$fake_pm; }
        is_wsl() { return 1; }
        uname() { echo Linux; }
        id() { echo 1000; }
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
expect "apt updates once" "$out" "[dry-run] sudo apt-get update"
expect "apt installs git" "$out" "[dry-run] sudo apt-get install -y git"
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

out=$(plan dnf "git xterm go npm javac java python3 pyright-langserver typescript-language-server jdtls groovy-language-server gopls xclip vscode-html-language-server ngserver prettier black")
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
expect "typescript server via npm" "$out" "typescript typescript-language-server"
expect "angular server via npm" "$out" "npm install -g --prefix $HOME/.local @angular/language-server @angular/language-service typescript"
expect "html server via npm" "$out" "npm install -g --prefix $HOME/.local vscode-langservers-extracted"
expect "prettier via npm" "$out" "npm install -g --prefix $HOME/.local prettier"
expect "black via pip" "$out" "python3 -m pip install --user black"
expect "jdtls download is planned" "$out" "download https://download.eclipse.org/jdtls"
expect "groovy server download is planned" "$out" "groovy-language-server release jar"

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
expect_not "no groovy server" "$out" "groovy"
expect_not "no typescript server" "$out" "typescript-language-server"
out=$(FAKE_LANGS="python html;css" plan apt-get "git xclip xterm go npm java python3 pipx" "lsp")
expect "spaces and semicolons separate languages: html" "$out" "vscode-langservers-extracted"
expect "spaces and semicolons separate languages: python" "$out" "pyright"
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

# main with JD_BIN (the pip package runs the script on jd's first start): no download of jd, no PATH change, git is
# configured with the jd that is installed. The steps are replaced by fakes that record the calls.
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
expect_not "with JD_BIN the PATH stays" "$out" "path"
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

if [ "$failures" -gt 0 ]; then echo "$failures failure(s)"; exit 1; fi
echo "all install tests passed"
