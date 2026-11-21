#!/usr/bin/env -S -u BASH_ENV bash
# -S -u BASH_ENV (GNU env >= 8.30) keeps WSL's BASH_ENV hook from printing an nsenter error; `bash setup.sh` still reads it.
# Creates (or recreates) playground/sandbox/ with everything needed to try jelly-dev:
#   sandbox/files/        generated extra inputs (binary, CRLF, escape sequences, optional big files)
#   sandbox/dirs/         two directory trees to compare
#   sandbox/merge/        inputs for `jd merge`
#   sandbox/repo-merge/   a git repository stopped in the middle of a merge with conflicts
#   sandbox/repo-changes/ a git repository with history, tags, a branch and many kinds of local changes
#
# Usage: playground/setup.sh [--big]   (--big also generates ~30 MB files to see the loading animation)
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
project="$(cd "$here/.." && pwd)"
sandbox="$here/sandbox"
jd="$project/bin/jd"
big=false
case "${1:-}" in
  "") ;;
  --big) big=true ;;
  *) echo "usage: playground/setup.sh [--big]" >&2; exit 2 ;;
esac

# Safety: only ever delete the sandbox folder inside this playground.
if [ "$(basename "$here")" != "playground" ] || [ "$(basename "$sandbox")" != "sandbox" ] || [ -L "$sandbox" ]; then
  echo "unexpected location, refusing to continue" >&2
  exit 1
fi
# The repositories embed the binary path in git config command lines; characters the shell would interpret break them.
case "$jd" in
  *\"*|*\$*|*\`*) echo "the project path contains \", \$ or a backtick: move the project first" >&2; exit 1 ;;
esac

# The sandbox repositories must not depend on (or change) your own git configuration.
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
export GIT_AUTHOR_NAME="Playground" GIT_AUTHOR_EMAIL="playground@example.com"
export GIT_COMMITTER_NAME="Playground" GIT_COMMITTER_EMAIL="playground@example.com"

if command -v go >/dev/null 2>&1; then
  echo "building $jd"
  (cd "$project" && go build -o "$jd" ./cmd/jelly-dev)
elif [ -x "$jd" ]; then
  echo "warning: Go is not installed, using the existing (possibly outdated) binary $jd" >&2
else
  echo "Go is not installed and $jd does not exist: build jelly-dev first" >&2
  exit 1
fi

rm -rf -- "$sandbox"
mkdir -p "$sandbox"/{files,dirs,merge}

# ---------------------------------------------------------------- files: generated extras
cd "$sandbox/files"
printf 'PNG-like\000\001\002 binary data A\000\n' > image_v1.bin
printf 'PNG-like\000\001\002 binary data B\000\n' > image_v2.bin
printf 'line one\r\nline two\r\nline three\r\n' > crlf_windows.txt
printf 'line one\nline two\nline three\n' > crlf_unix.txt
printf 'line one\nline two changed\nline three\n' > crlf_unix_changed.txt
# Escape sequences inside a file must never reach your terminal: they are shown as "·".
printf 'plain line\n\033[31mred text\033[0m and a bell \007 and a title \033]0;pwned\007 end\nlast line\n' > escape_v1.txt
printf 'plain line\n\033[32mgreen text\033[0m and a bell \007 and a title \033]0;pwned\007 end\nlast line\n' > escape_v2.txt
if $big; then
  echo "generating big files (about 30 MB each) ..."
  awk 'BEGIN{for(i=1;i<=600000;i++) print "line " i " lorem ipsum dolor sit amet consectetur"}' > big_v1.txt
  awk 'BEGIN{for(i=1;i<=600000;i++){ if (i%5000==0) print "CHANGED " i " lorem ipsum"; else print "line " i " lorem ipsum dolor sit amet consectetur"}}' > big_v2.txt
fi
cp "$here"/files/*_v1.* "$here"/files/*_v2.* "$here"/files/clipboard_sample.txt "$here"/files/empty.txt .

# ---------------------------------------------------------------- dirs
cp -R "$here/dirs/left" "$here/dirs/right" "$sandbox/dirs/"
printf 'logo version 1\000\001\n' > "$sandbox/dirs/left/logo.bin"
printf 'logo version 2\000\002\n' > "$sandbox/dirs/right/logo.bin"
ln -s README.md "$sandbox/dirs/right/link_to_readme"        # a link to a file is compared like a file
mkdir -p "$sandbox/dirs/left/.git" "$sandbox/dirs/right/.git"
echo "left" > "$sandbox/dirs/left/.git/config"; echo "right" > "$sandbox/dirs/right/.git/config" # skipped

# ---------------------------------------------------------------- merge inputs
cp "$here"/merge/*.txt "$sandbox/merge/"

# ---------------------------------------------------------------- helpers for the repositories
new_repo() { # new_repo <path>: empty repository with a local identity and the jelly-dev tools configured
  git init -q -b main "$1"
  git -C "$1" config user.name "Playground"
  git -C "$1" config user.email "playground@example.com"
  git -C "$1" config merge.tool jd
  git -C "$1" config mergetool.jd.cmd "\"$jd\" merge \"\$BASE\" \"\$LOCAL\" \"\$REMOTE\" \"\$MERGED\""
  git -C "$1" config mergetool.jd.trustExitCode true
  git -C "$1" config mergetool.keepBackup false
  git -C "$1" config mergetool.prompt false
  git -C "$1" config diff.tool jd
  git -C "$1" config difftool.jd.cmd "\"$jd\" \"\$LOCAL\" \"\$REMOTE\""
  git -C "$1" config difftool.prompt false
}

write_app() { # write_app <greet text> <farewell text> <version>
  cat <<EOF
package main

import "fmt"

// greet prints a greeting.
func greet(name string) {
	fmt.Println("$1" + name)
}

// farewell prints a goodbye.
func farewell(name string) {
	fmt.Println("$2" + name)
}

// version returns the program version.
func version() string {
	return "$3"
}

func main() {
	greet("world")
	farewell("world")
}
EOF
}

# ---------------------------------------------------------------- repo-merge: stopped in a conflicted merge
repo="$sandbox/repo-merge"
new_repo "$repo"
cd "$repo"
write_app "Hello, " "Bye, " "1.0.0" > app.go
cat > README.md <<'EOF'
# Merge demo

A small project that is about to be merged.

Features:
- greeting
EOF
printf '{\n  "port": 8080,\n  "name": "demo",\n  "debug": false\n}\n' > config.json
printf 'todo one\ntodo two\ntodo three\n' > notes.txt
mkdir docs
printf '# Changelog\n\n- 1.0.0 first release\n' > docs/changelog.md
git add -A && git commit -q -m "initial version"

git checkout -q -b feature
write_app "Hi, " "See you, " "2.0.0" > app.go
cat > README.md <<'EOF'
# Merge demo

A small project that is about to be merged with the feature branch.

Features:
- greeting
- farewell message
EOF
printf '{\n  "port": 8080,\n  "name": "demo",\n  "debug": true\n}\n' > config.json
printf 'todo one\ntodo two (done)\ntodo three\n' > notes.txt
printf '# Changelog\n\n- 1.0.0 first release\n- 2.0.0 feature branch release\n' > docs/changelog.md
git add -A && git commit -q -m "feature: greeting, farewell, version 2"

git checkout -q main
write_app "Hello there, " "Bye, " "1.1.0" > app.go
cat > README.md <<'EOF'
# Merge demo

A small project that is merged on the main branch.

Features:
- greeting
- version command
EOF
printf '{\n  "port": 9090,\n  "name": "demo",\n  "debug": false\n}\n' > config.json
printf '# Changelog\n\n- 1.0.0 first release\n- 1.1.0 main branch release\n' > docs/changelog.md
git add -A && git commit -q -m "main: greeting, version 1.1"

git merge feature >/dev/null 2>&1 || true   # stops with conflicts: app.go, README.md, docs/changelog.md

# ---------------------------------------------------------------- repo-changes: history and local changes
repo="$sandbox/repo-changes"
new_repo "$repo"
cd "$repo"
mkdir -p src docs data
write_app "Hello, " "Bye, " "1.0.0" > app.go
printf '# Changes demo\n\nA repository with history and local changes.\n' > README.md
printf 'first note\nsecond note\nthird note\nfourth note\n' > notes.txt
printf 'old file that will be deleted\n' > legacy.txt
printf '{\n  "name": "demo",\n  "port": 8080\n}\n' > config.json
printf 'binary \000 version 1\n' > data/image.bin
printf '*.log\n' > .gitignore
printf 'first guide\n' > "docs/user guide.md"
if $big; then
  awk 'BEGIN{for(i=1;i<=600000;i++) print "line " i " lorem ipsum dolor sit amet consectetur"}' > data/big.txt
fi
git add -A && git commit -q -m "initial version"
git tag v0.1.0

write_app "Hello, " "Goodbye, " "1.1.0" > app.go
printf '# Changes demo\n\nA repository with history, tags and local changes.\n' > README.md
git add -A && git commit -q -m "second version: goodbye and docs"
git tag v0.2.0

git checkout -q -b feature/login
printf 'package main\n\n// login checks a user name.\nfunc login(user string) bool { return user != "" }\n' > src/login.go
write_app "Welcome, " "Goodbye, " "1.2.0" > app.go
git add -A && git commit -q -m "feature: login"
git checkout -q main

# local changes on main: modified, staged, deleted, renamed, untracked, binary, odd file names
write_app "Hello, " "Goodbye, " "1.1.1" > app.go                         # modified, not staged
mkdir -p src                                                             # git removed it when leaving the branch
printf 'package main\n\n// newFeature is staged for the next commit.\nfunc newFeature() {}\n' > src/new_feature.go
git add src/new_feature.go                                               # added and staged
rm legacy.txt                                                            # deleted, not staged
git mv notes.txt docs/notes.txt                                          # renamed and staged
printf 'first note\nsecond note (edited)\nthird note\nfourth note\n' > docs/notes.txt   # renamed AND edited
printf 'binary \000 version 2\n' > data/image.bin                        # binary change
printf 'scratch work\n' > scratch.txt                                    # untracked
printf 'ignored\n' > debug.log                                           # ignored by .gitignore
printf 'second guide\n' > "docs/user guide.md"                           # file name with a space
printf 'odd name\n' > "$(printf 'evil\033[31mname.txt')"                 # escape character in the file name
if $big; then
  awk 'BEGIN{for(i=1;i<=600000;i++){ if (i%5000==0) print "CHANGED " i " lorem ipsum"; else print "line " i " lorem ipsum dolor sit amet consectetur"}}' > data/big.txt
fi

cat <<EOF

Playground ready in: $sandbox

  files/          jd $here/files/greeting_v1.go $here/files/greeting_v2.go
  dirs/           jd $sandbox/dirs/left $sandbox/dirs/right
  merge/          jd merge $sandbox/merge/base.txt $sandbox/merge/local.txt $sandbox/merge/remote.txt $sandbox/merge/merged.txt
  repo-changes/   cd $sandbox/repo-changes && $jd git
  repo-merge/     cd $sandbox/repo-merge && git mergetool        (3 conflicted files)

See $here/README.md for the full list of things to try.
EOF
