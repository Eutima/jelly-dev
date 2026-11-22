package refactor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eutima/jelly-dev/internal/lsp"
)

func project(t *testing.T, files map[string]string) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func replaceFirstWord(path, word string) lsp.FileEdit {
	return lsp.FileEdit{Path: path, Edits: []lsp.TextEdit{{Range: lsp.Range{End: lsp.Position{Character: 3}}, NewText: word}}}
}

func TestApply_ChangesFilesAndUndoRestoresThem_WhenNothingChangedSince(t *testing.T) {
	root := project(t, map[string]string{"a.go": "foo()\n", "b.go": "foo = 1\n"})
	a, b := filepath.Join(root, "a.go"), filepath.Join(root, "b.go")
	journal := Open(root)
	result, err := journal.Apply("rename foo", lsp.WorkspaceEdit{
		Files:   []lsp.FileEdit{replaceFirstWord(a, "bar"), replaceFirstWord(b, "bar")},
		Renames: []lsp.FileRename{{Old: b, New: filepath.Join(root, "sub", "c.go")}},
	})
	if err != nil || result.Files != 2 || result.Edits != 2 || result.Moves != 1 {
		t.Fatalf("Apply = %+v, %v", result, err)
	}
	if read(t, a) != "bar()\n" || read(t, filepath.Join(root, "sub", "c.go")) != "bar = 1\n" {
		t.Fatalf("a = %q", read(t, a))
	}
	if _, err := os.Stat(b); err == nil {
		t.Fatal("b.go must have moved")
	}
	// a new Open (a restarted jd) still undoes it
	undone, err := Open(root).Undo()
	if err != nil || undone.Files != 2 || undone.Moves != 1 {
		t.Fatalf("Undo = %+v, %v", undone, err)
	}
	if read(t, a) != "foo()\n" || read(t, b) != "foo = 1\n" {
		t.Fatalf("restored a = %q b = %q", read(t, a), read(t, b))
	}
	if _, err := Open(root).Undo(); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("second undo: %v", err)
	}
}

func TestUndo_Refuses_WhenFileChangedAfterTheRefactoring(t *testing.T) {
	root := project(t, map[string]string{"a.go": "foo()\n"})
	a := filepath.Join(root, "a.go")
	journal := Open(root)
	if _, err := journal.Apply("rename", lsp.WorkspaceEdit{Files: []lsp.FileEdit{replaceFirstWord(a, "bar")}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a, []byte("bar()\n// my later work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.Undo(); err == nil || !strings.Contains(err.Error(), "changed after") {
		t.Fatalf("Undo = %v", err)
	}
	if read(t, a) != "bar()\n// my later work\n" {
		t.Fatalf("later work was overwritten: %q", read(t, a))
	}
	if _, ok := journal.Last(); !ok {
		t.Fatal("a refused undo must keep the journal entry")
	}
}

func TestApply_WritesNothing_WhenOneFileIsOutsideTheProject(t *testing.T) {
	root := project(t, map[string]string{"a.go": "foo()\n"})
	outside := project(t, map[string]string{"x.go": "foo()\n"})
	a, x := filepath.Join(root, "a.go"), filepath.Join(outside, "x.go")
	_, err := Open(root).Apply("rename", lsp.WorkspaceEdit{Files: []lsp.FileEdit{replaceFirstWord(a, "bar"), replaceFirstWord(x, "bar")}})
	if err == nil || read(t, a) != "foo()\n" || read(t, x) != "foo()\n" {
		t.Fatalf("err = %v, a = %q, x = %q", err, read(t, a), read(t, x))
	}
}

func TestApply_Refuses_WhenRenameTargetExists_OrEditDoesNotFit(t *testing.T) {
	root := project(t, map[string]string{"a.go": "foo()\n", "b.go": "x\n"})
	a, b := filepath.Join(root, "a.go"), filepath.Join(root, "b.go")
	if _, err := Open(root).Apply("move", lsp.WorkspaceEdit{Renames: []lsp.FileRename{{Old: a, New: b}}}); err == nil {
		t.Fatal("a rename onto an existing file must be refused")
	}
	bad := lsp.FileEdit{Path: a, Edits: []lsp.TextEdit{{Range: lsp.Range{Start: lsp.Position{Line: 9}, End: lsp.Position{Line: 9}}, NewText: "x"}}}
	if _, err := Open(root).Apply("bad", lsp.WorkspaceEdit{Files: []lsp.FileEdit{bad}}); err == nil || read(t, a) != "foo()\n" {
		t.Fatalf("err = %v, a = %q", err, read(t, a))
	}
	if _, ok := Open(root).Last(); ok {
		t.Fatal("a refused refactoring must not reach the journal")
	}
}

func TestApply_RenamesFileOnly_AndUndoMovesItBack(t *testing.T) {
	root := project(t, map[string]string{"old.go": "package x\n"})
	oldPath, newPath := filepath.Join(root, "old.go"), filepath.Join(root, "new.go")
	journal := Open(root)
	if _, err := journal.Apply("rename file", lsp.WorkspaceEdit{Renames: []lsp.FileRename{{Old: oldPath, New: newPath}}}); err != nil {
		t.Fatal(err)
	}
	if read(t, newPath) != "package x\n" {
		t.Fatal("file not moved")
	}
	if _, err := journal.Undo(); err != nil || read(t, oldPath) != "package x\n" {
		t.Fatalf("Undo: %v", err)
	}
}

func TestApply_Refuses_WhenALinkLeadsOutOfTheProject(t *testing.T) {
	root := project(t, map[string]string{"a.go": "foo()\n"})
	outside := project(t, map[string]string{"x.go": "foo()\n"})
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip("symbolic links are not available: " + err.Error())
	}
	through := filepath.Join(root, "link", "x.go")
	if _, err := Open(root).Apply("rename", lsp.WorkspaceEdit{Files: []lsp.FileEdit{replaceFirstWord(through, "bar")}}); err == nil || read(t, filepath.Join(outside, "x.go")) != "foo()\n" {
		t.Fatalf("a link out of the project must not be followed: %v", err)
	}
}

func TestApply_Fails_WhenAnotherWindowHoldsTheLock_AndTakesOverAStaleLock(t *testing.T) {
	root := project(t, map[string]string{"a.go": "foo()\n"})
	a := filepath.Join(root, "a.go")
	journal := Open(root)
	lock := journal.path + ".lock"
	if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.Apply("rename", lsp.WorkspaceEdit{Files: []lsp.FileEdit{replaceFirstWord(a, "bar")}}); err != nil {
		t.Fatalf("a stale lock must be taken over: %v", err)
	}
	if _, err := os.Stat(lock); err == nil {
		t.Fatal("the lock must be released")
	}
}
