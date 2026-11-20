# jelly-diff

A terminal diff and merge tool with a JetBrains-style UI: side-by-side panes, colored change
blocks and word-level highlights. It will diff files, diff the clipboard, show `git diff` and
act as a three-pane `git mergetool`.

- Type: tool
- Tier: 1
- Owner: Roy
- Stack: Go + Bubble Tea (deliberate deviation from the Python tool default)

## Status

Step 1 of 8: the diff engine (`internal/diff`) is done. The UI is not built yet.

## Run

```
go build -o bin/jelly-diff ./cmd/jelly-diff
go test ./...
```

## Config

See `.env.example`.

## Known limitations

- A missing trailing newline at the end of a file is not shown as a difference.
