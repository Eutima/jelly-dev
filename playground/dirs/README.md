# Directory trees

`left/` and `right/` are templates for `jelly-dev <dir-a> <dir-b>`. `../setup.sh` copies them to
`../sandbox/dirs/` and adds a binary file that differs, a symbolic link and a `.git` directory (which is skipped).

| Path | Status in the list |
|---|---|
| `README.md`, `src/main.go`, `docs/deep/nested/file.txt` | M (modified) |
| `src/util.go` | D (only left) |
| `src/extra.go` | A (only right) |
| `docs/guide.md`, `config.json` | identical, not listed |
| `logo.bin` (generated) | M, but binary: "Binary file - not shown" |
| `link_to_readme` (generated) | A: a link to a file is compared like a file |
