# Merge inputs

```
jelly-diff merge base.txt local.txt remote.txt merged.txt
```

(`../setup.sh` copies these files to `../sandbox/merge/` so you can save without changing the originals.)

What the three files contain compared with `base.txt`:

| Place | local (ours) | remote (theirs) | Result |
|---|---|---|---|
| Intro line | "our team works" | "everybody works" | **conflict** |
| Setup | unchanged | adds "Clone the repository." | merged automatically |
| Daily work | adds "and assign it to yourself" | unchanged | merged automatically |
| Release day | "Thursday" | "Monday" | **conflict** |
| Support line | "email" | "service desk" | **conflict**, but `m` resolves it (different words) |
| Footer | unchanged | "March" | merged automatically |

Things to try: `n`/`p` to jump between changes, `o`/`t`/`b`/`r` to resolve, `m` to resolve all simple conflicts at once (one `u` undoes it), `e` to edit a line by hand, `d` to delete
a line, `i` to insert one, `u` to undo, `s` to save (only possible when no conflict is left), `q` to quit.

`addadd_local.txt` and `addadd_remote.txt` simulate a file that both sides added independently; pass a base path that
does not exist:

```
jelly-diff merge does-not-exist addadd_local.txt addadd_remote.txt merged.txt
```
