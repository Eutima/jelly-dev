package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// rewrite changes a file on disk the way another program does, with a later time of change.
func rewrite(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
}

func openWatched(t *testing.T) (ProjectModel, string) {
	t.Helper()
	model, fx := newProjectFixture(t, 100, map[string]string{"a.txt": "one\ntwo\n"})
	path := filepath.Join(fx.root, "a.txt")
	if err := model.openFile(path, true); err != nil {
		t.Fatal(err)
	}
	model.focus = focusProjectEdit
	model = update(model, watchTickMsg{}) // the first look only records the stamp
	return model, path
}

func TestWatch_ShowsTheNewText_WhenAnotherProgramWritesTheOpenFile(t *testing.T) {
	model, path := openWatched(t)
	rewrite(t, path, "one\ntwo\nthree\n")
	updated, cmd := model.Update(watchTickMsg{})
	model = updated.(ProjectModel)
	if got := model.editor.vim.Lines(); len(got) != 3 || got[2] != "three" {
		t.Fatalf("lines %q", got)
	}
	if !strings.Contains(model.status+model.editor.status, "Reloaded a.txt") || cmd == nil {
		t.Fatalf("status %q, next tick %v", model.editor.status, cmd != nil)
	}
}

func TestWatch_ReplacesUnsavedEditsAndUndoBringsThemBack_WhenTheFileChangedOnDisk(t *testing.T) {
	model, path := openWatched(t)
	model, _ = projectKeys(model, "i", "X", "esc")
	if !model.editor.Modified() {
		t.Fatal("the test needs unsaved edits")
	}
	rewrite(t, path, "disk\n")
	model = update(model, watchTickMsg{})
	if got := model.editor.vim.Lines(); len(got) != 1 || got[0] != "disk" || model.editor.Modified() {
		t.Fatalf("lines %q modified %v", got, model.editor.Modified())
	}
	model, _ = projectKeys(model, "u")
	if got := model.editor.vim.Lines(); got[0] != "Xone" {
		t.Fatalf("undo must bring the edit back: %q", got)
	}
}

func TestWatch_LeavesTheTabAlone_WhenNothingChanged(t *testing.T) {
	model, _ := openWatched(t)
	model, _ = projectKeys(model, "i", "X", "esc")
	model = update(model, watchTickMsg{})
	if got := model.editor.vim.Lines(); got[0] != "Xone" || !model.editor.Modified() {
		t.Fatalf("lines %q", got)
	}
}

func TestWatch_IgnoresTheOwnSave(t *testing.T) {
	model, _ := openWatched(t)
	model, _ = projectKeys(model, "i", "X", "esc", ":", "w", "enter")
	model = update(model, watchTickMsg{})
	if got := model.editor.vim.Lines(); got[0] != "Xone" || strings.Contains(model.status+model.editor.status, "Reloaded") {
		t.Fatalf("lines %q status %q", got, model.editor.status)
	}
}
