# jelly-diff

A terminal diff and merge tool with a JetBrains-style UI: side-by-side panes, colored change
blocks and word-level highlights. It will diff files, diff the clipboard, show `git diff` and
act as a three-pane `git mergetool`.

- Type: tool
- Tier: 1
- Owner: Roy
- Stack: Go + Bubble Tea (deliberate deviation from the Python tool default)

## Status

Steps 1-4 of 8 done: file diff, word-level highlights, unified view and clipboard compare.

Usage: `jelly-diff <file-a> <file-b>`. Flags must come before file names.
Exit codes: 0 ok (also when there are no differences), 1 runtime error, 2 usage error.
Clipboard: `jelly-diff --clip <file>` (clipboard on the right) or `--clip-left`. Reads via PowerShell on
WSL/Windows, `pbpaste` on macOS, `wl-paste`/`xclip`/`xsel` on Linux.
Keys: `n`/`p` next/previous change, `j`/`k` scroll, `g`/`G` top/bottom, `t` split/unified, mouse wheel, `q` quit.
The mouse is captured for wheel scrolling; hold Shift while dragging to select text in most terminals.

## Run

```
go build -o bin/jelly-diff ./cmd/jelly-diff
go test ./...
```

Requires Go 1.24 (the toolchain is fetched automatically by Go >= 1.21).

## Config

See `.env.example`.

## Known limitations

- A missing trailing newline at the end of a file is not shown as a difference.
- Binary detection only checks for NUL bytes (UTF-16 files are shown as garbage); files are read fully into memory.
