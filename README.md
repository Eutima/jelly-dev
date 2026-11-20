# jelly-diff

A terminal diff and merge tool with a JetBrains-style UI: side-by-side panes, colored change
blocks and word-level highlights. It will diff files, diff the clipboard, show `git diff` and
act as a three-pane `git mergetool`.

- Type: tool
- Tier: 1
- Owner: Roy
- Stack: Go + Bubble Tea (deliberate deviation from the Python tool default)

## Status

Steps 1-6 of 8 done (merge engine `internal/merge` is a library for now, the merge UI follows in step 7): file diff, word-level highlights, unified view, clipboard compare and git changes.

Usage: `jelly-diff <file-a> <file-b>`. Flags must come before file names.
Exit codes: 0 ok (also when there are no differences), 1 runtime error, 2 usage error.
Clipboard: `jelly-diff --clip <file>` (clipboard on the right) or `--clip-left`. Reads via PowerShell on
WSL/Windows, `pbpaste` on macOS, `wl-paste`/`xclip`/`xsel` on Linux.
Git: `jelly-diff git [--staged] [<rev> | <a>..<b>] [-- <path>...]` shows a changed-file list (working tree vs HEAD incl.
untracked files by default) next to the diff of the selected file. In the list: `j`/`k` select, `enter` focus the
diff, `tab` switch focus, `esc` back. Three-dot ranges are not supported; flags must precede the revision.
Keys (diff): `n`/`p` next/previous change, `j`/`k` scroll, `g`/`G` top/bottom, `t` split/unified, mouse wheel, `q` quit.
The mouse is captured for wheel scrolling; hold Shift while dragging to select text in most terminals.

## Run

```
go build -o bin/jelly-diff ./cmd/jelly-diff
go test ./...
```

Requires Go 1.24 (the toolchain is fetched automatically by Go >= 1.21).

Git mode runs the `git` command, so the repository's own git configuration applies; do not open untrusted
repositories with it. Symbolic links are shown as their target path and are never followed.

Inputs must be regular files (or symbolic links to them): pipes such as `<(command)` are rejected.

## Config

See `.env.example`.

## Known limitations

- A missing trailing newline at the end of a file is not shown as a difference.
- Binary detection only checks for NUL bytes (UTF-16 files are shown as garbage); files and git blobs are limited to 64 MiB.
