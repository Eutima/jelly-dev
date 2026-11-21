# Playground

Sample data for trying every function of jelly-dev by hand. Nothing here touches your own git configuration or
repositories: the generated repositories live in `playground/sandbox/` (ignored by git) and carry their own local
settings.

## Set up

`playground/` is a separate Go module on purpose (see `go.mod`): the sample `.go` files are test data, and some of them
deliberately contain conflicts or duplicate declarations, so the main module's `go build ./...` must not see them.

From the project root:

```
playground/setup.sh            # builds bin/jelly-dev and creates playground/sandbox/
playground/setup.sh --big      # the same, plus ~30 MB files to see the loading animation (takes a few seconds more)
export PATH="$PWD/bin:$PATH"   # so that `jelly-dev` works in the examples below
```

Run `playground/setup.sh` again at any time to get a fresh sandbox (for example after resolving the conflicts).
`playground/smoke.sh` runs the setup and checks that the repositories are in the promised states (three conflicted
files, the local changes, tags, branch).

> If the script prints `nsenter: failed to parse pid` (some WSL setups export `BASH_ENV=/etc/bash.bashrc`), run it as
> `env -u BASH_ENV bash playground/setup.sh`.

What is where:

| Folder | Content |
|---|---|
| `files/` | committed sample file pairs (`*_v1` / `*_v2`), see `files/README.md` |
| `dirs/`, `merge/` | committed templates, copied into the sandbox by the script |
| `sandbox/files/` | the sample files plus generated extras: binary, CRLF, escape sequences, big files |
| `sandbox/dirs/` | two directory trees: `left/` and `right/` |
| `sandbox/merge/` | inputs for `jelly-dev merge` |
| `sandbox/repo-changes/` | git repository with history, tags, a branch and many kinds of local changes |
| `sandbox/repo-merge/` | git repository **stopped in the middle of a merge** with three conflicted files |

Keys that work everywhere: `q` quit, `j`/`k` or arrows move or scroll, `n`/`p` next/previous change, `g`/`G`
first/last, mouse wheel scrolls. `t` switches between side-by-side and unified view in the diff views.

## Screenshots

The screenshots in the main README come from these examples: `python3 playground/screenshots.py` runs `jd` in a pseudo terminal on a
copy of `sandbox/`, sends the keys of each shot, reads the screen with a terminal emulator and writes `docs/screenshots/*.png`
(`--only diff ide` takes some of them). It needs `pip install pyte pillow`, DejaVu Sans Mono and `playground/setup.sh` to have run.
Nothing of your own configuration is used: `jd` gets a throw-away home and config folder.

## 1. Compare two files

```
cd playground/sandbox/files
jelly-dev greeting_v1.go greeting_v2.go     # Go: word highlights, added function, syntax colors
jelly-dev notes_v1.txt notes_v2.txt         # plain text
jelly-dev settings_v1.json settings_v2.json # several separate changes
jelly-dev tabs_unicode_v1.txt tabs_unicode_v2.txt   # tabs and wide (CJK) characters
jelly-dev long_lines_v1.txt long_lines_v2.txt       # very long lines: try a narrow terminal
jelly-dev empty.txt notes_v1.txt            # everything added
jelly-dev crlf_windows.txt crlf_unix.txt    # prints "no differences": line endings are normalized
jelly-dev crlf_unix.txt crlf_unix_changed.txt
jelly-dev escape_v1.txt escape_v2.txt       # escape sequences are shown as "·", they never reach the terminal
jelly-dev image_v1.bin image_v2.bin         # fails with "binary file" (exit code 1)
echo $?
```

Press `t` for the unified view. Resize the window: the panes follow.

## 2. Compare a file with the clipboard

Copy the sample text first, then compare:

```
cd playground/sandbox/files
clip.exe < clipboard_sample.txt              # WSL / Windows
# xclip -selection clipboard < clipboard_sample.txt   # Linux (X11)
# wl-copy < clipboard_sample.txt                      # Linux (Wayland)
# pbcopy < clipboard_sample.txt                       # macOS

jelly-dev --clip greeting_v1.go             # file on the left, clipboard on the right
jelly-dev --clip-left greeting_v1.go        # clipboard on the left
```

## 3. Compare two directories

```
cd playground/sandbox/dirs
jelly-dev left right
```

The list on the left shows `M` (changed), `D` (only in `left`), `A` (only in `right`); `logo.bin` is listed but shows
"Binary file - not shown", and `link_to_readme` (a symbolic link to a file) is compared like a file. Identical files
and the `.git` folders are not listed. `j`/`k` select a
file, `enter` or `tab` moves focus to the diff, `esc`/`h` back to the list.

## 4. Browse changes of a git repository

```
cd playground/sandbox/repo-changes
jelly-dev git                       # working tree against HEAD, including untracked files
jelly-dev git --staged              # only what is staged
jelly-dev git v0.1.0                # working tree against a tag
jelly-dev git v0.1.0..v0.2.0        # two revisions
jelly-dev git main..feature/login   # two branches
jelly-dev git -- docs               # only paths below docs/
```

You will see: a modified file, a staged new file, a deleted file, a renamed **and** edited file, an untracked file,
a changed binary file, a file name with a space and one with an escape character in its name (shown as `·`).

With `--big`, `data/big.txt` is a ~30 MB file with changes: selecting it shows the jelly while it loads; move to
another file while it loads and the first load is cancelled.

### As `git difftool`

The sandbox repositories are already configured for it (locally):

```
cd playground/sandbox/repo-changes
git difftool                  # one file at a time
git difftool --dir-diff       # all changes as two directories
git difftool --staged
```

## 5. Merge

### Directly, with three files

```
cd playground/sandbox/merge
jelly-dev merge base.txt local.txt remote.txt merged.txt
cat merged.txt                # after saving with `s`
```

`merge/README.md` lists which changes are conflicts. Keys in the merge view:

| Key | Action |
|---|---|
| `j`/`k`, arrows | move the line cursor in the middle (result) pane |
| `n` / `p` | next / previous change |
| `o` / `t` / `b` / `r` | take **o**urs / **t**heirs / **b**oth / revert to the base |
| `e` or `enter` | edit the line under the cursor (`enter` applies, `esc` cancels) |
| `d` / `i` | delete the line / insert a new line below |
| `u` | undo the last step |
| `s` | save (only possible when no conflict is left) |
| `q` | quit (asks first when there is unsaved work) |

### With `git mergetool` (three conflicted files)

```
cd playground/sandbox/repo-merge
git status                    # README.md, app.go and docs/changelog.md are "both modified"
git mergetool                 # opens jelly-dev once per conflicted file
git status                    # nothing is unmerged any more
git diff --cached app.go      # what you resolved
```

Quit without saving (`q` twice) and git treats the file as not merged. To start over: `git merge --abort`, then
`git merge feature`, or just run `playground/setup.sh` again.

### Set up your own repository

```
jelly-dev install-git               # prints the git config commands
jelly-dev install-git --apply       # writes them (global); add --local for one repository
```

## 6. Colors

```
JELLY_DEV_THEME=light jelly-dev greeting_v1.go greeting_v2.go   # for light terminals
JELLY_DEV_THEME=mono  jelly-dev greeting_v1.go greeting_v2.go   # attributes only: bold, strikethrough, reverse
NO_COLOR=1 jelly-dev greeting_v1.go greeting_v2.go               # the same as mono
```

## Other things to try

- `jelly-dev --version`
- A very narrow or very small terminal window.
From `playground/sandbox`:

- `jelly-dev files/notes_v1.txt` (wrong argument count): prints usage and exits with code 2.
- `jelly-dev files dirs/left/README.md`: comparing a directory with a file is an error.

On terminals that do not answer the background-color query, every start waits about 5 seconds (see the main README).
