# jelly-diff

A terminal diff and merge tool with a JetBrains-style UI: side-by-side panes, colored change
blocks and word-level highlights. It will diff files, diff the clipboard, show `git diff` and
act as a three-pane `git mergetool`.

- Type: tool
- Tier: 1
- Owner: Roy
- Stack: Go + Bubble Tea (deliberate deviation from the Python tool default)

## Status

Done: file diff, word-level highlights, unified view, clipboard compare, git changes, directory diff, three-pane
merge, git tool setup, themes (dark, light, mono), syntax highlighting. Open: release packaging.

Usage: `jelly-diff <file-a> <file-b>`. Flags must come before file names.
Exit codes: 0 ok (also when there are no differences), 1 runtime error, 2 usage error.
Clipboard: `jelly-diff --clip <file>` (clipboard on the right) or `--clip-left`. Reads via PowerShell on
WSL/Windows, `pbpaste` on macOS, `wl-paste`/`xclip`/`xsel` on Linux.
Git: `jelly-diff git [--staged] [<rev> | <a>..<b>] [-- <path>...]` shows a changed-file list (working tree vs HEAD incl.
untracked files by default) next to the diff of the selected file. In the list: `j`/`k` select, `enter` focus the
diff, `tab` switch focus, `esc` back. Three-dot ranges are not supported; flags must precede the revision.
Directories: `jelly-diff <dir-a> <dir-b>` lists the files that differ (modified, only left, only right) with the same
list-plus-diff view; use it from git with `git difftool --dir-diff` (its temporary directories hold links to the working tree files, which
are followed). Compared are regular files and links to regular files; links to directories, dangling links, special
files and `.git` directories are skipped.
Keys (diff): `n`/`p` next/previous change, `j`/`k` scroll, `g`/`G` top/bottom, `t` split/unified, mouse wheel, `q` quit.
The mouse is captured for wheel scrolling; hold Shift while dragging to select text in most terminals.

Merge: `jelly-diff merge <base> <local> <remote> <merged>` shows ours | result | theirs. Non-conflicting changes are
merged automatically; conflicts start with the base lines. Keys: `n`/`p` select a change, `o` ours, `t` theirs,
`b` both, `r` revert to base, `u` undo, `s` save (only when every conflict is resolved), `q` quit (asks before
discarding). The result is written atomically with the line endings of the local file and a final newline; the exit
code is 0 only after saving. Editing single lines in the result pane is not implemented yet.
Use as git tool: `jelly-diff install-git` prints the `git config` commands, `--apply` writes them (`--local` for
the current repository only), then use `git difftool` and `git mergetool`.

## Run

```
go build -o bin/jelly-diff ./cmd/jelly-diff
go test ./...
```

Requires Go 1.24 (the toolchain is fetched automatically by Go >= 1.21).

Git mode runs the `git` command, so the repository's own git configuration applies; do not open untrusted
repositories with it. Symbolic links are shown as their target path and are never followed.

Inputs must be regular files (or symbolic links to them): pipes such as `<(command)` are rejected.

Syntax highlighting (library: [chroma](https://github.com/alecthomas/chroma), MIT) is chosen by file name and shown in
the diff, clipboard, git and directory views: monokai colors in the dark theme, github colors in the light theme, none in
mono. Changed words keep their own highlight. Files over 512 KiB, files without a known lexer and the merge view are
not highlighted.

## Config

Environment variables (see `.env.example`):

- `JELLY_DIFF_THEME`: `dark` (default), `light` or `mono` (no colors: bold = added, strikethrough = deleted, reverse video = changed words).
- `NO_COLOR`: any non-empty value selects `mono` unless `JELLY_DIFF_THEME` is set.

## Known limitations

- A missing trailing newline at the end of a file is not shown as a difference.
- Binary detection only checks for NUL bytes (UTF-16 files are shown as garbage); files and git blobs are limited to 64 MiB.
- While a file loads in the list views a wobbling jelly is shown; slow operations before the UI opens (listing a large directory or repository, reading the clipboard) show a small jelly wave on stderr after 300 ms (only on an interactive terminal).
- In git mode files load in the background ("Loading ..." is shown); only the latest selection is loaded when you move quickly. Plain `jelly-diff a b` still reads both files before the UI opens.
